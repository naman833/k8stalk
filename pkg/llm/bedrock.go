package llm

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/naman833/k8stalk/pkg/config"
)

// BedrockProvider implements Provider for AWS Bedrock's Converse API.
type BedrockProvider struct {
	client    *http.Client
	region    string
	model     string
	accessKey string
	secretKey string
	sessToken string
}

func NewBedrockProvider(cfg config.ProviderConfig) (Provider, error) {
	region := cfg.Region
	if region == "" {
		region = os.Getenv("AWS_REGION")
	}
	if region == "" {
		region = os.Getenv("AWS_DEFAULT_REGION")
	}
	if region == "" {
		region = "us-east-1"
	}

	accessKey := os.Getenv("AWS_ACCESS_KEY_ID")
	secretKey := os.Getenv("AWS_SECRET_ACCESS_KEY")
	sessToken := os.Getenv("AWS_SESSION_TOKEN")

	if accessKey == "" || secretKey == "" {
		return nil, fmt.Errorf("AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY environment variables required for bedrock")
	}

	model := cfg.Model
	if model == "" {
		// Bedrock Converse model IDs are region-specific and may require an
		// inference-profile prefix (e.g. "us.anthropic.claude-sonnet-4-6-v1:0").
		// Override with --model if this default isn't available in your region.
		model = "anthropic.claude-sonnet-4-6-v1:0"
	}

	return &BedrockProvider{
		client:    &http.Client{},
		region:    region,
		model:     model,
		accessKey: accessKey,
		secretKey: secretKey,
		sessToken: sessToken,
	}, nil
}

func (b *BedrockProvider) Name() string       { return "amazonbedrock" }
func (b *BedrockProvider) SupportsTools() bool { return true }

// Bedrock Converse API types
type bedrockConverseRequest struct {
	ModelID    string                `json:"-"` // passed in URL
	Messages   []bedrockMessage     `json:"messages"`
	System     []bedrockSystemBlock `json:"system,omitempty"`
	ToolConfig *bedrockToolConfig   `json:"toolConfig,omitempty"`
}

type bedrockSystemBlock struct {
	Text string `json:"text"`
}

type bedrockMessage struct {
	Role    string         `json:"role"`
	Content []bedrockBlock `json:"content"`
}

type bedrockBlock struct {
	Text       string              `json:"text,omitempty"`
	ToolUse    *bedrockToolUse     `json:"toolUse,omitempty"`
	ToolResult *bedrockToolResult  `json:"toolResult,omitempty"`
}

type bedrockToolUse struct {
	ToolUseID string         `json:"toolUseId"`
	Name      string         `json:"name"`
	Input     map[string]any `json:"input"`
}

type bedrockToolResult struct {
	ToolUseID string         `json:"toolUseId"`
	Content   []bedrockBlock `json:"content"`
}

type bedrockToolConfig struct {
	Tools []bedrockToolDef `json:"tools"`
}

type bedrockToolDef struct {
	ToolSpec *bedrockToolSpecDef `json:"toolSpec"`
}

type bedrockToolSpecDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema bedrockSchema  `json:"inputSchema"`
}

type bedrockSchema struct {
	JSON map[string]any `json:"json"`
}

type bedrockConverseResponse struct {
	Output     bedrockOutput `json:"output"`
	StopReason string        `json:"stopReason"`
}

type bedrockOutput struct {
	Message bedrockMessage `json:"message"`
}

func (b *BedrockProvider) Chat(ctx context.Context, messages []Message, tools []ToolSpec) (*ChatResponse, error) {
	req := b.buildRequest(messages, tools)

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	endpoint := fmt.Sprintf("https://bedrock-runtime.%s.amazonaws.com/model/%s/converse", b.region, b.model)

	httpReq, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	if err := b.signRequest(httpReq, body); err != nil {
		return nil, fmt.Errorf("sign request: %w", err)
	}

	resp, err := b.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("bedrock request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("bedrock returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var brResp bedrockConverseResponse
	if err := json.NewDecoder(resp.Body).Decode(&brResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	result := &ChatResponse{}

	switch brResp.StopReason {
	case "tool_use":
		result.StopReason = "tool_use"
	case "end_turn":
		result.StopReason = "end_turn"
	case "max_tokens":
		result.StopReason = "max_tokens"
	default:
		result.StopReason = brResp.StopReason
	}

	for _, block := range brResp.Output.Message.Content {
		if block.Text != "" {
			result.Content += block.Text
		}
		if block.ToolUse != nil {
			result.ToolCalls = append(result.ToolCalls, ToolCall{
				ID:    block.ToolUse.ToolUseID,
				Name:  block.ToolUse.Name,
				Input: block.ToolUse.Input,
			})
		}
	}

	return result, nil
}

func (b *BedrockProvider) ChatStream(ctx context.Context, messages []Message, tools []ToolSpec) (<-chan StreamChunk, error) {
	// Use non-streaming Converse API and emit as a single chunk
	// Bedrock's ConverseStream has a different response format that's complex to parse
	resp, err := b.Chat(ctx, messages, tools)
	if err != nil {
		return nil, err
	}

	ch := make(chan StreamChunk, 4)
	go func() {
		defer close(ch)
		if resp.Content != "" {
			ch <- StreamChunk{TextDelta: resp.Content}
		}
		for _, tc := range resp.ToolCalls {
			tc := tc
			ch <- StreamChunk{ToolCall: &tc}
		}
		ch <- StreamChunk{Done: true}
	}()

	return ch, nil
}

func (b *BedrockProvider) buildRequest(messages []Message, tools []ToolSpec) bedrockConverseRequest {
	req := bedrockConverseRequest{}

	// Convert tools
	if len(tools) > 0 {
		toolDefs := make([]bedrockToolDef, 0, len(tools))
		for _, t := range tools {
			schema := map[string]any{
				"type":       "object",
				"properties": t.InputSchema,
			}
			toolDefs = append(toolDefs, bedrockToolDef{
				ToolSpec: &bedrockToolSpecDef{
					Name:        t.Name,
					Description: t.Description,
					InputSchema: bedrockSchema{JSON: schema},
				},
			})
		}
		req.ToolConfig = &bedrockToolConfig{Tools: toolDefs}
	}

	// Convert messages
	for _, m := range messages {
		switch m.Role {
		case RoleSystem:
			req.System = append(req.System, bedrockSystemBlock{Text: m.Content})
		case RoleUser:
			req.Messages = append(req.Messages, bedrockMessage{
				Role:    "user",
				Content: []bedrockBlock{{Text: m.Content}},
			})
		case RoleAssistant:
			blocks := []bedrockBlock{}
			if m.Content != "" {
				blocks = append(blocks, bedrockBlock{Text: m.Content})
			}
			for _, tc := range m.ToolCalls {
				blocks = append(blocks, bedrockBlock{
					ToolUse: &bedrockToolUse{
						ToolUseID: tc.ID,
						Name:      tc.Name,
						Input:     tc.Input,
					},
				})
			}
			req.Messages = append(req.Messages, bedrockMessage{
				Role:    "assistant",
				Content: blocks,
			})
		case RoleTool:
			// Bedrock expects tool results as user messages with toolResult blocks
			req.Messages = append(req.Messages, bedrockMessage{
				Role: "user",
				Content: []bedrockBlock{{
					ToolResult: &bedrockToolResult{
						ToolUseID: m.ToolCallID,
						Content:   []bedrockBlock{{Text: m.Content}},
					},
				}},
			})
		}
	}

	return req
}

// SigV4 signing implementation for AWS requests

func (b *BedrockProvider) signRequest(req *http.Request, payload []byte) error {
	now := time.Now().UTC()
	datestamp := now.Format("20060102")
	amzDate := now.Format("20060102T150405Z")

	req.Header.Set("X-Amz-Date", amzDate)
	if b.sessToken != "" {
		req.Header.Set("X-Amz-Security-Token", b.sessToken)
	}

	// Create canonical request
	payloadHash := sha256Hex(payload)
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)

	canonicalURI := req.URL.Path
	if canonicalURI == "" {
		canonicalURI = "/"
	}
	canonicalQueryString := req.URL.RawQuery

	// Signed headers
	signedHeaders := b.getSignedHeaders(req)
	canonicalHeaders := b.getCanonicalHeaders(req, signedHeaders)

	canonicalRequest := strings.Join([]string{
		req.Method,
		canonicalURI,
		canonicalQueryString,
		canonicalHeaders,
		strings.Join(signedHeaders, ";"),
		payloadHash,
	}, "\n")

	// Create string to sign
	service := "bedrock"
	credentialScope := fmt.Sprintf("%s/%s/%s/aws4_request", datestamp, b.region, service)
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		credentialScope,
		sha256Hex([]byte(canonicalRequest)),
	}, "\n")

	// Calculate signature
	signingKey := b.deriveSigningKey(datestamp, service)
	signature := hex.EncodeToString(hmacSHA256(signingKey, []byte(stringToSign)))

	// Add authorization header
	authHeader := fmt.Sprintf("AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		b.accessKey, credentialScope, strings.Join(signedHeaders, ";"), signature)
	req.Header.Set("Authorization", authHeader)

	// Generate unique request ID
	req.Header.Set("X-Amzn-Trace-Id", "k8stalk-"+uuid.New().String())

	return nil
}

func (b *BedrockProvider) getSignedHeaders(req *http.Request) []string {
	headers := make([]string, 0)
	for key := range req.Header {
		headers = append(headers, strings.ToLower(key))
	}
	headers = append(headers, "host")
	sort.Strings(headers)
	return headers
}

func (b *BedrockProvider) getCanonicalHeaders(req *http.Request, signedHeaders []string) string {
	var builder strings.Builder
	for _, h := range signedHeaders {
		if h == "host" {
			builder.WriteString("host:" + req.URL.Host + "\n")
		} else {
			values := req.Header.Values(http.CanonicalHeaderKey(h))
			builder.WriteString(h + ":" + strings.Join(values, ",") + "\n")
		}
	}
	return builder.String()
}

func (b *BedrockProvider) deriveSigningKey(datestamp, service string) []byte {
	kDate := hmacSHA256([]byte("AWS4"+b.secretKey), []byte(datestamp))
	kRegion := hmacSHA256(kDate, []byte(b.region))
	kService := hmacSHA256(kRegion, []byte(service))
	kSigning := hmacSHA256(kService, []byte("aws4_request"))
	return kSigning
}

func hmacSHA256(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}

func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

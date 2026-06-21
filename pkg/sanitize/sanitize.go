package sanitize

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
)

// Sanitizer masks sensitive data before it's sent to cloud LLM providers.
// It maintains a reversible mapping so responses can be de-anonymized.
type Sanitizer struct {
	enabled  bool
	mu       sync.Mutex
	forward  map[string]string // original -> masked
	reverse  map[string]string // masked -> original
	counters map[string]int    // per-type counters for generating placeholder names
}

// New creates a new Sanitizer. If enabled is false, it passes data through unchanged.
func New(enabled bool) *Sanitizer {
	return &Sanitizer{
		enabled:  enabled,
		forward:  make(map[string]string),
		reverse:  make(map[string]string),
		counters: make(map[string]int),
	}
}

// Sanitize masks sensitive patterns in the input string.
func (s *Sanitizer) Sanitize(input string) string {
	if !s.enabled {
		return input
	}

	result := input

	// Mask IP addresses
	result = s.maskPattern(result, ipv4Regex, "IP")

	// Mask email addresses
	result = s.maskPattern(result, emailRegex, "EMAIL")

	// Mask potential secrets (base64 strings that look like tokens)
	result = s.maskPattern(result, base64TokenRegex, "TOKEN")

	// Mask hostnames/domains in URLs
	result = s.maskPattern(result, hostnameRegex, "HOST")

	return result
}

// Desanitize reverses the masking, replacing placeholders with original values.
func (s *Sanitizer) Desanitize(input string) string {
	if !s.enabled {
		return input
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	result := input
	for masked, original := range s.reverse {
		result = strings.ReplaceAll(result, masked, original)
	}
	return result
}

// maskPattern finds all matches of a regex and replaces them with numbered placeholders.
func (s *Sanitizer) maskPattern(input string, re *regexp.Regexp, category string) string {
	return re.ReplaceAllStringFunc(input, func(match string) string {
		s.mu.Lock()
		defer s.mu.Unlock()

		// Check if already masked
		if masked, ok := s.forward[match]; ok {
			return masked
		}

		s.counters[category]++
		masked := fmt.Sprintf("<MASKED_%s_%d>", category, s.counters[category])
		s.forward[match] = masked
		s.reverse[masked] = match
		return masked
	})
}

// Reset clears all mappings.
func (s *Sanitizer) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.forward = make(map[string]string)
	s.reverse = make(map[string]string)
	s.counters = make(map[string]int)
}

// Compiled regex patterns for sensitive data detection.
var (
	ipv4Regex = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)

	emailRegex = regexp.MustCompile(`\b[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}\b`)

	// Matches long base64 strings (likely tokens/secrets), at least 20 chars
	base64TokenRegex = regexp.MustCompile(`\b[A-Za-z0-9+/]{20,}={0,2}\b`)

	// Matches hostnames that look like internal/external domains
	hostnameRegex = regexp.MustCompile(`\b[a-z0-9](?:[a-z0-9\-]*[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9\-]*[a-z0-9])?){2,}\b`)
)

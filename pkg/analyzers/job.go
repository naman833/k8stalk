package analyzers

import (
	"context"
	"fmt"

	"github.com/naman833/k8stalk/pkg/agent"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type JobAnalyzer struct {
	clientset kubernetes.Interface
	namespace string
}

func NewJobAnalyzer(clientset kubernetes.Interface, namespace string) *JobAnalyzer {
	return &JobAnalyzer{clientset: clientset, namespace: namespace}
}

func (j *JobAnalyzer) Spec() agent.ToolSpec {
	return agent.ToolSpec{
		Name:        "Job",
		Description: "Analyze jobs for issues: failed jobs, backoff limit exceeded, deadline exceeded",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"namespace": map[string]any{"type": "string", "description": "Kubernetes namespace"},
				"name":      map[string]any{"type": "string", "description": "Specific job name"},
			},
		},
	}
}

func (j *JobAnalyzer) Execute(ctx context.Context, input map[string]any) (agent.ToolResult, error) {
	ns := j.namespace
	if v, ok := input["namespace"].(string); ok && v != "" {
		ns = v
	}

	jobList, err := j.clientset.BatchV1().Jobs(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return agent.ToolResult{IsError: true, Content: err.Error()}, err
	}

	var findings []agent.Finding
	for _, job := range jobList.Items {
		findings = append(findings, analyzeJob(job)...)
	}

	content := fmt.Sprintf("Analyzed %d jobs, found %d issues", len(jobList.Items), len(findings))
	return agent.ToolResult{Content: content, Findings: findings}, nil
}

func analyzeJob(job batchv1.Job) []agent.Finding {
	var findings []agent.Finding
	resource := fmt.Sprintf("Job/%s/%s", job.Name, job.Namespace)

	for _, cond := range job.Status.Conditions {
		if cond.Type == batchv1.JobFailed && cond.Status == "True" {
			findings = append(findings, agent.Finding{
				Severity:    "critical",
				Resource:    resource,
				Summary:     fmt.Sprintf("Job failed: %s", cond.Reason),
				RawEvidence: fmt.Sprintf("Condition: Failed=True, Reason: %s, Message: %s, Failed: %d", cond.Reason, cond.Message, job.Status.Failed),
			})
		}
	}

	// Active but exceeding backoff limit
	if job.Status.Failed > 0 && job.Spec.BackoffLimit != nil && job.Status.Failed >= *job.Spec.BackoffLimit {
		findings = append(findings, agent.Finding{
			Severity:    "critical",
			Resource:    resource,
			Summary:     fmt.Sprintf("Job exceeded backoff limit (%d failures)", job.Status.Failed),
			RawEvidence: fmt.Sprintf("Failed: %d, BackoffLimit: %d", job.Status.Failed, *job.Spec.BackoffLimit),
		})
	}

	return findings
}

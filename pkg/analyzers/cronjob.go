package analyzers

import (
	"context"
	"fmt"
	"time"

	"github.com/naman833/k8stalk/pkg/agent"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type CronJobAnalyzer struct {
	clientset kubernetes.Interface
	namespace string
}

func NewCronJobAnalyzer(clientset kubernetes.Interface, namespace string) *CronJobAnalyzer {
	return &CronJobAnalyzer{clientset: clientset, namespace: namespace}
}

func (c *CronJobAnalyzer) Spec() agent.ToolSpec {
	return agent.ToolSpec{
		Name:        "CronJob",
		Description: "Analyze cronjobs for issues: suspended cronjobs, last schedule missed, failing child jobs",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"namespace": map[string]any{"type": "string", "description": "Kubernetes namespace"},
				"name":      map[string]any{"type": "string", "description": "Specific cronjob name"},
			},
		},
	}
}

func (c *CronJobAnalyzer) Execute(ctx context.Context, input map[string]any) (agent.ToolResult, error) {
	ns := c.namespace
	if v, ok := input["namespace"].(string); ok && v != "" {
		ns = v
	}

	cronList, err := c.clientset.BatchV1().CronJobs(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return agent.ToolResult{IsError: true, Content: err.Error()}, err
	}

	var findings []agent.Finding
	for _, cron := range cronList.Items {
		findings = append(findings, analyzeCronJob(cron)...)
	}

	content := fmt.Sprintf("Analyzed %d cronjobs, found %d issues", len(cronList.Items), len(findings))
	return agent.ToolResult{Content: content, Findings: findings}, nil
}

func analyzeCronJob(cron batchv1.CronJob) []agent.Finding {
	var findings []agent.Finding
	resource := fmt.Sprintf("CronJob/%s/%s", cron.Name, cron.Namespace)

	// Suspended
	if cron.Spec.Suspend != nil && *cron.Spec.Suspend {
		findings = append(findings, agent.Finding{
			Severity:    "info",
			Resource:    resource,
			Summary:     "CronJob is suspended",
			RawEvidence: fmt.Sprintf("Schedule: %s, Suspend: true", cron.Spec.Schedule),
		})
	}

	// Last schedule time too old (missed schedule)
	if cron.Status.LastScheduleTime != nil {
		since := time.Since(cron.Status.LastScheduleTime.Time)
		// If last scheduled more than 24h ago and not suspended, might be stuck
		if since > 24*time.Hour && (cron.Spec.Suspend == nil || !*cron.Spec.Suspend) {
			findings = append(findings, agent.Finding{
				Severity:    "warning",
				Resource:    resource,
				Summary:     fmt.Sprintf("CronJob last scheduled %s ago", since.Round(time.Minute)),
				RawEvidence: fmt.Sprintf("Schedule: %s, LastSchedule: %s", cron.Spec.Schedule, cron.Status.LastScheduleTime.Time.Format(time.RFC3339)),
			})
		}
	} else if cron.Spec.Suspend == nil || !*cron.Spec.Suspend {
		// Never scheduled
		if !cron.CreationTimestamp.IsZero() && time.Since(cron.CreationTimestamp.Time) > 1*time.Hour {
			findings = append(findings, agent.Finding{
				Severity:    "warning",
				Resource:    resource,
				Summary:     "CronJob has never been scheduled",
				RawEvidence: fmt.Sprintf("Schedule: %s, Created: %s, LastSchedule: never", cron.Spec.Schedule, cron.CreationTimestamp.Time.Format(time.RFC3339)),
			})
		}
	}

	return findings
}

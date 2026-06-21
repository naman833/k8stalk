package analyzers

import (
	"context"
	"testing"

	"github.com/naman833/k8stalk/pkg/agent"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestPodAnalyzer(t *testing.T) {
	tests := []struct {
		name           string
		pods           []corev1.Pod
		input          map[string]any
		wantFindings   int
		wantSeverity   string
		wantContains   string
	}{
		{
			name: "healthy pod produces no findings",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "healthy-pod", Namespace: "default"},
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
						ContainerStatuses: []corev1.ContainerStatus{
							{Name: "app", Ready: true, RestartCount: 0, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}},
						},
					},
				},
			},
			input:        map[string]any{},
			wantFindings: 0,
		},
		{
			name: "CrashLoopBackOff detected",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "crash-pod", Namespace: "default"},
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
						ContainerStatuses: []corev1.ContainerStatus{
							{
								Name:         "app",
								Ready:        false,
								RestartCount: 10,
								State: corev1.ContainerState{
									Waiting: &corev1.ContainerStateWaiting{
										Reason:  "CrashLoopBackOff",
										Message: "back-off 5m0s restarting failed container",
									},
								},
							},
						},
					},
				},
			},
			input:        map[string]any{},
			wantFindings: 1,
			wantSeverity: "critical",
			wantContains: "CrashLoopBackOff",
		},
		{
			name: "ImagePullBackOff detected",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "image-pod", Namespace: "default"},
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
						ContainerStatuses: []corev1.ContainerStatus{
							{
								Name:  "app",
								Ready: false,
								Image: "registry.example.com/app:latest",
								State: corev1.ContainerState{
									Waiting: &corev1.ContainerStateWaiting{
										Reason:  "ImagePullBackOff",
										Message: "Back-off pulling image",
									},
								},
							},
						},
					},
				},
			},
			input:        map[string]any{},
			wantFindings: 1,
			wantSeverity: "critical",
			wantContains: "cannot pull image",
		},
		{
			name: "OOMKilled detected",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "oom-pod", Namespace: "default"},
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
						ContainerStatuses: []corev1.ContainerStatus{
							{
								Name:         "app",
								Ready:        false,
								RestartCount: 3,
								State: corev1.ContainerState{
									Terminated: &corev1.ContainerStateTerminated{
										Reason:   "OOMKilled",
										ExitCode: 137,
									},
								},
							},
						},
					},
				},
			},
			input:        map[string]any{},
			wantFindings: 1,
			wantSeverity: "critical",
			wantContains: "OOMKilled",
		},
		{
			name: "OOMKilled in lastState surfaced while container is running",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "lastoom-pod", Namespace: "default"},
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
						ContainerStatuses: []corev1.ContainerStatus{
							{
								Name:         "app",
								Ready:        true,
								RestartCount: 1,
								State: corev1.ContainerState{
									Running: &corev1.ContainerStateRunning{},
								},
								LastTerminationState: corev1.ContainerState{
									Terminated: &corev1.ContainerStateTerminated{
										Reason:      "OOMKilled",
										ExitCode:    137,
										ContainerID: "containerd://abc123",
									},
								},
							},
						},
					},
				},
			},
			input:        map[string]any{},
			wantFindings: 1,
			wantSeverity: "critical",
			wantContains: "OOMKilled",
		},
		{
			name: "generic lastState termination reason surfaced as info",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "completed-pod", Namespace: "default"},
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
						ContainerStatuses: []corev1.ContainerStatus{
							{
								Name:         "app",
								Ready:        true,
								RestartCount: 1,
								State: corev1.ContainerState{
									Running: &corev1.ContainerStateRunning{},
								},
								LastTerminationState: corev1.ContainerState{
									Terminated: &corev1.ContainerStateTerminated{
										Reason:   "Completed",
										ExitCode: 0,
									},
								},
							},
						},
					},
				},
			},
			input:        map[string]any{},
			wantFindings: 1,
			wantSeverity: "info",
			wantContains: "Completed",
		},
		{
			name: "Pending pod detected",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "pending-pod", Namespace: "default"},
					Status: corev1.PodStatus{
						Phase: corev1.PodPending,
						Conditions: []corev1.PodCondition{
							{
								Type:    corev1.PodScheduled,
								Status:  corev1.ConditionFalse,
								Reason:  "Unschedulable",
								Message: "0/3 nodes are available: insufficient memory",
							},
						},
					},
				},
			},
			input:        map[string]any{},
			wantFindings: 1,
			wantSeverity: "critical",
			wantContains: "insufficient memory",
		},
		{
			name: "namespace filter works",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "crash-pod", Namespace: "production"},
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
						ContainerStatuses: []corev1.ContainerStatus{
							{
								Name:         "app",
								Ready:        false,
								RestartCount: 10,
								State: corev1.ContainerState{
									Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"},
								},
							},
						},
					},
				},
			},
			input:        map[string]any{"namespace": "production"},
			wantFindings: 1,
			wantSeverity: "critical",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clientset := fake.NewSimpleClientset()

			// Create pods in fake client
			for i := range tt.pods {
				_, err := clientset.CoreV1().Pods(tt.pods[i].Namespace).Create(
					context.Background(), &tt.pods[i], metav1.CreateOptions{},
				)
				if err != nil {
					t.Fatalf("failed to create pod: %v", err)
				}
			}

			analyzer := NewPodAnalyzer(clientset, "")
			result, err := analyzer.Execute(context.Background(), tt.input)
			if err != nil {
				t.Fatalf("Execute failed: %v", err)
			}

			if len(result.Findings) != tt.wantFindings {
				t.Errorf("got %d findings, want %d. Findings: %+v", len(result.Findings), tt.wantFindings, result.Findings)
			}

			if tt.wantFindings > 0 && tt.wantSeverity != "" {
				if result.Findings[0].Severity != tt.wantSeverity {
					t.Errorf("got severity %q, want %q", result.Findings[0].Severity, tt.wantSeverity)
				}
			}

			if tt.wantContains != "" && tt.wantFindings > 0 {
				found := false
				for _, f := range result.Findings {
					if contains(f.Summary, tt.wantContains) || contains(f.RawEvidence, tt.wantContains) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("findings don't contain %q. Findings: %+v", tt.wantContains, result.Findings)
				}
			}

			// Verify it implements the Tool interface
			var _ agent.Tool = analyzer
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

package analyzers

import (
	"github.com/naman833/k8stalk/pkg/agent"
	"github.com/naman833/k8stalk/pkg/k8s"
)

// RegisterAll registers all available analyzers into the tool registry.
func RegisterAll(registry *agent.Registry, client *k8s.Client, namespace string) {
	registry.Register(NewPodAnalyzer(client.Clientset, namespace))
	registry.Register(NewDeploymentAnalyzer(client.Clientset, namespace))
	registry.Register(NewStatefulSetAnalyzer(client.Clientset, namespace))
	registry.Register(NewReplicaSetAnalyzer(client.Clientset, namespace))
	registry.Register(NewServiceAnalyzer(client.Clientset, namespace))
	registry.Register(NewIngressAnalyzer(client.Clientset, namespace))
	registry.Register(NewPVCAnalyzer(client.Clientset, namespace))
	registry.Register(NewHPAAnalyzer(client.Clientset, namespace))
	registry.Register(NewPDBAnalyzer(client.Clientset, namespace))
	registry.Register(NewJobAnalyzer(client.Clientset, namespace))
	registry.Register(NewCronJobAnalyzer(client.Clientset, namespace))
	registry.Register(NewNodeAnalyzer(client.Clientset))
	registry.Register(NewNetworkPolicyAnalyzer(client.Clientset, namespace))
	registry.Register(NewLogsAnalyzer(client.Clientset, namespace))
	registry.Register(NewWebhookAnalyzer(client.Clientset))
	registry.Register(NewEventAnalyzer(client.Clientset, namespace))
}

package k8s

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// GetPods returns pods in the given namespace. Empty namespace means all namespaces.
func GetPods(ctx context.Context, clientset kubernetes.Interface, namespace string) ([]corev1.Pod, error) {
	podList, err := clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list pods: %w", err)
	}
	return podList.Items, nil
}

// GetEvents returns events for a specific resource.
func GetEvents(ctx context.Context, clientset kubernetes.Interface, namespace, name string) ([]corev1.Event, error) {
	fieldSelector := fmt.Sprintf("involvedObject.name=%s", name)
	eventList, err := clientset.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{
		FieldSelector: fieldSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	return eventList.Items, nil
}

// GetNodes returns all nodes in the cluster.
func GetNodes(ctx context.Context, clientset kubernetes.Interface) ([]corev1.Node, error) {
	nodeList, err := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list nodes: %w", err)
	}
	return nodeList.Items, nil
}

// GetNamespaces returns all namespaces.
func GetNamespaces(ctx context.Context, clientset kubernetes.Interface) ([]corev1.Namespace, error) {
	nsList, err := clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list namespaces: %w", err)
	}
	return nsList.Items, nil
}

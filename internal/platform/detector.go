package platform

import (
	"context"
	"strings"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type Platform string

const (
	PlatformEKS       Platform = "eks"
	PlatformAKS       Platform = "aks"
	PlatformGKE       Platform = "gke"
	PlatformOnPremise Platform = "on-premise"
)

func (p Platform) String() string {
	return string(p)
}

const (
	labelEKSNodeGroup    = "eks.amazonaws.com/nodegroup"
	labelEKSCapacity     = "eks.amazonaws.com/capacityType"
	labelGKENodePool     = "cloud.google.com/gke-nodepool"
	labelAKSNodepoolName = "kubernetes.azure.com/agentpool"
)

// DetectPlatform determines the cloud platform from the first node's providerID and labels.
func DetectPlatform(ctx context.Context, clientset kubernetes.Interface) Platform {
	nodes, err := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{Limit: 1})
	if err != nil || len(nodes.Items) == 0 {
		return PlatformOnPremise
	}

	return DetectFromNode(&nodes.Items[0])
}

// DetectFromNode determines the platform from a single node's providerID and labels.
func DetectFromNode(node *v1.Node) Platform {
	if p := platformFromProviderID(node.Spec.ProviderID); p != "" {
		return p
	}

	if p := platformFromLabels(node.Labels); p != "" {
		return p
	}

	return PlatformOnPremise
}

func platformFromProviderID(providerID string) Platform {
	switch {
	case strings.HasPrefix(providerID, "aws://"):
		return PlatformEKS
	case strings.HasPrefix(providerID, "azure://"):
		return PlatformAKS
	case strings.HasPrefix(providerID, "gce://"):
		return PlatformGKE
	default:
		return ""
	}
}

func platformFromLabels(labels map[string]string) Platform {
	if labels == nil {
		return ""
	}

	if _, ok := labels[labelEKSNodeGroup]; ok {
		return PlatformEKS
	}
	if _, ok := labels[labelEKSCapacity]; ok {
		return PlatformEKS
	}
	if _, ok := labels[labelGKENodePool]; ok {
		return PlatformGKE
	}
	if _, ok := labels[labelAKSNodepoolName]; ok {
		return PlatformAKS
	}

	return ""
}

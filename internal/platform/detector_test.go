package platform

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestPlatformString(t *testing.T) {
	testCases := []struct {
		platform Platform
		expected string
	}{
		{PlatformEKS, "eks"},
		{PlatformAKS, "aks"},
		{PlatformGKE, "gke"},
		{PlatformOnPremise, "on-premise"},
	}

	for _, tc := range testCases {
		t.Run(tc.expected, func(t *testing.T) {
			if tc.platform.String() != tc.expected {
				t.Errorf("expected %v, got %v", tc.expected, tc.platform.String())
			}
		})
	}
}

func TestDetectFromNode_ProviderID(t *testing.T) {
	testCases := []struct {
		name       string
		providerID string
		expected   Platform
	}{
		{"aws", "aws://us-east-1/i-0abc123", PlatformEKS},
		{"azure", "azure:///subscriptions/sub-id/resourceGroups/rg/providers/Microsoft.Compute/virtualMachines/vm", PlatformAKS},
		{"gce", "gce://my-project/us-central1-a/my-instance", PlatformGKE},
		{"unknown provider", "digitalocean://droplet-123", PlatformOnPremise},
		{"empty", "", PlatformOnPremise},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			node := &v1.Node{
				Spec: v1.NodeSpec{ProviderID: tc.providerID},
			}
			result := DetectFromNode(node)
			if result != tc.expected {
				t.Errorf("expected %v, got %v", tc.expected, result)
			}
		})
	}
}

func TestDetectFromNode_Labels(t *testing.T) {
	testCases := []struct {
		name     string
		labels   map[string]string
		expected Platform
	}{
		{"eks nodegroup label", map[string]string{"eks.amazonaws.com/nodegroup": "ng-1"}, PlatformEKS},
		{"eks capacity label", map[string]string{"eks.amazonaws.com/capacityType": "ON_DEMAND"}, PlatformEKS},
		{"gke nodepool label", map[string]string{"cloud.google.com/gke-nodepool": "default-pool"}, PlatformGKE},
		{"aks agentpool label", map[string]string{"kubernetes.azure.com/agentpool": "nodepool1"}, PlatformAKS},
		{"no labels", map[string]string{}, PlatformOnPremise},
		{"nil labels", nil, PlatformOnPremise},
		{"unrelated labels", map[string]string{"app": "test"}, PlatformOnPremise},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			node := &v1.Node{
				ObjectMeta: metav1.ObjectMeta{Labels: tc.labels},
			}
			result := DetectFromNode(node)
			if result != tc.expected {
				t.Errorf("expected %v, got %v", tc.expected, result)
			}
		})
	}
}

func TestDetectFromNode_ProviderIDTakesPrecedence(t *testing.T) {
	node := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{"kubernetes.azure.com/agentpool": "pool1"},
		},
		Spec: v1.NodeSpec{ProviderID: "aws://us-east-1/i-123"},
	}

	result := DetectFromNode(node)
	if result != PlatformEKS {
		t.Errorf("expected providerID (eks) to take precedence over labels (aks), got %v", result)
	}
}

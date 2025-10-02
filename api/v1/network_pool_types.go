/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// ISPLineType defines the ISP line type
type ISPLineType string

const (
	ISPLineTypeTelecom ISPLineType = "telecom"
	ISPLineTypeUnicom  ISPLineType = "unicom"
	ISPLineTypeMobile  ISPLineType = "mobile"
	ISPLineTypeBGP     ISPLineType = "bgp"
)

// Protocol defines the network protocol type
type Protocol string

const (
	ProtocolTCP    Protocol = "TCP"
	ProtocolUDP    Protocol = "UDP"
	ProtocolTCPUDP Protocol = "TCP/UDP"
)

// LoadBalancerRef defines reference to an existing load balancer
type LoadBalancerRef struct {
	// ID is the unique identifier of the load balancer
	ID string `json:"id"`

	// Region is the region where the load balancer is located
	Region string `json:"region"`

	// ResourceGroup is the resource group name (if applicable)
	ResourceGroup string `json:"resourceGroup,omitempty"`

	// Provider is the cloud provider (e.g., "volcengine")
	Provider string `json:"provider,omitempty"`
}

// PortRange defines a range of ports
type PortRange struct {
	// Min is the minimum port number
	Min int32 `json:"min"`

	// Max is the maximum port number
	Max int32 `json:"max"`

	// ExcludedPorts is a list of ports that should not be allocated
	ExcludedPorts []int32 `json:"excludedPorts,omitempty"`
}

// PrewarmConfig defines the port prewarming configuration
type PrewarmConfig struct {
	// Enabled indicates whether prewarming is enabled
	Enabled bool `json:"enabled"`

	// BufferSize is the number of ports to keep prewarmed
	BufferSize int32 `json:"bufferSize,omitempty"`
}

// ApolloNetworkPoolSpec defines the desired state of ApolloNetworkPool.
type ApolloNetworkPoolSpec struct {
	// LoadBalancers reference to existing load balancer
	LoadBalancers []LoadBalancerRef `json:"loadBalancers"`

	// ISPLineType specifies the ISP line type
	ISPLineType ISPLineType `json:"ispLineType"`

	// PortRange defines the available port range for allocation
	PortRange PortRange `json:"portRange"`

	// PrewarmConfig defines the port prewarming configuration
	PrewarmConfig PrewarmConfig `json:"prewarmConfig,omitempty"`

	// PodSelector is a label selector to identify which pods should use this network pool
	PodSelector *metav1.LabelSelector `json:"podSelector,omitempty"`
}

// PodPortAllocation represents the mapping between a pod port and its allocated LB port
type PodPortAllocation struct {
	// PodPort is the port number exposed by the pod/container
	PodPort int32 `json:"podPort"`

	// LBPort is the allocated load balancer port that maps to the pod port
	LBPort int32 `json:"lbPort"`

	// Protocol specifies the network protocol for this port mapping
	Protocol Protocol `json:"protocol"`

	// PortName is the name of the port (from container port definition)
	PortName string `json:"portName,omitempty"`
}
type LoadBalancerAllocateStatus struct {
	// Phase represents the current phase of the network pool
	Phase string `json:"phase,omitempty"`

	// AllocatedPorts contains information about currently allocated ports
	AllocatedPorts []PortAllocation `json:"allocatedPorts,omitempty"`

	// PrewarmedPorts contains list of prewarmed port numbers
	PrewarmedPorts []int32 `json:"prewarmedPorts,omitempty"`

	// TotalPorts is the total number of ports in the configured range
	TotalPorts int32 `json:"totalPorts,omitempty"`

	// AllocatedCount is the number of currently allocated ports
	AllocatedCount int32 `json:"allocatedCount,omitempty"`

	// PrewarmedCount is the number of currently prewarmed ports
	PrewarmedCount int32 `json:"prewarmedCount,omitempty"`

	// AvailableCount is the number of available ports for allocation
	AvailableCount int32 `json:"availableCount,omitempty"`

	// LastUpdated is the timestamp of the last status update
	LastUpdated *metav1.Time `json:"lastUpdated,omitempty"`

	// IsSynced indicates whether the loadbalancer has been successfully synchronized.
	IsSynced bool `json:"isSynced,omitempty"`
}

// ApolloNetworkPoolStatus defines the observed state of ApolloNetworkPool.
type ApolloNetworkPoolStatus struct {
	// Allocations maps LoadBalancer ID to its allocation status
	Allocations map[string]LoadBalancerAllocateStatus `json:"allocations,omitempty"`

	// IsSynced indicates whether the network pool has been successfully synchronized.
	IsSynced bool `json:"isSynced,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster

// ApolloNetworkPool is the Schema for the apollo network pools API.
type ApolloNetworkPool struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ApolloNetworkPoolSpec   `json:"spec,omitempty"`
	Status ApolloNetworkPoolStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ApolloNetworkPoolList contains a list of ApolloNetworkPool.
type ApolloNetworkPoolList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ApolloNetworkPool `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ApolloNetworkPool{}, &ApolloNetworkPoolList{})
}

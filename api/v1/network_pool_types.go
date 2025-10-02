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

// PortAllocation represents port allocations to a specific pod
type PortAllocation struct {
	// PodName is the name of the pod using these ports
	PodName string `json:"podName"`

	// PodNamespace is the namespace of the pod using these ports
	PodNamespace string `json:"podNamespace"`

	// PodUID is the UID of the pod using these ports
	PodUID string `json:"podUID,omitempty"`

	// PortMappings contains the detailed port allocation mappings
	PortMappings []PodPortAllocation `json:"portMappings"`

	// LoadBalancer reference to the load balancer these ports are allocated on
	LoadBalancer LoadBalancerRef `json:"loadBalancer"`

	// AllocatedAt is the timestamp when the ports were allocated
	AllocatedAt *metav1.Time `json:"allocatedAt,omitempty"`
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

// GetAllPorts returns all individual LB ports from all allocations
func (status *LoadBalancerAllocateStatus) GetAllPorts() []int32 {
	var allPorts []int32
	for _, alloc := range status.AllocatedPorts {
		for _, mapping := range alloc.PortMappings {
			allPorts = append(allPorts, mapping.LBPort)
		}
	}
	return allPorts
}

// GetPortCount returns the total number of allocated LB ports
func (status *LoadBalancerAllocateStatus) GetPortCount() int32 {
	count := int32(0)
	for _, alloc := range status.AllocatedPorts {
		count += int32(len(alloc.PortMappings))
	}
	return count
}

// FindAllocationByPod finds port allocation for a specific pod
func (status *LoadBalancerAllocateStatus) FindAllocationByPod(podName, podNamespace string) *PortAllocation {
	for i := range status.AllocatedPorts {
		alloc := &status.AllocatedPorts[i]
		if alloc.PodName == podName && alloc.PodNamespace == podNamespace {
			return alloc
		}
	}
	return nil
}

// RemoveAllocationByPod removes port allocation for a specific pod
func (status *LoadBalancerAllocateStatus) RemoveAllocationByPod(podName, podNamespace string) bool {
	for i, alloc := range status.AllocatedPorts {
		if alloc.PodName == podName && alloc.PodNamespace == podNamespace {
			// Remove the allocation by slicing
			status.AllocatedPorts = append(status.AllocatedPorts[:i], status.AllocatedPorts[i+1:]...)
			return true
		}
	}
	return false
}

// GetAllLBPorts returns all allocated LB ports from the allocation
func (alloc *PortAllocation) GetAllLBPorts() []int32 {
	var lbPorts []int32
	for _, mapping := range alloc.PortMappings {
		lbPorts = append(lbPorts, mapping.LBPort)
	}
	return lbPorts
}

// GetAllPodPorts returns all pod ports from the allocation
func (alloc *PortAllocation) GetAllPodPorts() []int32 {
	var podPorts []int32
	for _, mapping := range alloc.PortMappings {
		podPorts = append(podPorts, mapping.PodPort)
	}
	return podPorts
}

// FindPortMappingByPodPort finds port mapping by pod port
func (alloc *PortAllocation) FindPortMappingByPodPort(podPort int32) *PodPortAllocation {
	for i := range alloc.PortMappings {
		if alloc.PortMappings[i].PodPort == podPort {
			return &alloc.PortMappings[i]
		}
	}
	return nil
}

// FindPortMappingByLBPort finds port mapping by LB port
func (alloc *PortAllocation) FindPortMappingByLBPort(lbPort int32) *PodPortAllocation {
	for i := range alloc.PortMappings {
		if alloc.PortMappings[i].LBPort == lbPort {
			return &alloc.PortMappings[i]
		}
	}
	return nil
}

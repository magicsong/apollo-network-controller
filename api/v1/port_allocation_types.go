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

// BindingType defines the type of load balancer binding
type BindingType string

const (
	// BindingTypeVolcengineCLB represents Volcengine Cloud Load Balancer
	BindingTypeVolcengineCLB BindingType = "volcengine-clb"
	// BindingTypeAWSALB represents AWS Application Load Balancer
	BindingTypeAWSALB BindingType = "aws-alb"
	// BindingTypeAzureLB represents Azure Load Balancer
	BindingTypeAzureLB BindingType = "azure-lb"
)

// PodInfo defines the basic information of the pod
type PodInfo struct {
	// Name is the name of the pod
	Name string `json:"name"`

	// Namespace is the namespace of the pod
	Namespace string `json:"namespace"`

	// UID is the UID of the pod for strong reference
	UID string `json:"uid,omitempty"`

	// PodIP is the IP address of the pod
	PodIP string `json:"podIP,omitempty"`

	// NodeName is the name of the node where the pod is running
	NodeName string `json:"nodeName,omitempty"`

	// Labels are the labels of the pod
	Labels map[string]string `json:"labels,omitempty"`
}

// PortBinding defines the binding information between pod port and load balancer port
type PortBinding struct {
	// PodPort is the port number exposed by the pod/container
	PodPort int32 `json:"podPort"`

	// LBPort is the allocated load balancer port that maps to the pod port
	LBPort int32 `json:"lbPort"`

	// Protocol specifies the network protocol for this port mapping
	Protocol Protocol `json:"protocol"`

	// PortName is the name of the port (from container port definition)
	PortName string `json:"portName,omitempty"`

	// BindingType defines the type of load balancer binding
	BindingType BindingType `json:"bindingType"`

	// LoadBalancerRef references the specific load balancer
	LoadBalancerRef LoadBalancerRef `json:"loadBalancerRef"`

	// BindingConfig contains additional binding configuration
	BindingConfig *BindingConfig `json:"bindingConfig,omitempty"`
}

// BindingConfig defines additional configuration for load balancer binding
type BindingConfig struct {
	// HealthCheckPath is the path for health check (for HTTP/HTTPS)
	HealthCheckPath string `json:"healthCheckPath,omitempty"`

	// HealthCheckInterval is the interval between health checks in seconds
	HealthCheckInterval int32 `json:"healthCheckInterval,omitempty"`

	// HealthCheckTimeout is the timeout for health check in seconds
	HealthCheckTimeout int32 `json:"healthCheckTimeout,omitempty"`

	// TargetWeight is the weight for load balancing
	TargetWeight int32 `json:"targetWeight,omitempty"`

	// SessionAffinity defines session affinity configuration
	SessionAffinity string `json:"sessionAffinity,omitempty"`
}

// AllocationPhase represents the phase of port allocation
type AllocationPhase string

const (
	// AllocationPhasePending indicates the allocation is pending
	AllocationPhasePending AllocationPhase = "Pending"

	// AllocationPhaseBinding indicates the ports are being bound to load balancer
	AllocationPhaseBinding AllocationPhase = "Binding"

	// AllocationPhaseBound indicates the ports are successfully bound
	AllocationPhaseBound AllocationPhase = "Bound"

	// AllocationPhaseFailed indicates the allocation failed
	AllocationPhaseFailed AllocationPhase = "Failed"

	// AllocationPhaseUnbinding indicates the ports are being unbound
	AllocationPhaseUnbinding AllocationPhase = "Unbinding"
)

// BindingStatus represents the binding status for a specific port
type BindingStatus struct {
	// PodPort is the pod port number
	PodPort int32 `json:"podPort"`

	// LBPort is the load balancer port number
	LBPort int32 `json:"lbPort"`

	// Phase represents the current binding phase
	Phase AllocationPhase `json:"phase"`

	// Message provides human-readable details about the binding status
	Message string `json:"message,omitempty"`

	// LastProbeTime is the last time the binding was checked
	LastProbeTime *metav1.Time `json:"lastProbeTime,omitempty"`

	// BoundAt is the timestamp when the port was successfully bound
	BoundAt *metav1.Time `json:"boundAt,omitempty"`

	// LoadBalancerConfig contains the actual configuration applied to load balancer
	LoadBalancerConfig map[string]string `json:"loadBalancerConfig,omitempty"`
}

// ApolloPortAllocationSpec defines the desired state of ApolloPortAllocation
type ApolloPortAllocationSpec struct {
	// NetworkPoolRef references the parent network pool
	NetworkPoolRef PoolReference `json:"networkPoolRef"`

	// PodInfo contains the basic information of the pod
	PodInfo PodInfo `json:"podInfo"`

	// PortBindings contains the detailed port binding information
	PortBindings []PortBinding `json:"portBindings"`

	// RequestedAt is the timestamp when the ports were requested
	RequestedAt *metav1.Time `json:"requestedAt,omitempty"`
}

// PoolReference references an ApolloNetworkPool
type PoolReference struct {
	// Name is the name of the network pool
	Name string `json:"name"`

	// Namespace is the namespace of the network pool (if namespace-scoped)
	Namespace string `json:"namespace,omitempty"`
}

// ApolloPortAllocationStatus defines the observed state of ApolloPortAllocation
type ApolloPortAllocationStatus struct {
	// Phase represents the current overall phase of the port allocation
	Phase AllocationPhase `json:"phase,omitempty"`

	// BindingStatuses contains the binding status for each port
	BindingStatuses []BindingStatus `json:"bindingStatuses,omitempty"`

	// AllocatedAt is the timestamp when the ports were allocated
	AllocatedAt *metav1.Time `json:"allocatedAt,omitempty"`

	// BoundAt is the timestamp when all ports were successfully bound
	BoundAt *metav1.Time `json:"boundAt,omitempty"`

	// LastUpdated is the timestamp of the last status update
	LastUpdated *metav1.Time `json:"lastUpdated,omitempty"`

	// Conditions represent the latest available observations of the allocation's current state
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Message provides human-readable details about the overall allocation status
	Message string `json:"message,omitempty"`

	// ObservedGeneration reflects the generation of the most recently observed spec
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Pool",type=string,JSONPath=`.spec.networkPoolRef.name`
// +kubebuilder:printcolumn:name="Pod",type=string,JSONPath=`.spec.podInfo.name`
// +kubebuilder:printcolumn:name="Namespace",type=string,JSONPath=`.spec.podInfo.namespace`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Bindings",type=string,JSONPath=`.status.bindingStatuses[*].phase`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
// +kubebuilder:resource:shortName=apa

// ApolloPortAllocation is the Schema for tracking individual port allocations and their binding status
type ApolloPortAllocation struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ApolloPortAllocationSpec   `json:"spec,omitempty"`
	Status ApolloPortAllocationStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ApolloPortAllocationList contains a list of ApolloPortAllocation.
type ApolloPortAllocationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ApolloPortAllocation `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ApolloPortAllocation{}, &ApolloPortAllocationList{})
}

// GetAllocatedLBPorts returns all allocated LB ports
func (apa *ApolloPortAllocation) GetAllocatedLBPorts() []int32 {
	var lbPorts []int32
	for _, binding := range apa.Spec.PortBindings {
		lbPorts = append(lbPorts, binding.LBPort)
	}
	return lbPorts
}

// GetAllocatedPodPorts returns all allocated pod ports
func (apa *ApolloPortAllocation) GetAllocatedPodPorts() []int32 {
	var podPorts []int32
	for _, binding := range apa.Spec.PortBindings {
		podPorts = append(podPorts, binding.PodPort)
	}
	return podPorts
}

// IsBound returns true if all ports are successfully bound
func (apa *ApolloPortAllocation) IsBound() bool {
	if apa.Status.Phase != AllocationPhaseBound {
		return false
	}
	
	for _, status := range apa.Status.BindingStatuses {
		if status.Phase != AllocationPhaseBound {
			return false
		}
	}
	return true
}

// IsPending returns true if the allocation is in pending phase
func (apa *ApolloPortAllocation) IsPending() bool {
	return apa.Status.Phase == AllocationPhasePending
}

// IsFailed returns true if the allocation is in failed phase
func (apa *ApolloPortAllocation) IsFailed() bool {
	return apa.Status.Phase == AllocationPhaseFailed
}

// GetBindingStatusByPodPort finds binding status by pod port
func (apa *ApolloPortAllocation) GetBindingStatusByPodPort(podPort int32) *BindingStatus {
	for i := range apa.Status.BindingStatuses {
		if apa.Status.BindingStatuses[i].PodPort == podPort {
			return &apa.Status.BindingStatuses[i]
		}
	}
	return nil
}

// GetBindingStatusByLBPort finds binding status by LB port
func (apa *ApolloPortAllocation) GetBindingStatusByLBPort(lbPort int32) *BindingStatus {
	for i := range apa.Status.BindingStatuses {
		if apa.Status.BindingStatuses[i].LBPort == lbPort {
			return &apa.Status.BindingStatuses[i]
		}
	}
	return nil
}

// GetPortBindingByPodPort finds port binding by pod port
func (apa *ApolloPortAllocation) GetPortBindingByPodPort(podPort int32) *PortBinding {
	for i := range apa.Spec.PortBindings {
		if apa.Spec.PortBindings[i].PodPort == podPort {
			return &apa.Spec.PortBindings[i]
		}
	}
	return nil
}

// GetPortBindingByLBPort finds port binding by LB port
func (apa *ApolloPortAllocation) GetPortBindingByLBPort(lbPort int32) *PortBinding {
	for i := range apa.Spec.PortBindings {
		if apa.Spec.PortBindings[i].LBPort == lbPort {
			return &apa.Spec.PortBindings[i]
		}
	}
	return nil
}

// GetLoadBalancerRefs returns all unique load balancer references
func (apa *ApolloPortAllocation) GetLoadBalancerRefs() []LoadBalancerRef {
	lbRefMap := make(map[string]LoadBalancerRef)
	
	for _, binding := range apa.Spec.PortBindings {
		lbRefMap[binding.LoadBalancerRef.ID] = binding.LoadBalancerRef
	}
	
	var lbRefs []LoadBalancerRef
	for _, lbRef := range lbRefMap {
		lbRefs = append(lbRefs, lbRef)
	}
	
	return lbRefs
}

// CountPortsByBindingType returns the count of ports by binding type
func (apa *ApolloPortAllocation) CountPortsByBindingType() map[BindingType]int {
	counts := make(map[BindingType]int)
	
	for _, binding := range apa.Spec.PortBindings {
		counts[binding.BindingType]++
	}
	
	return counts
}

// GetDefaultBindingType returns the default binding type (volcengine-clb)
func GetDefaultBindingType() BindingType {
	return BindingTypeVolcengineCLB
}

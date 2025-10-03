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

	// AllocationPhasePortConflict indicates the ports are in conflict
	AllocationPhasePortConflict AllocationPhase = "PortConflict"
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

// PortAllocationSpec defines the desired state of PortAllocation
type PortAllocationSpec struct {
	// PodInfo contains the basic information of the pod
	PodInfo PodInfo `json:"podInfo"`

	// PortBindings contains the detailed port binding information
	PortBindings []PortBinding `json:"portBindings"`
}

// PortAllocationStatus defines the observed state of PortAllocation
type PortAllocationStatus struct {
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
// +kubebuilder:resource:shortName=pa

// PortAllocation is the Schema for tracking individual port allocations and their binding status
type PortAllocation struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PortAllocationSpec   `json:"spec,omitempty"`
	Status PortAllocationStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// PortAllocationList contains a list of PortAllocation.
type PortAllocationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PortAllocation `json:"items"`
}

func init() {
	SchemeBuilder.Register(&PortAllocation{}, &PortAllocationList{})
}

// GetAllAllocatedPodPorts returns all allocated pod ports from all allocations in the list as a map for efficient lookup
func (apl *PortAllocationList) GetAllocatedPortsByLB(loadBalanceID string) map[int32]bool {
	ports := make(map[int32]bool)
	for _, allocation := range apl.Items {
		for _, binding := range allocation.Spec.PortBindings {
			if binding.LoadBalancerRef.ID == loadBalanceID {
				ports[binding.LBPort] = true
			}
		}
	}
	return ports
}

// GetBoundAllocations returns all allocations that are successfully bound
func (apl *PortAllocationList) GetBoundAllocations() []PortAllocation {
	var boundAllocations []PortAllocation
	for _, allocation := range apl.Items {
		if allocation.Status.Phase == AllocationPhaseBound {
			allBound := true
			for _, status := range allocation.Status.BindingStatuses {
				if status.Phase != AllocationPhaseBound {
					allBound = false
					break
				}
			}
			if allBound {
				boundAllocations = append(boundAllocations, allocation)
			}
		}
	}
	return boundAllocations
}

// GetPendingAllocations returns all allocations that are in pending phase
func (apl *PortAllocationList) GetPendingAllocations() []PortAllocation {
	var pendingAllocations []PortAllocation
	for _, allocation := range apl.Items {
		if allocation.Status.Phase == AllocationPhasePending {
			pendingAllocations = append(pendingAllocations, allocation)
		}
	}
	return pendingAllocations
}

// GetFailedAllocations returns all allocations that are in failed phase
func (apl *PortAllocationList) GetFailedAllocations() []PortAllocation {
	var failedAllocations []PortAllocation
	for _, allocation := range apl.Items {
		if allocation.Status.Phase == AllocationPhaseFailed {
			failedAllocations = append(failedAllocations, allocation)
		}
	}
	return failedAllocations
}

// FindAllocationsByPodPort finds all allocations that contain a specific pod port
func (apl *PortAllocationList) FindAllocationsByPodPort(podPort int32) []PortAllocation {
	var matchingAllocations []PortAllocation
	for _, allocation := range apl.Items {
		for _, binding := range allocation.Spec.PortBindings {
			if binding.PodPort == podPort {
				matchingAllocations = append(matchingAllocations, allocation)
				break
			}
		}
	}
	return matchingAllocations
}

// FindAllocationsByLBPort finds all allocations that contain a specific LB port
func (apl *PortAllocationList) FindAllocationsByLBPort(lbPort int32) []PortAllocation {
	var matchingAllocations []PortAllocation
	for _, allocation := range apl.Items {
		for _, binding := range allocation.Spec.PortBindings {
			if binding.LBPort == lbPort {
				matchingAllocations = append(matchingAllocations, allocation)
				break
			}
		}
	}
	return matchingAllocations
}

// FindAllocationsByPodInfo finds all allocations for a specific pod
func (apl *PortAllocationList) FindAllocationsByPodInfo(namespace, name string) []PortAllocation {
	var matchingAllocations []PortAllocation
	for _, allocation := range apl.Items {
		if allocation.Spec.PodInfo.Namespace == namespace && allocation.Spec.PodInfo.Name == name {
			matchingAllocations = append(matchingAllocations, allocation)
		}
	}
	return matchingAllocations
}

// GetAllLoadBalancerRefs returns all unique load balancer references from all allocations
func (apl *PortAllocationList) GetAllLoadBalancerRefs() []LoadBalancerRef {
	lbRefMap := make(map[string]LoadBalancerRef)
	
	for _, allocation := range apl.Items {
		for _, binding := range allocation.Spec.PortBindings {
			lbRefMap[binding.LoadBalancerRef.ID] = binding.LoadBalancerRef
		}
	}
	
	var lbRefs []LoadBalancerRef
	for _, lbRef := range lbRefMap {
		lbRefs = append(lbRefs, lbRef)
	}
	
	return lbRefs
}

// CountAllPortsByBindingType returns the total count of ports by binding type across all allocations
func (apl *PortAllocationList) CountAllPortsByBindingType() map[BindingType]int {
	counts := make(map[BindingType]int)
	
	for _, allocation := range apl.Items {
		for _, binding := range allocation.Spec.PortBindings {
			counts[binding.BindingType]++
		}
	}
	
	return counts
}

// GetAllocationsByPhase returns all allocations in a specific phase
func (apl *PortAllocationList) GetAllocationsByPhase(phase AllocationPhase) []PortAllocation {
	var matchingAllocations []PortAllocation
	for _, allocation := range apl.Items {
		if allocation.Status.Phase == phase {
			matchingAllocations = append(matchingAllocations, allocation)
		}
	}
	return matchingAllocations
}

// CountAllocationsByPhase returns the count of allocations by phase
func (apl *PortAllocationList) CountAllocationsByPhase() map[AllocationPhase]int {
	counts := make(map[AllocationPhase]int)
	
	for _, allocation := range apl.Items {
		counts[allocation.Status.Phase]++
	}
	
	return counts
}

// HasPortConflicts checks if there are any port conflicts in the list
func (apl *PortAllocationList) HasPortConflicts() bool {
	lbPortMap := make(map[int32]bool)
	
	for _, allocation := range apl.Items {
		for _, binding := range allocation.Spec.PortBindings {
			if lbPortMap[binding.LBPort] {
				return true // Found conflict
			}
			lbPortMap[binding.LBPort] = true
		}
	}
	
	return false
}

// GetDefaultBindingType returns the default binding type (volcengine-clb)
func GetDefaultBindingType() BindingType {
	return BindingTypeVolcengineCLB
}

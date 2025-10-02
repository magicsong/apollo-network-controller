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
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestApolloPortAllocation_HelperMethods(t *testing.T) {
	// Create a test ApolloPortAllocation
	allocation := &ApolloPortAllocation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-allocation",
			Namespace: "default",
		},
		Spec: ApolloPortAllocationSpec{
			NetworkPoolRef: PoolReference{
				Name: "test-pool",
			},
			PodInfo: PodInfo{
				Name:      "test-pod",
				Namespace: "default",
				UID:       "12345",
				PodIP:     "10.244.0.5",
			},
			PortBindings: []PortBinding{
				{
					PodPort:     8080,
					LBPort:      8080,
					Protocol:    ProtocolTCP,
					PortName:    "http",
					BindingType: BindingTypeVolcengineCLB,
					LoadBalancerRef: LoadBalancerRef{
						ID:       "clb-123",
						Region:   "cn-beijing",
						Provider: "volcengine",
					},
				},
				{
					PodPort:     9090,
					LBPort:      9090,
					Protocol:    ProtocolTCP,
					PortName:    "metrics",
					BindingType: BindingTypeVolcengineCLB,
					LoadBalancerRef: LoadBalancerRef{
						ID:       "clb-123",
						Region:   "cn-beijing",
						Provider: "volcengine",
					},
				},
			},
		},
		Status: ApolloPortAllocationStatus{
			Phase: AllocationPhaseBound,
			BindingStatuses: []BindingStatus{
				{
					PodPort: 8080,
					LBPort:  8080,
					Phase:   AllocationPhaseBound,
					Message: "Successfully bound",
				},
				{
					PodPort: 9090,
					LBPort:  9090,
					Phase:   AllocationPhaseBound,
					Message: "Successfully bound",
				},
			},
		},
	}

	t.Run("GetAllocatedLBPorts", func(t *testing.T) {
		lbPorts := allocation.GetAllocatedLBPorts()
		assert.Equal(t, []int32{8080, 9090}, lbPorts)
	})

	t.Run("GetAllocatedPodPorts", func(t *testing.T) {
		podPorts := allocation.GetAllocatedPodPorts()
		assert.Equal(t, []int32{8080, 9090}, podPorts)
	})

	t.Run("IsBound", func(t *testing.T) {
		assert.True(t, allocation.IsBound())
		
		// Test with failed binding
		allocation.Status.BindingStatuses[0].Phase = AllocationPhaseFailed
		assert.False(t, allocation.IsBound())
	})

	t.Run("IsPending", func(t *testing.T) {
		allocation.Status.Phase = AllocationPhasePending
		assert.True(t, allocation.IsPending())
		
		allocation.Status.Phase = AllocationPhaseBound
		assert.False(t, allocation.IsPending())
	})

	t.Run("IsFailed", func(t *testing.T) {
		allocation.Status.Phase = AllocationPhaseFailed
		assert.True(t, allocation.IsFailed())
		
		allocation.Status.Phase = AllocationPhaseBound
		assert.False(t, allocation.IsFailed())
	})

	t.Run("GetBindingStatusByPodPort", func(t *testing.T) {
		status := allocation.GetBindingStatusByPodPort(8080)
		assert.NotNil(t, status)
		assert.Equal(t, int32(8080), status.PodPort)
		assert.Equal(t, int32(8080), status.LBPort)
		
		status = allocation.GetBindingStatusByPodPort(9999)
		assert.Nil(t, status)
	})

	t.Run("GetBindingStatusByLBPort", func(t *testing.T) {
		status := allocation.GetBindingStatusByLBPort(9090)
		assert.NotNil(t, status)
		assert.Equal(t, int32(9090), status.PodPort)
		assert.Equal(t, int32(9090), status.LBPort)
		
		status = allocation.GetBindingStatusByLBPort(9999)
		assert.Nil(t, status)
	})

	t.Run("GetPortBindingByPodPort", func(t *testing.T) {
		binding := allocation.GetPortBindingByPodPort(8080)
		assert.NotNil(t, binding)
		assert.Equal(t, int32(8080), binding.PodPort)
		assert.Equal(t, "http", binding.PortName)
		
		binding = allocation.GetPortBindingByPodPort(9999)
		assert.Nil(t, binding)
	})

	t.Run("GetPortBindingByLBPort", func(t *testing.T) {
		binding := allocation.GetPortBindingByLBPort(9090)
		assert.NotNil(t, binding)
		assert.Equal(t, int32(9090), binding.LBPort)
		assert.Equal(t, "metrics", binding.PortName)
		
		binding = allocation.GetPortBindingByLBPort(9999)
		assert.Nil(t, binding)
	})

	t.Run("GetLoadBalancerRefs", func(t *testing.T) {
		lbRefs := allocation.GetLoadBalancerRefs()
		assert.Len(t, lbRefs, 1) // Both bindings use the same LB
		assert.Equal(t, "clb-123", lbRefs[0].ID)
	})

	t.Run("CountPortsByBindingType", func(t *testing.T) {
		counts := allocation.CountPortsByBindingType()
		assert.Equal(t, 2, counts[BindingTypeVolcengineCLB])
		assert.Equal(t, 0, counts[BindingTypeAWSALB])
	})

	t.Run("GetDefaultBindingType", func(t *testing.T) {
		defaultType := GetDefaultBindingType()
		assert.Equal(t, BindingTypeVolcengineCLB, defaultType)
	})
}

func TestApolloPortAllocation_BindingTypes(t *testing.T) {
	tests := []struct {
		name        string
		bindingType BindingType
		expected    string
	}{
		{
			name:        "Volcengine CLB",
			bindingType: BindingTypeVolcengineCLB,
			expected:    "volcengine-clb",
		},
		{
			name:        "AWS ALB",
			bindingType: BindingTypeAWSALB,
			expected:    "aws-alb",
		},
		{
			name:        "Azure LB",
			bindingType: BindingTypeAzureLB,
			expected:    "azure-lb",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, string(tt.bindingType))
		})
	}
}

func TestApolloPortAllocation_AllocationPhases(t *testing.T) {
	tests := []struct {
		name     string
		phase    AllocationPhase
		expected string
	}{
		{
			name:     "Pending",
			phase:    AllocationPhasePending,
			expected: "Pending",
		},
		{
			name:     "Binding",
			phase:    AllocationPhaseBinding,
			expected: "Binding",
		},
		{
			name:     "Bound",
			phase:    AllocationPhaseBound,
			expected: "Bound",
		},
		{
			name:     "Failed",
			phase:    AllocationPhaseFailed,
			expected: "Failed",
		},
		{
			name:     "Unbinding",
			phase:    AllocationPhaseUnbinding,
			expected: "Unbinding",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, string(tt.phase))
		})
	}
}

func TestApolloPortAllocation_ComplexScenarios(t *testing.T) {
	t.Run("Multiple Load Balancers", func(t *testing.T) {
		allocation := &ApolloPortAllocation{
			Spec: ApolloPortAllocationSpec{
				PortBindings: []PortBinding{
					{
						PodPort:     8080,
						LBPort:      8080,
						BindingType: BindingTypeVolcengineCLB,
						LoadBalancerRef: LoadBalancerRef{
							ID: "clb-123",
						},
					},
					{
						PodPort:     9090,
						LBPort:      9090,
						BindingType: BindingTypeAWSALB,
						LoadBalancerRef: LoadBalancerRef{
							ID: "alb-456",
						},
					},
				},
			},
		}

		lbRefs := allocation.GetLoadBalancerRefs()
		assert.Len(t, lbRefs, 2)

		counts := allocation.CountPortsByBindingType()
		assert.Equal(t, 1, counts[BindingTypeVolcengineCLB])
		assert.Equal(t, 1, counts[BindingTypeAWSALB])
	})

	t.Run("Partial Binding Failure", func(t *testing.T) {
		allocation := &ApolloPortAllocation{
			Status: ApolloPortAllocationStatus{
				Phase: AllocationPhaseBinding,
				BindingStatuses: []BindingStatus{
					{
						PodPort: 8080,
						LBPort:  8080,
						Phase:   AllocationPhaseBound,
					},
					{
						PodPort: 9090,
						LBPort:  9090,
						Phase:   AllocationPhaseFailed,
					},
				},
			},
		}

		assert.False(t, allocation.IsBound())
		assert.False(t, allocation.IsPending())
		assert.False(t, allocation.IsFailed())
	})
}
package allocator

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apollov1 "github.com/magicsong/apollo-network-controller/api/v1"
)

func (pa *PoolAllocator) tryAllocatePort(ctx context.Context, poolName, namespace string,
	podName, podNamespace string, containerPorts []apollov1.PodPortAllocation) (*apollov1.PortAllocation, error) {

	// 1. Get current pool state
	pool := &apollov1.ApolloNetworkPool{}
	poolKey := types.NamespacedName{Name: poolName, Namespace: namespace}

	if err := pa.client.Get(ctx, poolKey, pool); err != nil {
		return nil, fmt.Errorf("failed to get pool %s: %w", poolName, err)
	}

	// 1.5. Get current allocations
	allocations := &apollov1.PortAllocationList{}
	err := pa.client.List(ctx, allocations, client.MatchingLabels{LabelPoolNameKey: pool.Name})
	if err != nil {
		return nil, fmt.Errorf("failed to list port allocations: %w", err)
	}

	// 2. Select load balancer
	selectedLB, err := pa.SelectLoadBalancer(ctx, pool, len(containerPorts))
	if err != nil {
		return nil, fmt.Errorf("failed to select load balancer: %w", err)
	}

	// 3. Find available ports for selected LB
	lbPorts, err := pa.findAvailablePorts(pool, allocations, selectedLB, len(containerPorts))
	if err != nil {
		return nil, fmt.Errorf("failed to find available ports on LB %s: %w", selectedLB.ID, err)
	}

	// 4. Create port bindings
	portBindings := make([]apollov1.PortBinding, len(containerPorts))
	for i, containerPort := range containerPorts {
		portBindings[i] = apollov1.PortBinding{
			PodPort:         containerPort.PodPort,
			LBPort:          lbPorts[i],
			Protocol:        containerPort.Protocol,
			PortName:        containerPort.PortName,
			BindingType:     apollov1.GetDefaultBindingType(),
			LoadBalancerRef: *selectedLB,
		}
	}

	// 5. Create allocation
	allocation := &apollov1.PortAllocation{
		Spec: apollov1.PortAllocationSpec{
			PodInfo: apollov1.PodInfo{
				Name:      podName,
				Namespace: podNamespace,
			},
			PortBindings: portBindings,
		},
		Status: apollov1.PortAllocationStatus{
			Phase:       apollov1.AllocationPhasePending,
			AllocatedAt: &metav1.Time{Time: time.Now()},
		},
	}

	// 6. Update pool atomically
	return allocation, pa.updatePoolWithAllocation(ctx, pool, allocation)
}

// getLBPortUsage returns the number of ports used by this LB based on allocation list
func (pa *PoolAllocator) getLBPortUsage(allocationList *apollov1.PortAllocationList, lb *apollov1.LoadBalancerRef) int32 {
	var usage int32 = 0
	
	// Count ports allocated to this specific LB from the allocation list
	for _, allocation := range allocationList.Items {
		for _, binding := range allocation.Spec.PortBindings {
			if binding.LoadBalancerRef.ID == lb.ID {
				usage++
			}
		}
	}
	
	return usage
}

// hasAvailablePorts checks if the LB has enough available ports for the requested count
func (pa *PoolAllocator) hasAvailablePorts(pool *apollov1.ApolloNetworkPool, allocationList *apollov1.PortAllocationList, lb *apollov1.LoadBalancerRef, portCount int) bool {
	usage := pa.getLBPortUsage(allocationList, lb)
	totalPorts := pool.Spec.PortRange.Max - pool.Spec.PortRange.Min + 1 - int32(len(pool.Spec.PortRange.ExcludedPorts))
	availablePorts := totalPorts - usage
	
	// Also consider prewarmed ports as occupied
	if pool.Status.Allocations != nil {
		if lbStatus, exists := pool.Status.Allocations[lb.ID]; exists {
			availablePorts -= int32(len(lbStatus.PrewarmedPorts))
		}
	}
	
	return availablePorts >= int32(portCount)
}

// hasAvailablePort checks if the LB has at least one available port (backward compatibility)
func (pa *PoolAllocator) hasAvailablePort(pool *apollov1.ApolloNetworkPool, allocationList *apollov1.PortAllocationList, lb *apollov1.LoadBalancerRef) bool {
	return pa.hasAvailablePorts(pool, allocationList, lb, 1)
}

// findAvailablePorts finds multiple available ports on the specified LB
func (pa *PoolAllocator) findAvailablePorts(pool *apollov1.ApolloNetworkPool,
	allocationList *apollov1.PortAllocationList, lb *apollov1.LoadBalancerRef, count int) ([]int32, error) {

	if count <= 0 {
		return nil, fmt.Errorf("port count must be positive, got %d", count)
	}

	// Build set of allocated ports for this LB from the allocation list
	allocatedPorts := make(map[int32]bool)

	// Get allocated ports for this specific LB from allocation list
	for _, allocation := range allocationList.Items {
		for _, binding := range allocation.Spec.PortBindings {
			if binding.LoadBalancerRef.ID == lb.ID {
				allocatedPorts[binding.LBPort] = true
			}
		}
	}

	// Also consider prewarmed ports from pool status as occupied
	if pool.Status.Allocations != nil {
		if lbStatus, exists := pool.Status.Allocations[lb.ID]; exists {
			for _, port := range lbStatus.PrewarmedPorts {
				allocatedPorts[port] = true
			}
		}
	}

	// Global excluded ports (apply to all LBs)
	for _, port := range pool.Spec.PortRange.ExcludedPorts {
		allocatedPorts[port] = true
	}

	// Find available ports
	availablePorts := make([]int32, 0, count)
	for port := pool.Spec.PortRange.Min; port <= pool.Spec.PortRange.Max && len(availablePorts) < count; port++ {
		if !allocatedPorts[port] {
			availablePorts = append(availablePorts, port)
		}
	}

	if len(availablePorts) < count {
		return nil, fmt.Errorf("only found %d available ports, need %d for LB %s in range %d-%d",
			len(availablePorts), count, lb.ID, pool.Spec.PortRange.Min, pool.Spec.PortRange.Max)
	}

	return availablePorts, nil
}

// PatchOperation represents a JSON patch operation
type PatchOperation struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value,omitempty"`
}

// updatePoolWithAllocation updates the pool with a new allocation using strategic merge patch
func (pa *PoolAllocator) updatePoolWithAllocation(ctx context.Context,
	pool *apollov1.ApolloNetworkPool, allocation *apollov1.PortAllocation) error {

	// Get LB ID from the first port binding (assuming all bindings use the same LB)
	if len(allocation.Spec.PortBindings) == 0 {
		return fmt.Errorf("no port bindings found in allocation")
	}
	lbID := allocation.Spec.PortBindings[0].LoadBalancerRef.ID

	// Calculate updated counters
	totalPorts := pool.Spec.PortRange.Max - pool.Spec.PortRange.Min + 1 - int32(len(pool.Spec.PortRange.ExcludedPorts))

	// Get current LB status or create new one
	var currentLBStatus apollov1.LoadBalancerAllocateStatus
	if pool.Status.Allocations != nil {
		if status, exists := pool.Status.Allocations[lbID]; exists {
			currentLBStatus = status
		}
	}

	// Add new allocation to the current status
	currentLBStatus.AllocatedPorts = append(currentLBStatus.AllocatedPorts, *allocation)

	// Update counters - count ports manually since GetPortCount method doesn't exist
	portCount := int32(0)
	for _, alloc := range currentLBStatus.AllocatedPorts {
		portCount += int32(len(alloc.Spec.PortBindings))
	}
	currentLBStatus.AllocatedCount = portCount
	currentLBStatus.TotalPorts = totalPorts
	currentLBStatus.AvailableCount = totalPorts - currentLBStatus.AllocatedCount - currentLBStatus.PrewarmedCount

	// Update timestamp
	now := metav1.Time{Time: time.Now()}
	currentLBStatus.LastUpdated = &now

	// Create strategic merge patch
	patch := map[string]interface{}{
		"status": map[string]interface{}{
			"allocations": map[string]interface{}{
				lbID: currentLBStatus,
			},
		},
	}

	patchBytes, err := json.Marshal(patch)
	if err != nil {
		return fmt.Errorf("failed to marshal patch: %w", err)
	}

	// Apply strategic merge patch
	return pa.client.Status().Patch(ctx, pool, client.RawPatch(types.StrategicMergePatchType, patchBytes))
}

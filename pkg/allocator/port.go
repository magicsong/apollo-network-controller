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

	// 2. Select load balancer
	selectedLB, err := pa.SelectLoadBalancer(pool, len(containerPorts))
	if err != nil {
		return nil, fmt.Errorf("failed to select load balancer: %w", err)
	}

	// 3. Find available ports for selected LB
	lbPorts, err := pa.findAvailablePorts(pool, selectedLB, len(containerPorts))
	if err != nil {
		return nil, fmt.Errorf("failed to find available ports on LB %s: %w", selectedLB.ID, err)
	}

	// 4. Create port mappings
	portMappings := make([]apollov1.PodPortAllocation, len(containerPorts))
	for i, containerPort := range containerPorts {
		portMappings[i] = apollov1.PodPortAllocation{
			PodPort:  containerPort.PodPort,
			LBPort:   lbPorts[i],
			Protocol: containerPort.Protocol,
			PortName: containerPort.PortName,
		}
	}

	// 5. Create allocation
	allocation := &apollov1.PortAllocation{
		PodName:      podName,
		PodNamespace: podNamespace,
		PortMappings: portMappings,
		LoadBalancer: *selectedLB,
		AllocatedAt:  &metav1.Time{Time: time.Now()},
	}

	// 6. Update pool atomically
	return allocation, pa.updatePoolWithAllocation(ctx, pool, allocation)
}

// getLBPortUsage returns the number of ports used by this LB
func (pa *PoolAllocator) getLBPortUsage(pool *apollov1.ApolloNetworkPool, lb *apollov1.LoadBalancerRef) int32 {
	// Check if we have allocation status for this LB
	if pool.Status.Allocations == nil {
		return 0
	}

	// Get allocation status for this specific LB
	lbStatus, exists := pool.Status.Allocations[lb.ID]
	if !exists {
		return 0
	}

	// Return the allocated count for this LB
	return lbStatus.AllocatedCount
}

// hasAvailablePorts checks if the LB has enough available ports for the requested count
func (pa *PoolAllocator) hasAvailablePorts(pool *apollov1.ApolloNetworkPool, lb *apollov1.LoadBalancerRef, portCount int) bool {
	usage := pa.getLBPortUsage(pool, lb)
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
func (pa *PoolAllocator) hasAvailablePort(pool *apollov1.ApolloNetworkPool, lb *apollov1.LoadBalancerRef) bool {
	return pa.hasAvailablePorts(pool, lb, 1)
}

// findAvailablePorts finds multiple available ports on the specified LB
func (pa *PoolAllocator) findAvailablePorts(pool *apollov1.ApolloNetworkPool,
	lb *apollov1.LoadBalancerRef, count int) ([]int32, error) {

	if count <= 0 {
		return nil, fmt.Errorf("port count must be positive, got %d", count)
	}

	// Build set of allocated ports for this LB
	allocatedPorts := make(map[int32]bool)

	// Get LB-specific allocation status
	if pool.Status.Allocations != nil {
		if lbStatus, exists := pool.Status.Allocations[lb.ID]; exists {
			// Get all ports from all allocations
			allPorts := lbStatus.GetAllPorts()
			for _, port := range allPorts {
				allocatedPorts[port] = true
			}

			// Prewarmed ports for this LB
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

	// Create patch for the specific LB allocation
	lbID := allocation.LoadBalancer.ID

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

	// Update counters
	currentLBStatus.AllocatedCount = currentLBStatus.GetPortCount()
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

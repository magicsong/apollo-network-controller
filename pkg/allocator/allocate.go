package allocator

import (
	"context"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apollov1 "github.com/magicsong/apollo-network-controller/api/v1"
)

func (pa *PoolAllocator) tryAllocatePort(ctx context.Context, poolName, namespace string,
	podName, podNamespace string, containerPorts []apollov1.PodPortAllocation) (*apollov1.PortAllocation, error) {

	// First check for duplicate allocations for the same pod
	if err := pa.checkForDuplicateAllocation(ctx, poolName, podName, podNamespace); err != nil {
		return nil, err
	}

	allocations, err := pa.getPoolAndAllocations(ctx, poolName, podNamespace)
	if err != nil {
		return nil, fmt.Errorf("failed to get pool and allocations: %w", err)
	}

	var portBindings []apollov1.PortBinding
	
	// Handle empty container ports case
	if len(containerPorts) == 0 {
		portBindings = []apollov1.PortBinding{}
	} else {
		// 3. Select load balancer
		selectedLB, err := pa.SelectLoadBalancer(ctx, allocations, len(containerPorts))
		if err != nil {
			return nil, fmt.Errorf("failed to select load balancer: %w", err)
		}

		// 4. Find available ports for selected LB
		lbPorts, err := pa.findAvailablePorts(allocations, selectedLB, len(containerPorts))
		if err != nil {
			return nil, fmt.Errorf("failed to find available ports on LB %s: %w", selectedLB.ID, err)
		}

		// 5. Create port bindings
		portBindings = make([]apollov1.PortBinding, len(containerPorts))
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
	}

	// 5. Create allocation
	allocation := &apollov1.PortAllocation{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("%s-%s", allocations.pool.Name, podName),
			Namespace:    allocations.pool.Namespace,
			Labels: map[string]string{
				LabelPoolNameKey:     allocations.pool.Name,
				LabelPodNameKey:      podName,
			},
			Annotations: map[string]string{
				AnnotationBindingTypeKey:  string(apollov1.GetDefaultBindingType()),
			},
		},
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
	return allocation, nil
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
func (pa *PoolAllocator) hasAvailablePorts(existAllocation *poolAndAllocations, lb *apollov1.LoadBalancerRef, portCount int) bool {
	usage := pa.getLBPortUsage(existAllocation.allocations, lb)
	totalPorts := existAllocation.pool.Spec.PortRange.Max - existAllocation.pool.Spec.PortRange.Min + 1 - int32(len(existAllocation.pool.Spec.PortRange.ExcludedPorts))
	availablePorts := totalPorts - usage
	return availablePorts >= int32(portCount)
}

// findAvailablePorts finds multiple available ports on the specified LB
func (pa *PoolAllocator) findAvailablePorts(
	existAllocation *poolAndAllocations, lb *apollov1.LoadBalancerRef, count int) ([]int32, error) {

	if count <= 0 {
		return nil, fmt.Errorf("port count must be positive, got %d", count)
	}

	// Build set of allocated ports for this LB from the allocation list
	allocatedPorts := make(map[int32]bool)
	if len(existAllocation.allocations.Items) > 0 {
		allocatedPorts = existAllocation.allocations.GetAllocatedPortsByLB(lb.ID)
	}

	// Global excluded ports (apply to all LBs)
	for _, port := range existAllocation.pool.Spec.PortRange.ExcludedPorts {
		allocatedPorts[port] = true
	}

	// Find available ports
	availablePorts := make([]int32, 0, count)
	for port := existAllocation.pool.Spec.PortRange.Min; port <= existAllocation.pool.Spec.PortRange.Max && len(availablePorts) < count; port++ {
		if !allocatedPorts[port] {
			availablePorts = append(availablePorts, port)
		}
	}

	if len(availablePorts) < count {
		return nil, fmt.Errorf("only found %d available ports, need %d for LB %s in range %d-%d",
			len(availablePorts), count, lb.ID, existAllocation.pool.Spec.PortRange.Min, existAllocation.pool.Spec.PortRange.Max)
	}

	return availablePorts, nil
}

// createAllocation creates a new port allocation in the apiserver
func (pa *PoolAllocator) createAllocation(ctx context.Context, allocation *apollov1.PortAllocation) error {
	if allocation == nil {
		return fmt.Errorf("allocation cannot be nil")
	}

	// Check if allocation already exists with the same name
	existing := &apollov1.PortAllocation{}
	key := types.NamespacedName{Name: allocation.Name, Namespace: allocation.Namespace}
	
	err := pa.client.Get(ctx, key, existing)
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			return fmt.Errorf("failed to check existing allocation: %w", err)
		}
		
		// No allocation with this name exists, create it
		if err := pa.client.Create(ctx, allocation); err != nil {
			return fmt.Errorf("failed to create port allocation: %w", err)
		}
		return nil
	}

	// Allocation already exists with the same name
	// Check if it's being deleted
	if existing.DeletionTimestamp != nil {
		return fmt.Errorf("allocation %s/%s is being deleted, cannot create new allocation with the same name", 
			existing.Namespace, existing.Name)
	}

	// Allocation already exists and is active - this should not happen
	return fmt.Errorf("allocation %s/%s already exists, cannot create duplicate allocation", 
		existing.Namespace, existing.Name)
}

// checkForDuplicateAllocation checks if there's already an allocation for the given pod in the pool
// This method prevents creating multiple allocations for the same pod in the same pool
func (pa *PoolAllocator) checkForDuplicateAllocation(ctx context.Context, poolName, podName, podNamespace string) error {
	// List all allocations for this pool
	allocations := &apollov1.PortAllocationList{}
	err := pa.client.List(ctx, allocations, client.MatchingLabels{LabelPoolNameKey: poolName})
	if err != nil {
		return fmt.Errorf("failed to list port allocations for pool %s: %w", poolName, err)
	}

	// Check if there's already an allocation for this pod in this pool
	for _, existing := range allocations.Items {
		// Check if it's for the same pod
		if existing.Spec.PodInfo.Name == podName && existing.Spec.PodInfo.Namespace == podNamespace {
			// Check if it's being deleted
			if existing.DeletionTimestamp != nil {
				return fmt.Errorf("pod %s/%s already has an allocation (%s) in pool %s that is being deleted, cannot create new allocation", 
					podNamespace, podName, existing.Name, poolName)
			}
			
			// Active allocation exists for the same pod
			return fmt.Errorf("pod %s/%s already has an active allocation (%s) in pool %s, multiple allocations per pod are not allowed", 
				podNamespace, podName, existing.Name, poolName)
		}
	}

	return nil
}

package allocator

import (
	"context"
	"fmt"

	apollov1 "github.com/magicsong/apollo-network-controller/api/v1"
)

// SelectLoadBalancer selects the best load balancer based on strategy for multiple ports
func (pa *PoolAllocator) SelectLoadBalancer(ctx context.Context, allocations *poolAndAllocations, portCount int) (*apollov1.LoadBalancerRef, error) {
	switch pa.strategy {
	case StrategyRoundRobin:
		return pa.selectRoundRobin(ctx, allocations, portCount)
	case StrategyLeastUsed:
		return pa.selectLeastUsed(allocations, portCount)
	default:
		return pa.selectRoundRobin(ctx, allocations, portCount)
	}
}

// selectRoundRobin implements round-robin selection across load balancers
func (pa *PoolAllocator) selectRoundRobin(ctx context.Context, existAllocation *poolAndAllocations, portCount int) (*apollov1.LoadBalancerRef, error) {
	if len(existAllocation.pool.Spec.LoadBalancers) == 0 {
		return nil, fmt.Errorf("no load balancers configured")
	}

	poolKey := fmt.Sprintf("%s/%s", existAllocation.pool.Namespace, existAllocation.pool.Name)

	pa.indexMutex.Lock()
	defer pa.indexMutex.Unlock()

	currentIndex := pa.roundRobinIndex[poolKey]

	// Try each LB starting from current index
	for i := 0; i < len(existAllocation.pool.Spec.LoadBalancers); i++ {
		lbIndex := (currentIndex + i) % len(existAllocation.pool.Spec.LoadBalancers)
		lb := &existAllocation.pool.Spec.LoadBalancers[lbIndex]

		if pa.hasAvailablePorts(existAllocation, lb, portCount) {
			// Update index for next allocation
			pa.roundRobinIndex[poolKey] = (lbIndex + 1) % len(existAllocation.pool.Spec.LoadBalancers)
			return lb, nil
		}
	}

	return nil, fmt.Errorf("no load balancer has %d available ports", portCount)
}

// selectLeastUsed selects the LB with least allocated ports
func (pa *PoolAllocator) selectLeastUsed(existAllocation *poolAndAllocations, portCount int) (*apollov1.LoadBalancerRef, error) {
	if len(existAllocation.pool.Spec.LoadBalancers) == 0 {
		return nil, fmt.Errorf("no load balancers configured")
	}

	var bestLB *apollov1.LoadBalancerRef
	minUsage := int32(^uint32(0) >> 1) // max int32

	for i := range existAllocation.pool.Spec.LoadBalancers {
		lb := &existAllocation.pool.Spec.LoadBalancers[i]
		usage := pa.getLBPortUsage(existAllocation.allocations, lb)

		if usage < minUsage && pa.hasAvailablePorts(existAllocation, lb, portCount) {
			minUsage = usage
			bestLB = lb
		}
	}

	if bestLB == nil {
		return nil, fmt.Errorf("no load balancer has %d available ports", portCount)
	}

	return bestLB, nil
}

package allocator

import (
	"fmt"

	apollov1 "github.com/magicsong/apollo-network-controller/api/v1"
)

// SelectLoadBalancer selects the best load balancer based on strategy for multiple ports
func (pa *PoolAllocator) SelectLoadBalancer(pool *apollov1.ApolloNetworkPool, portCount int) (*apollov1.LoadBalancerRef, error) {
	switch pa.strategy {
	case StrategyRoundRobin:
		return pa.selectRoundRobin(pool, portCount)
	case StrategyLeastUsed:
		return pa.selectLeastUsed(pool, portCount)
	default:
		return pa.selectRoundRobin(pool, portCount)
	}
}

// selectRoundRobin implements round-robin selection across load balancers
func (pa *PoolAllocator) selectRoundRobin(pool *apollov1.ApolloNetworkPool, portCount int) (*apollov1.LoadBalancerRef, error) {
	if len(pool.Spec.LoadBalancers) == 0 {
		return nil, fmt.Errorf("no load balancers configured")
	}

	poolKey := fmt.Sprintf("%s/%s", pool.Namespace, pool.Name)

	pa.indexMutex.Lock()
	defer pa.indexMutex.Unlock()

	currentIndex := pa.roundRobinIndex[poolKey]

	// Try each LB starting from current index
	for i := 0; i < len(pool.Spec.LoadBalancers); i++ {
		lbIndex := (currentIndex + i) % len(pool.Spec.LoadBalancers)
		lb := &pool.Spec.LoadBalancers[lbIndex]

		if pa.hasAvailablePorts(pool, lb, portCount) {
			// Update index for next allocation
			pa.roundRobinIndex[poolKey] = (lbIndex + 1) % len(pool.Spec.LoadBalancers)
			return lb, nil
		}
	}

	return nil, fmt.Errorf("no load balancer has %d available ports", portCount)
}

// selectLeastUsed selects the LB with least allocated ports
func (pa *PoolAllocator) selectLeastUsed(pool *apollov1.ApolloNetworkPool, portCount int) (*apollov1.LoadBalancerRef, error) {
	if len(pool.Spec.LoadBalancers) == 0 {
		return nil, fmt.Errorf("no load balancers configured")
	}

	var bestLB *apollov1.LoadBalancerRef
	minUsage := int32(^uint32(0) >> 1) // max int32

	for i := range pool.Spec.LoadBalancers {
		lb := &pool.Spec.LoadBalancers[i]
		usage := pa.getLBPortUsage(pool, lb)

		if usage < minUsage && pa.hasAvailablePorts(pool, lb, portCount) {
			minUsage = usage
			bestLB = lb
		}
	}

	if bestLB == nil {
		return nil, fmt.Errorf("no load balancer has %d available ports", portCount)
	}

	return bestLB, nil
}
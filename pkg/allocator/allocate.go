package allocator

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/types"

	apollov1 "github.com/magicsong/apollo-network-controller/api/v1"
)

// LoadBalancerSelector handles LB selection logic
type LoadBalancerSelector struct {
    pool     *apollov1.ApolloNetworkPool
    strategy LoadBalancerStrategy
}

// SelectLoadBalancer selects the best load balancer based on strategy
func (pa *PoolAllocator) SelectLoadBalancer(pool *apollov1.ApolloNetworkPool) (*apollov1.LoadBalancerRef, error) {
    selector := &LoadBalancerSelector{
        pool:     pool,
        strategy: pa.strategy,
    }
    
    switch pa.strategy {
    case StrategyRoundRobin:
        return selector.selectRoundRobin(pa)
    case StrategyLeastUsed:
        return selector.selectLeastUsed()
    default:
        return selector.selectRoundRobin(pa)
    }
}

// selectRoundRobin implements round-robin selection across load balancers
func (s *LoadBalancerSelector) selectRoundRobin(pa *PoolAllocator) (*apollov1.LoadBalancerRef, error) {
    if len(s.pool.Spec.LoadBalancers) == 0 {
        return nil, fmt.Errorf("no load balancers configured")
    }
    
    poolKey := fmt.Sprintf("%s/%s", s.pool.Namespace, s.pool.Name)
    
    pa.indexMutex.Lock()
    defer pa.indexMutex.Unlock()
    
    currentIndex := pa.roundRobinIndex[poolKey]
    
    // Try each LB starting from current index
    for i := 0; i < len(s.pool.Spec.LoadBalancers); i++ {
        lbIndex := (currentIndex + i) % len(s.pool.Spec.LoadBalancers)
        lb := &s.pool.Spec.LoadBalancers[lbIndex]
        
        if s.hasAvailablePort(lb) {
            // Update index for next allocation
            pa.roundRobinIndex[poolKey] = (lbIndex + 1) % len(s.pool.Spec.LoadBalancers)
            return lb, nil
        }
    }
    
    return nil, fmt.Errorf("no load balancer has available ports")
}

// selectLeastUsed selects the LB with least allocated ports
func (s *LoadBalancerSelector) selectLeastUsed() (*apollov1.LoadBalancerRef, error) {
    if len(s.pool.Spec.LoadBalancers) == 0 {
        return nil, fmt.Errorf("no load balancers configured")
    }
    
    var bestLB *apollov1.LoadBalancerRef
    minUsage := int32(^uint32(0) >> 1) // max int32
    
    for i := range s.pool.Spec.LoadBalancers {
        lb := &s.pool.Spec.LoadBalancers[i]
        usage := s.getLBPortUsage(lb)
        
        if usage < minUsage && s.hasAvailablePort(lb) {
            minUsage = usage
            bestLB = lb
        }
    }
    
    if bestLB == nil {
        return nil, fmt.Errorf("no load balancer has available ports")
    }
    
    return bestLB, nil
}

func (pa *PoolAllocator) tryAllocatePort(ctx context.Context, poolName, namespace string,
    podName, podNamespace string,protocol apollov1.Protocol) (*apollov1.PortAllocation, error) {
    
    // 1. Get current pool state
    pool := &apollov1.ApolloNetworkPool{}
    poolKey := types.NamespacedName{Name: poolName}
    
    if err := pa.client.Get(ctx, poolKey, pool); err != nil {
        return nil, fmt.Errorf("failed to get pool %s: %w", poolName, err)
    }
    
    // 2. Select load balancer
    selectedLB, err := pa.SelectLoadBalancer(pool)
    if err != nil {
        return nil, fmt.Errorf("failed to select load balancer: %w", err)
    }
    
    // 3. Find available port for selected LB
    port, err := pa.findAvailablePorts(pool, selectedLB,1)
    if err != nil {
        return nil, fmt.Errorf("failed to find available port on LB %s: %w", selectedLB.ID, err)
    }
    
    // 4. Create allocation
    allocation := &apollov1.PortAllocation{
        Port:           port,
        Protocol:       protocol,
        LoadBalancerID: selectedLB.ID,
        PodName:        podName,
        PodNamespace:   podNamespace,
        AllocatedAt:    &metav1.Time{Time: time.Now()},
    }
    
    // 5. Update pool atomically
    return allocation, pa.updatePoolWithAllocation(ctx, pool, allocation)
}

// getLBPortUsage returns the number of ports used by this LB
func (s *LoadBalancerSelector) getLBPortUsage(lb *apollov1.LoadBalancerRef) int32 {
    // Check if we have allocation status for this LB
    if s.pool.Status.Allocations == nil {
        return 0
    }
    
    // Get allocation status for this specific LB
    lbStatus, exists := s.pool.Status.Allocations[lb.ID]
    if !exists {
        return 0
    }
    
    // Return the allocated count for this LB
    return lbStatus.AllocatedCount
}

// hasAvailablePort checks if the LB has available ports (fixed implementation)
func (s *LoadBalancerSelector) hasAvailablePort(lb *apollov1.LoadBalancerRef) bool {
    usage := s.getLBPortUsage(lb)
    totalPorts := s.pool.Spec.PortRange.Max - s.pool.Spec.PortRange.Min + 1 - int32(len(s.pool.Spec.PortRange.ExcludedPorts))
    return usage < totalPorts
}

// getTotalAllocatedPorts returns total allocated ports across all LBs
func (s *LoadBalancerSelector) getTotalAllocatedPorts() int32 {
    if s.pool.Status.Allocations == nil {
        return 0
    }
    
    total := int32(0)
    for _, lbStatus := range s.pool.Status.Allocations {
        total += lbStatus.AllocatedCount
    }
    return total
}

// getLBAllocationStatus returns the allocation status for a specific LB
func (s *LoadBalancerSelector) getLBAllocationStatus(lb *apollov1.LoadBalancerRef) *apollov1.LoadBalancerAllocateStatus {
    if s.pool.Status.Allocations == nil {
        return nil
    }
    
    if lbStatus, exists := s.pool.Status.Allocations[lb.ID]; exists {
        return &lbStatus
    }
    return nil
}

// findAvailablePorts finds multiple available ports on the specified LB
// findAvailablePorts finds multiple available ports on the specified LB (updated for new structure)
func (pa *PoolAllocator) findAvailablePorts(pool *apollov1.ApolloNetworkPool, 
    lb *apollov1.LoadBalancerRef, count int32) ([]int32, error) {
    
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
    for port := pool.Spec.PortRange.Min; port <= pool.Spec.PortRange.Max && int32(len(availablePorts)) < count; port++ {
        if !allocatedPorts[port] {
            availablePorts = append(availablePorts, port)
        }
    }
    
    if int32(len(availablePorts)) < count {
        return nil, fmt.Errorf("only found %d available ports, need %d for LB %s in range %d-%d", 
            len(availablePorts), count, lb.ID, pool.Spec.PortRange.Min, pool.Spec.PortRange.Max)
    }
    
    return availablePorts, nil
}
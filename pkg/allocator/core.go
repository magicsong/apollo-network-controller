package allocator

import (
	"context"
	"fmt"
	"sync"

	apollov1 "github.com/magicsong/apollo-network-controller/api/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// LoadBalancerStrategy defines the strategy for selecting load balancers
type LoadBalancerStrategy string

const (
	StrategyRoundRobin LoadBalancerStrategy = "round-robin"
	StrategyLeastUsed  LoadBalancerStrategy = "least-used"
	StrategyWeighted   LoadBalancerStrategy = "weighted"
)

// PoolAllocator manages port allocation across multiple load balancers
type PoolAllocator struct {
	client          client.Client
	strategy        LoadBalancerStrategy
	roundRobinIndex map[string]int // poolName -> current LB index
	indexMutex      sync.RWMutex

	// Per-pool allocation locks to prevent conflicts
	poolMutexes sync.Map // key: string (poolName), value: *sync.Mutex
}

// NewPoolAllocator creates a new pool allocator
func NewPoolAllocator(client client.Client, strategy LoadBalancerStrategy) *PoolAllocator {
	return &PoolAllocator{
		client:          client,
		strategy:        strategy,
		roundRobinIndex: make(map[string]int),
	}
}

type poolAndAllocations struct {
	pool        *apollov1.ApolloNetworkPool
	allocations *apollov1.PortAllocationList
}

// getPoolAndAllocations retrieves both the pool and its allocations in a single operation
func (pa *PoolAllocator) getPoolAndAllocations(ctx context.Context, poolName, namespace string) (*poolAndAllocations, error) {
	// Get current pool state
	pool := &apollov1.ApolloNetworkPool{}
	poolKey := types.NamespacedName{Name: poolName, Namespace: namespace}

	if err := pa.client.Get(ctx, poolKey, pool); err != nil {
		return nil, fmt.Errorf("failed to get pool %s: %w", poolName, err)
	}

	// Get current allocations for this pool
	allocations := &apollov1.PortAllocationList{}
	err := pa.client.List(ctx, allocations, client.MatchingLabels{LabelPoolNameKey: pool.Name})
	if err != nil {
		return nil, fmt.Errorf("failed to list port allocations for pool %s: %w", pool.Name, err)
	}

	return &poolAndAllocations{
		pool:        pool,
		allocations: allocations,
	}, nil
}

// AllocatePort allocates a port for the given pod
func (pa *PoolAllocator) AllocatePort(ctx context.Context, poolName, namespace string,
	podName, podNamespace string, containerPorts []apollov1.PodPortAllocation) (*apollov1.PortAllocation, error) {

	// Get per-pool mutex to prevent concurrent allocations on same pool
	poolMutex := pa.getPoolMutex(poolName, namespace)
	poolMutex.Lock()
	defer poolMutex.Unlock()
	
	var allocation *apollov1.PortAllocation
	var err error

	// Retry with exponential backoff for optimistic locking
	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		allocation, err = pa.tryAllocatePort(ctx, poolName, namespace, podName, podNamespace, containerPorts)
		if err != nil {
			return err
		}
		
		// Create the allocation in apiserver
		return pa.createAllocation(ctx, allocation)
	})

	return allocation, err
}

// getPoolMutex gets or creates a mutex for the specific pool
func (pa *PoolAllocator) getPoolMutex(poolName, namespace string) *sync.Mutex {
	key := fmt.Sprintf("%s/%s", namespace, poolName)

	// LoadOrStore is atomic and thread-safe
	value, _ := pa.poolMutexes.LoadOrStore(key, &sync.Mutex{})
	return value.(*sync.Mutex)
}


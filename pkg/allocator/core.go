package allocator

import (
	"context"
	"fmt"
	"sync"

	apollov1 "github.com/magicsong/apollo-network-controller/api/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
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
	log := logf.FromContext(ctx)
	log.V(1).Info("Getting pool and allocations", "pool", poolName, "namespace", namespace)

	// Get current pool state
	pool := &apollov1.ApolloNetworkPool{}
	poolKey := types.NamespacedName{Name: poolName, Namespace: namespace}

	if err := pa.client.Get(ctx, poolKey, pool); err != nil {
		log.Error(err, "Failed to get pool", "pool", poolName, "namespace", namespace)
		return nil, fmt.Errorf("failed to get pool %s: %w", poolName, err)
	}

	// Get current allocations for this pool
	allocations := &apollov1.PortAllocationList{}
	err := pa.client.List(ctx, allocations, client.MatchingLabels{LabelPoolNameKey: pool.Name})
	if err != nil {
		log.Error(err, "Failed to list port allocations", "pool", pool.Name)
		return nil, fmt.Errorf("failed to list port allocations for pool %s: %w", pool.Name, err)
	}

	log.V(4).Info("Successfully retrieved pool and allocations", 
		"pool", poolName, 
		"loadBalancers", len(pool.Spec.LoadBalancers),
		"allocations", len(allocations.Items))

	return &poolAndAllocations{
		pool:        pool,
		allocations: allocations,
	}, nil
}

// AllocatePort allocates a port for the given pod
func (pa *PoolAllocator) AllocatePort(ctx context.Context, poolName, namespace string,
	podName, podNamespace string, containerPorts []apollov1.PodPortAllocation) (*apollov1.PortAllocation, error) {

	log := logf.FromContext(ctx)
	log.Info("Starting port allocation", 
		"pool", poolName, 
		"namespace", namespace,
		"pod", fmt.Sprintf("%s/%s", podNamespace, podName),
		"ports", len(containerPorts))

	// Get per-pool mutex to prevent concurrent allocations on same pool
	poolMutex := pa.getPoolMutex(poolName, namespace)
	log.V(2).Info("Acquiring pool mutex", "pool", poolName)
	poolMutex.Lock()
	defer func() {
		poolMutex.Unlock()
		log.V(2).Info("Released pool mutex", "pool", poolName)
	}()
	
	var allocation *apollov1.PortAllocation
	var err error
	var retryCount int

	// Retry with exponential backoff for optimistic locking
	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		retryCount++
		if retryCount > 1 {
			log.V(1).Info("Retrying port allocation due to conflict", 
				"pool", poolName, 
				"pod", fmt.Sprintf("%s/%s", podNamespace, podName),
				"attempt", retryCount)
		}

		allocation, err = pa.tryAllocatePort(ctx, poolName, namespace, podName, podNamespace, containerPorts)
		if err != nil {
			log.V(1).Info("Failed to try allocate port", "error", err.Error())
			return err
		}
		
		// Create the allocation in apiserver
		return pa.createAllocation(ctx, allocation)
	})

	if err != nil {
		log.Error(err, "Failed to allocate port after retries", 
			"pool", poolName, 
			"pod", fmt.Sprintf("%s/%s", podNamespace, podName),
			"attempts", retryCount)
		return nil, err
	}

	log.Info("Successfully allocated ports", 
		"pool", poolName, 
		"pod", fmt.Sprintf("%s/%s", podNamespace, podName),
		"allocation", allocation.Name,
		"portBindings", len(allocation.Spec.PortBindings),
		"attempts", retryCount)

	return allocation, nil
}

// getPoolMutex gets or creates a mutex for the specific pool
func (pa *PoolAllocator) getPoolMutex(poolName, namespace string) *sync.Mutex {
	key := fmt.Sprintf("%s/%s", namespace, poolName)

	// LoadOrStore is atomic and thread-safe
	value, loaded := pa.poolMutexes.LoadOrStore(key, &sync.Mutex{})
	
	// Only log when we create a new mutex to avoid log spam
	if !loaded {
		// Note: We can't use context here since this is a utility method
		// The actual logging happens in AllocatePort where we have context
		logf.FromContext(context.Background()).V(2).Info("Created new pool mutex", "pool", poolName)
	}
	
	return value.(*sync.Mutex)
}


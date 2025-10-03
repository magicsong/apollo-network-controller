package allocator

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apollov1 "github.com/magicsong/apollo-network-controller/api/v1"
)

// createTestPool creates a test ApolloNetworkPool
func createTestPool(name, namespace string, lbCount int, minPort, maxPort int32) *apollov1.ApolloNetworkPool {
	loadBalancers := make([]apollov1.LoadBalancerRef, lbCount)
	for i := 0; i < lbCount; i++ {
		loadBalancers[i] = apollov1.LoadBalancerRef{
			ID:       fmt.Sprintf("lb-%d", i+1),
			Region:   "us-west-1",
			Provider: "test-provider",
		}
	}

	return &apollov1.ApolloNetworkPool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: apollov1.ApolloNetworkPoolSpec{
			LoadBalancers: loadBalancers,
			PortRange: apollov1.PortRange{
				Min: minPort,
				Max: maxPort,
			},
		},
	}
}

func TestAllocatePort_Success_SingleAllocation(t *testing.T) {
	// Setup
	scheme := runtime.NewScheme()
	_ = apollov1.AddToScheme(scheme)
	
	pool := createTestPool("test-pool", "default", 1, 30000, 30099)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool).Build()
	
	allocator := NewPoolAllocator(fakeClient, StrategyRoundRobin)

	containerPorts := []apollov1.PodPortAllocation{
		{PodPort: 8080, Protocol: apollov1.ProtocolTCP, PortName: "http"},
	}

	// Execute
	allocation, err := allocator.AllocatePort(context.TODO(), "test-pool", "default", "test-pod", "default", containerPorts)

	// Verify
	require.NoError(t, err)
	require.NotNil(t, allocation)
	
	assert.Equal(t, "test-pool-test-pod", allocation.Name)
	assert.Equal(t, "default", allocation.Namespace)
	assert.Len(t, allocation.Spec.PortBindings, 1)
	
	// Verify the allocation was created in apiserver
	createdAllocation := &apollov1.PortAllocation{}
	err = fakeClient.Get(context.TODO(), 
		client.ObjectKey{Name: allocation.Name, Namespace: allocation.Namespace}, 
		createdAllocation)
	require.NoError(t, err)
	assert.Equal(t, allocation.Name, createdAllocation.Name)
}

func TestAllocatePort_Update_ExistingAllocation(t *testing.T) {
	// Setup
	scheme := runtime.NewScheme()
	_ = apollov1.AddToScheme(scheme)
	
	pool := createTestPool("test-pool", "default", 1, 30000, 30099)
	
	// Create existing allocation
	existingAllocation := &apollov1.PortAllocation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pool-test-pod",
			Namespace: "default",
			Labels: map[string]string{
				LabelPoolNameKey: "test-pool",
				LabelPodNameKey:  "test-pod",
			},
		},
		Spec: apollov1.PortAllocationSpec{
			PodInfo: apollov1.PodInfo{
				Name:      "test-pod",
				Namespace: "default",
			},
			PortBindings: []apollov1.PortBinding{
				{
					PodPort:  8080,
					LBPort:   30000,
					Protocol: apollov1.ProtocolTCP,
					PortName: "http",
					BindingType: apollov1.GetDefaultBindingType(),
					LoadBalancerRef: apollov1.LoadBalancerRef{
						ID:       "lb-1",
						Region:   "us-west-1",
						Provider: "test-provider",
					},
				},
			},
		},
		Status: apollov1.PortAllocationStatus{
			Phase: apollov1.AllocationPhasePending,
		},
	}
	
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool, existingAllocation).Build()
	
	allocator := NewPoolAllocator(fakeClient, StrategyRoundRobin)

	containerPorts := []apollov1.PodPortAllocation{
		{PodPort: 8080, Protocol: apollov1.ProtocolTCP, PortName: "http"},
		{PodPort: 9090, Protocol: apollov1.ProtocolTCP, PortName: "metrics"},
	}

	// Execute
	allocation, err := allocator.AllocatePort(context.TODO(), "test-pool", "default", "test-pod", "default", containerPorts)

	// Verify
	require.NoError(t, err)
	require.NotNil(t, allocation)
	
	// Should have both ports now
	assert.Len(t, allocation.Spec.PortBindings, 2)
	
	// Verify the allocation was updated in apiserver
	updatedAllocation := &apollov1.PortAllocation{}
	err = fakeClient.Get(context.TODO(), 
		client.ObjectKey{Name: allocation.Name, Namespace: allocation.Namespace}, 
		updatedAllocation)
	require.NoError(t, err)
	assert.Len(t, updatedAllocation.Spec.PortBindings, 2)
}

func TestAllocatePort_Error_AllocationBeingDeleted(t *testing.T) {
	// Setup
	scheme := runtime.NewScheme()
	_ = apollov1.AddToScheme(scheme)
	
	pool := createTestPool("test-pool", "default", 1, 30000, 30099)
	
	// Create allocation being deleted
	now := metav1.Now()
	deletingAllocation := &apollov1.PortAllocation{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "test-pool-test-pod",
			Namespace:         "default",
			DeletionTimestamp: &now,
			Labels: map[string]string{
				LabelPoolNameKey: "test-pool",
				LabelPodNameKey:  "test-pod",
			},
		},
		Spec: apollov1.PortAllocationSpec{
			PodInfo: apollov1.PodInfo{
				Name:      "test-pod",
				Namespace: "default",
			},
		},
	}
	
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool, deletingAllocation).Build()
	
	allocator := NewPoolAllocator(fakeClient, StrategyRoundRobin)

	containerPorts := []apollov1.PodPortAllocation{
		{PodPort: 8080, Protocol: apollov1.ProtocolTCP, PortName: "http"},
	}

	// Execute
	allocation, err := allocator.AllocatePort(context.TODO(), "test-pool", "default", "test-pod", "default", containerPorts)

	// Verify
	assert.Error(t, err)
	assert.Nil(t, allocation)
	assert.Contains(t, err.Error(), "is being deleted")
}

func TestAllocatePort_Concurrent_NoPortConflicts(t *testing.T) {
	// Setup
	scheme := runtime.NewScheme()
	_ = apollov1.AddToScheme(scheme)
	
	// Create pool with limited ports (30000-30019, 20 ports total)
	pool := createTestPool("test-pool", "default", 1, 30000, 30019)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool).Build()
	
	allocator := NewPoolAllocator(fakeClient, StrategyRoundRobin)

	// Number of concurrent goroutines
	const numGoroutines = 10
	const portsPerPod = 2 // Each pod requests 2 ports
	
	var wg sync.WaitGroup
	var mu sync.Mutex
	results := make([]*apollov1.PortAllocation, 0, numGoroutines)
	errors := make([]error, 0, numGoroutines)

	// Launch concurrent allocations
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(podIndex int) {
			defer wg.Done()
			
			containerPorts := []apollov1.PodPortAllocation{
				{PodPort: 8080, Protocol: apollov1.ProtocolTCP, PortName: "http"},
				{PodPort: 9090, Protocol: apollov1.ProtocolTCP, PortName: "metrics"},
			}
			
			podName := fmt.Sprintf("test-pod-%d", podIndex)
			allocation, err := allocator.AllocatePort(context.TODO(), "test-pool", "default", podName, "default", containerPorts)
			
			mu.Lock()
			if err != nil {
				errors = append(errors, err)
			} else {
				results = append(results, allocation)
			}
			mu.Unlock()
		}(i)
	}

	// Wait for all goroutines to complete
	wg.Wait()

	// Verify results
	mu.Lock()
	defer mu.Unlock()
	
	t.Logf("Successful allocations: %d, Errors: %d", len(results), len(errors))
	
	// Should have either successful allocations or errors due to insufficient ports
	// With 20 ports available and 2 ports per pod, we can allocate at most 10 pods
	assert.True(t, len(results) <= 10, "Should not allocate more than 10 pods with 20 available ports")
	
	if len(results) > 0 {
		// Collect all allocated LB ports to check for conflicts
		allocatedPorts := make(map[int32]bool)
		conflictCount := 0
		
		for _, allocation := range results {
			for _, binding := range allocation.Spec.PortBindings {
				if allocatedPorts[binding.LBPort] {
					conflictCount++
					t.Errorf("Port conflict detected: port %d allocated multiple times", binding.LBPort)
				}
				allocatedPorts[binding.LBPort] = true
			}
		}
		
		assert.Equal(t, 0, conflictCount, "No port conflicts should occur")
		
		// Verify all allocated ports are within range
		for port := range allocatedPorts {
			assert.True(t, port >= 30000 && port <= 30019, 
				"Allocated port %d should be within range 30000-30019", port)
		}
	}
	
	// If there are errors, they should be due to insufficient ports
	for _, err := range errors {
		assert.Contains(t, err.Error(), "available ports", 
			"Errors should be due to insufficient available ports, got: %v", err)
	}
}

func TestAllocatePort_Concurrent_SamePod_Sequential(t *testing.T) {
	// Test that concurrent allocations for the same pod are serialized
	scheme := runtime.NewScheme()
	_ = apollov1.AddToScheme(scheme)
	
	pool := createTestPool("test-pool", "default", 1, 30000, 30099)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool).Build()
	
	allocator := NewPoolAllocator(fakeClient, StrategyRoundRobin)

	const numGoroutines = 5
	var wg sync.WaitGroup
	var mu sync.Mutex
	results := make([]*apollov1.PortAllocation, 0, numGoroutines)
	errors := make([]error, 0, numGoroutines)

	// All goroutines try to allocate for the same pod
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			
			containerPorts := []apollov1.PodPortAllocation{
				{PodPort: 8080, Protocol: apollov1.ProtocolTCP, PortName: "http"},
			}
			
			allocation, err := allocator.AllocatePort(context.TODO(), "test-pool", "default", "same-pod", "default", containerPorts)
			
			mu.Lock()
			if err != nil {
				errors = append(errors, err)
			} else {
				results = append(results, allocation)
			}
			mu.Unlock()
		}()
	}

	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	
	// All should succeed due to mutex protection, and they should all update the same allocation
	assert.Equal(t, numGoroutines, len(results), "All allocations should succeed")
	assert.Empty(t, errors, "No errors should occur")
	
	// All results should have the same name (same pod)
	if len(results) > 0 {
		expectedName := results[0].Name
		for _, result := range results {
			assert.Equal(t, expectedName, result.Name, "All allocations should be for the same pod")
		}
	}
}

func TestCreateOrUpdateAllocation_Create(t *testing.T) {
	// Setup
	scheme := runtime.NewScheme()
	_ = apollov1.AddToScheme(scheme)
	
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	allocator := NewPoolAllocator(fakeClient, StrategyRoundRobin)

	allocation := &apollov1.PortAllocation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-allocation",
			Namespace: "default",
		},
		Spec: apollov1.PortAllocationSpec{
			PodInfo: apollov1.PodInfo{
				Name:      "test-pod",
				Namespace: "default",
			},
		},
	}

	// Execute
	err := allocator.createOrUpdateAllocation(context.TODO(), allocation)

	// Verify
	assert.NoError(t, err)
	
	// Check that allocation was created
	created := &apollov1.PortAllocation{}
	err = fakeClient.Get(context.TODO(), 
		client.ObjectKey{Name: "test-allocation", Namespace: "default"}, 
		created)
	assert.NoError(t, err)
	assert.Equal(t, "test-pod", created.Spec.PodInfo.Name)
}

func TestCreateOrUpdateAllocation_Update(t *testing.T) {
	// Setup
	scheme := runtime.NewScheme()
	_ = apollov1.AddToScheme(scheme)
	
	existing := &apollov1.PortAllocation{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "test-allocation",
			Namespace:       "default",
			ResourceVersion: "1",
		},
		Spec: apollov1.PortAllocationSpec{
			PodInfo: apollov1.PodInfo{
				Name:      "old-pod",
				Namespace: "default",
			},
		},
	}
	
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing).Build()
	allocator := NewPoolAllocator(fakeClient, StrategyRoundRobin)

	updated := &apollov1.PortAllocation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-allocation",
			Namespace: "default",
		},
		Spec: apollov1.PortAllocationSpec{
			PodInfo: apollov1.PodInfo{
				Name:      "new-pod",
				Namespace: "default",
			},
		},
	}

	// Execute
	err := allocator.createOrUpdateAllocation(context.TODO(), updated)

	// Verify
	assert.NoError(t, err)
	
	// Check that allocation was updated
	result := &apollov1.PortAllocation{}
	err = fakeClient.Get(context.TODO(), 
		client.ObjectKey{Name: "test-allocation", Namespace: "default"}, 
		result)
	assert.NoError(t, err)
	assert.Equal(t, "new-pod", result.Spec.PodInfo.Name)
	assert.Equal(t, "1", result.ResourceVersion) // Should preserve resource version
}

func TestCreateOrUpdateAllocation_Error_BeingDeleted(t *testing.T) {
	// Setup
	scheme := runtime.NewScheme()
	_ = apollov1.AddToScheme(scheme)
	
	now := metav1.Now()
	existing := &apollov1.PortAllocation{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "test-allocation",
			Namespace:         "default",
			DeletionTimestamp: &now,
		},
		Spec: apollov1.PortAllocationSpec{
			PodInfo: apollov1.PodInfo{
				Name:      "test-pod",
				Namespace: "default",
			},
		},
	}
	
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing).Build()
	allocator := NewPoolAllocator(fakeClient, StrategyRoundRobin)

	updated := &apollov1.PortAllocation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-allocation",
			Namespace: "default",
		},
		Spec: apollov1.PortAllocationSpec{
			PodInfo: apollov1.PodInfo{
				Name:      "updated-pod",
				Namespace: "default",
			},
		},
	}

	// Execute
	err := allocator.createOrUpdateAllocation(context.TODO(), updated)

	// Verify
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "is being deleted")
}

func TestCreateOrUpdateAllocation_Error_NilAllocation(t *testing.T) {
	// Setup
	scheme := runtime.NewScheme()
	_ = apollov1.AddToScheme(scheme)
	
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	allocator := NewPoolAllocator(fakeClient, StrategyRoundRobin)

	// Execute
	err := allocator.createOrUpdateAllocation(context.TODO(), nil)

	// Verify
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "allocation cannot be nil")
}
package allocator

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apollov1 "github.com/magicsong/apollo-network-controller/api/v1"
)

// =============================================================================
// ReleasePort Tests
// =============================================================================

func TestReleasePort_Success(t *testing.T) {
	// Setup
	scheme := runtime.NewScheme()
	_ = apollov1.AddToScheme(scheme)
	
	pool := createTestPool("test-pool", "default", 1, 30000, 30099)
	
	// Create existing allocation
	allocation := &apollov1.PortAllocation{
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
			Phase:       apollov1.AllocationPhasePending,
			AllocatedAt: &metav1.Time{Time: time.Now()},
		},
	}
	
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool, allocation).Build()
	allocator := NewPoolAllocator(fakeClient, StrategyRoundRobin)

	// Execute
	err := allocator.ReleasePort(context.TODO(), "test-pool", "default", "test-pod", "default")

	// Verify
	require.NoError(t, err)
	
	// Verify allocation was deleted
	deletedAllocation := &apollov1.PortAllocation{}
	err = fakeClient.Get(context.TODO(), 
		client.ObjectKey{Name: "test-pool-test-pod", Namespace: "default"}, 
		deletedAllocation)
	assert.Error(t, err)
	assert.True(t, client.IgnoreNotFound(err) == nil) // Should be NotFound error
}

func TestReleasePort_Error_AllocationNotFound(t *testing.T) {
	// Setup
	scheme := runtime.NewScheme()
	_ = apollov1.AddToScheme(scheme)
	
	pool := createTestPool("test-pool", "default", 1, 30000, 30099)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool).Build()
	allocator := NewPoolAllocator(fakeClient, StrategyRoundRobin)

	// Execute - try to release non-existent allocation
	err := allocator.ReleasePort(context.TODO(), "test-pool", "default", "nonexistent-pod", "default")

	// Verify
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "port allocation not found for pod default/nonexistent-pod in pool test-pool")
}

func TestReleasePort_Error_WrongPodInfo(t *testing.T) {
	// Setup
	scheme := runtime.NewScheme()
	_ = apollov1.AddToScheme(scheme)
	
	pool := createTestPool("test-pool", "default", 1, 30000, 30099)
	
	// Create allocation for different pod
	allocation := &apollov1.PortAllocation{
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
				Name:      "different-pod", // Different pod name
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
	}
	
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool, allocation).Build()
	allocator := NewPoolAllocator(fakeClient, StrategyRoundRobin)

	// Execute - try to release allocation with wrong pod info
	err := allocator.ReleasePort(context.TODO(), "test-pool", "default", "test-pod", "default")

	// Verify
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "allocation test-pool-test-pod does not belong to pod default/test-pod")
}

func TestReleasePort_Error_WrongPool(t *testing.T) {
	// Setup
	scheme := runtime.NewScheme()
	_ = apollov1.AddToScheme(scheme)
	
	pool := createTestPool("test-pool", "default", 1, 30000, 30099)
	
	// Create allocation for different pool
	allocation := &apollov1.PortAllocation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pool-test-pod",
			Namespace: "default",
			Labels: map[string]string{
				LabelPoolNameKey: "different-pool", // Different pool
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
	
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool, allocation).Build()
	allocator := NewPoolAllocator(fakeClient, StrategyRoundRobin)

	// Execute - try to release allocation from wrong pool
	err := allocator.ReleasePort(context.TODO(), "test-pool", "default", "test-pod", "default")

	// Verify
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "allocation test-pool-test-pod does not belong to pool test-pool")
}

func TestReleasePort_Error_AllocationBeingDeleted(t *testing.T) {
	// Setup
	scheme := runtime.NewScheme()
	_ = apollov1.AddToScheme(scheme)
	
	pool := createTestPool("test-pool", "default", 1, 30000, 30099)
	
	// Create allocation that is being deleted
	now := metav1.Now()
	allocation := &apollov1.PortAllocation{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "test-pool-test-pod",
			Namespace:         "default",
			DeletionTimestamp: &now,
			Finalizers:        []string{"apollo.network/port-cleanup"},
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
	
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool, allocation).Build()
	allocator := NewPoolAllocator(fakeClient, StrategyRoundRobin)

	// Execute - try to release allocation that is already being deleted
	err := allocator.ReleasePort(context.TODO(), "test-pool", "default", "test-pod", "default")

	// Verify
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "allocation test-pool-test-pod is already being deleted")
}

func TestReleasePort_Success_DifferentNamespace(t *testing.T) {
	// Setup
	scheme := runtime.NewScheme()
	_ = apollov1.AddToScheme(scheme)
	
	pool := createTestPool("test-pool", "production", 1, 30000, 30099)
	
	// Create allocation in production namespace
	allocation := &apollov1.PortAllocation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pool-test-pod",
			Namespace: "production",
			Labels: map[string]string{
				LabelPoolNameKey: "test-pool",
				LabelPodNameKey:  "test-pod",
			},
		},
		Spec: apollov1.PortAllocationSpec{
			PodInfo: apollov1.PodInfo{
				Name:      "test-pod",
				Namespace: "production",
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
	}
	
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool, allocation).Build()
	allocator := NewPoolAllocator(fakeClient, StrategyRoundRobin)

	// Execute
	err := allocator.ReleasePort(context.TODO(), "test-pool", "production", "test-pod", "production")

	// Verify
	require.NoError(t, err)
	
	// Verify allocation was deleted
	deletedAllocation := &apollov1.PortAllocation{}
	err = fakeClient.Get(context.TODO(), 
		client.ObjectKey{Name: "test-pool-test-pod", Namespace: "production"}, 
		deletedAllocation)
	assert.Error(t, err)
	assert.True(t, client.IgnoreNotFound(err) == nil)
}

func TestReleasePort_Success_MultiplePortBindings(t *testing.T) {
	// Setup
	scheme := runtime.NewScheme()
	_ = apollov1.AddToScheme(scheme)
	
	pool := createTestPool("test-pool", "default", 1, 30000, 30099)
	
	// Create allocation with multiple port bindings
	allocation := &apollov1.PortAllocation{
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
				{
					PodPort:  9090,
					LBPort:   30001,
					Protocol: apollov1.ProtocolTCP,
					PortName: "metrics",
					BindingType: apollov1.GetDefaultBindingType(),
					LoadBalancerRef: apollov1.LoadBalancerRef{
						ID:       "lb-1",
						Region:   "us-west-1",
						Provider: "test-provider",
					},
				},
			},
		},
	}
	
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool, allocation).Build()
	allocator := NewPoolAllocator(fakeClient, StrategyRoundRobin)

	// Execute
	err := allocator.ReleasePort(context.TODO(), "test-pool", "default", "test-pod", "default")

	// Verify
	require.NoError(t, err)
	
	// Verify allocation was deleted
	deletedAllocation := &apollov1.PortAllocation{}
	err = fakeClient.Get(context.TODO(), 
		client.ObjectKey{Name: "test-pool-test-pod", Namespace: "default"}, 
		deletedAllocation)
	assert.Error(t, err)
	assert.True(t, client.IgnoreNotFound(err) == nil)
}

func TestReleasePort_Concurrent_SameAllocation(t *testing.T) {
	// Test concurrent release of the same allocation
	scheme := runtime.NewScheme()
	_ = apollov1.AddToScheme(scheme)
	
	pool := createTestPool("test-pool", "default", 1, 30000, 30099)
	
	// Create allocation
	allocation := &apollov1.PortAllocation{
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
	}
	
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool, allocation).Build()
	allocator := NewPoolAllocator(fakeClient, StrategyRoundRobin)

	const numGoroutines = 5
	var wg sync.WaitGroup
	var mu sync.Mutex
	successCount := 0
	errorCount := 0
	errors := make([]error, 0)

	// Launch concurrent releases
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			
			err := allocator.ReleasePort(context.TODO(), "test-pool", "default", "test-pod", "default")
			
			mu.Lock()
			if err != nil {
				errorCount++
				errors = append(errors, err)
			} else {
				successCount++
			}
			mu.Unlock()
		}()
	}

	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	
	// Only one release should succeed, others should fail because allocation no longer exists
	assert.Equal(t, 1, successCount, "Only one release should succeed")
	assert.Equal(t, numGoroutines-1, errorCount, "Other releases should fail")
	
	// All errors should be about allocation not found
	for _, err := range errors {
		assert.Contains(t, err.Error(), "port allocation not found")
	}
	
	// Verify allocation was deleted
	deletedAllocation := &apollov1.PortAllocation{}
	err := fakeClient.Get(context.TODO(), 
		client.ObjectKey{Name: "test-pool-test-pod", Namespace: "default"}, 
		deletedAllocation)
	assert.Error(t, err)
	assert.True(t, client.IgnoreNotFound(err) == nil)
}

func TestReleasePort_Concurrent_DifferentAllocations(t *testing.T) {
	// Test concurrent release of different allocations
	scheme := runtime.NewScheme()
	_ = apollov1.AddToScheme(scheme)
	
	pool := createTestPool("test-pool", "default", 1, 30000, 30099)
	
	// Create multiple allocations
	const numAllocations = 5
	allocations := make([]*apollov1.PortAllocation, numAllocations)
	allObjects := []client.Object{pool}
	
	for i := 0; i < numAllocations; i++ {
		podName := fmt.Sprintf("test-pod-%d", i)
		allocName := fmt.Sprintf("test-pool-%s", podName)
		
		allocation := &apollov1.PortAllocation{
			ObjectMeta: metav1.ObjectMeta{
				Name:      allocName,
				Namespace: "default",
				Labels: map[string]string{
					LabelPoolNameKey: "test-pool",
					LabelPodNameKey:  podName,
				},
			},
			Spec: apollov1.PortAllocationSpec{
				PodInfo: apollov1.PodInfo{
					Name:      podName,
					Namespace: "default",
				},
				PortBindings: []apollov1.PortBinding{
					{
						PodPort:  8080,
						LBPort:   int32(30000 + i),
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
		}
		
		allocations[i] = allocation
		allObjects = append(allObjects, allocation)
	}
	
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(allObjects...).Build()
	allocator := NewPoolAllocator(fakeClient, StrategyRoundRobin)

	var wg sync.WaitGroup
	var mu sync.Mutex
	successCount := 0
	errorCount := 0
	errors := make([]error, 0)

	// Launch concurrent releases for different allocations
	for i := 0; i < numAllocations; i++ {
		wg.Add(1)
		go func(podIndex int) {
			defer wg.Done()
			
			podName := fmt.Sprintf("test-pod-%d", podIndex)
			err := allocator.ReleasePort(context.TODO(), "test-pool", "default", podName, "default")
			
			mu.Lock()
			if err != nil {
				errorCount++
				errors = append(errors, err)
			} else {
				successCount++
			}
			mu.Unlock()
		}(i)
	}

	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	
	// All releases should succeed since they are for different allocations
	assert.Equal(t, numAllocations, successCount, "All releases should succeed")
	assert.Equal(t, 0, errorCount, "No releases should fail")
	assert.Empty(t, errors, "No errors should occur")
	
	// Verify all allocations were deleted
	for i := 0; i < numAllocations; i++ {
		podName := fmt.Sprintf("test-pod-%d", i)
		allocName := fmt.Sprintf("test-pool-%s", podName)
		
		deletedAllocation := &apollov1.PortAllocation{}
		err := fakeClient.Get(context.TODO(), 
			client.ObjectKey{Name: allocName, Namespace: "default"}, 
			deletedAllocation)
		assert.Error(t, err)
		assert.True(t, client.IgnoreNotFound(err) == nil)
	}
}

// =============================================================================
// Integration Tests: Allocate + Release
// =============================================================================

func TestAllocateAndRelease_Integration(t *testing.T) {
	// Test the complete cycle: allocate -> release
	scheme := runtime.NewScheme()
	_ = apollov1.AddToScheme(scheme)
	
	pool := createTestPool("test-pool", "default", 1, 30000, 30099)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool).Build()
	allocator := NewPoolAllocator(fakeClient, StrategyRoundRobin)

	containerPorts := []apollov1.PodPortAllocation{
		{PodPort: 8080, Protocol: apollov1.ProtocolTCP, PortName: "http"},
		{PodPort: 9090, Protocol: apollov1.ProtocolTCP, PortName: "metrics"},
	}

	// Step 1: Allocate ports
	allocation, err := allocator.AllocatePort(context.TODO(), "test-pool", "default", "test-pod", "default", containerPorts)
	require.NoError(t, err)
	require.NotNil(t, allocation)
	assert.Equal(t, "test-pool-test-pod", allocation.Name)
	assert.Len(t, allocation.Spec.PortBindings, 2)
	
	// Verify allocation exists in apiserver
	createdAllocation := &apollov1.PortAllocation{}
	err = fakeClient.Get(context.TODO(), 
		client.ObjectKey{Name: allocation.Name, Namespace: allocation.Namespace}, 
		createdAllocation)
	require.NoError(t, err)
	
	// Step 2: Release ports
	err = allocator.ReleasePort(context.TODO(), "test-pool", "default", "test-pod", "default")
	require.NoError(t, err)
	
	// Step 3: Verify allocation was deleted
	deletedAllocation := &apollov1.PortAllocation{}
	err = fakeClient.Get(context.TODO(), 
		client.ObjectKey{Name: allocation.Name, Namespace: allocation.Namespace}, 
		deletedAllocation)
	assert.Error(t, err)
	assert.True(t, client.IgnoreNotFound(err) == nil)
	
	// Step 4: Verify we can allocate again for the same pod (since old allocation is gone)
	newAllocation, err := allocator.AllocatePort(context.TODO(), "test-pool", "default", "test-pod", "default", containerPorts)
	require.NoError(t, err)
	require.NotNil(t, newAllocation)
	assert.Equal(t, "test-pool-test-pod", newAllocation.Name)
	assert.Len(t, newAllocation.Spec.PortBindings, 2)
}

func TestAllocateAndRelease_Integration_MultiplePods(t *testing.T) {
	// Test allocation and release for multiple pods
	scheme := runtime.NewScheme()
	_ = apollov1.AddToScheme(scheme)
	
	pool := createTestPool("test-pool", "default", 2, 30000, 30099)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool).Build()
	allocator := NewPoolAllocator(fakeClient, StrategyRoundRobin)

	containerPorts := []apollov1.PodPortAllocation{
		{PodPort: 8080, Protocol: apollov1.ProtocolTCP, PortName: "http"},
	}

	const numPods = 5
	podNames := make([]string, numPods)
	allocations := make([]*apollov1.PortAllocation, numPods)
	
	// Step 1: Allocate for multiple pods
	for i := 0; i < numPods; i++ {
		podName := fmt.Sprintf("test-pod-%d", i)
		podNames[i] = podName
		
		allocation, err := allocator.AllocatePort(context.TODO(), "test-pool", "default", podName, "default", containerPorts)
		require.NoError(t, err)
		require.NotNil(t, allocation)
		allocations[i] = allocation
	}
	
	// Step 2: Verify all allocations exist
	for i, allocation := range allocations {
		createdAllocation := &apollov1.PortAllocation{}
		err := fakeClient.Get(context.TODO(), 
			client.ObjectKey{Name: allocation.Name, Namespace: allocation.Namespace}, 
			createdAllocation)
		require.NoError(t, err, "Allocation %d should exist", i)
	}
	
	// Step 3: Release all allocations
	for i, podName := range podNames {
		err := allocator.ReleasePort(context.TODO(), "test-pool", "default", podName, "default")
		require.NoError(t, err, "Release %d should succeed", i)
	}
	
	// Step 4: Verify all allocations were deleted
	for i, allocation := range allocations {
		deletedAllocation := &apollov1.PortAllocation{}
		err := fakeClient.Get(context.TODO(), 
			client.ObjectKey{Name: allocation.Name, Namespace: allocation.Namespace}, 
			deletedAllocation)
		assert.Error(t, err, "Allocation %d should be deleted", i)
		assert.True(t, client.IgnoreNotFound(err) == nil)
	}
}
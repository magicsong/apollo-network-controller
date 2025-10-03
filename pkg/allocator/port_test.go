package allocator

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apollov1 "github.com/magicsong/apollo-network-controller/api/v1"
)

// createTestPoolForPortAllocation creates a test pool for port allocation testing
func createTestPoolForPortAllocation(name, namespace string, lbCount int) *apollov1.ApolloNetworkPool {
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
				Min:           30000,
				Max:           30099, // 100 ports total
				ExcludedPorts: []int32{30050, 30051}, // exclude 2 ports
			},
		},
	}
}

// createTestPortAllocation creates a test port allocation
func createTestPortAllocation(name, namespace, poolName, podName, podNamespace string, lbID string, lbPorts []int32, podPorts []int32) *apollov1.PortAllocation {
	portBindings := make([]apollov1.PortBinding, len(podPorts))
	for i, podPort := range podPorts {
		lbPort := int32(30000)
		if i < len(lbPorts) {
			lbPort = lbPorts[i]
		}
		portBindings[i] = apollov1.PortBinding{
			PodPort:  podPort,
			LBPort:   lbPort,
			Protocol: apollov1.ProtocolTCP,
			PortName: fmt.Sprintf("port-%d", podPort),
			BindingType: apollov1.GetDefaultBindingType(),
			LoadBalancerRef: apollov1.LoadBalancerRef{
				ID:       lbID,
				Region:   "us-west-1",
				Provider: "test-provider",
			},
		}
	}

	return &apollov1.PortAllocation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				LabelPoolNameKey: poolName,
				LabelPodNameKey:  podName,
			},
			Annotations: map[string]string{
				AnnotationBindingTypeKey: string(apollov1.GetDefaultBindingType()),
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
}

func TestTryAllocatePort_Success_SinglePort(t *testing.T) {
	// Setup
	scheme := runtime.NewScheme()
	_ = apollov1.AddToScheme(scheme)
	
	pool := createTestPoolForPortAllocation("test-pool", "default", 2)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool).Build()
	
	allocator := NewPoolAllocator(fakeClient, StrategyRoundRobin)

	containerPorts := []apollov1.PodPortAllocation{
		{PodPort: 8080, Protocol: apollov1.ProtocolTCP, PortName: "http"},
	}

	// Execute
	allocation, err := allocator.tryAllocatePort(context.TODO(), "test-pool", "default", "test-pod", "default", containerPorts)

	// Verify
	require.NoError(t, err)
	require.NotNil(t, allocation)

	assert.Equal(t, "test-pool-test-pod", allocation.Name)
	assert.Equal(t, "default", allocation.Namespace)
	assert.Equal(t, "test-pool", allocation.Labels[LabelPoolNameKey])
	assert.Equal(t, "test-pod", allocation.Labels[LabelPodNameKey])
	assert.Equal(t, string(apollov1.GetDefaultBindingType()), allocation.Annotations[AnnotationBindingTypeKey])

	assert.Equal(t, "test-pod", allocation.Spec.PodInfo.Name)
	assert.Equal(t, "default", allocation.Spec.PodInfo.Namespace)
	assert.Len(t, allocation.Spec.PortBindings, 1)

	binding := allocation.Spec.PortBindings[0]
	assert.Equal(t, int32(8080), binding.PodPort)
	assert.True(t, binding.LBPort >= 30000 && binding.LBPort <= 30099)
	assert.NotEqual(t, int32(30050), binding.LBPort) // Should not use excluded port
	assert.NotEqual(t, int32(30051), binding.LBPort) // Should not use excluded port
	assert.Equal(t, apollov1.ProtocolTCP, binding.Protocol)
	assert.Equal(t, "http", binding.PortName)
	assert.Equal(t, apollov1.GetDefaultBindingType(), binding.BindingType)
	assert.NotEmpty(t, binding.LoadBalancerRef.ID)

	assert.Equal(t, apollov1.AllocationPhasePending, allocation.Status.Phase)
	assert.NotNil(t, allocation.Status.AllocatedAt)
}

func TestTryAllocatePort_Success_MultiplePorts(t *testing.T) {
	// Setup
	scheme := runtime.NewScheme()
	_ = apollov1.AddToScheme(scheme)
	
	pool := createTestPoolForPortAllocation("test-pool", "default", 2)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool).Build()
	
	allocator := NewPoolAllocator(fakeClient, StrategyRoundRobin)

	containerPorts := []apollov1.PodPortAllocation{
		{PodPort: 8080, Protocol: apollov1.ProtocolTCP, PortName: "http"},
		{PodPort: 9090, Protocol: apollov1.ProtocolTCP, PortName: "metrics"},
		{PodPort: 3000, Protocol: apollov1.ProtocolUDP, PortName: "debug"},
	}

	// Execute
	allocation, err := allocator.tryAllocatePort(context.TODO(), "test-pool", "default", "test-pod", "default", containerPorts)

	// Verify
	require.NoError(t, err)
	require.NotNil(t, allocation)

	assert.Len(t, allocation.Spec.PortBindings, 3)

	// Verify each port binding
	for i, binding := range allocation.Spec.PortBindings {
		expectedPodPort := containerPorts[i].PodPort
		assert.Equal(t, expectedPodPort, binding.PodPort)
		assert.True(t, binding.LBPort >= 30000 && binding.LBPort <= 30099)
		assert.NotEqual(t, int32(30050), binding.LBPort) // Should not use excluded port
		assert.NotEqual(t, int32(30051), binding.LBPort) // Should not use excluded port
		assert.Equal(t, containerPorts[i].Protocol, binding.Protocol)
		assert.Equal(t, containerPorts[i].PortName, binding.PortName)
		assert.NotEmpty(t, binding.LoadBalancerRef.ID)
	}

	// Verify all ports are allocated from the same LB
	firstLBID := allocation.Spec.PortBindings[0].LoadBalancerRef.ID
	for _, binding := range allocation.Spec.PortBindings {
		assert.Equal(t, firstLBID, binding.LoadBalancerRef.ID)
	}
}

func TestTryAllocatePort_WithExistingAllocations(t *testing.T) {
	// Setup
	scheme := runtime.NewScheme()
	_ = apollov1.AddToScheme(scheme)
	
	pool := createTestPoolForPortAllocation("test-pool", "default", 2)
	
	// Create existing allocation that uses some ports
	existingAllocation := createTestPortAllocation(
		"test-pool-existing-pod", "default", "test-pool", "existing-pod", "default",
		"lb-1", []int32{30000, 30001}, []int32{8080, 9090},
	)
	
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool, existingAllocation).Build()
	
	allocator := NewPoolAllocator(fakeClient, StrategyRoundRobin)

	containerPorts := []apollov1.PodPortAllocation{
		{PodPort: 8080, Protocol: apollov1.ProtocolTCP, PortName: "http"},
	}

	// Execute
	allocation, err := allocator.tryAllocatePort(context.TODO(), "test-pool", "default", "test-pod", "default", containerPorts)

	// Verify
	require.NoError(t, err)
	require.NotNil(t, allocation)

	assert.Len(t, allocation.Spec.PortBindings, 1)
	binding := allocation.Spec.PortBindings[0]
	
	// Should not allocate ports 30000 or 30001 (already used by existing allocation)
	assert.NotEqual(t, int32(30000), binding.LBPort)
	assert.NotEqual(t, int32(30001), binding.LBPort)
	assert.True(t, binding.LBPort >= 30002 && binding.LBPort <= 30099)
	assert.NotEqual(t, int32(30050), binding.LBPort) // Should not use excluded port
	assert.NotEqual(t, int32(30051), binding.LBPort) // Should not use excluded port
}

func TestTryAllocatePort_PoolNotFound(t *testing.T) {
	// Setup
	scheme := runtime.NewScheme()
	_ = apollov1.AddToScheme(scheme)
	
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	
	allocator := NewPoolAllocator(fakeClient, StrategyRoundRobin)

	containerPorts := []apollov1.PodPortAllocation{
		{PodPort: 8080, Protocol: apollov1.ProtocolTCP, PortName: "http"},
	}

	// Execute
	allocation, err := allocator.tryAllocatePort(context.TODO(), "nonexistent-pool", "default", "test-pod", "default", containerPorts)

	// Verify
	assert.Error(t, err)
	assert.Nil(t, allocation)
	assert.Contains(t, err.Error(), "failed to get pool and allocations")
}

func TestTryAllocatePort_NoLoadBalancers(t *testing.T) {
	// Setup
	scheme := runtime.NewScheme()
	_ = apollov1.AddToScheme(scheme)
	
	pool := &apollov1.ApolloNetworkPool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "empty-pool",
			Namespace: "default",
		},
		Spec: apollov1.ApolloNetworkPoolSpec{
			LoadBalancers: []apollov1.LoadBalancerRef{}, // No load balancers
			PortRange: apollov1.PortRange{
				Min: 30000,
				Max: 30099,
			},
		},
	}
	
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool).Build()
	
	allocator := NewPoolAllocator(fakeClient, StrategyRoundRobin)

	containerPorts := []apollov1.PodPortAllocation{
		{PodPort: 8080, Protocol: apollov1.ProtocolTCP, PortName: "http"},
	}

	// Execute
	allocation, err := allocator.tryAllocatePort(context.TODO(), "empty-pool", "default", "test-pod", "default", containerPorts)

	// Verify
	assert.Error(t, err)
	assert.Nil(t, allocation)
	assert.Contains(t, err.Error(), "failed to select load balancer")
}

func TestTryAllocatePort_InsufficientPorts(t *testing.T) {
	// Setup
	scheme := runtime.NewScheme()
	_ = apollov1.AddToScheme(scheme)
	
	// Create a pool with very limited port range
	pool := &apollov1.ApolloNetworkPool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "limited-pool",
			Namespace: "default",
		},
		Spec: apollov1.ApolloNetworkPoolSpec{
			LoadBalancers: []apollov1.LoadBalancerRef{
				{ID: "lb-1", Region: "us-west-1", Provider: "test-provider"},
			},
			PortRange: apollov1.PortRange{
				Min: 30000,
				Max: 30001, // Only 2 ports available
			},
		},
	}
	
	// Create existing allocations that use all available ports
	existingAllocation1 := createTestPortAllocation(
		"limited-pool-pod1", "default", "limited-pool", "pod1", "default",
		"lb-1", []int32{30000}, []int32{8080},
	)
	existingAllocation2 := createTestPortAllocation(
		"limited-pool-pod2", "default", "limited-pool", "pod2", "default",
		"lb-1", []int32{30001}, []int32{8080},
	)
	
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool, existingAllocation1, existingAllocation2).Build()
	
	allocator := NewPoolAllocator(fakeClient, StrategyRoundRobin)

	containerPorts := []apollov1.PodPortAllocation{
		{PodPort: 8080, Protocol: apollov1.ProtocolTCP, PortName: "http"},
	}

	// Execute
	allocation, err := allocator.tryAllocatePort(context.TODO(), "limited-pool", "default", "test-pod", "default", containerPorts)

	// Verify
	assert.Error(t, err)
	assert.Nil(t, allocation)
	assert.Contains(t, err.Error(), "failed to find available ports")
}

func TestTryAllocatePort_LeastUsedStrategy(t *testing.T) {
	// Setup
	scheme := runtime.NewScheme()
	_ = apollov1.AddToScheme(scheme)
	
	pool := createTestPoolForPortAllocation("test-pool", "default", 3)
	
	// Create existing allocations with different usage patterns
	// lb-1: 2 ports used, lb-2: 1 port used, lb-3: 0 ports used
	existingAllocation1 := createTestPortAllocation(
		"test-pool-pod1", "default", "test-pool", "pod1", "default",
		"lb-1", []int32{30000, 30001}, []int32{8080, 9090},
	)
	existingAllocation2 := createTestPortAllocation(
		"test-pool-pod2", "default", "test-pool", "pod2", "default",
		"lb-2", []int32{30000}, []int32{8080},
	)
	
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool, existingAllocation1, existingAllocation2).Build()
	
	allocator := NewPoolAllocator(fakeClient, StrategyLeastUsed)

	containerPorts := []apollov1.PodPortAllocation{
		{PodPort: 8080, Protocol: apollov1.ProtocolTCP, PortName: "http"},
	}

	// Execute
	allocation, err := allocator.tryAllocatePort(context.TODO(), "test-pool", "default", "test-pod", "default", containerPorts)

	// Verify
	require.NoError(t, err)
	require.NotNil(t, allocation)

	// Should select lb-3 (least used - 0 ports)
	assert.Equal(t, "lb-3", allocation.Spec.PortBindings[0].LoadBalancerRef.ID)
}

func TestTryAllocatePort_EmptyContainerPorts(t *testing.T) {
	// Setup
	scheme := runtime.NewScheme()
	_ = apollov1.AddToScheme(scheme)
	
	pool := createTestPoolForPortAllocation("test-pool", "default", 2)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool).Build()
	
	allocator := NewPoolAllocator(fakeClient, StrategyRoundRobin)

	// Execute with empty container ports
	allocation, err := allocator.tryAllocatePort(context.TODO(), "test-pool", "default", "test-pod", "default", []apollov1.PodPortAllocation{})

	// Verify
	require.NoError(t, err)
	require.NotNil(t, allocation)

	// Should create allocation with empty port bindings
	assert.Len(t, allocation.Spec.PortBindings, 0)
	assert.Equal(t, apollov1.AllocationPhasePending, allocation.Status.Phase)
}

func TestTryAllocatePort_PortExclusion(t *testing.T) {
	// Setup
	scheme := runtime.NewScheme()
	_ = apollov1.AddToScheme(scheme)
	
	// Create pool with many excluded ports
	pool := &apollov1.ApolloNetworkPool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pool",
			Namespace: "default",
		},
		Spec: apollov1.ApolloNetworkPoolSpec{
			LoadBalancers: []apollov1.LoadBalancerRef{
				{ID: "lb-1", Region: "us-west-1", Provider: "test-provider"},
			},
			PortRange: apollov1.PortRange{
				Min:           30000,
				Max:           30010,
				ExcludedPorts: []int32{30000, 30001, 30002, 30003, 30004, 30005, 30006, 30007, 30008}, // Exclude most ports
			},
		},
	}
	
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool).Build()
	
	allocator := NewPoolAllocator(fakeClient, StrategyRoundRobin)

	containerPorts := []apollov1.PodPortAllocation{
		{PodPort: 8080, Protocol: apollov1.ProtocolTCP, PortName: "http"},
	}

	// Execute
	allocation, err := allocator.tryAllocatePort(context.TODO(), "test-pool", "default", "test-pod", "default", containerPorts)

	// Verify
	require.NoError(t, err)
	require.NotNil(t, allocation)

	// Should use one of the non-excluded ports (30009 or 30010)
	binding := allocation.Spec.PortBindings[0]
	assert.True(t, binding.LBPort == 30009 || binding.LBPort == 30010)
}

// Test helper functions
func TestGetLBPortUsage(t *testing.T) {
	allocator := NewPoolAllocator(nil, StrategyRoundRobin)
	
	allocations := &apollov1.PortAllocationList{
		Items: []apollov1.PortAllocation{
			*createTestPortAllocation("alloc1", "default", "pool", "pod1", "default", "lb-1", []int32{30000, 30001}, []int32{8080, 9090}),
			*createTestPortAllocation("alloc2", "default", "pool", "pod2", "default", "lb-1", []int32{30002}, []int32{8080}),
			*createTestPortAllocation("alloc3", "default", "pool", "pod3", "default", "lb-2", []int32{30000}, []int32{8080}),
		},
	}
	
	lb1 := &apollov1.LoadBalancerRef{ID: "lb-1"}
	lb2 := &apollov1.LoadBalancerRef{ID: "lb-2"}
	lb3 := &apollov1.LoadBalancerRef{ID: "lb-3"}
	
	assert.Equal(t, int32(3), allocator.getLBPortUsage(allocations, lb1))
	assert.Equal(t, int32(1), allocator.getLBPortUsage(allocations, lb2))
	assert.Equal(t, int32(0), allocator.getLBPortUsage(allocations, lb3))
}

func TestHasAvailablePorts(t *testing.T) {
	allocator := NewPoolAllocator(nil, StrategyRoundRobin)
	
	pool := createTestPoolForPortAllocation("test-pool", "default", 2)
	allocations := &apollov1.PortAllocationList{
		Items: []apollov1.PortAllocation{
			*createTestPortAllocation("alloc1", "default", "pool", "pod1", "default", "lb-1", []int32{30000, 30001}, []int32{8080, 9090}),
		},
	}
	
	poolAlloc := &poolAndAllocations{
		pool:        pool,
		allocations: allocations,
	}
	
	lb1 := &apollov1.LoadBalancerRef{ID: "lb-1"}
	lb2 := &apollov1.LoadBalancerRef{ID: "lb-2"}
	
	// lb-1 has 2 ports used, 96 available (98 total - 2 used)
	assert.True(t, allocator.hasAvailablePorts(poolAlloc, lb1, 1))
	assert.True(t, allocator.hasAvailablePorts(poolAlloc, lb1, 96))
	assert.False(t, allocator.hasAvailablePorts(poolAlloc, lb1, 97))
	
	// lb-2 has 0 ports used, 98 available
	assert.True(t, allocator.hasAvailablePorts(poolAlloc, lb2, 1))
	assert.True(t, allocator.hasAvailablePorts(poolAlloc, lb2, 98))
	assert.False(t, allocator.hasAvailablePorts(poolAlloc, lb2, 99))
}

func TestFindAvailablePorts(t *testing.T) {
	allocator := NewPoolAllocator(nil, StrategyRoundRobin)
	
	pool := createTestPoolForPortAllocation("test-pool", "default", 2)
	allocations := &apollov1.PortAllocationList{
		Items: []apollov1.PortAllocation{
			*createTestPortAllocation("alloc1", "default", "pool", "pod1", "default", "lb-1", []int32{30000, 30001, 30050, 30051}, []int32{8080, 9090, 3000, 4000}),
		},
	}
	
	poolAlloc := &poolAndAllocations{
		pool:        pool,
		allocations: allocations,
	}
	
	lb1 := &apollov1.LoadBalancerRef{ID: "lb-1"}
	
	// Find 3 available ports for lb-1
	ports, err := allocator.findAvailablePorts(poolAlloc, lb1, 3)
	require.NoError(t, err)
	require.Len(t, ports, 3)
	
	// Should not include already allocated ports (30000, 30001) or excluded ports (30050, 30051)
	for _, port := range ports {
		assert.NotEqual(t, int32(30000), port)
		assert.NotEqual(t, int32(30001), port)
		assert.NotEqual(t, int32(30050), port)
		assert.NotEqual(t, int32(30051), port)
		assert.True(t, port >= 30000 && port <= 30099)
	}
	
	// Should be consecutive available ports (starting from 30002)
	assert.Equal(t, int32(30002), ports[0])
	assert.Equal(t, int32(30003), ports[1])
	assert.Equal(t, int32(30004), ports[2])
}

func TestFindAvailablePorts_InvalidCount(t *testing.T) {
	allocator := NewPoolAllocator(nil, StrategyRoundRobin)
	
	pool := createTestPoolForPortAllocation("test-pool", "default", 1)
	poolAlloc := &poolAndAllocations{
		pool:        pool,
		allocations: &apollov1.PortAllocationList{},
	}
	
	lb := &apollov1.LoadBalancerRef{ID: "lb-1"}
	
	// Test with invalid count
	ports, err := allocator.findAvailablePorts(poolAlloc, lb, 0)
	assert.Error(t, err)
	assert.Nil(t, ports)
	assert.Contains(t, err.Error(), "port count must be positive")
	
	ports, err = allocator.findAvailablePorts(poolAlloc, lb, -1)
	assert.Error(t, err)
	assert.Nil(t, ports)
	assert.Contains(t, err.Error(), "port count must be positive")
}
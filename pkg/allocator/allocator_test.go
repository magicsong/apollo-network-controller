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
// Helper Functions
// =============================================================================

// createTestPool creates a test ApolloNetworkPool with specified parameters
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

// createTestPoolWithExclusions creates a test pool with excluded ports
func createTestPoolWithExclusions(name, namespace string, lbCount int, minPort, maxPort int32, excludedPorts []int32) *apollov1.ApolloNetworkPool {
	pool := createTestPool(name, namespace, lbCount, minPort, maxPort)
	pool.Spec.PortRange.ExcludedPorts = excludedPorts
	return pool
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

// getPortRange 获取端口范围的辅助函数
func getPortRange(ports map[int32]bool) []int32 {
	portList := make([]int32, 0, len(ports))
	for port := range ports {
		portList = append(portList, port)
	}
	return portList
}

// =============================================================================
// Basic Allocation Tests
// =============================================================================

func TestBasicAllocation(t *testing.T) {
	// Setup
	scheme := runtime.NewScheme()
	_ = apollov1.AddToScheme(scheme)
	
	pool := createTestPool("test-pool", "default", 1, 30000, 30010)
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
	assert.Len(t, allocation.Spec.PortBindings, 1)
	
	binding := allocation.Spec.PortBindings[0]
	assert.Equal(t, int32(8080), binding.PodPort)
	assert.True(t, binding.LBPort >= 30000 && binding.LBPort <= 30010)
	assert.Equal(t, "lb-1", binding.LoadBalancerRef.ID)
}

// =============================================================================
// AllocatePort Tests (Integration Tests)
// =============================================================================

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

	// Execute - this should now fail due to duplicate allocation prevention
	_, err := allocator.AllocatePort(context.TODO(), "test-pool", "default", "test-pod", "default", containerPorts)

	// Verify - should get an error about duplicate allocation
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already has an active allocation")
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
			Finalizers:        []string{"test/finalizer"}, // Add a dummy finalizer to satisfy fake client
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
	_, err := allocator.AllocatePort(context.TODO(), "test-pool", "default", "test-pod", "default", containerPorts)

	// Verify
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "is being deleted")
}

// =============================================================================
// Concurrent Allocation Tests
// =============================================================================

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
	
	// Only the first allocation should succeed, the rest should fail due to duplicate prevention
	assert.Equal(t, 1, len(results), "Only one allocation should succeed")
	assert.Equal(t, numGoroutines-1, len(errors), "All other allocations should fail")
	
	// All errors should be about duplicate allocation
	for _, err := range errors {
		assert.Contains(t, err.Error(), "already has an active allocation")
	}
}

// TestConcurrentPortAllocation 测试并发端口分配不会产生冲突
func TestConcurrentPortAllocation(t *testing.T) {
	// Setup
	scheme := runtime.NewScheme()
	_ = apollov1.AddToScheme(scheme)
	
	// 创建有限端口的测试池
	pool := createTestPoolWithExclusions("concurrent-test-pool", "default", 1, 30000, 30020, []int32{})
	
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool).Build()
	allocator := NewPoolAllocator(fakeClient, StrategyRoundRobin)

	// 并发测试参数
	numConcurrentAllocations := 15 // 少于可用端口数
	var wg sync.WaitGroup
	var mu sync.Mutex
	
	successfulAllocations := make([]*apollov1.PortAllocation, 0)
	allocationErrors := make([]error, 0)

	// 启动并发端口分配
	for i := 0; i < numConcurrentAllocations; i++ {
		wg.Add(1)
		go func(podIndex int) {
			defer wg.Done()
			
			containerPorts := []apollov1.PodPortAllocation{
				{PodPort: 8080, Protocol: apollov1.ProtocolTCP, PortName: "http"},
			}
			
			podName := fmt.Sprintf("concurrent-pod-%d", podIndex)
			allocation, err := allocator.AllocatePort(
				context.TODO(), 
				"concurrent-test-pool", 
				"default", 
				podName, 
				"default", 
				containerPorts,
			)
			
			mu.Lock()
			if err != nil {
				allocationErrors = append(allocationErrors, err)
			} else {
				successfulAllocations = append(successfulAllocations, allocation)
			}
			mu.Unlock()
		}(i)
	}

	// 等待所有协程完成
	wg.Wait()

	// 验证结果
	require.Empty(t, allocationErrors, "不应该有分配错误: %v", allocationErrors)
	require.Len(t, successfulAllocations, numConcurrentAllocations, "应该成功分配所有请求的端口")

	// 验证端口无冲突
	allocatedPorts := make(map[int32]bool)
	for i, allocation := range successfulAllocations {
		require.Len(t, allocation.Spec.PortBindings, 1, "分配 %d 应该有一个端口绑定", i)
		
		binding := allocation.Spec.PortBindings[0]
		lbPort := binding.LBPort
		
		// 检查端口冲突
		assert.False(t, allocatedPorts[lbPort], 
			"端口 %d 不应该被分配给多个 Pod", lbPort)
		allocatedPorts[lbPort] = true
		
		// 验证端口在有效范围内
		assert.True(t, lbPort >= 30000 && lbPort <= 30020, 
			"端口 %d 应该在有效范围 30000-30020 内", lbPort)
		
		// 验证使用正确的负载均衡器
		assert.Equal(t, "lb-1", binding.LoadBalancerRef.ID, 
			"应该使用 lb-1 负载均衡器")
	}

	// 验证分配的端口数量正确
	assert.Len(t, allocatedPorts, numConcurrentAllocations, 
		"应该分配了 %d 个唯一端口", numConcurrentAllocations)

	t.Logf("成功并发分配了 %d 个端口，端口范围: %v", 
		len(allocatedPorts), getPortRange(allocatedPorts))
}

// TestConcurrentMultiPortAllocation 测试并发多端口分配
func TestConcurrentMultiPortAllocation(t *testing.T) {
	// Setup
	scheme := runtime.NewScheme()
	_ = apollov1.AddToScheme(scheme)
	
	pool := createTestPool("multiport-test-pool", "default", 2, 30000, 30050)
	
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool).Build()
	allocator := NewPoolAllocator(fakeClient, StrategyLeastUsed)

	numConcurrentAllocations := 8
	portsPerAllocation := 3
	var wg sync.WaitGroup
	var mu sync.Mutex
	
	successfulAllocations := make([]*apollov1.PortAllocation, 0)
	allocationErrors := make([]error, 0)

	// 启动并发多端口分配
	for i := 0; i < numConcurrentAllocations; i++ {
		wg.Add(1)
		go func(podIndex int) {
			defer wg.Done()
			
			containerPorts := []apollov1.PodPortAllocation{
				{PodPort: 8080, Protocol: apollov1.ProtocolTCP, PortName: "http"},
				{PodPort: 9090, Protocol: apollov1.ProtocolTCP, PortName: "metrics"},
				{PodPort: 3000, Protocol: apollov1.ProtocolUDP, PortName: "debug"},
			}
			
			podName := fmt.Sprintf("multiport-pod-%d", podIndex)
			allocation, err := allocator.AllocatePort(
				context.TODO(), 
				"multiport-test-pool", 
				"default", 
				podName, 
				"default", 
				containerPorts,
			)
			
			mu.Lock()
			if err != nil {
				allocationErrors = append(allocationErrors, err)
			} else {
				successfulAllocations = append(successfulAllocations, allocation)
			}
			mu.Unlock()
		}(i)
	}

	wg.Wait()

	// 验证结果
	require.Empty(t, allocationErrors, "不应该有分配错误: %v", allocationErrors)
	require.Len(t, successfulAllocations, numConcurrentAllocations)

	// 按负载均衡器统计端口使用
	lbPortUsage := make(map[string]map[int32]bool)
	
	for i, allocation := range successfulAllocations {
		require.Len(t, allocation.Spec.PortBindings, portsPerAllocation, 
			"分配 %d 应该有 %d 个端口绑定", i, portsPerAllocation)
		
		// 验证同一分配内的所有端口使用相同的负载均衡器
		firstLBID := allocation.Spec.PortBindings[0].LoadBalancerRef.ID
		for _, binding := range allocation.Spec.PortBindings {
			assert.Equal(t, firstLBID, binding.LoadBalancerRef.ID, 
				"同一分配的所有端口应该使用相同的负载均衡器")
			
			// 初始化负载均衡器端口映射
			if lbPortUsage[binding.LoadBalancerRef.ID] == nil {
				lbPortUsage[binding.LoadBalancerRef.ID] = make(map[int32]bool)
			}
			
			// 检查端口冲突
			assert.False(t, lbPortUsage[binding.LoadBalancerRef.ID][binding.LBPort], 
				"端口 %d 在负载均衡器 %s 上不应该被重复分配", 
				binding.LBPort, binding.LoadBalancerRef.ID)
			lbPortUsage[binding.LoadBalancerRef.ID][binding.LBPort] = true
		}
	}

	// 验证负载均衡效果
	assert.True(t, len(lbPortUsage) > 1, "应该使用多个负载均衡器")
	
	// 计算总端口使用量
	totalUsedPorts := 0
	for lbID, portMap := range lbPortUsage {
		portCount := len(portMap)
		totalUsedPorts += portCount
		t.Logf("负载均衡器 %s 使用了 %d 个端口", lbID, portCount)
	}
	
	expectedTotalPorts := numConcurrentAllocations * portsPerAllocation
	assert.Equal(t, expectedTotalPorts, totalUsedPorts, 
		"总端口使用量应该等于预期")
}

// =============================================================================
// CreateOrUpdate Allocation Tests
// =============================================================================

func TestCreateAllocation_Create(t *testing.T) {
	// Setup
	scheme := runtime.NewScheme()
	_ = apollov1.AddToScheme(scheme)
	
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	allocator := NewPoolAllocator(fakeClient, StrategyRoundRobin)

	allocation := &apollov1.PortAllocation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-allocation",
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
		},
	}

	// Execute
	err := allocator.createAllocation(context.TODO(), allocation)

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

func TestCreateAllocation_AlreadyExists(t *testing.T) {
	// Setup
	scheme := runtime.NewScheme()
	_ = apollov1.AddToScheme(scheme)
	
	existing := &apollov1.PortAllocation{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "test-allocation",
			Namespace:       "default",
			ResourceVersion: "1",
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
	
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing).Build()
	allocator := NewPoolAllocator(fakeClient, StrategyRoundRobin)

	newAllocation := &apollov1.PortAllocation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-allocation",
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
		},
	}

	// Execute - should fail because allocation already exists
	err := allocator.createAllocation(context.TODO(), newAllocation)

	// Verify
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
	assert.Contains(t, err.Error(), "cannot create duplicate allocation")
}

func TestCreateAllocation_Error_BeingDeleted(t *testing.T) {
	// Setup
	scheme := runtime.NewScheme()
	_ = apollov1.AddToScheme(scheme)
	
	now := metav1.Now()
	existing := &apollov1.PortAllocation{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "test-allocation",
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
	
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing).Build()
	allocator := NewPoolAllocator(fakeClient, StrategyRoundRobin)

	newAllocation := &apollov1.PortAllocation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-allocation",
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
		},
	}

	// Execute
	err := allocator.createAllocation(context.TODO(), newAllocation)

	// Verify
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "is being deleted")
}

func TestCreateAllocation_Error_NilAllocation(t *testing.T) {
	// Setup
	scheme := runtime.NewScheme()
	_ = apollov1.AddToScheme(scheme)
	
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	allocator := NewPoolAllocator(fakeClient, StrategyRoundRobin)

	// Execute
	err := allocator.createAllocation(context.TODO(), nil)

	// Verify
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "allocation cannot be nil")
}

// =============================================================================
// TryAllocatePort Tests (Internal Logic Tests)
// =============================================================================

func TestTryAllocatePort_Success_SinglePort(t *testing.T) {
	// Setup
	scheme := runtime.NewScheme()
	_ = apollov1.AddToScheme(scheme)
	
	pool := createTestPoolWithExclusions("test-pool", "default", 2, 30000, 30099, []int32{30050, 30051})
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
	
	pool := createTestPoolWithExclusions("test-pool", "default", 2, 30000, 30099, []int32{30050, 30051})
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
	
	pool := createTestPoolWithExclusions("test-pool", "default", 2, 30000, 30099, []int32{30050, 30051})
	
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

func TestTryAllocatePort_LeastUsedStrategy(t *testing.T) {
	// Setup
	scheme := runtime.NewScheme()
	_ = apollov1.AddToScheme(scheme)
	
	pool := createTestPoolWithExclusions("test-pool", "default", 3, 30000, 30099, []int32{30050, 30051})
	
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
	
	pool := createTestPoolWithExclusions("test-pool", "default", 2, 30000, 30099, []int32{30050, 30051})
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

// =============================================================================
// Error Condition Tests
// =============================================================================

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
	assert.Contains(t, err.Error(), "no load balancer has 1 available ports")
}

// =============================================================================
// Helper Function Tests
// =============================================================================

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
	
	pool := createTestPoolWithExclusions("test-pool", "default", 2, 30000, 30099, []int32{30050, 30051})
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
	
	pool := createTestPoolWithExclusions("test-pool", "default", 2, 30000, 30099, []int32{30050, 30051})
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
	
	pool := createTestPoolWithExclusions("test-pool", "default", 1, 30000, 30099, []int32{})
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

func TestAllocatePort_DuplicateAllocation_ActiveAllocation(t *testing.T) {
	// Setup
	scheme := runtime.NewScheme()
	_ = apollov1.AddToScheme(scheme)
	
	pool := createTestPool("test-pool", "default", 1, 30000, 30099)
	
	// Create existing active allocation
	existingAllocation := &apollov1.PortAllocation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pool-test-pod", // This is the expected allocation name format
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
		{PodPort: 9090, Protocol: apollov1.ProtocolTCP, PortName: "metrics"},
	}

	// Execute - try to allocate for the same pod again
	allocation, err := allocator.AllocatePort(context.TODO(), "test-pool", "default", "test-pod", "default", containerPorts)

	// Verify
	assert.Error(t, err)
	assert.Nil(t, allocation)
	assert.Contains(t, err.Error(), "already has an active allocation")
	assert.Contains(t, err.Error(), "multiple allocations per pod are not allowed")
}

func TestAllocatePort_DuplicateAllocation_DeletingAllocation(t *testing.T) {
	// Setup
	scheme := runtime.NewScheme()
	_ = apollov1.AddToScheme(scheme)
	
	pool := createTestPool("test-pool", "default", 1, 30000, 30099)
	
	// Create existing allocation that is being deleted
	now := metav1.Now()
	deletingAllocation := &apollov1.PortAllocation{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "test-pool-test-pod",
			Namespace:         "default",
			DeletionTimestamp: &now,
			Finalizers:        []string{"apollo.network/port-cleanup"}, // Add finalizer to make it valid
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

	// Execute - try to allocate for a pod that's being deleted
	allocation, err := allocator.AllocatePort(context.TODO(), "test-pool", "default", "test-pod", "default", containerPorts)

	// Verify
	assert.Error(t, err)
	assert.Nil(t, allocation)
	assert.Contains(t, err.Error(), "is being deleted")
	assert.Contains(t, err.Error(), "cannot create new allocation")
}

func TestAllocatePort_DuplicateAllocation_DifferentPod(t *testing.T) {
	// Setup
	scheme := runtime.NewScheme()
	_ = apollov1.AddToScheme(scheme)
	
	pool := createTestPool("test-pool", "default", 1, 30000, 30099)
	
	// Create existing allocation for a different pod (edge case - same allocation name but different pod)
	existingAllocation := &apollov1.PortAllocation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pool-test-pod", // Same name format
			Namespace: "default",
			Labels: map[string]string{
				LabelPoolNameKey: "test-pool",
				LabelPodNameKey:  "different-pod", // Different pod
			},
		},
		Spec: apollov1.PortAllocationSpec{
			PodInfo: apollov1.PodInfo{
				Name:      "different-pod", // Different pod
				Namespace: "different-namespace",
			},
		},
	}
	
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool, existingAllocation).Build()
	allocator := NewPoolAllocator(fakeClient, StrategyRoundRobin)

	containerPorts := []apollov1.PodPortAllocation{
		{PodPort: 8080, Protocol: apollov1.ProtocolTCP, PortName: "http"},
	}

	// Execute - this should fail because there's already an allocation with the same name
	// but for a different pod, which indicates a naming conflict
	_, err := allocator.AllocatePort(context.TODO(), "test-pool", "default", "test-pod", "default", containerPorts)

	// Verify - this should fail due to name conflict
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
	assert.Contains(t, err.Error(), "cannot create duplicate allocation")
}

func TestAllocatePort_NoExistingAllocation(t *testing.T) {
	// Setup
	scheme := runtime.NewScheme()
	_ = apollov1.AddToScheme(scheme)
	
	pool := createTestPool("test-pool", "default", 1, 30000, 30099)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool).Build()
	allocator := NewPoolAllocator(fakeClient, StrategyRoundRobin)

	containerPorts := []apollov1.PodPortAllocation{
		{PodPort: 8080, Protocol: apollov1.ProtocolTCP, PortName: "http"},
	}

	// Execute - this should succeed as there's no existing allocation
	allocation, err := allocator.AllocatePort(context.TODO(), "test-pool", "default", "test-pod", "default", containerPorts)

	// Verify
	require.NoError(t, err)
	require.NotNil(t, allocation)
	assert.Equal(t, "test-pool-test-pod", allocation.Name)
	assert.Equal(t, "test-pod", allocation.Spec.PodInfo.Name)
	assert.Equal(t, "default", allocation.Spec.PodInfo.Namespace)
}
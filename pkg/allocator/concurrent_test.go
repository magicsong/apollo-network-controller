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
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apollov1 "github.com/magicsong/apollo-network-controller/api/v1"
)

// TestConcurrentPortAllocation 测试并发端口分配不会产生冲突
func TestConcurrentPortAllocation(t *testing.T) {
	// Setup
	scheme := runtime.NewScheme()
	_ = apollov1.AddToScheme(scheme)
	
	// 创建有限端口的测试池
	pool := &apollov1.ApolloNetworkPool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "concurrent-test-pool",
			Namespace: "default",
		},
		Spec: apollov1.ApolloNetworkPoolSpec{
			LoadBalancers: []apollov1.LoadBalancerRef{
				{ID: "lb-1", Region: "us-west-1", Provider: "test-provider"},
			},
			PortRange: apollov1.PortRange{
				Min:           30000,
				Max:           30020, // 21 端口可用
				ExcludedPorts: []int32{}, // 无排除端口
			},
		},
	}
	
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
	
	pool := &apollov1.ApolloNetworkPool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "multiport-test-pool",
			Namespace: "default",
		},
		Spec: apollov1.ApolloNetworkPoolSpec{
			LoadBalancers: []apollov1.LoadBalancerRef{
				{ID: "lb-1", Region: "us-west-1", Provider: "test-provider"},
				{ID: "lb-2", Region: "us-west-1", Provider: "test-provider"},
			},
			PortRange: apollov1.PortRange{
				Min: 30000,
				Max: 30050, // 51 端口可用
			},
		},
	}
	
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

// getPortRange 获取端口范围的辅助函数
func getPortRange(ports map[int32]bool) []int32 {
	portList := make([]int32, 0, len(ports))
	for port := range ports {
		portList = append(portList, port)
	}
	return portList
}
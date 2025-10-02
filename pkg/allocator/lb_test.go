package allocator

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apollov1 "github.com/magicsong/apollo-network-controller/api/v1"
)

// createTestPool creates a test ApolloNetworkPool for testing
func createTestPool(name, namespace string, lbCount int) *apollov1.ApolloNetworkPool {
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
		Status: apollov1.ApolloNetworkPoolStatus{
			Allocations: make(map[string]apollov1.LoadBalancerAllocateStatus),
		},
	}
}

// createPoolWithAllocations creates a test pool with existing allocations
func createPoolWithAllocations(name, namespace string, lbCount int) *apollov1.ApolloNetworkPool {
	pool := createTestPool(name, namespace, lbCount)
	
	// Add some existing allocations
	pool.Status.Allocations = map[string]apollov1.LoadBalancerAllocateStatus{
		"lb-1": {
			AllocatedPorts: []apollov1.PortAllocation{
				{
					PodName:      "test-pod-1",
					PodNamespace: "default",
					PortMappings: []apollov1.PodPortAllocation{
						{PodPort: 8080, LBPort: 30000, Protocol: apollov1.ProtocolTCP},
						{PodPort: 9090, LBPort: 30001, Protocol: apollov1.ProtocolTCP},
					},
					LoadBalancer: apollov1.LoadBalancerRef{ID: "lb-1"},
				},
			},
			AllocatedCount: 2,
			TotalPorts:     98, // 100 - 2 excluded
			AvailableCount: 96,
		},
		"lb-2": {
			AllocatedPorts: []apollov1.PortAllocation{
				{
					PodName:      "test-pod-2",
					PodNamespace: "default",
					PortMappings: []apollov1.PodPortAllocation{
						{PodPort: 8080, LBPort: 30000, Protocol: apollov1.ProtocolTCP},
					},
					LoadBalancer: apollov1.LoadBalancerRef{ID: "lb-2"},
				},
			},
			AllocatedCount: 1,
			TotalPorts:     98,
			AvailableCount: 97,
		},
	}
	
	return pool
}

func TestSelectLoadBalancer_RoundRobin_Basic(t *testing.T) {
	allocator := NewPoolAllocator(nil, StrategyRoundRobin)
	pool := createTestPool("test-pool", "default", 3)

	// Test basic round-robin selection
	for i := 0; i < 6; i++ {
		lb, err := allocator.SelectLoadBalancer(pool, 1)
		require.NoError(t, err)
		
		expectedLBID := fmt.Sprintf("lb-%d", (i%3)+1)
		assert.Equal(t, expectedLBID, lb.ID, "Round-robin should cycle through LBs")
	}
}

func TestSelectLoadBalancer_RoundRobin_WithAllocations(t *testing.T) {
	allocator := NewPoolAllocator(nil, StrategyRoundRobin)
	pool := createPoolWithAllocations("test-pool", "default", 3)

	// All LBs should still be available for single port allocation
	for i := 0; i < 3; i++ {
		lb, err := allocator.SelectLoadBalancer(pool, 1)
		require.NoError(t, err)
		assert.NotEmpty(t, lb.ID)
	}
}

func TestSelectLoadBalancer_LeastUsed_Basic(t *testing.T) {
	allocator := NewPoolAllocator(nil, StrategyLeastUsed)
	pool := createPoolWithAllocations("test-pool", "default", 3)

	// lb-3 has no allocations, should be selected first
	lb, err := allocator.SelectLoadBalancer(pool, 1)
	require.NoError(t, err)
	assert.Equal(t, "lb-3", lb.ID, "Should select LB with least usage")

	// If we keep selecting, it should still prefer lb-3 until it gets more usage
	lb, err = allocator.SelectLoadBalancer(pool, 1)
	require.NoError(t, err)
	assert.Equal(t, "lb-3", lb.ID, "Should continue selecting least used LB")
}

func TestSelectLoadBalancer_MultiplePortsRequired(t *testing.T) {
	allocator := NewPoolAllocator(nil, StrategyRoundRobin)
	pool := createTestPool("test-pool", "default", 2)

	tests := []struct {
		name       string
		portCount  int
		shouldFail bool
	}{
		{"Single port", 1, false},
		{"Multiple ports - valid", 10, false},
		{"Too many ports", 99, true}, // Only 98 ports available (100 - 2 excluded)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lb, err := allocator.SelectLoadBalancer(pool, tt.portCount)
			if tt.shouldFail {
				assert.Error(t, err)
				assert.Nil(t, lb)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, lb)
			}
		})
	}
}

func TestSelectLoadBalancer_NoAvailableLBs(t *testing.T) {
	allocator := NewPoolAllocator(nil, StrategyRoundRobin)
	
	// Empty pool
	emptyPool := &apollov1.ApolloNetworkPool{
		Spec: apollov1.ApolloNetworkPoolSpec{
			LoadBalancers: []apollov1.LoadBalancerRef{},
		},
	}

	lb, err := allocator.SelectLoadBalancer(emptyPool, 1)
	assert.Error(t, err)
	assert.Nil(t, lb)
	assert.Contains(t, err.Error(), "no load balancers configured")
}

// TestSelectLoadBalancer_ConcurrentAccess tests thread safety of load balancer selection
func TestSelectLoadBalancer_ConcurrentAccess(t *testing.T) {
	allocator := NewPoolAllocator(nil, StrategyRoundRobin)
	pool := createTestPool("test-pool", "default", 3)

	const numGoroutines = 100
	const selectionsPerGoroutine = 10

	var wg sync.WaitGroup
	results := make(chan string, numGoroutines*selectionsPerGoroutine)
	errors := make(chan error, numGoroutines*selectionsPerGoroutine)

	// Launch concurrent goroutines
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(routineID int) {
			defer wg.Done()
			
			for j := 0; j < selectionsPerGoroutine; j++ {
				lb, err := allocator.SelectLoadBalancer(pool, 1)
				if err != nil {
					errors <- err
				} else {
					results <- lb.ID
				}
				
				// Add small random delay to increase chance of race conditions
				time.Sleep(time.Microsecond * time.Duration(routineID%10))
			}
		}(i)
	}

	wg.Wait()
	close(results)
	close(errors)

	// Check for errors
	errorCount := 0
	for err := range errors {
		t.Errorf("Unexpected error during concurrent access: %v", err)
		errorCount++
	}
	assert.Equal(t, 0, errorCount, "Should have no errors during concurrent access")

	// Collect results and verify distribution
	lbCounts := make(map[string]int)
	totalSelections := 0
	
	for lbID := range results {
		lbCounts[lbID]++
		totalSelections++
	}

	// Verify we got all expected selections
	assert.Equal(t, numGoroutines*selectionsPerGoroutine, totalSelections)

	// Verify all LBs were selected (round-robin should distribute)
	assert.Equal(t, 3, len(lbCounts), "All 3 LBs should have been selected")

	// Verify reasonable distribution (not perfectly equal due to concurrency, but should be close)
	expectedCount := totalSelections / 3
	tolerance := expectedCount / 10 // 10% tolerance
	
	for lbID, count := range lbCounts {
		assert.True(t, 
			count >= expectedCount-tolerance && count <= expectedCount+tolerance,
			"LB %s selection count %d should be within tolerance of expected %d", lbID, count, expectedCount)
	}
}

// TestSelectLoadBalancer_RoundRobinIndexConsistency tests that round-robin index is properly maintained
func TestSelectLoadBalancer_RoundRobinIndexConsistency(t *testing.T) {
	allocator := NewPoolAllocator(nil, StrategyRoundRobin)
	pool := createTestPool("test-pool", "default", 3)

	// Track selections to verify proper round-robin behavior
	var selections []string
	
	// Make selections and record them
	for i := 0; i < 9; i++ { // 3 full cycles
		lb, err := allocator.SelectLoadBalancer(pool, 1)
		require.NoError(t, err)
		selections = append(selections, lb.ID)
	}

	// Verify the pattern repeats every 3 selections
	expectedPattern := []string{"lb-1", "lb-2", "lb-3"}
	for i, selection := range selections {
		expected := expectedPattern[i%3]
		assert.Equal(t, expected, selection, 
			"Selection %d should be %s but got %s", i, expected, selection)
	}
}

// TestSelectLoadBalancer_MultiplePoolsConcurrency tests concurrent access to different pools
func TestSelectLoadBalancer_MultiplePoolsConcurrency(t *testing.T) {
	allocator := NewPoolAllocator(nil, StrategyRoundRobin)
	
	// Create multiple pools
	pools := []*apollov1.ApolloNetworkPool{
		createTestPool("pool-1", "default", 2),
		createTestPool("pool-2", "default", 3),
		createTestPool("pool-3", "kube-system", 2),
	}

	const numGoroutines = 50
	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines*len(pools))

	// Launch concurrent goroutines for each pool
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(routineID int) {
			defer wg.Done()
			
			// Each goroutine selects from all pools
			for _, pool := range pools {
				for j := 0; j < 5; j++ {
					_, err := allocator.SelectLoadBalancer(pool, 1)
					if err != nil {
						errors <- fmt.Errorf("pool %s/%s error: %w", pool.Namespace, pool.Name, err)
					}
				}
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for errors
	for err := range errors {
		t.Errorf("Unexpected error during multi-pool concurrent access: %v", err)
	}
}

// TestSelectLoadBalancer_StressTest performs a stress test with high concurrency
func TestSelectLoadBalancer_StressTest(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	allocator := NewPoolAllocator(nil, StrategyRoundRobin)
	pool := createTestPool("stress-pool", "default", 5)

	const numGoroutines = 200
	const selectionsPerGoroutine = 100
	const duration = 5 * time.Second

	var wg sync.WaitGroup
	stop := make(chan struct{})
	results := make(chan bool, numGoroutines*selectionsPerGoroutine)

	// Start stress test
	startTime := time.Now()
	
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			
			for {
				select {
				case <-stop:
					return
				default:
					_, err := allocator.SelectLoadBalancer(pool, 1)
					results <- err == nil
				}
			}
		}()
	}

	// Stop after duration
	time.Sleep(duration)
	close(stop)
	wg.Wait()
	close(results)

	// Analyze results
	totalSelections := 0
	successfulSelections := 0
	
	for success := range results {
		totalSelections++
		if success {
			successfulSelections++
		}
	}

	elapsedTime := time.Since(startTime)
	throughput := float64(totalSelections) / elapsedTime.Seconds()

	t.Logf("Stress test results:")
	t.Logf("  Duration: %v", elapsedTime)
	t.Logf("  Total selections: %d", totalSelections)
	t.Logf("  Successful selections: %d", successfulSelections)
	t.Logf("  Success rate: %.2f%%", float64(successfulSelections)/float64(totalSelections)*100)
	t.Logf("  Throughput: %.2f selections/second", throughput)

	// Assertions
	assert.Greater(t, totalSelections, 1000, "Should perform substantial work")
	assert.Equal(t, successfulSelections, totalSelections, "All selections should succeed")
	assert.Greater(t, throughput, 100.0, "Should maintain reasonable throughput")
}

// TestSelectLoadBalancer_RaceConditionDetection tests for potential race conditions
func TestSelectLoadBalancer_RaceConditionDetection(t *testing.T) {
	allocator := NewPoolAllocator(nil, StrategyRoundRobin)
	pool := createTestPool("race-pool", "default", 2)

	const iterations = 1000
	var wg sync.WaitGroup
	
	// Channel to collect all selected LB IDs with their selection order
	type selection struct {
		order int
		lbID  string
	}
	selections := make(chan selection, iterations)

	// Launch goroutines that select LBs simultaneously
	for i := 0; i < iterations; i++ {
		wg.Add(1)
		go func(order int) {
			defer wg.Done()
			
			lb, err := allocator.SelectLoadBalancer(pool, 1)
			if err != nil {
				t.Errorf("Selection %d failed: %v", order, err)
				return
			}
			
			selections <- selection{order: order, lbID: lb.ID}
		}(i)
	}

	wg.Wait()
	close(selections)

	// Collect results
	results := make([]selection, 0, iterations)
	for sel := range selections {
		results = append(results, sel)
	}

	// Verify we got all selections
	assert.Equal(t, iterations, len(results), "Should receive all selections")

	// Verify all LB IDs are valid
	validLBs := map[string]bool{"lb-1": true, "lb-2": true}
	for _, sel := range results {
		assert.True(t, validLBs[sel.lbID], "LB ID %s should be valid", sel.lbID)
	}
}

// Benchmark tests
func BenchmarkSelectLoadBalancer_RoundRobin(b *testing.B) {
	allocator := NewPoolAllocator(nil, StrategyRoundRobin)
	pool := createTestPool("bench-pool", "default", 3)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := allocator.SelectLoadBalancer(pool, 1)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSelectLoadBalancer_LeastUsed(b *testing.B) {
	allocator := NewPoolAllocator(nil, StrategyLeastUsed)
	pool := createPoolWithAllocations("bench-pool", "default", 5)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := allocator.SelectLoadBalancer(pool, 1)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSelectLoadBalancer_RoundRobin_Parallel(b *testing.B) {
	allocator := NewPoolAllocator(nil, StrategyRoundRobin)
	pool := createTestPool("bench-pool", "default", 3)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, err := allocator.SelectLoadBalancer(pool, 1)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}
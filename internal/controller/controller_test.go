package controller

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	networkv1 "github.com/magicsong/apollo-network-controller/api/v1"
	"github.com/magicsong/apollo-network-controller/pkg/allocator"
)

func TestApolloNetworkPoolReconciler_Reconcile(t *testing.T) {
	// Setup test scheme
	scheme := runtime.NewScheme()
	require.NoError(t, networkv1.AddToScheme(scheme))

	tests := []struct {
		name           string
		pool           *networkv1.ApolloNetworkPool
		allocations    []*networkv1.PortAllocation
		expectedResult ctrl.Result
		expectError    bool
		validate       func(t *testing.T, pool *networkv1.ApolloNetworkPool)
	}{
		{
			name: "reconcile pool with no allocations",
			pool: &networkv1.ApolloNetworkPool{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pool",
					// No namespace since ApolloNetworkPool is cluster-scoped
				},
				Spec: networkv1.ApolloNetworkPoolSpec{
					LoadBalancers: []networkv1.LoadBalancerRef{
						{ID: "lb-1", Region: "us-west-1", Provider: "test"},
						{ID: "lb-2", Region: "us-west-1", Provider: "test"},
					},
					PortRange: networkv1.PortRange{
						Min: 30000,
						Max: 30099,
					},
				},
			},
			allocations: []*networkv1.PortAllocation{},
			expectedResult: ctrl.Result{
				RequeueAfter: 5 * time.Minute,
			},
			expectError: false,
			validate: func(t *testing.T, pool *networkv1.ApolloNetworkPool) {
				require.NotNil(t, pool.Status.Allocations)
				assert.True(t, pool.Status.IsSynced)

				// Check lb-1 status
				lb1Status, exists := pool.Status.Allocations["lb-1"]
				assert.True(t, exists)
				assert.Equal(t, "Active", lb1Status.Phase)
				assert.Equal(t, int32(100), lb1Status.TotalPorts) // 30000-30099 = 100 ports
				assert.Equal(t, int32(0), lb1Status.AllocatedCount)
				assert.Equal(t, int32(100), lb1Status.AvailableCount)
				assert.Empty(t, lb1Status.AllocatedPorts)
				assert.True(t, lb1Status.IsSynced)
				assert.NotNil(t, lb1Status.LastUpdated)

				// Check lb-2 status
				lb2Status, exists := pool.Status.Allocations["lb-2"]
				assert.True(t, exists)
				assert.Equal(t, "Active", lb2Status.Phase)
				assert.Equal(t, int32(100), lb2Status.TotalPorts)
				assert.Equal(t, int32(0), lb2Status.AllocatedCount)
				assert.Equal(t, int32(100), lb2Status.AvailableCount)
				assert.Empty(t, lb2Status.AllocatedPorts)
				assert.True(t, lb2Status.IsSynced)
			},
		},
		{
			name: "reconcile pool with allocations",
			pool: &networkv1.ApolloNetworkPool{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pool",
					// No namespace since ApolloNetworkPool is cluster-scoped
				},
				Spec: networkv1.ApolloNetworkPoolSpec{
					LoadBalancers: []networkv1.LoadBalancerRef{
						{ID: "lb-1", Region: "us-west-1", Provider: "test"},
						{ID: "lb-2", Region: "us-west-1", Provider: "test"},
					},
					PortRange: networkv1.PortRange{
						Min: 30000,
						Max: 30019, // 20 ports total
						ExcludedPorts: []int32{30005, 30006}, // 2 excluded ports
					},
				},
			},
			allocations: []*networkv1.PortAllocation{
				// Allocation 1: Uses lb-1 with 2 ports
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pool-pod1",
						Namespace: "default",
						Labels: map[string]string{
							allocator.LabelPoolNameKey: "test-pool",
							allocator.LabelPodNameKey:  "pod1",
						},
					},
					Spec: networkv1.PortAllocationSpec{
						PodInfo: networkv1.PodInfo{
							Name:      "pod1",
							Namespace: "default",
						},
						PortBindings: []networkv1.PortBinding{
							{
								PodPort: 8080,
								LBPort:  30000,
								LoadBalancerRef: networkv1.LoadBalancerRef{
									ID: "lb-1",
								},
							},
							{
								PodPort: 9090,
								LBPort:  30001,
								LoadBalancerRef: networkv1.LoadBalancerRef{
									ID: "lb-1",
								},
							},
						},
					},
				},
				// Allocation 2: Uses lb-2 with 1 port
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pool-pod2",
						Namespace: "default",
						Labels: map[string]string{
							allocator.LabelPoolNameKey: "test-pool",
							allocator.LabelPodNameKey:  "pod2",
						},
					},
					Spec: networkv1.PortAllocationSpec{
						PodInfo: networkv1.PodInfo{
							Name:      "pod2",
							Namespace: "default",
						},
						PortBindings: []networkv1.PortBinding{
							{
								PodPort: 8080,
								LBPort:  30000,
								LoadBalancerRef: networkv1.LoadBalancerRef{
									ID: "lb-2",
								},
							},
						},
					},
				},
			},
			expectedResult: ctrl.Result{
				RequeueAfter: 5 * time.Minute,
			},
			expectError: false,
			validate: func(t *testing.T, pool *networkv1.ApolloNetworkPool) {
				require.NotNil(t, pool.Status.Allocations)
				assert.True(t, pool.Status.IsSynced)

				// Check lb-1 status (2 ports allocated)
				lb1Status, exists := pool.Status.Allocations["lb-1"]
				assert.True(t, exists)
				assert.Equal(t, "Active", lb1Status.Phase)
				assert.Equal(t, int32(18), lb1Status.TotalPorts) // 20 - 2 excluded = 18 ports
				assert.Equal(t, int32(2), lb1Status.AllocatedCount)
				assert.Equal(t, int32(16), lb1Status.AvailableCount) // 18 - 2 allocated = 16
				assert.Len(t, lb1Status.AllocatedPorts, 1) // 1 allocation with 2 ports
				assert.True(t, lb1Status.IsSynced)

				// Check lb-2 status (1 port allocated)
				lb2Status, exists := pool.Status.Allocations["lb-2"]
				assert.True(t, exists)
				assert.Equal(t, "Active", lb2Status.Phase)
				assert.Equal(t, int32(18), lb2Status.TotalPorts)
				assert.Equal(t, int32(1), lb2Status.AllocatedCount)
				assert.Equal(t, int32(17), lb2Status.AvailableCount) // 18 - 1 allocated = 17
				assert.Len(t, lb2Status.AllocatedPorts, 1) // 1 allocation with 1 port
				assert.True(t, lb2Status.IsSynced)
			},
		},
		{
			name: "reconcile pool with prewarming enabled",
			pool: &networkv1.ApolloNetworkPool{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pool",
					// No namespace since ApolloNetworkPool is cluster-scoped
				},
				Spec: networkv1.ApolloNetworkPoolSpec{
					LoadBalancers: []networkv1.LoadBalancerRef{
						{ID: "lb-1", Region: "us-west-1", Provider: "test"},
					},
					PortRange: networkv1.PortRange{
						Min: 30000,
						Max: 30019, // 20 ports total
					},
					PrewarmConfig: networkv1.PrewarmConfig{
						Enabled:    true,
						BufferSize: 5,
					},
				},
			},
			allocations: []*networkv1.PortAllocation{},
			expectedResult: ctrl.Result{
				RequeueAfter: 5 * time.Minute,
			},
			expectError: false,
			validate: func(t *testing.T, pool *networkv1.ApolloNetworkPool) {
				require.NotNil(t, pool.Status.Allocations)
				assert.True(t, pool.Status.IsSynced)

				// Check lb-1 status
				lb1Status, exists := pool.Status.Allocations["lb-1"]
				assert.True(t, exists)
				assert.Equal(t, "Active", lb1Status.Phase)
				assert.Equal(t, int32(20), lb1Status.TotalPorts)
				assert.Equal(t, int32(0), lb1Status.AllocatedCount)
				assert.Equal(t, int32(5), lb1Status.PrewarmedCount)
				assert.Equal(t, int32(15), lb1Status.AvailableCount) // 20 - 0 allocated - 5 prewarmed = 15
				assert.True(t, lb1Status.IsSynced)
			},
		},
		{
			name: "reconcile non-existent pool",
			pool: nil, // Pool doesn't exist
			expectedResult: ctrl.Result{},
			expectError:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup fake client
			objects := []client.Object{}
			if tt.pool != nil {
				objects = append(objects, tt.pool)
			}
			for _, alloc := range tt.allocations {
				objects = append(objects, alloc)
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				WithStatusSubresource(&networkv1.ApolloNetworkPool{}).
				Build()

			// Create reconciler
			reconciler := &ApolloNetworkPoolReconciler{
				Client: fakeClient,
				Scheme: scheme,
			}

			// Create reconcile request
			req := reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name: "test-pool",
					// No namespace since ApolloNetworkPool is cluster-scoped
				},
			}

			// Execute reconcile
			result, err := reconciler.Reconcile(context.TODO(), req)

			// Verify results
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			assert.Equal(t, tt.expectedResult, result)

			// If pool exists, validate its status
			if tt.pool != nil && tt.validate != nil {
				updatedPool := &networkv1.ApolloNetworkPool{}
				err := fakeClient.Get(context.TODO(), req.NamespacedName, updatedPool)
				require.NoError(t, err)
				tt.validate(t, updatedPool)
			}
		})
	}
}

func TestApolloNetworkPoolReconciler_calculateLBStatus(t *testing.T) {
	reconciler := &ApolloNetworkPoolReconciler{}

	pool := &networkv1.ApolloNetworkPool{
		Spec: networkv1.ApolloNetworkPoolSpec{
			PortRange: networkv1.PortRange{
				Min:           30000,
				Max:           30099,
				ExcludedPorts: []int32{30050, 30051},
			},
			PrewarmConfig: networkv1.PrewarmConfig{
				Enabled:    false,
				BufferSize: 0,
			},
		},
	}

	lbRef := &networkv1.LoadBalancerRef{
		ID:       "lb-1",
		Region:   "us-west-1",
		Provider: "test",
	}

	allocations := []networkv1.PortAllocation{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "alloc-1",
				Namespace: "default",
			},
			Spec: networkv1.PortAllocationSpec{
				PortBindings: []networkv1.PortBinding{
					{
						PodPort: 8080,
						LBPort:  30000,
						LoadBalancerRef: networkv1.LoadBalancerRef{ID: "lb-1"},
					},
					{
						PodPort: 9090,
						LBPort:  30001,
						LoadBalancerRef: networkv1.LoadBalancerRef{ID: "lb-1"},
					},
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "alloc-2",
				Namespace: "default",
			},
			Spec: networkv1.PortAllocationSpec{
				PortBindings: []networkv1.PortBinding{
					{
						PodPort: 8080,
						LBPort:  30000,
						LoadBalancerRef: networkv1.LoadBalancerRef{ID: "lb-2"}, // Different LB
					},
				},
			},
		},
	}

	status := reconciler.calculateLBStatus(pool, lbRef, allocations)

	// Verify calculated status
	assert.Equal(t, "Active", status.Phase)
	assert.Equal(t, int32(98), status.TotalPorts) // 100 - 2 excluded = 98
	assert.Equal(t, int32(2), status.AllocatedCount) // 2 ports from alloc-1
	assert.Equal(t, int32(0), status.PrewarmedCount)
	assert.Equal(t, int32(96), status.AvailableCount) // 98 - 2 = 96
	assert.Len(t, status.AllocatedPorts, 1) // Only alloc-1 uses lb-1
	assert.Equal(t, "alloc-1", status.AllocatedPorts[0].Name)
	assert.True(t, status.IsSynced)
}
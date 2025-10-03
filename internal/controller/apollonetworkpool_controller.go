/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	networkv1 "github.com/magicsong/apollo-network-controller/api/v1"
	"github.com/magicsong/apollo-network-controller/pkg/allocator"
)

// ApolloNetworkPoolReconciler reconciles a ApolloNetworkPool object
type ApolloNetworkPoolReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=network.apollo.network,resources=apollonetworkpools,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=network.apollo.network,resources=apollonetworkpools/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=network.apollo.network,resources=apollonetworkpools/finalizers,verbs=update
// +kubebuilder:rbac:groups=network.apollo.network,resources=portallocations,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.

// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/reconcile
func (r *ApolloNetworkPoolReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Fetch the ApolloNetworkPool instance
	pool := &networkv1.ApolloNetworkPool{}
	err := r.Get(ctx, req.NamespacedName, pool)
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			log.Error(err, "Unable to fetch ApolloNetworkPool")
			return ctrl.Result{}, err
		}
		// Pool was deleted, nothing to do
		return ctrl.Result{}, nil
	}

	// Update the pool status
	if err := r.updatePoolStatus(ctx, pool); err != nil {
		log.Error(err, "Failed to update pool status", "pool", pool.Name)
		return ctrl.Result{RequeueAfter: time.Minute}, err
	}

	return ctrl.Result{RequeueAfter: 10 * time.Minute}, nil
}

// updatePoolStatus calculates and updates the status of the ApolloNetworkPool
func (r *ApolloNetworkPoolReconciler) updatePoolStatus(ctx context.Context, pool *networkv1.ApolloNetworkPool) error {
	log := logf.FromContext(ctx)

	// Get all PortAllocations that belong to this pool
	allocationList := &networkv1.PortAllocationList{}
	err := r.List(ctx, allocationList, client.MatchingLabels{
		allocator.LabelPoolNameKey: pool.Name,
	})
	if err != nil {
		return fmt.Errorf("failed to list PortAllocations for pool %s: %w", pool.Name, err)
	}

	// Use retry on conflict to handle resource version conflicts
	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// Fetch the latest version of the pool
		latestPool := &networkv1.ApolloNetworkPool{}
		poolKey := client.ObjectKey{Name: pool.Name}
		// Since ApolloNetworkPool is cluster-scoped, we don't set namespace
		if err := r.Get(ctx, poolKey, latestPool); err != nil {
			return err
		}

		// Initialize status if needed
		if latestPool.Status.Allocations == nil {
			latestPool.Status.Allocations = make(map[string]networkv1.LoadBalancerAllocateStatus)
		}

		// Calculate status for each load balancer
		now := metav1.Now()
		for _, lbRef := range latestPool.Spec.LoadBalancers {
			lbStatus := r.calculateLBStatus(latestPool, &lbRef, allocationList.Items)
			lbStatus.LastUpdated = &now
			lbStatus.IsSynced = true

			latestPool.Status.Allocations[lbRef.ID] = lbStatus
		}

		// Set overall pool sync status
		latestPool.Status.IsSynced = true

		// Update the status - this might fail with conflict, triggering retry
		return r.Status().Update(ctx, latestPool)
	})

	if err != nil {
		return fmt.Errorf("failed to update pool status after retries: %w", err)
	}

	log.V(1).Info("Updated pool status", 
		"pool", pool.Name, 
		"allocations", len(allocationList.Items),
		"loadBalancers", len(pool.Spec.LoadBalancers))

	return nil
}

// calculateLBStatus calculates the status for a specific load balancer
func (r *ApolloNetworkPoolReconciler) calculateLBStatus(
	pool *networkv1.ApolloNetworkPool, 
	lbRef *networkv1.LoadBalancerRef, 
	allAllocations []networkv1.PortAllocation) networkv1.LoadBalancerAllocateStatus {
	
	// Filter allocations for this specific load balancer
	var lbAllocations []networkv1.PortAllocation
	var allocatedPorts int32
	
	for _, allocation := range allAllocations {
		// Check if any port binding uses this load balancer
		hasLBBinding := false
		for _, binding := range allocation.Spec.PortBindings {
			if binding.LoadBalancerRef.ID == lbRef.ID {
				hasLBBinding = true
				allocatedPorts++
			}
		}
		
		if hasLBBinding {
			lbAllocations = append(lbAllocations, allocation)
		}
	}

	// Calculate total available ports in the range
	totalPorts := pool.Spec.PortRange.Max - pool.Spec.PortRange.Min + 1
	totalPorts -= int32(len(pool.Spec.PortRange.ExcludedPorts))

	// Calculate prewarmed ports (implement based on your requirements)
	// For now, we'll assume no prewarming unless specified
	prewarmedCount := int32(0)
	var prewarmedPorts []int32
	if pool.Spec.PrewarmConfig.Enabled {
		prewarmedCount = pool.Spec.PrewarmConfig.BufferSize
		// TODO: Implement actual prewarming logic
	}

	// Calculate available ports
	availableCount := totalPorts - allocatedPorts - prewarmedCount
	if availableCount < 0 {
		availableCount = 0
	}

	return networkv1.LoadBalancerAllocateStatus{
		Phase:            "Active",
		AllocatedPorts:   lbAllocations,
		PrewarmedPorts:   prewarmedPorts,
		TotalPorts:       totalPorts,
		AllocatedCount:   allocatedPorts,
		PrewarmedCount:   prewarmedCount,
		AvailableCount:   availableCount,
		IsSynced:         true,
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *ApolloNetworkPoolReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&networkv1.ApolloNetworkPool{}).
		Watches(
			&networkv1.PortAllocation{},
			handler.EnqueueRequestsFromMapFunc(r.mapPortAllocationToPool),
		).
		Named("apollonetworkpool").
		Complete(r)
}

// mapPortAllocationToPool maps PortAllocation events to ApolloNetworkPool reconcile requests
func (r *ApolloNetworkPoolReconciler) mapPortAllocationToPool(ctx context.Context, obj client.Object) []reconcile.Request {
	allocation, ok := obj.(*networkv1.PortAllocation)
	if !ok {
		return nil
	}

	// Get the pool name from the allocation's labels
	poolName := allocation.Labels[allocator.LabelPoolNameKey]
	if poolName == "" {
		return nil
	}

	// Return reconcile request for the pool (cluster-scoped, no namespace)
	return []reconcile.Request{
		{
			NamespacedName: types.NamespacedName{
				Name: poolName,
			},
		},
	}
}

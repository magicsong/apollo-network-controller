package allocator

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	apollov1 "github.com/magicsong/apollo-network-controller/api/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ReleasePort releases a port allocation using patch
func (pa *PoolAllocator) ReleasePort(ctx context.Context, poolName, namespace string,
	podName, podNamespace string) error {

	// Use pool-level mutex
	poolMutex := pa.getPoolMutex(poolName, namespace)
	poolMutex.Lock()
	defer poolMutex.Unlock()

	// Get current pool
	pool := &apollov1.ApolloNetworkPool{}
	poolKey := types.NamespacedName{Name: poolName, Namespace: namespace}

	if err := pa.client.Get(ctx, poolKey, pool); err != nil {
		return fmt.Errorf("failed to get pool %s: %w", poolName, err)
	}

	// Find and remove allocation
	var targetLBID string
	var updatedAllocations []apollov1.PortAllocation
	found := false

	if pool.Status.Allocations != nil {
		for lbID, lbStatus := range pool.Status.Allocations {
			for _, alloc := range lbStatus.AllocatedPorts {
				if alloc.Spec.PodInfo.Name == podName && alloc.Spec.PodInfo.Namespace == podNamespace {
					targetLBID = lbID
					found = true
					// Skip this allocation (remove it)
					continue
				}
				updatedAllocations = append(updatedAllocations, alloc)
			}
			if found {
				break
			}
		}
	}

	if !found {
		return fmt.Errorf("port allocation not found for pod %s/%s", podNamespace, podName)
	}

	// Calculate updated counters
	totalPorts := pool.Spec.PortRange.Max - pool.Spec.PortRange.Min + 1 - int32(len(pool.Spec.PortRange.ExcludedPorts))
	currentLBStatus := pool.Status.Allocations[targetLBID]

	// Update the status with removed allocation
	currentLBStatus.AllocatedPorts = updatedAllocations
	// Count ports manually since GetPortCount method doesn't exist
	portCount := int32(0)
	for _, alloc := range currentLBStatus.AllocatedPorts {
		portCount += int32(len(alloc.Spec.PortBindings))
	}
	currentLBStatus.AllocatedCount = portCount
	currentLBStatus.AvailableCount = totalPorts - currentLBStatus.AllocatedCount - currentLBStatus.PrewarmedCount

	// Update timestamp
	now := metav1.Time{Time: time.Now()}
	currentLBStatus.LastUpdated = &now

	// Create patch
	patch := map[string]interface{}{
		"status": map[string]interface{}{
			"allocations": map[string]interface{}{
				targetLBID: currentLBStatus,
			},
		},
	}

	patchBytes, err := json.Marshal(patch)
	if err != nil {
		return fmt.Errorf("failed to marshal patch: %w", err)
	}

	// Apply patch
	return pa.client.Status().Patch(ctx, pool, client.RawPatch(types.StrategicMergePatchType, patchBytes))
}

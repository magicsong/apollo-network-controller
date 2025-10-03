package allocator

import (
	"context"
	"fmt"

	apollov1 "github.com/magicsong/apollo-network-controller/api/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ReleasePort releases a port allocation by deleting the PortAllocation resource
func (pa *PoolAllocator) ReleasePort(ctx context.Context, poolName, namespace string,
	podName, podNamespace string) error {

	// Use pool-level mutex to prevent race conditions
	poolMutex := pa.getPoolMutex(poolName, namespace)
	poolMutex.Lock()
	defer poolMutex.Unlock()

	// Construct the expected allocation name (consistent with AllocatePort naming)
	allocationName := fmt.Sprintf("%s-%s", poolName, podName)
	allocationKey := types.NamespacedName{
		Name:      allocationName,
		Namespace: namespace,
	}

	// Get the allocation resource
	allocation := &apollov1.PortAllocation{}
	err := pa.client.Get(ctx, allocationKey, allocation)
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			return fmt.Errorf("failed to get allocation %s: %w", allocationName, err)
		}
		// Allocation not found
		return fmt.Errorf("port allocation not found for pod %s/%s in pool %s", podNamespace, podName, poolName)
	}

	// Verify the allocation belongs to the correct pod and namespace
	if allocation.Spec.PodInfo.Name != podName || allocation.Spec.PodInfo.Namespace != podNamespace {
		return fmt.Errorf("allocation %s does not belong to pod %s/%s", allocationName, podNamespace, podName)
	}

	// Verify the allocation belongs to the correct pool
	if allocation.Labels[LabelPoolNameKey] != poolName {
		return fmt.Errorf("allocation %s does not belong to pool %s", allocationName, poolName)
	}

	// Check if allocation is already being deleted
	if allocation.DeletionTimestamp != nil {
		return fmt.Errorf("allocation %s is already being deleted", allocationName)
	}

	// Delete the allocation resource
	if err := pa.client.Delete(ctx, allocation); err != nil {
		return fmt.Errorf("failed to delete allocation %s: %w", allocationName, err)
	}

	return nil
}

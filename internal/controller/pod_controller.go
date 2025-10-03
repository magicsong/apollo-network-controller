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
	"encoding/json"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	apollov1 "github.com/magicsong/apollo-network-controller/api/v1"
	"github.com/magicsong/apollo-network-controller/pkg/allocator"
)

const (
	// ApolloNetworkFinalizer is the finalizer name for Apollo Network port allocation
	ApolloNetworkFinalizer = "apollo.magicsong.io/port-allocation"
)

// PodReconciler reconciles a Pod object
type PodReconciler struct {
	client.Client
	Scheme     *runtime.Scheme
	Allocator  *allocator.PoolAllocator
}

// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=pods/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core,resources=pods/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// Optimizations for large scale (4000+ Pods):
// 1. Memory efficient Pod processing - only process managed Pods
// 2. Finalizer management for controlled cleanup
// 3. Multi-pool support for better resource distribution
func (r *PodReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Memory optimization: Use lightweight Pod retrieval
	pod := &corev1.Pod{}
	err := r.Get(ctx, req.NamespacedName, pod)
	if err != nil {
		if errors.IsNotFound(err) {
			// Pod was deleted, nothing to do
			log.V(2).Info("Pod not found, likely deleted", "pod", req.NamespacedName)
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get Pod", "pod", req.NamespacedName)
		return ctrl.Result{}, err
	}

	// Early exit for unmanaged Pods to save memory and CPU
	if !r.shouldManagePod(pod) {
		log.V(2).Info("Pod not marked for port allocation, skipping", "pod", pod.Name)
		return ctrl.Result{}, nil
	}

	log.V(1).Info("Reconciling managed Pod", "pod", pod.Name, "namespace", pod.Namespace, "phase", pod.Status.Phase)

	// Handle Pod deletion with finalizer
	if pod.DeletionTimestamp != nil {
		log.Info("Pod is being deleted, releasing ports", "pod", pod.Name, "namespace", pod.Namespace)
		return r.handlePodDeletion(ctx, pod)
	}

	// Pod is active, ensure ports are allocated and finalizer is set
	log.Info("Pod is active, ensuring port allocation", "pod", pod.Name, "namespace", pod.Namespace)
	return r.handlePodAllocation(ctx, pod)
}

// shouldManagePod checks if the Pod has our annotations and should be managed
func (r *PodReconciler) shouldManagePod(pod *corev1.Pod) bool {
	if pod.Annotations == nil {
		return false
	}
	
	// Check if allocation is enabled
	enableAlloc, exists := pod.Annotations[allocator.PodAnnotationEnableAllocKey]
	if !exists || enableAlloc != "true" {
		return false
	}
	
	// Check if network pool(s) is specified (supports both single and multi-pool)
	_, exists = pod.Annotations[allocator.PodAnnotationNetworkPoolKey]
	return exists
}

// parseNetworkPools parses network pools from Pod annotation (supports multi-pool)
// Supports formats:
// - Single pool: "pool1" 
// - Multi pool: "pool1,pool2,pool3" or JSON array ["pool1","pool2","pool3"]
func (r *PodReconciler) parseNetworkPools(pod *corev1.Pod) ([]string, error) {
	poolsAnnotation, exists := pod.Annotations[allocator.PodAnnotationNetworkPoolKey]
	if !exists || poolsAnnotation == "" {
		return nil, fmt.Errorf("network pool annotation is missing or empty")
	}

	var rawPools []string

	// Try JSON array format first (if it looks like JSON)
	if strings.HasPrefix(poolsAnnotation, "[") && strings.HasSuffix(poolsAnnotation, "]") {
		if err := json.Unmarshal([]byte(poolsAnnotation), &rawPools); err != nil {
			// If it looks like JSON but fails to parse, return error (don't fallback to comma-separated)
			return nil, fmt.Errorf("failed to parse pools JSON array: %w", err)
		}
	} else {
		// Fallback to comma-separated format
		rawPools = strings.Split(poolsAnnotation, ",")
	}

	// Clean, validate and deduplicate pools
	poolsSet := make(map[string]struct{})
	var validPools []string
	
	for _, pool := range rawPools {
		pool = strings.TrimSpace(pool)
		if pool != "" {
			// Use map to deduplicate pools while preserving order
			if _, exists := poolsSet[pool]; !exists {
				poolsSet[pool] = struct{}{}
				validPools = append(validPools, pool)
			}
		}
	}

	if len(validPools) == 0 {
		return nil, fmt.Errorf("no valid pools found in annotation")
	}

	return validPools, nil
}

// handlePodDeletion handles port release when Pod is being deleted
// Uses finalizer to ensure proper cleanup
func (r *PodReconciler) handlePodDeletion(ctx context.Context, pod *corev1.Pod) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	
	// Check if our finalizer is present
	if !controllerutil.ContainsFinalizer(pod, ApolloNetworkFinalizer) {
		log.V(1).Info("Pod does not have our finalizer, skipping cleanup", "pod", pod.Name)
		return ctrl.Result{}, nil
	}

	// Parse network pools (support multi-pool)
	pools, err := r.parseNetworkPools(pod)
	if err != nil {
		log.Error(err, "Failed to parse network pools for deletion", "pod", pod.Name)
		// Remove finalizer even if we can't parse pools to avoid blocking deletion
		return r.removeFinalizer(ctx, pod)
	}

	// Release ports for all pools
	var releaseErrors []error
	for _, poolName := range pools {
		log.V(1).Info("Releasing ports for pool", "pod", pod.Name, "pool", poolName)
		
		// Note: ApolloNetworkPool is cluster-scoped, so we use empty namespace
		err := r.Allocator.ReleasePort(ctx, poolName, "", pod.Name, pod.Namespace)
		if err != nil {
			log.Error(err, "Failed to release ports for pool", "pod", pod.Name, "pool", poolName)
			releaseErrors = append(releaseErrors, fmt.Errorf("pool %s: %w", poolName, err))
		} else {
			log.Info("Successfully released ports for pool", "pod", pod.Name, "pool", poolName)
		}
	}

	// Remove finalizer to allow Pod deletion
	// Note: We remove finalizer even if some releases failed to avoid blocking Pod deletion
	if len(releaseErrors) > 0 {
		log.Error(fmt.Errorf("some port releases failed: %v", releaseErrors), 
			"Port release had errors, but removing finalizer to allow Pod deletion", 
			"pod", pod.Name)
	}

	return r.removeFinalizer(ctx, pod)
}

// handlePodAllocation handles port allocation for active Pods
// Supports multi-pool allocation and adds finalizer for controlled cleanup  
func (r *PodReconciler) handlePodAllocation(ctx context.Context, pod *corev1.Pod) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	
	// Parse network pools
	pools, err := r.parseNetworkPools(pod)
	if err != nil {
		log.Error(err, "Failed to parse network pools", "pod", pod.Name)
		return ctrl.Result{}, err
	}

	// Parse container ports from annotation
	containerPorts, err := r.parseContainerPorts(pod)
	if err != nil {
		log.Error(err, "Failed to parse container ports", "pod", pod.Name)
		return ctrl.Result{}, err
	}

	log.V(1).Info("Parsed container ports for pod", "pod", pod.Name, "ports", len(containerPorts), "pools", len(pools))

	// Check if allocations already exist for all pools
	allAllocationsExist := true
	for _, poolName := range pools {
		existingAllocation, err := r.findExistingAllocation(ctx, poolName, pod.Name, pod.Namespace)
		if err != nil {
			log.Error(err, "Failed to check existing allocation", "pod", pod.Name, "pool", poolName)
			return ctrl.Result{}, err
		}

		if existingAllocation == nil {
			allAllocationsExist = false
			break
		}
	}

	if allAllocationsExist {
		log.V(1).Info("Port allocations already exist for all pools", "pod", pod.Name, "pools", len(pools))
		// Ensure finalizer is set even if allocations exist
		if err := r.ensureFinalizer(ctx, pod); err != nil {
			log.Error(err, "Failed to ensure finalizer", "pod", pod.Name)
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// Allocate ports for each pool that doesn't have allocation yet
	var allocationErrors []error
	successfulAllocations := 0
	
	for _, poolName := range pools {
		// Check if this specific pool already has allocation
		existingAllocation, err := r.findExistingAllocation(ctx, poolName, pod.Name, pod.Namespace)
		if err != nil {
			log.Error(err, "Failed to check existing allocation for pool", "pod", pod.Name, "pool", poolName)
			allocationErrors = append(allocationErrors, fmt.Errorf("pool %s: %w", poolName, err))
			continue
		}

		if existingAllocation != nil {
			log.V(1).Info("Port allocation already exists for pool", "pod", pod.Name, "pool", poolName)
			successfulAllocations++
			continue
		}

		// Allocate ports for this pool
		// Note: ApolloNetworkPool is cluster-scoped, so we use empty namespace
		allocation, err := r.Allocator.AllocatePort(ctx, poolName, "", pod.Name, pod.Namespace, containerPorts)
		if err != nil {
			log.Error(err, "Failed to allocate ports for pool", "pod", pod.Name, "pool", poolName)
			allocationErrors = append(allocationErrors, fmt.Errorf("pool %s: %w", poolName, err))
			continue
		}

		log.Info("Successfully allocated ports for pool", "pod", pod.Name, "pool", poolName, 
			"allocation", allocation.Name, "bindings", len(allocation.Spec.PortBindings))
		successfulAllocations++
	}

	// Add finalizer if we have at least one successful allocation
	if successfulAllocations > 0 {
		if err := r.ensureFinalizer(ctx, pod); err != nil {
			log.Error(err, "Failed to add finalizer after successful allocation", "pod", pod.Name)
			return ctrl.Result{}, err
		}
	}

	// Return error if all allocations failed
	if len(allocationErrors) > 0 && successfulAllocations == 0 {
		return ctrl.Result{}, fmt.Errorf("all port allocations failed: %v", allocationErrors)
	}

	// Log partial failures but don't return error (some allocations succeeded)
	if len(allocationErrors) > 0 {
		log.Error(fmt.Errorf("some allocations failed: %v", allocationErrors), 
			"Partial allocation failure", "pod", pod.Name, 
			"successful", successfulAllocations, "failed", len(allocationErrors))
	}

	return ctrl.Result{}, nil
}

// parseContainerPorts parses container ports with priority given to annotation
// Priority order:
// 1. Pod annotation (apollo.magicsong.io/container-ports) - highest priority
// 2. Pod spec containers ports - second priority  
// 3. Empty list - if neither annotation nor spec ports exist
func (r *PodReconciler) parseContainerPorts(pod *corev1.Pod) ([]apollov1.PodPortAllocation, error) {
	// Priority 1: Try to get from annotation (most reliable source)
	if containerPortsJSON, exists := pod.Annotations[allocator.PodAnnotationContainerPortsKey]; exists && containerPortsJSON != "" {
		var containerPorts []apollov1.PodPortAllocation
		if err := json.Unmarshal([]byte(containerPortsJSON), &containerPorts); err != nil {
			return nil, fmt.Errorf("failed to unmarshal container ports JSON from annotation: %w", err)
		}
		// Successfully parsed from annotation
		return containerPorts, nil
	}

	// Priority 2: Fallback to Pod spec containers (if available)
	// Note: Containers might not declare ports even if they expose them
	var containerPorts []apollov1.PodPortAllocation
	for _, container := range pod.Spec.Containers {
		for _, port := range container.Ports {
			containerPorts = append(containerPorts, apollov1.PodPortAllocation{
				PodPort:  port.ContainerPort,
				Protocol: apollov1.Protocol(port.Protocol),
				PortName: port.Name,
			})
		}
	}

	// Priority 3: Return empty list if no ports found
	// This allows for pods that may expose ports dynamically or don't need LB allocation
	return containerPorts, nil
}



// ensureFinalizer adds our finalizer to the Pod using patch for efficiency
func (r *PodReconciler) ensureFinalizer(ctx context.Context, pod *corev1.Pod) error {
	if controllerutil.ContainsFinalizer(pod, ApolloNetworkFinalizer) {
		return nil // Finalizer already exists
	}

	log := logf.FromContext(ctx)
	log.V(1).Info("Adding finalizer to Pod", "pod", pod.Name, "finalizer", ApolloNetworkFinalizer)

	// Use patch for memory efficiency (important for 4000+ Pod scenarios)
	patch := client.MergeFrom(pod.DeepCopy())
	controllerutil.AddFinalizer(pod, ApolloNetworkFinalizer)
	
	return r.Patch(ctx, pod, patch)
}

// removeFinalizer removes our finalizer from the Pod using patch for efficiency
func (r *PodReconciler) removeFinalizer(ctx context.Context, pod *corev1.Pod) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(pod, ApolloNetworkFinalizer) {
		return ctrl.Result{}, nil // Finalizer doesn't exist
	}

	log := logf.FromContext(ctx)
	log.V(1).Info("Removing finalizer from Pod", "pod", pod.Name, "finalizer", ApolloNetworkFinalizer)

	// Use patch for memory efficiency
	patch := client.MergeFrom(pod.DeepCopy())
	controllerutil.RemoveFinalizer(pod, ApolloNetworkFinalizer)
	
	err := r.Patch(ctx, pod, patch)
	return ctrl.Result{}, err
}

// findExistingAllocation checks if there's already a port allocation for this pod in the specified pool
// Memory optimization: Use labels for efficient querying
func (r *PodReconciler) findExistingAllocation(ctx context.Context, poolName, podName, podNamespace string) (*apollov1.PortAllocation, error) {
	// List allocations with specific labels (more efficient than listing all)
	allocationList := &apollov1.PortAllocationList{}
	err := r.List(ctx, allocationList, client.MatchingLabels{
		allocator.LabelPoolNameKey: poolName,
		allocator.LabelPodNameKey:  podName,
	}, client.Limit(100))
	if err != nil {
		return nil, err
	}

	// Find allocation for this specific pod
	for _, allocation := range allocationList.Items {
		if allocation.Spec.PodInfo.Name == podName && allocation.Spec.PodInfo.Namespace == podNamespace {
			return &allocation, nil
		}
	}

	return nil, nil
}

// podTransform reduces memory usage by keeping only essential Pod fields in cache
// This is crucial for large scale deployments (4000+ Pods)
func PodTransform(obj interface{}) (interface{}, error) {
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		return obj, nil
	}
	
	// Create a new Pod with only essential fields to reduce memory usage
	transformedPod := &corev1.Pod{
		TypeMeta: pod.TypeMeta,
		ObjectMeta: metav1.ObjectMeta{
			Name:              pod.Name,
			Namespace:         pod.Namespace,
			UID:               pod.UID,
			ResourceVersion:   pod.ResourceVersion,
			Generation:        pod.Generation,
			DeletionTimestamp: pod.DeletionTimestamp,
			Finalizers:        pod.Finalizers,
			// Keep only our relevant annotations
			Annotations:       filterRelevantAnnotations(pod.Annotations),
			Labels:            pod.Labels, // Keep labels for potential filtering needs
		},
		Spec: corev1.PodSpec{
			// Only keep container ports info, discard other spec details
			Containers: transformContainers(pod.Spec.Containers),
		},
		Status: corev1.PodStatus{
			Phase: pod.Status.Phase, // Keep phase for lifecycle management
		},
	}
	
	return transformedPod, nil
}

// filterRelevantAnnotations keeps only Apollo-related annotations to reduce memory
func filterRelevantAnnotations(annotations map[string]string) map[string]string {
	if annotations == nil {
		return nil
	}
	
	relevantAnnotations := make(map[string]string)
	
	// Keep only our specific annotations
	if val, exists := annotations[allocator.PodAnnotationEnableAllocKey]; exists {
		relevantAnnotations[allocator.PodAnnotationEnableAllocKey] = val
	}
	if val, exists := annotations[allocator.PodAnnotationNetworkPoolKey]; exists {
		relevantAnnotations[allocator.PodAnnotationNetworkPoolKey] = val
	}
	if val, exists := annotations[allocator.PodAnnotationContainerPortsKey]; exists {
		relevantAnnotations[allocator.PodAnnotationContainerPortsKey] = val
	}
	
	// Return nil if no relevant annotations found to save memory
	if len(relevantAnnotations) == 0 {
		return nil
	}
	
	return relevantAnnotations
}

// transformContainers keeps only port information from containers to reduce memory
func transformContainers(containers []corev1.Container) []corev1.Container {
	if len(containers) == 0 {
		return nil
	}
	
	transformedContainers := make([]corev1.Container, len(containers))
	for i, container := range containers {
		transformedContainers[i] = corev1.Container{
			Name:  container.Name,
			Ports: container.Ports, // Only keep ports, discard other container details
		}
	}
	
	return transformedContainers
}

// SetupWithManager sets up the controller with the Manager.
func (r *PodReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Pod{}).
		Named("pod").
		WithOptions(controller.Options{
			MaxConcurrentReconciles: 10, // Allow parallel processing for better throughput
		}).
		Complete(r)
}

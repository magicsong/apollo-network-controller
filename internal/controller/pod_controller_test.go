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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	apollov1 "github.com/magicsong/apollo-network-controller/api/v1"
	"github.com/magicsong/apollo-network-controller/pkg/allocator"
)

var _ = Describe("Pod Controller", func() {
	Context("When reconciling a Pod resource", func() {
		const (
			podName      = "test-pod"
			podNamespace = "test-namespace"
			poolName     = "test-pool"
		)

		var (
			ctx        context.Context
			pod        *corev1.Pod
			pool       *apollov1.ApolloNetworkPool
			reconciler *PodReconciler
		)

		BeforeEach(func() {
			ctx = context.Background()

			// Create test pool
			pool = &apollov1.ApolloNetworkPool{
				ObjectMeta: metav1.ObjectMeta{
					Name: poolName,
				},
				Spec: apollov1.ApolloNetworkPoolSpec{
					LoadBalancers: []apollov1.LoadBalancerRef{
						{
							ID:       "test-lb-1",
							Region:   "us-west-1",
							Provider: "test-provider",
						},
					},
					ISPLineType: apollov1.ISPLineTypeBGP,
					PortRange: apollov1.PortRange{
						Min: 30000,
						Max: 30099,
					},
				},
			}
			Expect(k8sClient.Create(ctx, pool)).To(Succeed())

			// Create test namespace
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: podNamespace,
				},
			}
			Expect(k8sClient.Create(ctx, ns)).To(Succeed())

			// Setup reconciler
			poolAllocator := allocator.NewPoolAllocator(k8sClient, allocator.StrategyRoundRobin)
			reconciler = &PodReconciler{
				Client:    k8sClient,
				Scheme:    k8sClient.Scheme(),
				Allocator: poolAllocator,
			}
		})

		AfterEach(func() {
			// Clean up resources
			if pod != nil {
				Expect(k8sClient.Delete(ctx, pod)).To(Succeed())
			}
			Expect(k8sClient.Delete(ctx, pool)).To(Succeed())
			
			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: podNamespace}}
			Expect(k8sClient.Delete(ctx, ns)).To(Succeed())
		})

		It("should skip Pods without Apollo annotations", func() {
			pod = &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      podName,
					Namespace: podNamespace,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test-container",
							Image: "test-image",
							Ports: []corev1.ContainerPort{
								{ContainerPort: 8080, Protocol: corev1.ProtocolTCP},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, pod)).To(Succeed())

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      podName,
					Namespace: podNamespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			// Should not create any PortAllocation
			allocationList := &apollov1.PortAllocationList{}
			Expect(k8sClient.List(ctx, allocationList)).To(Succeed())
			Expect(allocationList.Items).To(BeEmpty())
		})

		It("should allocate ports for Pods with Apollo annotations", func() {
			containerPorts := []apollov1.PodPortAllocation{
				{PodPort: 8080, Protocol: apollov1.ProtocolTCP, PortName: "http"},
				{PodPort: 9090, Protocol: apollov1.ProtocolTCP, PortName: "metrics"},
			}
			containerPortsJSON, _ := json.Marshal(containerPorts)

			pod = &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      podName,
					Namespace: podNamespace,
					Annotations: map[string]string{
						allocator.PodAnnotationEnableAllocKey:     "true",
						allocator.PodAnnotationNetworkPoolKey:     poolName,
						allocator.PodAnnotationContainerPortsKey: string(containerPortsJSON),
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test-container",
							Image: "test-image",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, pod)).To(Succeed())

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      podName,
					Namespace: podNamespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			// Should create PortAllocation
			Eventually(func() []apollov1.PortAllocation {
				allocationList := &apollov1.PortAllocationList{}
				_ = k8sClient.List(ctx, allocationList)
				return allocationList.Items
			}, time.Second*5).Should(HaveLen(1))

			allocationList := &apollov1.PortAllocationList{}
			Expect(k8sClient.List(ctx, allocationList)).To(Succeed())
			allocation := allocationList.Items[0]

			Expect(allocation.Spec.PodInfo.Name).To(Equal(podName))
			Expect(allocation.Spec.PodInfo.Namespace).To(Equal(podNamespace))
			Expect(allocation.Spec.PortBindings).To(HaveLen(2))
		})

		It("should not duplicate allocations for existing pods", func() {
			containerPorts := []apollov1.PodPortAllocation{
				{PodPort: 8080, Protocol: apollov1.ProtocolTCP, PortName: "http"},
			}
			containerPortsJSON, _ := json.Marshal(containerPorts)

			pod = &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      podName,
					Namespace: podNamespace,
					Annotations: map[string]string{
						allocator.PodAnnotationEnableAllocKey:     "true",
						allocator.PodAnnotationNetworkPoolKey:     poolName,
						allocator.PodAnnotationContainerPortsKey: string(containerPortsJSON),
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test-container",
							Image: "test-image",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, pod)).To(Succeed())

			// First reconcile
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      podName,
					Namespace: podNamespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			// Second reconcile (should not create duplicate)
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      podName,
					Namespace: podNamespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			// Should still have only one allocation
			allocationList := &apollov1.PortAllocationList{}
			Expect(k8sClient.List(ctx, allocationList)).To(Succeed())
			Expect(allocationList.Items).To(HaveLen(1))
		})

		It("should release ports when Pod is being deleted", func() {
			containerPorts := []apollov1.PodPortAllocation{
				{PodPort: 8080, Protocol: apollov1.ProtocolTCP, PortName: "http"},
			}
			containerPortsJSON, _ := json.Marshal(containerPorts)

			pod = &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      podName,
					Namespace: podNamespace,
					Annotations: map[string]string{
						allocator.PodAnnotationEnableAllocKey:     "true",
						allocator.PodAnnotationNetworkPoolKey:     poolName,
						allocator.PodAnnotationContainerPortsKey: string(containerPortsJSON),
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test-container",
							Image: "test-image",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, pod)).To(Succeed())

			// First allocate ports
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      podName,
					Namespace: podNamespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			// Wait for allocation to be created
			Eventually(func() []apollov1.PortAllocation {
				allocationList := &apollov1.PortAllocationList{}
				_ = k8sClient.List(ctx, allocationList)
				return allocationList.Items
			}, time.Second*5).Should(HaveLen(1))

			// Mark pod for deletion
			now := metav1.Now()
			pod.DeletionTimestamp = &now
			Expect(k8sClient.Update(ctx, pod)).To(Succeed())

			// Reconcile for deletion
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      podName,
					Namespace: podNamespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			// Eventually allocation should be deleted
			Eventually(func() []apollov1.PortAllocation {
				allocationList := &apollov1.PortAllocationList{}
				_ = k8sClient.List(ctx, allocationList)
				return allocationList.Items
			}, time.Second*5).Should(BeEmpty())
		})

		It("should parse container ports from Pod spec when annotation is missing", func() {
			pod = &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      podName,
					Namespace: podNamespace,
					Annotations: map[string]string{
						allocator.PodAnnotationEnableAllocKey: "true",
						allocator.PodAnnotationNetworkPoolKey: poolName,
						// No container ports annotation
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test-container",
							Image: "test-image",
							Ports: []corev1.ContainerPort{
								{ContainerPort: 8080, Protocol: corev1.ProtocolTCP, Name: "http"},
								{ContainerPort: 9090, Protocol: corev1.ProtocolTCP, Name: "metrics"},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, pod)).To(Succeed())

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      podName,
					Namespace: podNamespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			// Should create PortAllocation with ports from Pod spec
			Eventually(func() []apollov1.PortAllocation {
				allocationList := &apollov1.PortAllocationList{}
				_ = k8sClient.List(ctx, allocationList)
				return allocationList.Items
			}, time.Second*5).Should(HaveLen(1))

			allocationList := &apollov1.PortAllocationList{}
			Expect(k8sClient.List(ctx, allocationList)).To(Succeed())
			allocation := allocationList.Items[0]

			Expect(allocation.Spec.PortBindings).To(HaveLen(2))
			Expect(allocation.Spec.PortBindings[0].PodPort).To(Equal(int32(8080)))
			Expect(allocation.Spec.PortBindings[1].PodPort).To(Equal(int32(9090)))
		})
	})
})

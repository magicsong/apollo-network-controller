package controller

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/magicsong/apollo-network-controller/pkg/allocator"
)

func TestPodTransform(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected func(*testing.T, interface{})
	}{
		{
			name: "Transform full Pod to minimal Pod",
			input: &corev1.Pod{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "Pod",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-namespace",
					UID:       "test-uid",
					Annotations: map[string]string{
						allocator.PodAnnotationEnableAllocKey:       "true",
						allocator.PodAnnotationNetworkPoolKey:       "test-pool",
						allocator.PodAnnotationContainerPortsKey:    `[{"podPort":8080,"protocol":"TCP","portName":"http"}]`,
						"irrelevant.annotation.com/key":            "value",
						"another.irrelevant.annotation.com/key2":   "value2",
					},
					Labels: map[string]string{
						"app": "test-app",
					},
					Finalizers: []string{ApolloNetworkFinalizer},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "container1",
							Image: "nginx:latest",
							Ports: []corev1.ContainerPort{
								{
									Name:          "http",
									ContainerPort: 8080,
									Protocol:      "TCP",
								},
							},
							Env: []corev1.EnvVar{
								{Name: "ENV1", Value: "value1"},
								{Name: "ENV2", Value: "value2"},
							},
							Resources: corev1.ResourceRequirements{
								// Large resource specifications...
							},
						},
						{
							Name:  "container2",
							Image: "redis:latest",
							Ports: []corev1.ContainerPort{
								{
									Name:          "redis",
									ContainerPort: 6379,
									Protocol:      "TCP",
								},
							},
						},
					},
					NodeName: "test-node",
					RestartPolicy: corev1.RestartPolicyAlways,
					// ... many other fields that we don't need
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
					Conditions: []corev1.PodCondition{
						{Type: corev1.PodReady, Status: corev1.ConditionTrue},
					},
					ContainerStatuses: []corev1.ContainerStatus{
						// Container status details...
					},
				},
			},
			expected: func(t *testing.T, result interface{}) {
				pod, ok := result.(*corev1.Pod)
				require.True(t, ok, "Result should be a Pod")

				// Verify essential metadata is preserved
				assert.Equal(t, "test-pod", pod.Name)
				assert.Equal(t, "test-namespace", pod.Namespace)
				assert.Equal(t, "test-uid", string(pod.UID))
				assert.Equal(t, []string{ApolloNetworkFinalizer}, pod.Finalizers)

				// Verify only relevant annotations are kept
				assert.Len(t, pod.Annotations, 3, "Should only keep Apollo annotations")
				assert.Equal(t, "true", pod.Annotations[allocator.PodAnnotationEnableAllocKey])
				assert.Equal(t, "test-pool", pod.Annotations[allocator.PodAnnotationNetworkPoolKey])
				assert.Equal(t, `[{"podPort":8080,"protocol":"TCP","portName":"http"}]`, pod.Annotations[allocator.PodAnnotationContainerPortsKey])
				assert.NotContains(t, pod.Annotations, "irrelevant.annotation.com/key")

				// Verify labels are preserved
				assert.Equal(t, map[string]string{"app": "test-app"}, pod.Labels)

				// Verify only container names and ports are kept
				require.Len(t, pod.Spec.Containers, 2)
				
				container1 := pod.Spec.Containers[0]
				assert.Equal(t, "container1", container1.Name)
				assert.Len(t, container1.Ports, 1)
				assert.Equal(t, "http", container1.Ports[0].Name)
				assert.Equal(t, int32(8080), container1.Ports[0].ContainerPort)
				// Verify other fields are not preserved
				assert.Empty(t, container1.Image, "Image should not be preserved")
				assert.Empty(t, container1.Env, "Environment variables should not be preserved")

				container2 := pod.Spec.Containers[1]
				assert.Equal(t, "container2", container2.Name)
				assert.Len(t, container2.Ports, 1)
				assert.Equal(t, "redis", container2.Ports[0].Name)

				// Verify only Phase is kept from status
				assert.Equal(t, corev1.PodRunning, pod.Status.Phase)
				assert.Empty(t, pod.Status.Conditions, "Conditions should not be preserved")
				assert.Empty(t, pod.Status.ContainerStatuses, "Container statuses should not be preserved")

				// Verify other spec fields are not preserved
				assert.Empty(t, pod.Spec.NodeName, "NodeName should not be preserved")
				assert.Empty(t, pod.Spec.RestartPolicy, "RestartPolicy should not be preserved")
			},
		},
		{
			name: "Handle Pod with no annotations",
			input: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "no-annotations-pod",
					Namespace: "test-namespace",
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "container1"},
					},
				},
			},
			expected: func(t *testing.T, result interface{}) {
				pod, ok := result.(*corev1.Pod)
				require.True(t, ok)
				assert.Equal(t, "no-annotations-pod", pod.Name)
				assert.Nil(t, pod.Annotations, "Should return nil for no relevant annotations")
				assert.Len(t, pod.Spec.Containers, 1)
				assert.Equal(t, "container1", pod.Spec.Containers[0].Name)
			},
		},
		{
			name: "Handle Pod with no containers",
			input: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "no-containers-pod",
					Namespace: "test-namespace",
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{},
				},
			},
			expected: func(t *testing.T, result interface{}) {
				pod, ok := result.(*corev1.Pod)
				require.True(t, ok)
				assert.Equal(t, "no-containers-pod", pod.Name)
				assert.Nil(t, pod.Spec.Containers, "Should return nil for empty containers")
			},
		},
		{
			name:  "Handle non-Pod object",
			input: &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "test-service"}},
			expected: func(t *testing.T, result interface{}) {
				service, ok := result.(*corev1.Service)
				require.True(t, ok, "Non-Pod objects should be returned unchanged")
				assert.Equal(t, "test-service", service.Name)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := PodTransform(tt.input)
			require.NoError(t, err, "Transform should not return error")
			tt.expected(t, result)
		})
	}
}

func TestFilterRelevantAnnotations(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]string
		expected map[string]string
	}{
		{
			name: "Filter keeps only Apollo annotations",
			input: map[string]string{
				allocator.PodAnnotationEnableAllocKey:       "true",
				allocator.PodAnnotationNetworkPoolKey:       "test-pool",
				allocator.PodAnnotationContainerPortsKey:    `[{"podPort":8080}]`,
				"kubernetes.io/created-by":                  "controller",
				"app.kubernetes.io/name":                    "test-app",
				"prometheus.io/scrape":                      "true",
			},
			expected: map[string]string{
				allocator.PodAnnotationEnableAllocKey:    "true",
				allocator.PodAnnotationNetworkPoolKey:    "test-pool",  
				allocator.PodAnnotationContainerPortsKey: `[{"podPort":8080}]`,
			},
		},
		{
			name: "Filter with partial Apollo annotations",
			input: map[string]string{
				allocator.PodAnnotationEnableAllocKey: "true",
				"other.annotation.com/key":            "value",
			},
			expected: map[string]string{
				allocator.PodAnnotationEnableAllocKey: "true",
			},
		},
		{
			name:     "Filter with no Apollo annotations",
			input:    map[string]string{"other.annotation.com/key": "value"},
			expected: nil,
		},
		{
			name:     "Filter with nil input",
			input:    nil,
			expected: nil,
		},
		{
			name:     "Filter with empty map",
			input:    map[string]string{},
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filterRelevantAnnotations(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestTransformContainers(t *testing.T) {
	tests := []struct {
		name     string
		input    []corev1.Container
		expected []corev1.Container
	}{
		{
			name: "Transform containers keeps only name and ports",
			input: []corev1.Container{
				{
					Name:  "container1",
					Image: "nginx:latest",
					Ports: []corev1.ContainerPort{
						{Name: "http", ContainerPort: 8080, Protocol: "TCP"},
					},
					Env: []corev1.EnvVar{
						{Name: "ENV1", Value: "value1"},
					},
					Resources: corev1.ResourceRequirements{
						// Resource specifications...
					},
				},
				{
					Name:  "container2", 
					Image: "redis:latest",
					Ports: []corev1.ContainerPort{
						{Name: "redis", ContainerPort: 6379, Protocol: "TCP"},
					},
				},
			},
			expected: []corev1.Container{
				{
					Name: "container1",
					Ports: []corev1.ContainerPort{
						{Name: "http", ContainerPort: 8080, Protocol: "TCP"},
					},
				},
				{
					Name: "container2",
					Ports: []corev1.ContainerPort{
						{Name: "redis", ContainerPort: 6379, Protocol: "TCP"},
					},
				},
			},
		},
		{
			name:     "Transform empty containers",
			input:    []corev1.Container{},
			expected: nil,
		},
		{
			name:     "Transform nil containers",
			input:    nil,
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := transformContainers(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
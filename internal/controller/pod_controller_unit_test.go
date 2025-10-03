package controller

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/magicsong/apollo-network-controller/pkg/allocator"
)

func TestPodReconciler_shouldManagePod(t *testing.T) {
	reconciler := &PodReconciler{}

	tests := []struct {
		name     string
		pod      *corev1.Pod
		expected bool
	}{
		{
			name: "Pod without annotations",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pod",
				},
			},
			expected: false,
		},
		{
			name: "Pod with enable annotation false",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pod",
					Annotations: map[string]string{
						allocator.PodAnnotationEnableAllocKey: "false",
						allocator.PodAnnotationNetworkPoolKey: "test-pool",
					},
				},
			},
			expected: false,
		},
		{
			name: "Pod with enable annotation but no pool",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pod",
					Annotations: map[string]string{
						allocator.PodAnnotationEnableAllocKey: "true",
					},
				},
			},
			expected: false,
		},
		{
			name: "Pod with all required annotations",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pod",
					Annotations: map[string]string{
						allocator.PodAnnotationEnableAllocKey: "true",
						allocator.PodAnnotationNetworkPoolKey: "test-pool",
					},
				},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := reconciler.shouldManagePod(tt.pod)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestPodReconciler_parseContainerPorts(t *testing.T) {
	reconciler := &PodReconciler{}

	tests := []struct {
		name     string
		pod      *corev1.Pod
		expected int
		hasError bool
		desc     string
	}{
		{
			name: "Priority 1: Pod with container ports annotation",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pod",
					Annotations: map[string]string{
						allocator.PodAnnotationContainerPortsKey: `[{"podPort":8080,"protocol":"TCP","portName":"http"}]`,
					},
				},
			},
			expected: 1,
			hasError: false,
			desc:     "Should parse from annotation (highest priority)",
		},
		{
			name: "Priority 1: Pod with annotation takes precedence over spec",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pod",
					Annotations: map[string]string{
						allocator.PodAnnotationContainerPortsKey: `[{"podPort":3000,"protocol":"TCP","portName":"custom"}]`,
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
			},
			expected: 1,
			hasError: false,
			desc:     "Should use annotation even when spec has ports",
		},
		{
			name: "Pod with empty annotation should fallback to spec",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pod",
					Annotations: map[string]string{
						allocator.PodAnnotationContainerPortsKey: "",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test-container",
							Image: "test-image",
							Ports: []corev1.ContainerPort{
								{ContainerPort: 8080, Protocol: corev1.ProtocolTCP, Name: "http"},
							},
						},
					},
				},
			},
			expected: 1,
			hasError: false,
			desc:     "Empty annotation should fallback to spec",
		},
		{
			name: "Pod with invalid JSON annotation",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pod",
					Annotations: map[string]string{
						allocator.PodAnnotationContainerPortsKey: `invalid-json`,
					},
				},
			},
			expected: 0,
			hasError: true,
			desc:     "Invalid JSON should return error",
		},
		{
			name: "Priority 2: Pod with container ports in spec only",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pod",
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
			},
			expected: 2,
			hasError: false,
			desc:     "Should fallback to spec when no annotation",
		},
		{
			name: "Priority 3: Pod with no container ports anywhere",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pod",
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test-container",
							Image: "test-image",
							// No ports declared - common for many containers
						},
					},
				},
			},
			expected: 0,
			hasError: false,
			desc:     "Should return empty list when no ports found anywhere",
		},
		{
			name: "Multiple containers with ports in spec",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pod",
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "web-container",
							Image: "nginx",
							Ports: []corev1.ContainerPort{
								{ContainerPort: 80, Protocol: corev1.ProtocolTCP, Name: "http"},
								{ContainerPort: 443, Protocol: corev1.ProtocolTCP, Name: "https"},
							},
						},
						{
							Name:  "metrics-container",
							Image: "prometheus",
							Ports: []corev1.ContainerPort{
								{ContainerPort: 9090, Protocol: corev1.ProtocolTCP, Name: "metrics"},
							},
						},
					},
				},
			},
			expected: 3,
			hasError: false,
			desc:     "Should collect ports from all containers",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := reconciler.parseContainerPorts(tt.pod)
			
			if tt.hasError {
				assert.Error(t, err, tt.desc)
			} else {
				assert.NoError(t, err, tt.desc)
				assert.Len(t, result, tt.expected, tt.desc)
			}
		})
	}
}

// TestPodReconciler_parseNetworkPools tests the multi-pool parsing functionality
func TestPodReconciler_parseNetworkPools(t *testing.T) {
	reconciler := &PodReconciler{}

	tests := []struct {
		name     string
		pod      *corev1.Pod
		expected []string
		hasError bool
		desc     string
	}{
		{
			name: "Single pool - simple string",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pod", 
					Annotations: map[string]string{
						allocator.PodAnnotationNetworkPoolKey: "pool1",
					},
				},
			},
			expected: []string{"pool1"},
			hasError: false,
			desc:     "Should parse single pool from simple string",
		},
		{
			name: "Multi-pool - comma separated",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pod",
					Annotations: map[string]string{
						allocator.PodAnnotationNetworkPoolKey: "pool1,pool2,pool3",
					},
				},
			},
			expected: []string{"pool1", "pool2", "pool3"},
			hasError: false,
			desc:     "Should parse multiple pools from comma-separated string",
		},
		{
			name: "Multi-pool - comma separated with spaces",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pod",
					Annotations: map[string]string{
						allocator.PodAnnotationNetworkPoolKey: " pool1 , pool2 , pool3 ",
					},
				},
			},
			expected: []string{"pool1", "pool2", "pool3"},
			hasError: false,
			desc:     "Should parse and trim spaces from comma-separated pools",
		},
		{
			name: "Multi-pool - JSON array",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pod",
					Annotations: map[string]string{
						allocator.PodAnnotationNetworkPoolKey: `["pool1","pool2","pool3"]`,
					},
				},
			},
			expected: []string{"pool1", "pool2", "pool3"},
			hasError: false,
			desc:     "Should parse pools from JSON array format",
		},
		{
			name: "Invalid JSON array - malformed syntax",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pod",
					Annotations: map[string]string{
						allocator.PodAnnotationNetworkPoolKey: `["pool1",,"pool2"]`,
					},
				},
			},
			expected: nil,
			hasError: true,
			desc:     "Should return error for invalid JSON with double comma",
		},
		{
			name: "Empty annotation",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pod",
					Annotations: map[string]string{
						allocator.PodAnnotationNetworkPoolKey: "",
					},
				},
			},
			expected: nil,
			hasError: true,
			desc:     "Should return error for empty annotation",
		},
		{
			name: "Missing annotation",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pod",
				},
			},
			expected: nil,
			hasError: true,
			desc:     "Should return error for missing annotation",
		},
		{
			name: "Empty pools after parsing",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pod",
					Annotations: map[string]string{
						allocator.PodAnnotationNetworkPoolKey: " , , ",
					},
				},
			},
			expected: nil,
			hasError: true,
			desc:     "Should return error when no valid pools found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := reconciler.parseNetworkPools(tt.pod)
			
			if tt.hasError {
				assert.Error(t, err, tt.desc)
				assert.Nil(t, result, tt.desc)
			} else {
				assert.NoError(t, err, tt.desc)
				assert.Equal(t, tt.expected, result, tt.desc)
			}
		})
	}
}
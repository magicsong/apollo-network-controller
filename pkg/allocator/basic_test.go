package allocator

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apollov1 "github.com/magicsong/apollo-network-controller/api/v1"
)

func TestBasicAllocation(t *testing.T) {
	// Setup
	scheme := runtime.NewScheme()
	_ = apollov1.AddToScheme(scheme)
	
	pool := &apollov1.ApolloNetworkPool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pool",
			Namespace: "default",
		},
		Spec: apollov1.ApolloNetworkPoolSpec{
			LoadBalancers: []apollov1.LoadBalancerRef{
				{ID: "lb-1", Region: "us-west-1", Provider: "test-provider"},
			},
			PortRange: apollov1.PortRange{
				Min: 30000,
				Max: 30010,
			},
		},
	}
	
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pool).Build()
	allocator := NewPoolAllocator(fakeClient, StrategyRoundRobin)

	containerPorts := []apollov1.PodPortAllocation{
		{PodPort: 8080, Protocol: apollov1.ProtocolTCP, PortName: "http"},
	}

	// Execute
	allocation, err := allocator.AllocatePort(context.TODO(), "test-pool", "default", "test-pod", "default", containerPorts)

	// Verify
	require.NoError(t, err)
	require.NotNil(t, allocation)
	assert.Equal(t, "test-pool-test-pod", allocation.Name)
	assert.Len(t, allocation.Spec.PortBindings, 1)
	
	binding := allocation.Spec.PortBindings[0]
	assert.Equal(t, int32(8080), binding.PodPort)
	assert.True(t, binding.LBPort >= 30000 && binding.LBPort <= 30010)
	assert.Equal(t, "lb-1", binding.LoadBalancerRef.ID)
}
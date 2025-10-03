package allocator

const (
	// Label keys for PortAllocation resources
	LabelPoolNameKey      = "apollo.magicsong.io/pool-name"
	LabelPodNameKey       = "apollo.magicsong.io/pod-name"
	LabelPodNamespaceKey  = "apollo.magicsong.io/pod-namespace"
	LabelLBIdKey          = "apollo.magicsong.io/lb-id"
	LabelLBProviderKey    = "apollo.magicsong.io/lb-provider"
	LabelLBRegionKey      = "apollo.magicsong.io/lb-region"
	
	// Annotation keys for PortAllocation resources
	AnnotationAllocatedAtKey    = "apollo.magicsong.io/allocated-at"
	AnnotationPortCountKey      = "apollo.magicsong.io/port-count"
	AnnotationBindingTypeKey    = "apollo.magicsong.io/binding-type"
	AnnotationAllocatedByKey    = "apollo.magicsong.io/allocated-by"
	AnnotationAllocationIdKey   = "apollo.magicsong.io/allocation-id"

	// Pod annotation keys - these are used to mark Pods that need port allocation
	PodAnnotationNetworkPoolKey    = "apollo.magicsong.io/network-pool"  // Specifies which pool to use
	PodAnnotationContainerPortsKey = "apollo.magicsong.io/container-ports" // JSON array of container ports
	PodAnnotationEnableAllocKey    = "apollo.magicsong.io/enable-allocation" // Enable allocation for this pod
)
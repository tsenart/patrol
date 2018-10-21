package patrol

// A Cluster retrieves a dynamic list of cluster nodes.
type Cluster interface {
	Nodes() []string
}

// A StaticCluster implements the Cluster interface with static
// list of node addresses.
type StaticCluster struct {
	nodes []string
}

// NewStaticCluster returns a new StaticCluster with the given nodes.
func NewStaticCluster(nodes []string) *StaticCluster {
	return &StaticCluster{nodes: nodes}
}

// Nodes returns the cluster nodes.
func (c *StaticCluster) Nodes() []string {
	return c.nodes
}

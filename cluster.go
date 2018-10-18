package patrol

import (
	"net"

	"github.com/hashicorp/memberlist"
)

// A Cluster retrieves a dynamic list of cluster nodes.
type Cluster interface {
	Nodes() ([]string, error)
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
func (c *StaticCluster) Nodes() ([]string, error) {
	return c.nodes, nil
}

// A MemberlistCluster implements the Cluster interface backed by
// the https://github.com/hashicorp/memberlist package.
type MemberlistCluster struct {
	ml   *memberlist.Memberlist
	port string
}

// NewMemberlistCluster returns a new MemberlistCluster with the
// given config.
func NewMemberlistCluster(port string, cfg *memberlist.Config) (*MemberlistCluster, error) {
	ml, err := memberlist.Create(cfg)
	if err != nil {
		return nil, err
	}

	return &MemberlistCluster{ml: ml}, nil
}

// Nodes returns the cluster nodes.
func (c *MemberlistCluster) Nodes() ([]string, error) {
	members := c.ml.Members()
	nodes := make([]string, len(members))
	for i, member := range members {
		nodes[i] = net.JoinHostPort(member.Addr.String(), c.port)
	}
	return nodes, nil
}

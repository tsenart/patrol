package patrol

import (
	"github.com/hashicorp/memberlist"
)

// A Cluster retrieves a dynamic list of cluster nodes.
type Cluster interface {
	Nodes() ([]string, error)
}

// A MemberlistCluster implements the Cluster interface backed by
// the https://github.com/hashicorp/memberlist package.
type MemberlistCluster struct {
	ml *memberlist.Memberlist
}

// NewMemberlistCluster returns a new MemberlistCluster with the
// given config.
func NewMemberlistCluster(cfg *memberlist.Config) (*MemberlistCluster, error) {
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
		nodes[i] = member.Addr.String()
	}
	return nodes, nil
}

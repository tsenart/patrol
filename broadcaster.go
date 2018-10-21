package patrol

import (
	"context"
	"log"
	"net"
)

// A Broadcaster can broadcast a Bucket to the whole Cluster.
type Broadcaster interface {
	Broadcast(context.Context, *Bucket) error
}

// Unicaster implements the Broadcaster interface backed
// by a best effort UDP unicast communication scheme.
type Unicaster struct {
	log     *log.Logger
	cluster Cluster
	dialer  net.Dialer
}

// NewUnicaster returns a new Unicaster for the given Cluster.
func NewUnicaster(log *log.Logger, cluster Cluster) *Unicaster {
	return &Unicaster{
		log:     log,
		cluster: cluster,
		dialer:  net.Dialer{},
	}
}

// Broadcast broadcasts the given Bucket across the cluster.
func (u *Unicaster) Broadcast(ctx context.Context, b *Bucket) error {
	data, err := b.MarshalBinary()
	if err != nil {
		return err
	}

	nodes := u.cluster.Nodes()
	errs := make(chan error, len(nodes))
	for _, node := range nodes {
		go u.unicast(ctx, node, data, errs)
	}

	for range nodes {
		if err := <-errs; err != nil {
			u.log.Printf("broadcast error: %v", err)
		}
	}

	return nil
}

func (u *Unicaster) unicast(ctx context.Context, node string, data []byte, errs chan error) {
	errs <- u.send(ctx, node, data)
}

func (u *Unicaster) send(ctx context.Context, node string, data []byte) error {
	conn, err := u.dialer.DialContext(ctx, "udp", node)
	if err != nil {
		return err
	}
	defer conn.Close()
	_, err = conn.Write(data)
	return err
}

// A BroadcastedRepo wraps another Repo to Broadcast all updates or inserts
// across the cluster.
type BroadcastedRepo struct {
	Broadcaster
	Repo
}

// UpsertBucket upserts the given Bucket in the underlying Repo and then
// Broadcasts it.
func (br *BroadcastedRepo) UpsertBucket(ctx context.Context, b *Bucket) error {
	if err := br.Repo.UpsertBucket(ctx, b); err != nil {
		return err
	}
	return br.Broadcast(ctx, b)
}

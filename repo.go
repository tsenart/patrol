package patrol

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"go.uber.org/zap"
	"golang.org/x/sync/singleflight"
)

// A Repo stores, retrieves and replicates Buckets across the cluster.
type Repo struct {
	log     *zap.Logger
	peers   []string
	conn    net.PacketConn
	clock   func() time.Time
	mu      sync.RWMutex
	buckets map[string]*Bucket
	incasts singleflight.Group
}

// NewRepo returns a new Repo that receives and sends UDP packets from the given addr to all peers.
func NewRepo(log *zap.Logger, clock func() time.Time, addr string, peers []string) (*Repo, error) {
	conn, err := net.ListenPacket("udp", addr)
	if err != nil {
		return nil, err
	}

	addrs := make([]string, 0, len(peers))
	for _, peer := range peers {
		if peer != addr {
			addrs = append(addrs, peer)
		}
	}

	log.Debug("peers", zap.String("self", addr), zap.Strings("others", addrs))

	return &Repo{
		log:     log,
		peers:   addrs,
		conn:    conn,
		clock:   clock,
		buckets: map[string]*Bucket{},
	}, nil
}

// Receive starts receiving and applying Bucket state updates from other peers.
func (r *Repo) Receive(ctx context.Context) error {
	buf := make([]byte, 256)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		remote, addr, err := r.receive(buf)
		switch e := err.(type) {
		case nil:
		case net.Error:
			if e.Temporary() || e.Timeout() {
				continue
			}
		default:
			return err
		}

		r.log.Debug("received", zap.Stringer("peer", addr), zap.Object("bucket", remote))

		local, ok := r.bucket(remote.name, 0)
		local.Merge(remote)

		r.log.Debug("merged",
			zap.Stringer("peer", addr),
			zap.Object("remote", remote),
			zap.Object("local", local),
		)

		if ok && remote.IsZero() && !local.IsZero() {
			// Node is in-casting this Bucket's state. Send it over.
			if err = r.unicast(local, addr); err != nil {
				return fmt.Errorf("unicasting %q to %s failed: %v", local.name, addr, err)
			}
		}
	}
}

// Bucket gets a Bucket by its name from the local Repo. It creates one with
// the given initial tokens if doesn't exist.
func (r *Repo) Bucket(ctx context.Context, name string, tokens int) (*Bucket, bool) {
	b, ok := r.bucket(name, tokens)
	if !ok {
		r.incasts.Do(b.name, func() (interface{}, error) {
			if err := r.Broadcast(&Bucket{name: name}); err != nil {
				r.log.Debug("incast failed", zap.String("bucket", name), zap.Error(err))
			}
			return nil, nil
		})
	}
	r.log.Debug("get", zap.Bool("new", !ok), zap.Object("bucket", b))
	return b, ok
}

func (r *Repo) bucket(name string, tokens int) (*Bucket, bool) {
	r.mu.Lock()
	b, ok := r.buckets[name]
	if !ok {
		b = &Bucket{name: name, added: float64(tokens)}
		r.buckets[name] = b
	}
	r.mu.Unlock()
	return b, ok
}

func (r *Repo) receive(buf []byte) (*Bucket, net.Addr, error) {
	err := r.conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	if err != nil {
		return nil, nil, err
	}

	n, addr, err := r.conn.ReadFrom(buf)
	if err != nil {
		return nil, addr, err
	}

	var b Bucket
	err = b.UnmarshalBinary(buf[:n])
	return &b, addr, err
}

// Broadcast broadcasts the given Bucket to all peers.
func (r *Repo) Broadcast(b *Bucket) error {
	r.log.Debug("broadcasting", zap.Object("bucket", b))

	data, err := b.MarshalBinary()
	if err != nil {
		return err
	}

	type operation struct {
		peer string
		err  error
	}

	opch := make(chan operation, len(r.peers))
	for _, peer := range r.peers {
		go func(op operation) {
			var addr net.Addr
			if addr, op.err = net.ResolveUDPAddr("udp", op.peer); err == nil {
				_, op.err = r.conn.WriteTo(data, addr)
			}
			opch <- op
		}(operation{peer: peer})
	}

	for range r.peers {
		if op := <-opch; op.err != nil {
			r.log.Error("broadcasting", zap.String("peer", op.peer), zap.Object("bucket", b))
		}
	}

	return nil
}

func (r *Repo) unicast(b *Bucket, addr net.Addr) error {
	r.log.Debug("unicasting", zap.Stringer("peer", addr), zap.Object("bucket", b))

	data, err := b.MarshalBinary()
	if err != nil {
		return err
	}
	_, err = r.conn.WriteTo(data, addr)
	return err
}

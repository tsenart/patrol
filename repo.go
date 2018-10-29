package patrol

import (
	"context"
	"net"
	"sync"
	"time"

	"go.uber.org/zap"
	"golang.org/x/sync/singleflight"
)

// A Repo creates Buckets and allows for them to be retrieved later.
// Implementations must be safe for concurrent use.
type Repo interface {
	GetBucket(ctx context.Context, name string) (*Bucket, bool)
	UpsertBucket(ctx context.Context, b *Bucket) (merged *Bucket, created bool)
}

// A ReplicatedRepo stores, retrieves and replicates Buckets across the cluster.
type ReplicatedRepo struct {
	log     *zap.Logger
	peers   []string
	conn    net.PacketConn
	repo    Repo
	incasts singleflight.Group
}

// NewReplicatedRepo returns a new Repo that receives and sends UDP packets from the given addr to all peers.
func NewReplicatedRepo(log *zap.Logger, r Repo, addr string, peers []string) (*ReplicatedRepo, error) {
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

	return &ReplicatedRepo{
		log:   log,
		peers: addrs,
		conn:  conn,
		repo:  r,
	}, nil
}

// Receive starts receiving and applying Bucket state updates from other peers.
func (r *ReplicatedRepo) Receive(ctx context.Context) error {

	var remote Bucket
	buf := make([]byte, bucketPacketSize)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		addr, err := r.receive(&remote, buf)
		switch e := err.(type) {
		case nil:
		case net.Error:
			if e.Temporary() || e.Timeout() {
				continue
			}
		default:
			return err
		}

		r.log.Debug("received", zap.Stringer("peer", addr), zap.Object("bucket", &remote))

		if local, ok := r.repo.GetBucket(ctx, remote.name); !remote.IsZero() {
			local.Merge(&remote)
			r.log.Debug("upsert",
				zap.Stringer("peer", addr),
				zap.Bool("created", !ok),
				zap.Object("remote", &remote),
				zap.Object("local", local),
			)
		} else if ok && !local.IsZero() { // Incast request
			if err = r.unicast(local, addr); err != nil {
				r.log.Error("unicast failed", zap.Object("bucket", local), zap.Stringer("peer", addr))
			}
		}
	}
}

// GetBucket gets a Bucket by its name from the local Repo. It creates if it doesn't exist,
// asking the cluster to send their most up to date version of the Bucket asynchronously.
func (r *ReplicatedRepo) GetBucket(ctx context.Context, name string) (*Bucket, bool) {
	b, ok := r.repo.GetBucket(ctx, name)
	if !ok {
		r.incasts.Do(b.name, func() (interface{}, error) {
			r.broadcast(&Bucket{name: name})
			return nil, nil
		})
	}
	r.log.Debug("get", zap.Bool("new", !ok), zap.Object("bucket", b))
	return b, ok
}

func (r *ReplicatedRepo) receive(b *Bucket, buf []byte) (net.Addr, error) {
	err := r.conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	if err != nil {
		return nil, err
	}

	n, addr, err := r.conn.ReadFrom(buf)
	if err != nil {
		return addr, err
	}

	return addr, b.UnmarshalBinary(buf[:n])
}

// UpsertBucket upserts the given Bucket and broadcasts to all nodes in the cluster.
func (r *ReplicatedRepo) UpsertBucket(ctx context.Context, b *Bucket) (upserted *Bucket, ok bool) {
	upserted, ok = r.repo.UpsertBucket(ctx, b)
	r.broadcast(upserted)
	return upserted, ok
}

func (r *ReplicatedRepo) broadcast(b *Bucket) {
	r.log.Debug("broadcasting", zap.Object("bucket", b))

	data, err := b.MarshalBinary()
	if err != nil {
		panic(err)
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
}

func (r *ReplicatedRepo) unicast(b *Bucket, addr net.Addr) error {
	r.log.Debug("unicasting", zap.Stringer("peer", addr), zap.Object("bucket", b))

	data, err := b.MarshalBinary()
	if err != nil {
		return err
	}
	_, err = r.conn.WriteTo(data, addr)
	return err
}

// A LocalRepo stores Buckets locally in-memory. It's safe for concurrent use.
type LocalRepo struct {
	mu      sync.RWMutex
	clock   func() time.Time
	buckets map[string]*Bucket
}

// NewLocalRepo returns a new LocalRepo with the given Buckets in it.
func NewLocalRepo(clock func() time.Time, bs ...*Bucket) *LocalRepo {
	r := LocalRepo{clock: clock, buckets: make(map[string]*Bucket, len(bs))}
	for _, b := range bs {
		r.buckets[b.name] = b
	}
	return &r
}

// GetBucket retrieves a Bucket with the given name, creating it first if it doesn't
// yet exist.
func (r *LocalRepo) GetBucket(_ context.Context, name string) (*Bucket, bool) {
	// First try the using a read lock which is going to be the most common case and
	// allows for concurrent reads.
	r.mu.RLock()
	b, ok := r.buckets[name]
	r.mu.RUnlock()

	if ok { // We have this bucket, so we return it immediately.
		return b, ok
	}

	// Since we don't have this bucket, we need to create it and acquire the write lock.
	// We prevent multiple writers from over-writing the bucket by first reading the Bucket
	// again with the write lock held.
	r.mu.Lock()
	if b, ok = r.buckets[name]; !ok {
		b = &Bucket{name: name, created: r.clock()}
		r.buckets[name] = b
	}
	r.mu.Unlock()

	return b, ok
}

// UpsertBucket upserts the given Bucket in the Repo. If it already exists, the given Bucket
// is merged with the stored Bucket.
func (r *LocalRepo) UpsertBucket(_ context.Context, b *Bucket) (upserted *Bucket, ok bool) {
	r.mu.RLock()
	prev := r.buckets[b.name]
	r.mu.RUnlock()

	if prev == b { // Fast path. Pointers are the same, so nothing to do.
		return prev, true
	}

	r.mu.Lock()
	if prev = r.buckets[b.name]; prev == nil {
		b.created = r.clock()
		r.buckets[b.name] = b
		r.mu.Unlock()
		return b, false
	}
	r.mu.Unlock()

	prev.Merge(b)
	return prev, true
}

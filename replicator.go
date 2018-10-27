package patrol

import (
	"context"
	"log"
	"net"
	"time"
)

// A Replicator server that handles incoming UDP messages
// containing Bucket updates.
type Replicator struct {
	log  *log.Logger
	conn net.PacketConn
	repo Repo
}

// NewReplicator returns a new Replicator server that listens on the given
// addr.
func NewReplicator(log *log.Logger, repo Repo, addr string) (*Replicator, error) {
	conn, err := net.ListenPacket("udp", addr)
	if err != nil {
		return nil, err
	}
	return &Replicator{
		log:  log,
		repo: repo,
		conn: conn,
	}, nil
}

// Start satarts the replicator server, consuming and applying UDP message
// updates to the configured Repo.
func (r *Replicator) Start(ctx context.Context) error {
	buf := make([]byte, 256)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		bucket, _, err := r.read(buf)
		switch e := err.(type) {
		case nil:
		case net.Error:
			if e.Temporary() || e.Timeout() {
				continue
			}
		default:
			return err
		}

		r.repo.UpsertBucket(ctx, &bucket)
	}
}

func (r *Replicator) read(buf []byte) (b Bucket, addr net.Addr, err error) {
	err = r.conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	if err != nil {
		return
	}

	n, addr, err := r.conn.ReadFrom(buf)
	if err != nil {
		return
	}

	err = b.UnmarshalBinary(buf[:n])
	return
}

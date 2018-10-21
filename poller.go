package patrol

import (
	"context"
	"log"
	"time"
)

// A Poller takes care of periodically polling all the
// cluster nodes for updated state and updating it locally.
type Poller struct {
	log     *log.Logger
	cluster Repo
	local   Repo
}

// NewPoller returns a new Poller with the given cluster Client and local Repo.
func NewPoller(lg *log.Logger, cluster, local Repo) *Poller {
	return &Poller{
		log:     lg,
		cluster: cluster,
		local:   local,
	}
}

// Poll initiates asynchrnous polling and updating of repo state at the
// given interval.
func (p *Poller) Poll(ctx context.Context, every time.Duration) error {
	ticker := time.NewTicker(every)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := p.poll(ctx); err != nil {
				p.log.Printf("poller: %v", err)
			}
		}
	}
}

func (p *Poller) poll(ctx context.Context) error {
	others, err := p.cluster.GetBuckets(ctx)
	if err != nil {
		return err
	}

	locals, err := p.local.GetBuckets(ctx)
	if err != nil {
		return err
	}

	for name, other := range others {
		if local, ok := locals[name]; ok {
			if other.Taken <= local.Taken {
				continue // skip update
			}
			taken := other.Taken
			*other = *local
			other.Taken = taken
		}
		if err = p.local.UpsertBucket(ctx, name, other); err != nil {
			return err
		}
	}

	return nil
}

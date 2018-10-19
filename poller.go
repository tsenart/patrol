package patrol

import (
	"compress/gzip"
	"context"
	"encoding/gob"
	"log"
	"net/http"
	"time"
)

// A Doer captures the Do method of an *http.Client
type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

// A Poller takes care of periodically polling all the
// cluster nodes for updated state and updating it locally.
type Poller struct {
	log     *log.Logger
	cluster Cluster
	cli     Doer
	repo    Repo
	port    string // API port of cluster nodes
}

// NewPoller returns a new Poller with the given Cluster, Doer and Repo.
func NewPoller(lg *log.Logger, cluster Cluster, cli Doer, repo Repo, port string) *Poller {
	return &Poller{
		log:     lg,
		cluster: cluster,
		cli:     cli,
		repo:    repo,
		port:    port,
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
			nodes, err := p.cluster.Nodes()
			if err != nil {
				return err
			}
			p.poll(ctx, nodes)
		}
	}
}

func (p *Poller) poll(ctx context.Context, nodes []string) {
	type result struct {
		node string
		err  error
	}

	results := make(chan result, len(nodes))
	for i := range nodes {
		go func(node string) {
			results <- result{node: node, err: p.sync(ctx, node)}
		}(nodes[i])
	}

	errs := make(map[string]error, len(nodes))
	for range nodes {
		select {
		case <-ctx.Done():
			return
		case r := <-results:
			if r.err != nil {
				errs[r.node] = r.err
			}
		}
	}

	for node, err := range errs {
		p.log.Printf("poller: failed to poll %q: %v", node, err)
	}
}

func (p *Poller) sync(ctx context.Context, node string) error {
	req, err := http.NewRequest("GET", "http://"+node+"/buckets", nil)
	if err != nil {
		return err
	}

	req.Header.Add("Accept", "application/x-gob")
	req.Header.Add("Accept-Encoding", "gzip")

	res, err := p.cli.Do(req.WithContext(ctx))
	if err != nil {
		return err
	}

	defer res.Body.Close()

	body, err := gzip.NewReader(res.Body)
	if err != nil {
		return err
	}

	var bs Buckets
	if err = gob.NewDecoder(body).Decode(&bs); err != nil {
		return err
	}

	return p.repo.UpdateBuckets(ctx, bs)
}

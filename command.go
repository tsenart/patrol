package patrol

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"

	"github.com/oklog/run"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

// A Command to be used in testing and the cmd/patrol.
type Command struct {
	Log              *log.Logger
	APIAddr          string
	ReplicatorAddr   string
	ClusterDiscovery string
	ClusterNodes     []string
}

// Run runs the Command and blocks until completion.
func (c *Command) Run(ctx context.Context) (err error) {
	var cluster Cluster
	switch c.ClusterDiscovery {
	case "static":
		cluster = NewStaticCluster(c.ClusterNodes)
	default:
		err = fmt.Errorf("unsupported cluster discovery: %q", c.ClusterDiscovery)
	}

	if err != nil {
		return err
	}

	repo := NewInMemoryRepo()
	replicator, err := NewReplicator(c.Log, repo, c.ReplicatorAddr)
	if err != nil {
		return err
	}

	api := NewAPI(c.Log, &BroadcastedRepo{
		Repo:        repo,
		Broadcaster: NewUnicaster(c.Log, cluster),
	})

	srv := http.Server{
		Addr:     c.APIAddr,
		Handler:  h2c.NewHandler(api, &http2.Server{}),
		ErrorLog: c.Log,
	}

	var g run.Group
	{ // HTTP API
		g.Add(func() error {
			c.Log.Printf("Listening on %s", c.APIAddr)
			return srv.ListenAndServe()
		}, func(error) {
			srv.Shutdown(ctx)
		})
	}

	{ // Replicator
		ctx, cancel := context.WithCancel(ctx)
		g.Add(func() error {
			return replicator.Start(ctx)
		}, func(error) {
			cancel()
		})
	}

	{ // Signal handling and cancellation
		ctx, cancel := context.WithCancel(ctx)
		g.Add(func() error {
			sigch := make(chan os.Signal, 1)
			signal.Notify(sigch, os.Interrupt)
			select {
			case <-sigch:
			case <-ctx.Done():
			}
			return nil
		}, func(error) {
			cancel()
		})
	}

	return g.Run()
}

package patrol

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/oklog/run"
	"go.uber.org/zap"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

// A Command to be used in testing and the cmd/patrol.
type Command struct {
	Log             *zap.Logger
	APIAddr         string
	NodeAddr        string
	PeerAddrs       []string
	Clock           func() time.Time // For testing
	ShutdownTimeout time.Duration
}

// Run runs the Command and blocks until completion.
func (c *Command) Run(ctx context.Context) (err error) {
	if c.ShutdownTimeout == 0 {
		return fmt.Errorf("ShutdownTimeout must be set")
	}

	repo, err := NewRepo(c.Log, c.Clock, c.NodeAddr, c.PeerAddrs)
	if err != nil {
		return err
	}

	defer c.Log.Sync()
	api := NewAPI(c.Log, c.Clock, repo)

	srv := http.Server{
		Addr:    c.APIAddr,
		Handler: h2c.NewHandler(api, &http2.Server{}),
	}

	var g run.Group
	{ // HTTP API
		g.Add(func() error {
			c.Log.Info("API serving", zap.String("addr", c.APIAddr))
			return srv.ListenAndServe()
		}, func(error) {
			ctx, cancel := context.WithTimeout(ctx, c.ShutdownTimeout)
			defer cancel()
			srv.Shutdown(ctx)
		})
	}

	{ // Replication
		ctx, cancel := context.WithCancel(ctx)
		g.Add(func() error {
			return repo.Receive(ctx)
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

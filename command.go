package patrol

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/hashicorp/memberlist"
	"github.com/oklog/run"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

// A Command to be used in testing and the cmd/patrol.
type Command struct {
	Log      *log.Logger
	Host     string
	Port     string
	Cluster  string
	Interval time.Duration
	Timeout  time.Duration
	Nodes    []string
}

// Run runs the Command and blocks until completion.
func (c *Command) Run(ctx context.Context) (err error) {
	var cluster Cluster
	switch c.Cluster {
	case "static":
		cluster = NewStaticCluster(c.Nodes)
	case "memberlist":
		cluster, err = NewMemberlistCluster(c.Port, memberlist.DefaultLANConfig())
	default:
		err = fmt.Errorf("unsupported cluster mode: %q", c.Cluster)
	}

	if err != nil {
		return err
	}

	dialer := net.Dialer{
		KeepAlive: c.Interval * 2,
		Timeout:   c.Timeout,
	}

	tr := http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		Dial:                  dialer.Dial,
		ResponseHeaderTimeout: c.Timeout,
		TLSHandshakeTimeout:   10 * time.Second,
	}

	doer := http.Client{
		Transport: &http2.Transport{
			AllowHTTP: true,
			DialTLS: func(network, addr string, cfg *tls.Config) (net.Conn, error) {
				return tr.Dial(network, addr)
			},
		},
	}

	http2.ConfigureTransport(&tr)

	client := NewClient(c.Log, &doer, cluster, c.Timeout)
	repo := NewInMemoryRepo()
	poller := NewPoller(c.Log, client, repo)
	api := NewAPI(c.Log, client, repo)

	addr := net.JoinHostPort(c.Host, c.Port)
	srv := http.Server{
		Addr:     addr,
		Handler:  h2c.NewHandler(api.Handler(), &http2.Server{}),
		ErrorLog: c.Log,
	}

	var g run.Group
	{ // HTTP API
		g.Add(func() error {
			c.Log.Printf("Listening on %s", addr)
			return srv.ListenAndServe()
		}, func(error) {
			srv.Shutdown(ctx)
		})
	}

	{ // Poller
		ctx, cancel := context.WithCancel(ctx)
		g.Add(func() error {
			return poller.Poll(ctx, c.Interval)
		}, func(error) {
			cancel()
		})
	}

	// Signal handling and cancellation
	{
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

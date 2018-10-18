package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/hashicorp/memberlist"
	"github.com/oklog/run"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	"github.com/tsenart/patrol"
)

func main() {
	opts := options{
		host:     "0.0.0.0",
		port:     "8080",
		cluster:  "static",
		interval: time.Second,
		timeout:  30 * time.Second,
	}

	fs := flag.NewFlagSet("patrol", flag.ExitOnError)
	fs.StringVar(&opts.host, "host", opts.host, "IP address to bind HTTP API to")
	fs.StringVar(&opts.port, "port", opts.port, "Port to bind HTTP API to")
	fs.StringVar(&opts.cluster, "cluster", opts.cluster, "Cluster mode [static | memberlist]")
	fs.DurationVar(&opts.interval, "interval", opts.interval, "Poller interval")
	fs.DurationVar(&opts.timeout, "timeout", opts.timeout, "Poller HTTP client timeout")
	fs.Var(&nodeFlag{nodes: &opts.nodes}, "node", "Static node for use with -cluster=static")

	fs.Parse(os.Args[1:])

	lg := log.New(os.Stderr, "", log.LstdFlags)
	if err := runPatrol(lg, &opts); err != nil {
		lg.Fatal(err)
	}
}

type options struct {
	host     string
	port     string
	cluster  string
	interval time.Duration
	timeout  time.Duration
	nodes    []string
}

func runPatrol(lg *log.Logger, opts *options) (err error) {
	var cluster patrol.Cluster
	switch opts.cluster {
	case "static":
		cluster = patrol.NewStaticCluster(opts.nodes)
	case "memberlist":
		cluster, err = patrol.NewMemberlistCluster(opts.port, memberlist.DefaultLANConfig())
	default:
		err = fmt.Errorf("unsupported cluster mode: %q", opts.cluster)
	}

	if err != nil {
		return err
	}

	dialer := net.Dialer{
		KeepAlive: opts.interval * 2,
		Timeout:   opts.timeout,
	}

	tr := http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		Dial:                  dialer.Dial,
		ResponseHeaderTimeout: opts.timeout,
		TLSHandshakeTimeout:   10 * time.Second,
	}

	client := http.Client{
		Transport: &http2.Transport{
			AllowHTTP: true,
			DialTLS: func(network, addr string, cfg *tls.Config) (net.Conn, error) {
				return tr.Dial(network, addr)
			},
		},
	}

	repo := patrol.NewInMemoryRepo()
	poller := patrol.NewPoller(lg, cluster, &client, repo, opts.port)
	api := patrol.NewAPI(lg, repo)

	addr := net.JoinHostPort(opts.host, opts.port)
	srv := http.Server{
		Addr:     addr,
		Handler:  h2c.NewHandler(api.Handler(), &http2.Server{}),
		ErrorLog: lg,
	}

	var g run.Group
	{ // HTTP API
		g.Add(func() error {
			lg.Printf("Listening on %s", addr)
			return srv.ListenAndServe()
		}, func(error) {
			srv.Shutdown(context.Background())
		})
	}

	{ // Poller
		ctx, cancel := context.WithCancel(context.Background())
		g.Add(func() error {
			return poller.Poll(ctx, opts.interval)
		}, func(error) {
			cancel()
		})
	}

	// Signal handling
	g.Add(func() error {
		sigch := make(chan os.Signal, 1)
		signal.Notify(sigch, os.Interrupt)
		<-sigch
		return nil
	}, func(error) {})

	return g.Run()
}

// nodeFlag implements the flag.Value interface for defining node addresses.
type nodeFlag struct{ nodes *[]string }

func (f *nodeFlag) Set(node string) error {
	_, _, err := net.SplitHostPort(node)
	if err != nil {
		return err
	}
	*f.nodes = append(*f.nodes, node)
	return nil
}

func (f *nodeFlag) String() string {
	if f.nodes == nil {
		return ""
	}
	return strings.Join(*f.nodes, ", ")
}

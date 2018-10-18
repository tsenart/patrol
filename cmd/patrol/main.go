package main

import (
	"context"
	"crypto/tls"
	"flag"
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

	"github.com/tsenart/patrol"
)

func main() {
	fs := flag.NewFlagSet("patrol", flag.ExitOnError)
	host := fs.String("host", "0.0.0.0", "IP address to bind HTTP API to")
	port := fs.String("port", "8080", "Port to bind HTTP API to")
	interval := fs.Duration("interval", time.Second, "Poller interval")
	timeout := fs.Duration("timeout", 30*time.Second, "Poller HTTP client timeout")

	fs.Parse(os.Args[1:])

	lg := log.New(os.Stderr, "", log.LstdFlags)
	if err := runPatrol(lg, *host, *port, *interval, *timeout); err != nil {
		lg.Fatal(err)
	}
}

func runPatrol(lg *log.Logger, host, port string, interval, timeout time.Duration) error {
	cluster, err := patrol.NewMemberlistCluster(memberlist.DefaultLANConfig())
	if err != nil {
		return err
	}

	dialer := net.Dialer{
		KeepAlive: interval * 2,
		Timeout:   timeout,
	}

	tr := http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		Dial:                  dialer.Dial,
		ResponseHeaderTimeout: timeout,
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
	poller := patrol.NewPoller(lg, cluster, &client, repo, port)
	api := patrol.NewAPI(lg, repo)

	srv := http.Server{
		Addr:     net.JoinHostPort(host, port),
		Handler:  h2c.NewHandler(api.Handler(), &http2.Server{}),
		ErrorLog: lg,
	}

	var g run.Group
	{ // HTTP API
		g.Add(func() error {
			return srv.ListenAndServe()
		}, func(error) {
			srv.Shutdown(context.Background())
		})
	}

	{ // Poller
		ctx, cancel := context.WithCancel(context.Background())
		g.Add(func() error {
			return poller.Poll(ctx, interval)
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

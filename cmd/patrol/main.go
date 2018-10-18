package main

import (
	"flag"
	"log"
	"net"
	"os"
	"strings"
	"time"

	"github.com/tsenart/patrol"
)

func main() {
	cmd := patrol.Command{
		Log:      log.New(os.Stderr, "", log.LstdFlags),
		Host:     "0.0.0.0",
		Port:     "8080",
		Cluster:  "static",
		Interval: time.Second,
		Timeout:  30 * time.Second,
	}

	fs := flag.NewFlagSet("patrol", flag.ExitOnError)
	fs.StringVar(&cmd.Host, "host", cmd.Host, "IP address to bind HTTP API to")
	fs.StringVar(&cmd.Port, "port", cmd.Port, "Port to bind HTTP API to")
	fs.StringVar(&cmd.Cluster, "cluster", cmd.Cluster, "Cluster mode [static | memberlist]")
	fs.DurationVar(&cmd.Interval, "interval", cmd.Interval, "Poller interval")
	fs.DurationVar(&cmd.Timeout, "timeout", cmd.Timeout, "Poller HTTP client timeout")
	fs.Var(&nodeFlag{nodes: &cmd.Nodes}, "node", "Static node for use with -cluster=static")

	fs.Parse(os.Args[1:])

	if err := cmd.Run(); err != nil {
		cmd.Log.Fatal(err)
	}
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

package main

import (
	"context"
	"flag"
	"log"
	"net"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/tsenart/patrol"
	"go.uber.org/zap"
)

func main() {
	cmd := patrol.Command{
		APIAddr:         "127.0.0.1:8080",
		NodeAddr:        "127.0.0.1:16000",
		ShutdownTimeout: 30 * time.Second,
	}

	runtime.SetMutexProfileFraction(50)
	fs := flag.NewFlagSet("patrol", flag.ExitOnError)
	fs.StringVar(&cmd.APIAddr, "api-addr", cmd.APIAddr, "HTTP API address")
	fs.StringVar(&cmd.NodeAddr, "node-addr", cmd.NodeAddr, "Node address")
	fs.Var(&addrsFlag{addrs: &cmd.PeerAddrs}, "peer-addr", "Peer node address")

	offset := fs.Duration("clock-offset", 0, "Offset to add to clock timestamps (for testing)")
	logenv := fs.String("log-env", "production", "Logging environment [development | production]")

	fs.Parse(os.Args[1:])

	cmd.Clock = func() time.Time {
		return time.Now().UTC().Add(*offset)
	}

	var err error
	switch *logenv {
	case "development":
		cmd.Log, err = zap.NewDevelopment()
	case "production":
		cmd.Log, err = zap.NewProduction()
	default:
		log.Fatalf("unspported -log-env value %q", *logenv)
	}

	if err != nil {
		log.Fatalf("failed to create logger: %v", err)
	}

	if err := cmd.Run(context.Background()); err != nil {
		cmd.Log.Fatal("error", zap.Error(err))
	}
}

// addrsFlag implements the flag.Value interface for defining addresses.
type addrsFlag struct{ addrs *[]string }

func (f *addrsFlag) Set(addr string) error {
	_, _, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	*f.addrs = append(*f.addrs, addr)
	return nil
}

func (f *addrsFlag) String() string {
	if f.addrs == nil {
		return ""
	}
	return strings.Join(*f.addrs, ", ")
}

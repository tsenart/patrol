package patrol

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"testing"
	"time"

	"github.com/tsenart/vegeta/lib"

	"github.com/oklog/run"
)

func TestCommand(t *testing.T) {
	nodes := []string{
		"127.0.0.1:12000",
		"127.0.0.1:12001",
		"127.0.0.1:12002",
	}

	var g run.Group
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := log.New(os.Stderr, "", log.LstdFlags)
	for _, node := range nodes {
		host, port, _ := net.SplitHostPort(node)
		cmd := Command{
			Log:      logger,
			Host:     host,
			Port:     port,
			Cluster:  "static",
			Interval: time.Second,
			Timeout:  30 * time.Second,
			Nodes:    nodes,
		}

		g.Add(func() error {
			return cmd.Run(ctx)
		}, func(error) {})
	}

	// Integration test
	g.Add(func() error {
		defer cancel()
		return testCommand(logger, nodes)
	}, func(error) {})

	if err := g.Run(); err != nil {
		t.Fatal(err)
	}
}

func testCommand(l *log.Logger, nodes []string) error {
	a := vegeta.NewAttacker(vegeta.H2C(true))

	targets := make([]vegeta.Target, len(nodes))
	for i, node := range nodes {
		targets[i] = vegeta.Target{
			Method: "POST",
			URL:    "http://" + node + "/take?bucket=foobar&rate=50:1s&count=1",
		}
	}

	tr := vegeta.NewStaticTargeter(targets...)
	rate := vegeta.Rate{Freq: 500, Per: time.Second} // > 50 / s
	results := a.Attack(tr, rate, 5*time.Second, "Patrol test")

	var m vegeta.Metrics
	for r := range results {
		m.Add(r)
	}

	m.Close()

	l.Printf("StatusCodes: %+v", m.StatusCodes)

	if m.Success >= 0.95 {
		return fmt.Errorf("HTTP 200s count=%f > 0.95", m.Success)
	}

	return nil
}

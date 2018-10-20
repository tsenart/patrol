package patrol

import (
	"context"
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

	ctx := context.Background()

	var g run.Group
	logger := log.New(os.Stderr, "", log.LstdFlags)
	for _, node := range nodes {
		host, port, _ := net.SplitHostPort(node)
		cmd := Command{
			Log:      logger,
			Host:     host,
			Port:     port,
			Cluster:  "static",
			Interval: 50 * time.Millisecond,
			Timeout:  30 * time.Second,
			Nodes:    nodes,
		}

		ctx, cancel := context.WithCancel(ctx)
		g.Add(func() error {
			return cmd.Run(ctx)
		}, func(error) {
			cancel()
		})
	}

	// Integration test
	g.Add(func() error {
		testCommand(t, nodes)
		return nil
	}, func(error) {
		// testCommand is not interruptable, it governs the run.Group
	})

	if err := g.Run(); err != nil {
		t.Fatal(err)
	}
}

func testCommand(t *testing.T, nodes []string) {
	a := vegeta.NewAttacker(vegeta.H2C(true))

	targets := make([]vegeta.Target, len(nodes))
	for i, node := range nodes {
		targets[i] = vegeta.Target{
			Method: "POST",
			URL:    "http://" + node + "/take/foobar?rate=10:s&count=1",
		}
	}

	tr := vegeta.NewStaticTargeter(targets...)
	rate := vegeta.Rate{Freq: 100, Per: time.Second} // > 10/s
	results := a.Attack(tr, rate, 5*time.Second, "Patrol test")

	var m vegeta.Metrics
	for r := range results {
		m.Add(r)
	}

	m.Close()

	if m.Success > 0.9 {
		t.Errorf("success rate should be below 0.9: got %f", m.Success)
	}
}

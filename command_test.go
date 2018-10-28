package patrol

import (
	"context"
	"log"
	"os"
	"testing"
	"time"

	"github.com/tsenart/vegeta/lib"

	"github.com/oklog/run"
)

func TestCommand(t *testing.T) {
	ctx := context.Background()

	apis := []string{
		"127.0.0.1:12000",
		"127.0.0.1:12001",
		"127.0.0.1:12002",
	}

	nodes := []string{
		"127.0.0.1:16000",
		"127.0.0.1:16001",
		"127.0.0.1:16002",
	}

	peers := func(node string, nodes []string) []string {
		var peers []string
		for i := range nodes {
			if nodes[i] != node {
				peers = append(peers, node)
			}
		}
		return peers
	}

	var g run.Group
	logger := log.New(os.Stderr, "", log.LstdFlags)
	for i, node := range nodes {
		offset := time.Duration(i) * time.Minute
		cmd := Command{
			Log:              logger,
			APIAddr:          apis[i],
			ReplicatorAddr:   node,
			ClusterDiscovery: "static",
			ClusterNodes:     peers(node, nodes),
			Clock: func() time.Time {
				// Test that unsynchronized clocks don't affect results.
				return time.Now().UTC().Add(offset)
			},
			ShutdownTimeout: 5 * time.Second,
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
		testCommand(t, apis)
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

	// Wait until APIs are serving.
	time.Sleep(time.Second)

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

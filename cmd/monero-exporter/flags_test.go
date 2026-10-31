package main

import (
	"testing"
	"time"

	"github.com/spf13/cobra"
)

func TestNoCollectorFlagsDisableSelectedCollectors(t *testing.T) {
	t.Parallel()

	c, _ := executeCommand(t,
		"--no-collector.peerlist",
		"--collector.net=false",
	)

	if c.colPeerlist || c.colNet {
		t.Fatal("peerlist and net should be disabled")
	}

	if !c.colOverall || !c.colLastBlock || !c.colTransactionPool || !c.colRPC || !c.colConnections {
		t.Fatal("expected the collectors that were not disabled to stay enabled")
	}
}

func TestCollectorIntervals(t *testing.T) {
	t.Parallel()

	c, cmd := executeCommand(t,
		"--collector.peerlist.interval=10m",
		"--collector.connections.interval=30s",
	)

	if c.colPeerlistInterval != 10*time.Minute {
		t.Fatalf("peerlist interval = %s", c.colPeerlistInterval)
	}

	if c.colConnectionsInterval != 30*time.Second {
		t.Fatalf("connections interval = %s", c.colConnectionsInterval)
	}

	if cmd.Flags().Lookup("collector.overall.interval") != nil {
		t.Fatal("overall collector must follow the global refresh interval")
	}
}

func TestTransactionPoolFullDefaultsOff(t *testing.T) {
	t.Parallel()

	c, _ := executeCommand(t)
	if c.colTransactionPoolFull {
		t.Fatal("full transaction pool should be opt-in")
	}

	enabled, _ := executeCommand(t, "--collector.transaction_pool.full")
	if !enabled.colTransactionPoolFull {
		t.Fatal("--collector.transaction_pool.full did not enable the full pool")
	}
}

func TestCollectorFlagDefaultsEnabled(t *testing.T) {
	t.Parallel()

	c, _ := executeCommand(t)

	if !c.colOverall || !c.colPeerlist || !c.colConnections || !c.colNet {
		t.Fatal("collectors should default to enabled")
	}

	if c.colPeerlistInterval != 0 || c.colNetInterval != 0 {
		t.Fatal("collector intervals should default to every cycle")
	}
}

func executeCommand(t *testing.T, args ...string) (*command, *cobra.Command) {
	t.Helper()

	c := &command{}
	cmd := c.Cmd()
	cmd.RunE = func(*cobra.Command, []string) error { return nil }
	cmd.SetArgs(args)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	return c, cmd
}

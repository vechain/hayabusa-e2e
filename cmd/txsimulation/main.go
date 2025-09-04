package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/lifecycle"
)

var (
	networkHubFlag           = flag.Bool("networkhub", false, "Run against NetworkHub")
	networkHubNodes          = flag.Int("networkhub-nodes", 2, "Number of nodes to create on NetworkHub (only used with --networkhub)")
	networkHubManyKeyNode    = flag.Bool("networkhub-manykeynode", true, "Create a many-key node on NetworkHub (only used with --networkhub)")
	delegationsEnabled       = flag.Bool("xnodes-enabled", true, "add and exit xnodes")
	devnetFlag               = flag.String("devnet", "", "Run against Devnet")
	devnetGenesisFlag        = flag.String("devnet-genesis-url", "https://vechain.github.io/thor-hayabusa/genesis.json", "Genesis JSON URL (only used with --devnet)")
	devnetKeysDir            = flag.String("keys-dir", "/path/to/your/devnet-keys", "Directory to store generated keys (only used with --devnet)")
	devnetLongTermValidators = flag.Int("devnet-longterm-validators", 2, "The amount of PoAs accounts to keep long term")
)

func main() {
	ctx := handleExitSignal()

	flag.Parse()
	if *networkHubFlag && *devnetFlag != "" {
		slog.Error("cannot use both --networkhub and --devnet flags at the same time")
		os.Exit(1)
	}

	var (
		engine *lifecycle.Engine
		stop   func()
	)

	if *networkHubFlag {
		engine, stop = startAgainstNetworkHub(ctx)
	} else {
		engine, stop = startAgainstDevnet(ctx)
	}
	defer stop()

	slog.Info("🚒 starting engine")
	engine.Run()
}

func handleExitSignal() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		exitSignalCh := make(chan os.Signal, 1)
		signal.Notify(exitSignalCh, os.Interrupt, syscall.SIGTERM)

		sig := <-exitSignalCh
		slog.Info("exit signal received", "signal", sig)
		cancel()
	}()
	return ctx
}

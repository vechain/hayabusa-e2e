package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/lifecycle"
)

var (
	networkHubFlag        = flag.Bool("networkhub", false, "Run against NetworkHub")
	networkHubNodes       = flag.Int("networkhub-nodes", 2, "Number of nodes to create on NetworkHub (only used with --networkhub)")
	networkHubManyKeyNode = flag.Bool("networkhub-manykeynode", true, "Create a many-key node on NetworkHub (only used with --networkhub)")
	delegationsEnabled    = flag.Bool("delegations-enabled", true, "add and exit delegations")
	devnetFlag            = flag.String("devnet", "", "Run against Devnet")
	devnetGenesisFlag     = flag.String("devnet-genesis-url", "https://vechain.github.io/thor-hayabusa/genesis.json", "Genesis JSON URL (only used with --devnet)")
	devnetKeysDir         = flag.String("keys-dir", "/path/to/your/devnet-keys", "Directory to store generated keys (only used with --devnet)")
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
	defer printOutput(engine)
	defer func() {
		if err := recover(); err != nil {
			slog.Warn("recovered from panic", "error", err)
		}
	}()

	slog.Info("🚒 starting engine")
	engine.Run()
}

func printOutput(engine *lifecycle.Engine) {
	tw := table.NewWriter()
	tw.AppendHeader(table.Row{"ID", "Type", "Status", "Queued Block", "Activated Block", "Exit Block", "Exited Block", "Validation ID"})
	for _, info := range engine.Info() {
		queued := -1
		if info.QueuedReceipt != nil {
			queued = int(info.QueuedReceipt.Meta.BlockNumber)
		}
		withdraw := -1
		if info.WithdrawReceipt != nil {
			withdraw = int(info.WithdrawReceipt.Meta.BlockNumber)
		}
		exit := -1
		if info.ExitReceipt != nil {
			exit = int(info.ExitReceipt.Meta.BlockNumber)
		}

		tw.AppendRow(table.Row{
			info.ID,
			info.Type.String(),
			info.Status.String(),
			queued,
			info.ActivatedBlock,
			exit,
			withdraw,
			info.ValidationID.String(),
		})
	}
	tw.SortBy([]table.SortBy{
		{Name: "Type", Mode: table.Dsc},   // Sort by Type first, Validators first
		{Name: "Status", Mode: table.Asc}, //
		{Name: "Activated", Mode: table.Asc},
	})
	content := tw.Render()

	//create the dir for the file
	if err := os.MkdirAll("fullnet-output", 0755); err != nil {
		slog.Error("failed to create output directory", "error", err)
		return
	}
	// create a file to write the table output
	seconds := time.Now().Unix()
	file, err := os.Create(fmt.Sprintf("./fullnet-output/lifecycle-%d.txt", seconds))
	if err != nil {
		slog.Error("failed to create output file", "error", err)
		return
	}
	defer file.Close()
	// write the table output to the file
	if _, err := file.WriteString(content); err != nil {
		slog.Error("failed to write to output file", "error", err)
		return
	}
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

package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/lifecycle"
	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/stack"
	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/utils"
	"github.com/vechain/hayabusa-e2e/cmd/txsimulation/validations"
	"github.com/vechain/hayabusa-e2e/hayabusa"
	utils2 "github.com/vechain/hayabusa-e2e/utils"
	"github.com/vechain/networkhub/thorbuilder"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/thorclient/bind"
	"github.com/vechain/thor/v2/thorclient/builtin"
)

func main() {
	ctx := handleExitSignal()
	config := &hayabusa.Config{
		Nodes:             2,
		MaxBlockProposers: 101,
		ForkBlock:         0,
		TransitionPeriod:  6,
		EpochLength:       6,
		CooldownPeriod:    6,
		MinStakingPeriod:  6,
		MidStakingPeriod:  48,
		HighStakingPeriod: 259200,
	}
	network, err := hayabusa.NewNetwork(config, ctx)
	if err != nil {
		slog.Error("failed to create hayabusa network", "error", err)
		os.Exit(1)
	}

	slog.SetLogLoggerLevel(slog.LevelInfo)

	if err := addManyKeyNode(network); err != nil {
		slog.Error("failed to add many key node", "error", err)
		os.Exit(1)
	}

	port := 8569
	for i, node := range network.NodeConfigs() {
		if i == 0 {
			node.AddAdditionalArg("enable-metrics", "true")
		}
		addr := net.JoinHostPort("localhost", strconv.Itoa(port))
		port++
		node.SetAPIAddr(addr)
		slog.Info("node API address", "node", node.GetID(), "address", addr)
	}
	if err := network.Start(); err != nil {
		slog.Error("failed to start network", "error", err)
		os.Exit(1)
	}
	client := network.ThorClient()
	staker, err := builtin.NewStaker(client)
	if err != nil {
		slog.Error("failed to create staker client", "error", err)
		os.Exit(1)
	}

	initialValidators := hayabusa.ValidatorAccounts[0:90]
	extraValidators := make(map[thor.Address]bind.Signer)
	for _, acc := range hayabusa.ValidatorAccounts[90:100] {
		extraValidators[acc.Address()] = acc
	}
	for _, acc := range hayabusa.AdditionalAccounts {
		extraValidators[acc.Address()] = acc
	}

	stack := stack.NewStack(ctx, staker, config, extraValidators, hayabusa.Stargate)
	validators := validations.NewState(stack)
	engine := lifecycle.NewEngine(stack, validators)

	defer printOutput(engine)

	// initial seeding of validator accounts
	for i, acc := range initialValidators {
		config := engine.GenerateValidatorConfig(acc, 0)
		if i < 50 { // create 50 long term validators
			config.StakingPeriods = 5000
		} else if i < 70 { // create 20 mid-term validators
			config.StakingPeriods = uint32(utils.RandomBetween(30, 100))
		} else {
			config.StakingPeriods = uint32(utils.RandomBetween(6, 12)) // create 20 short term validators
		}
		config.QueueDelay = lifecycle.Delay{Blocks: 0, Epochs: 0}
		cycle := lifecycle.NewValidatorLifecycle(config)
		engine.AddLifecycle(cycle)
	}

	if err := engine.Flush(lifecycle.StatusQueued); err != nil {
		slog.Error("failed to flush validator lifecycles", "error", err)
		os.Exit(1)
	}

	slog.Info("✅ validator lifecycles flushed")

	if err := utils2.WaitForPOS(staker, config.ForkBlock+config.TransitionPeriod); err != nil {
		slog.Error("failed to wait for POS", "error", err)
		os.Exit(1)
	}

	best, err := client.Block("best")
	if err != nil {
		slog.Error("failed to get best block", "error", err)
		os.Exit(1)
	}

	// initial seeding of delegator accounts
	for i := range uint32(200) {
		config := engine.GenerateDelegatorConfig(best.Number)
		config.QueueDelay = lifecycle.Delay{Blocks: i % 3, Epochs: 0}
		cycle := lifecycle.NewDelegatorLifecycle(config)
		engine.AddLifecycle(cycle)
	}

	if err := engine.Flush(lifecycle.StatusQueued); err != nil {
		slog.Error("failed to flush validator lifecycles", "error", err)
		os.Exit(1)
	}

	slog.Info("✅ delegator lifecycles flushed")
	slog.Info("🚒 starting engine")
	engine.Run()
	slog.Info("exit signal received, flushing lifecycles")
}

func addManyKeyNode(network *hayabusa.Network) error {
	args := make(map[string]string)
	keys := ""
	for i := 2; i < 101; i++ {
		hex := hexutil.Encode(hayabusa.ValidatorAccounts[i].D.Bytes())
		hex = strings.TrimPrefix(hex, "0x")
		keys += hex + ","
	}
	for _, acc := range hayabusa.AdditionalAccounts {
		hex := hexutil.Encode(acc.D.Bytes())
		hex = strings.TrimPrefix(hex, "0x")
		keys += hex + ","
	}
	keys = strings.TrimSuffix(keys, ",")
	args["keys"] = keys
	config := &thorbuilder.Config{
		DownloadConfig: &thorbuilder.DownloadConfig{
			RepoUrl:    "git@github.com:vechain/hayabusa.git",
			Branch:     "darren/testing/multiple-keys",
			IsReusable: true,
		},
		//BuildConfig: &thorbuilder.BuildConfig{
		//	DebugBuild:   false,
		//	ExistingPath: "/Users/darren/workspace/vechain/hayabusa",
		//},
	}
	return network.AttachNode(config, args)
}

func printOutput(engine *lifecycle.Engine) {
	tw := table.NewWriter()
	tw.AppendHeader(table.Row{"ID", "Type", "Status", "Queued Block", "Activated Block", "Exit Block", "ProcessExited Block", "Validation ID"})
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
	println(tw.Render())
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

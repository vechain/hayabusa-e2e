package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"

	"github.com/cqroot/prompt"
	"github.com/cqroot/prompt/input"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/vechain/hayabusa-e2e/hayabusa"
	"github.com/vechain/hayabusa-e2e/utils"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/thorclient/builtin"
)

func main() {
	config := &hayabusa.Config{
		Verbosity:         1,
		StakerVerbosity:   1,
		Nodes:             3,
		MaxBlockProposers: 3,
		ForkBlock:         0,
		TransitionPeriod:  6,
		EpochLength:       6,
		CooldownPeriod:    6,
		MinStakingPeriod:  12,
		MidStakingPeriod:  24,
		HighStakingPeriod: 180,
	}

	network, err := hayabusa.NewNetwork(config, context.Background())
	if err != nil {
		panic(err)
	}
	defer network.Stop()

	staker, err := builtin.NewStaker(network.ThorClient())
	if err != nil {
		fmt.Println("  - Error creating staker:", err)
		return
	}

	fmt.Println(" 🛠️ Setting up the staker contract with validators...")

	queuedEvents, err := addValidators(staker, config)
	if err != nil {
		fmt.Println("  - Error adding validators:", err)
		return
	}
	fmt.Println("\n🕐 Waiting for POS to become active... expected at block ", config.ForkBlock+config.TransitionPeriod)
	if err := utils.WaitForPOS(t.Context(), staker, config.ForkBlock+config.TransitionPeriod); err != nil {
		fmt.Println("  - Error waiting for PoS:", err)
		return
	}

	fmt.Println(" ✅ - Network started successfully")
	for i := range config.MaxBlockProposers {
		acc := hayabusa.ValidatorAccounts[i]
		fmt.Printf("\n  - Validator Node (%d):", i)
		fmt.Printf("    🏰 %s", acc.Node.Address())
		fmt.Printf("    🔑 %s", hexutil.Encode(acc.Node.D.Bytes()))
		fmt.Printf("\n  - Validator Endorsor (%d):", i)
		fmt.Printf("    🏰 %s", acc.Endorser.Address())
		fmt.Printf("    🔑 %s", hexutil.Encode(acc.Endorser.D.Bytes()))

		var event *builtin.ValidationQueuedEvent
		for _, e := range queuedEvents {
			if e.Node == acc.Node.Address() {
				event = &e
				break
			}
		}
		if event == nil {
			fmt.Println("    ❌ - No queued event found for this validator")
			continue
		} else {
			fmt.Printf("    ⏳ - Validation ID: %s", event.Node.String())
		}
	}
	for i := range 10 {
		acc := hayabusa.AdditionalAccounts[i]
		fmt.Printf("\n  - Additional Account (%d):", i)
		fmt.Printf("    🤑 %s", acc.Address())
		fmt.Printf("    🔑 %s", hexutil.Encode(acc.D.Bytes()))
	}
	fmt.Println("")

	fmt.Println("")
	fmt.Println("🌐 - Network URL: ", network.NodeConfigs()[0].GetAPIAddr())
	fmt.Println("📭 - Staker Address: ", staker.Raw().Address())
	res, err := prompt.New().Ask("Stargate Address:").Input("0xf077b491b355E64048cE21E3A6Fc4751eEeA77fa", input.WithValidateFunc(func(s string) error {
		_, err := thor.ParseAddress(s)
		return err
	}))
	if err != nil {
		panic(err)
	}
	fmt.Printf("\n🕐 Setting stargate address... %s", res)
	fmt.Println("")

	if err := setStargateAddr(network.ThorClient(), thor.MustParseAddress(res)); err != nil {
		fmt.Println("  - Error setting stargate address:", err)
		return
	}
	fmt.Println("")
	fmt.Println("✅ - Stargate address updated successfully")

	fmt.Println("\n✅ - PoS is now active")

	res, err = prompt.New().Ask("Start Devpal? (y/n)").Choose([]string{"y", "n"})
	if err != nil {
		panic(err)
	}
	var killDevpal func()
	if res == "y" {
		addr := network.NodeConfigs()[0].GetAPIAddr()
		parts := strings.Split(addr, ":")
		addr = fmt.Sprintf("http://localhost:%s", parts[len(parts)-1])
		killDevpal, err = startDevPal(addr)
		if err != nil {
			fmt.Println("  - Error starting devpal:", err)
			return
		}
	}
	defer killDevpal()

	fmt.Printf("\n\n\n✅ Network is setup, happy hacking!")
	fmt.Println("\n\n--> Press Ctrl+C to exit or stop")
	exitSignalCh := make(chan os.Signal, 1)
	signal.Notify(exitSignalCh, os.Interrupt, syscall.SIGTERM)
	sig := <-exitSignalCh
	slog.Info("exit signal received", "signal", sig)
}

func startDevPal(addr string) (func(), error) {
	path, err := exec.LookPath("npx")
	if err != nil {
		fmt.Println("  - npx not found in PATH")
		return nil, err
	}
	cmd := exec.Command(path, "@vechain/devpal", addr)
	fmt.Println(cmd.String())
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	go func() {
		if err := cmd.Start(); err != nil {
			fmt.Println("  - Error starting devpal:", err)
			return
		}
	}()
	return func() {
		if err := cmd.Process.Kill(); err != nil {
			fmt.Println("  - Error killing devpal:", err)
		}
	}, nil
}

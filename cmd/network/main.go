package main

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/cqroot/prompt"
	"github.com/cqroot/prompt/input"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/vechain/hayabusa-e2e/hayabusa"
	"github.com/vechain/hayabusa-e2e/utils"
	"github.com/vechain/thor/v2/genesis"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/thorclient/builtin"
)

func main() {
	config := &hayabusa.Config{
		Verbosity:         1,
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

	client, net, cancel, err := hayabusa.StartNetwork(config)
	if err != nil {
		panic(err)
	}
	defer cancel()

	staker, err := builtin.NewStaker(client)
	if err != nil {
		fmt.Println("  - Error creating staker:", err)
		return
	}

	fmt.Println(" ✅ - Network started successfully")
	for i, acc := range genesis.DevAccounts() {
		fmt.Println("")
		if uint32(i) < config.MaxBlockProposers {
			fmt.Printf("  - Validator (%d):", i)
			fmt.Printf("    🏰 %s", acc.Address)
			fmt.Printf("    🔑 %s", hexutil.Encode(acc.PrivateKey.D.Bytes()))
		} else {
			fmt.Printf("  - Account (%d):", i-int(config.MaxBlockProposers))
			fmt.Printf("    🤑 %s", acc.Address)
			fmt.Printf("    🔑 %s", hexutil.Encode(acc.PrivateKey.D.Bytes()))
		}
	}
	fmt.Println("")
	fmt.Println(" 🛠️ Setting up the staker contract with validators...")
	fmt.Println("  - In the meantime, please deploy or get ready to provide a stargate address")
	if err := addValidators(staker, config); err != nil {
		fmt.Println("  - Error adding validators:", err)
		return
	}

	fmt.Println("")
	fmt.Println("🌐 - Network URL: ", net.Config().Nodes[0].GetAPIAddr())
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

	if err := setStargateAddr(client, thor.MustParseAddress(res)); err != nil {
		fmt.Println("  - Error setting stargate address:", err)
		return
	}
	fmt.Println("")
	fmt.Println("✅ - Stargate address updated successfully")

	fmt.Println("\n🕐 Waiting for POS to become active... expected at block ", config.ForkBlock+config.TransitionPeriod)
	if err := utils.WaitForPOS(staker, config.ForkBlock+config.TransitionPeriod); err != nil {
		fmt.Println("  - Error waiting for PoS:", err)
		return
	}
	fmt.Println("\n✅ - PoS is now active")

	res, err = prompt.New().Ask("Start Devpal? (y/n)").Choose([]string{"y", "n"})
	if err != nil {
		panic(err)
	}
	var killDevpal func()
	if res == "y" {
		killDevpal, err = startDevPal(net.Config().Nodes[0].GetAPIAddr())
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
	cancel()
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

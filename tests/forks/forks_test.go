package forks

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/vechain/networkhub/thorbuilder"

	"github.com/stretchr/testify/assert"
	"github.com/vechain/hayabusa-e2e/hayabusa"
	"github.com/vechain/hayabusa-e2e/testutil"
	"github.com/vechain/hayabusa-e2e/utils"

	"github.com/vechain/networkhub/network/node"
	"github.com/vechain/thor/v2/thor"
	"github.com/vechain/thor/v2/thorclient"
	"github.com/vechain/thor/v2/thorclient/builtin"
)

func Test_VerifyLexicographicalOrder(t *testing.T) {
	// Due to unpredictable nature of the block ID, this test might require several runs
	_, _, _, _, nodes, doubleSignedBlocksChannel := newNetworkSetup(t)

	for range 2 {
		doubleSignedBlock := <-doubleSignedBlocksChannel

		var expectedBlock thor.Bytes32
		if bytes.Compare(doubleSignedBlock.block1.Bytes(), doubleSignedBlock.block2.Bytes()) < 0 {
			expectedBlock = doubleSignedBlock.block1
		} else {
			expectedBlock = doubleSignedBlock.block2
		}

		for _, node := range nodes {
			client := thorclient.New(node.GetHTTPAddr())
			block, err := client.Block(strconv.FormatUint(uint64(doubleSignedBlock.number), 10))
			assert.NoError(t, err)

			assert.Equal(t, bytes.Compare(expectedBlock.Bytes(), block.ID.Bytes()), 0)
		}
	}

}

type PropagatedDoubleSignedBlock struct {
	number uint32
	block1 thor.Bytes32
	block2 thor.Bytes32
}

func newNetworkSetup(t *testing.T) (*builtin.Staker, *hayabusa.Config, []thor.Bytes32, *thorclient.Client, []node.Config, chan PropagatedDoubleSignedBlock) {
	t.Helper()
	config := &hayabusa.Config{
		Nodes:             2,
		MaxBlockProposers: 6,
		ForkBlock:         0,
		TransitionPeriod:  4,
		EpochLength:       2,
		CooldownPeriod:    2,
		MinStakingPeriod:  4,
		MidStakingPeriod:  12,
		HighStakingPeriod: 259200,
		Name:              t.Name(),
		BlockInterval:     uint64(5),
	}

	network, err := hayabusa.NewNetwork(config, t.Context())
	require.NoError(t, err)
	t.Cleanup(network.Stop)
	client := network.ThorClient()
	require.NoError(t, network.Start())
	nodeConfig := &thorbuilder.Config{
		DownloadConfig: &thorbuilder.DownloadConfig{
			Branch:  "hayabusa/doublesigning-node",
			RepoUrl: "git@github.com:vechain/thor.git",
		},
	}
	builder := thorbuilder.New(nodeConfig)
	err = builder.Download()
	if err != nil {
		t.Fatalf("failed to download double signing node: %v", err)
	}

	if err := network.AttachNode(nodeConfig, nil); err != nil {
		t.Fatalf("failed to attach double signing node: %v", err)
	}

	doubleSignedBlocksChan := make(chan PropagatedDoubleSignedBlock, 100)

	go func() {
		// connect to the double signing node and recieve blocks
		tcpAddr, err := net.ResolveTCPAddr("tcp4", "127.0.0.1:45367")
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		// Connect to the address with tcp
		conn, err := net.DialTCP("tcp", nil, tcpAddr)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

		for {
			buffer := [68]byte{}
			_, err = io.ReadFull(conn, buffer[:])
			if err != nil {
				fmt.Println("Socket closed:", err)
				return
			}
			number := binary.BigEndian.Uint32(buffer[0:4])
			block1 := thor.BytesToBytes32(buffer[4:36])
			block2 := thor.BytesToBytes32(buffer[36:])

			if block1 != block2 {
				doubleSignedBlocksChan <- PropagatedDoubleSignedBlock{
					number,
					block1,
					block2,
				}
				fmt.Println("Sending double signed block to the channel")
			}
		}
	}()

	staker, err := builtin.NewStaker(client)
	if err != nil {
		t.Fatalf("failed to create staker: %v", err)
	}
	if err := utils.WaitForFork(t.Context(), staker, config.ForkBlock); err != nil {
		t.Fatalf("failed to wait for fork: %v", err)
	}

	// 10 to ensure that the double signing node will produce at least 2 blocks
	validationIDs := [10]thor.Bytes32{}

	// make some transactions so that the double signed blocks are different
	for i := range validationIDs {
		senders := &utils.Senders{}
		account := hayabusa.ValidatorAccounts[i]
		sender := staker.AddValidation(account.Node.Address(), builtin.MinStake(), config.MinStakingPeriod).
			Send().
			WithSigner(account.Endorser).
			WithOptions(testutil.TxOptions())
		senders.Add(sender)
		ctx := context.WithoutCancel(context.Background())
		for {
			// repeat until the transaction is included in the block
			if _, _, err := senders.Send(ctx); err != nil {
				fmt.Println("Transactions were not resolved in a double signed block: ", err)
				t.Fatalf("%v", err)
			} else {
				break
			}
		}
	}

	return staker, config, validationIDs[:], client, network.NodeConfigs(), doubleSignedBlocksChan
}

package network

import (
	"crypto/rand"
	"fmt"
	"github.com/vechain/draupnir/common"
	networkHubClient "github.com/vechain/networkhub/entrypoint/client"
	"github.com/vechain/networkhub/network"
	"github.com/vechain/networkhub/preset"
	"github.com/vechain/networkhub/thorbuilder"
	"github.com/vechain/thor/v2/thorclient"
	"time"
)

type CustomNetwork struct {
	nodeAddr     string
	chainTag     int
	smokeAccount *common.Account
	details      *ConnectionDetails
	networkHub   *networkHubClient.Client
	networkID    string
	branchName   string
	repoUrl      string
	downloadPath string
}

func NewCustomNetwork() *CustomNetwork {
	return NewCustomNetworkWithBranch("master")
}

func NewCustomNetworkWithBranch(branchName string) *CustomNetwork {
	return NewCustomNetworkWithBranchAndRepo("https://github.com/vechain/thor", branchName)
}

func NewCustomWithRepoAndDownloadPath(repoUrl string, downloadPath string) *CustomNetwork {
	return &CustomNetwork{
		details:      &ConnectionDetails{},
		repoUrl:      repoUrl,
		downloadPath: downloadPath,
	}
}

func NewCustomNetworkWithBranchAndRepo(repoUrl string, branchName string) *CustomNetwork {
	return &CustomNetwork{
		details:    &ConnectionDetails{},
		branchName: branchName,
		repoUrl:    repoUrl,
	}
}

func (c *CustomNetwork) Start() error {
	return c.StartWithNetwork(preset.LocalThreeMasterNodesNetwork())
}

func (c *CustomNetwork) StartWithNetwork(configuredNetwork *network.Network) error {
	var builder *thorbuilder.Builder
	if c.downloadPath != "" {
		builder = thorbuilder.NewWithRepoPath(c.repoUrl, c.downloadPath)
	} else {
		builder = thorbuilder.NewWithRepo(c.repoUrl, c.branchName, true)
	}
	if err := builder.Download(); err != nil {
		return err
	}

	path, err := builder.Build()
	if err != nil {
		return err
	}

	// update the thor binary path to the newly created one
	for _, node := range configuredNetwork.Nodes {
		node.SetExecArtifact(path)
		node.SetAPIAddr(fmt.Sprintf("127.0.0.1:%d", rndPort()))
		node.SetP2PListenPort(rndPort())
	}
	c.networkHub = networkHubClient.New()
	c.networkID, err = c.networkHub.Config(configuredNetwork)
	if err != nil {
		return err
	}

	// TODO this never stops -
	err = c.networkHub.Start(c.networkID)
	if err != nil {
		return err
	}

	// todo replace this with a health check
	time.Sleep(30 * time.Second)

	c.details.NetworkCfg = configuredNetwork
	c.details.Address = configuredNetwork.Nodes[0].GetHTTPAddr()
	c.details.SmokeAccount = common.NewAccount(configuredNetwork.Nodes[0].GetKey())

	// update the chainID for custom networks
	tmpClient := thorclient.New(c.details.Address)
	chainTag, err := tmpClient.ChainTag()
	if err != nil {
		return err
	}
	c.details.ChainTag = int(chainTag)

	return nil
}

func (c *CustomNetwork) Stop() error {
	return c.networkHub.Stop(c.networkID)
}

func (c *CustomNetwork) Details() *ConnectionDetails {
	return c.details
}

func rndPort() int {
	const (
		minPort = 49152
		maxPort = 65535
	)
	buf := make([]byte, 2)
	// Ignoring the error for brevity—not recommended in production code!
	_, _ = rand.Read(buf)

	// Convert 2 bytes to a 16-bit number, then mod by the range size.
	n := int(buf[0])<<8 | int(buf[1])
	return minPort + (n % (maxPort - minPort + 1))
}

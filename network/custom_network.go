package network

import (
	"crypto/rand"
	"fmt"
	"time"

	networkHubClient "github.com/vechain/networkhub/entrypoint/client"
	"github.com/vechain/networkhub/network"
	"github.com/vechain/networkhub/network/node"
	"github.com/vechain/networkhub/preset"
	"github.com/vechain/networkhub/thorbuilder"
	"github.com/vechain/thor/v2/thorclient"
)

type CustomNetwork struct {
	nodeAddr     string
	details      *ConnectionDetails
	networkHub   *networkHubClient.Client
	networkID    string
	branchName   string
	repoUrl      string
	downloadPath string
	debug        bool
}

func NewCustomNetwork() *CustomNetwork {
	return NewCustomNetworkWithBranch("master")
}

func NewCustomNetworkWithBranch(branchName string) *CustomNetwork {
	return NewCustomNetworkWithBranchAndRepo("https://github.com/vechain/thor", branchName)
}

func NewCustomWithRepoAndDownloadPath(repoUrl string, downloadPath string, debug bool) *CustomNetwork {
	return &CustomNetwork{
		details:      &ConnectionDetails{},
		repoUrl:      repoUrl,
		downloadPath: downloadPath,
		debug:        debug,
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
		builder = thorbuilder.NewWithRepoPath(c.repoUrl, c.downloadPath, c.debug)
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

	used := make(map[int]bool)
	for _, node := range configuredNetwork.Nodes {
		node.SetExecArtifact(path)
		node.SetAPIAddr(fmt.Sprintf("127.0.0.1:%d", rndPort(used)))
		node.SetP2PListenPort(rndPort(used))
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

	if err = configuredNetwork.HealthCheck(0, 20*time.Second); err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}

	c.details.NetworkCfg = configuredNetwork
	c.details.Address = configuredNetwork.Nodes[0].GetHTTPAddr()

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

func (c *CustomNetwork) Nodes() map[string]node.Lifecycle {
	return c.networkHub.Nodes(c.networkID)
}

func rndPort(used map[int]bool) int {
	const (
		minPort = 49152
		maxPort = 65535
	)
	for {
		buf := make([]byte, 2)
		// Ignoring the error for brevity—not recommended in production code!
		_, _ = rand.Read(buf)

		// Convert 2 bytes to a 16-bit number, then mod by the range size.
		n := int(buf[0])<<8 | int(buf[1])
		port := minPort + (n % (maxPort - minPort + 1))
		if _, ok := used[port]; !ok {
			used[port] = true
			return port
		}
	}
}

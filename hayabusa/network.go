package hayabusa

import "context"

type NetworkV2 struct {
	ctx    context.Context
	config *Config
}

func NewNetworkV2(config *Config, ctx context.Context) *NetworkV2 {
	return &NetworkV2{
		ctx:    ctx,
		config: config,
	}
}

func (n *NetworkV2) Start() error {
	return nil
}

func (n *NetworkV2) Stop() error {
	return nil
}

func (n *NetworkV2) Nodes()

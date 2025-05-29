package network

import (
	"github.com/ethereum/go-ethereum/common/hexutil"
)

var (
	networkHubCustom3Nodes = &ConnectionDetails{
		Address:  "http://localhost:8181",
		ChainTag: int(hexutil.MustDecode("0x6e")[0]), // 110
	}

	thorSolo = &ConnectionDetails{
		Address:  "http://localhost:8669",
		ChainTag: int(hexutil.MustDecode("0xf6")[0]), // 246
	}

	testnet = &ConnectionDetails{
		Address:  "https://testnet.green.prd.node.vechain.org",
		ChainTag: int(hexutil.MustDecode("0x27")[0]), // 39
	}

	mainnet = &ConnectionDetails{
		Address:  "https://mainnet.blue.prod.node.vechain.org",
		ChainTag: int(hexutil.MustDecode("0x4a")[0]), // 74
	}
)

package network

import (
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/vechain/draupnir/common"
)

var (
	networkHubCustom3Nodes = &ConnectionDetails{
		Address:      "http://localhost:8181",
		ChainTag:     int(hexutil.MustDecode("0x6e")[0]), // 110
		SmokeAccount: common.NewAccount("01a4107bfb7d5141ec519e75788c34295741a1eefbfe460320efd2ada944071e"),
	}

	thorSolo = &ConnectionDetails{
		Address:      "http://localhost:8669",
		ChainTag:     int(hexutil.MustDecode("0xf6")[0]), // 246
		SmokeAccount: common.NewAccount("99f0500549792796c14fed62011a51081dc5b5e68fe8bd8a13b86be829c4fd36"),
	}

	testnet = &ConnectionDetails{
		Address:      "https://testnet.green.prd.node.vechain.org",
		ChainTag:     int(hexutil.MustDecode("0x27")[0]), // 39
		SmokeAccount: common.NewAccount("99f0500549792796c14fed62011a51081dc5b5e68fe8bd8a13b86be829c4fd36"),
	}

	mainnet = &ConnectionDetails{
		Address:      "https://mainnet.blue.prod.node.vechain.org",
		ChainTag:     int(hexutil.MustDecode("0x4a")[0]), // 74
		SmokeAccount: common.NewAccount("99f0500549792796c14fed62011a51081dc5b5e68fe8bd8a13b86be829c4fd36"),
	}
)

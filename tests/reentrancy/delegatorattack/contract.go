package delegatorattack

import _ "embed"

//go:embed ReentrancyAttacker.abi
var ABI []byte

//go:embed ReentrancyAttacker.bin
var Bin string

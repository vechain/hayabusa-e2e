package validatorattack

import _ "embed"

//go:embed ValidatorReentrancyAttacker.abi
var ABI []byte

//go:embed ValidatorReentrancyAttacker.bin
var Bin string

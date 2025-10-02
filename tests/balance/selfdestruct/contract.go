package selfdestruct

import _ "embed"

//go:embed SelfDestructible.abi
var ABI []byte

//go:embed SelfDestructible.bin
var Bin string

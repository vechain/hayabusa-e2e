package receiverevert

import _ "embed"

//go:embed ReceiveRevert.abi
var ABI []byte

//go:embed ReceiveRevert.bin
var Bin string

package noreceive

import _ "embed"

//go:embed NoReceive.abi
var ABI []byte

//go:embed NoReceive.bin
var Bin string

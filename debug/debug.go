package debug

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/vechain/thor/v2/api/transactions"
	"math/big"
	"strconv"

	ethabi "github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/crypto"
	thordebug "github.com/vechain/thor/v2/api/debug"
	"github.com/vechain/thor/v2/thorclient/httpclient"
)

type Debug struct {
	client *httpclient.Client
}

func NewDebugger(client *httpclient.Client) *Debug {
	return &Debug{
		client: client,
	}
}

type RevertResponse struct {
	From    string `json:"from"`
	Gas     string `json:"gas"`
	GasUsed string `json:"gasUsed"`
	To      string `json:"to"`
	Input   string `json:"input"`
	Output  string `json:"output"`
	Value   string `json:"value"`
	Type    string `json:"type"`
}

func (d *Debug) DebugRevert(receipt *transactions.Receipt, clauseIndex uint32) (*RevertResponse, string, error) {
	target := receipt.Meta.BlockID.String() + "/" + receipt.Meta.TxID.String() + "/" + strconv.Itoa(int(clauseIndex))
	config := []byte(`{
		"OnlyTopCall": true
	}`)
	request := &thordebug.TraceClauseOption{
		Target: target,
		Name:   "call",
		Config: config,
	}
	response, statusCode, err := d.client.RawHTTPPost("/debug/tracers", request)
	if err != nil {
		return nil, "", err
	}
	if statusCode != 200 {
		return nil, "", errors.New("failed to get debug revert: " + string(response))
	}
	var revertResponse RevertResponse
	if err := json.Unmarshal(response, &revertResponse); err != nil {
		return nil, "", err
	}
	unpacked, _ := UnpackRevert([]byte(revertResponse.Output))
	return &revertResponse, unpacked, nil
}

// revertSelector is a special function selector for revert reason unpacking.
var revertSelector = crypto.Keccak256([]byte("Error(string)"))[:4]

// panicSelector is a special function selector for panic reason unpacking.
var panicSelector = crypto.Keccak256([]byte("Panic(uint256)"))[:4]

// panicReasons map is for readable panic codes
// see this linkage for the details
// https://docs.soliditylang.org/en/v0.8.21/control-structures.html#panic-via-assert-and-error-via-require
// the reason string list is copied from ether.js
// https://github.com/ethers-io/ethers.js/blob/fa3a883ff7c88611ce766f58bdd4b8ac90814470/src.ts/abi/interface.ts#L207-L218
var panicReasons = map[uint64]string{
	0x00: "generic panic",
	0x01: "assert(false)",
	0x11: "arithmetic underflow or overflow",
	0x12: "division or modulo by zero",
	0x21: "enum overflow",
	0x22: "invalid encoded storage byte array accessed",
	0x31: "out-of-bounds array access; popping on an empty array",
	0x32: "out-of-bounds access of an array or bytesN",
	0x41: "out of memory",
	0x51: "uninitialized function",
}

// UnpackRevert resolves the abi-encoded revert reason. According to the solidity
// spec https://solidity.readthedocs.io/en/latest/control-structures.html#revert,
// the provided revert reason is abi-encoded as if it were a call to function
// `Error(string)` or `Panic(uint256)`. So it's a special tool for it.
func UnpackRevert(data []byte) (string, error) {
	if len(data) < 4 {
		return "", errors.New("invalid data for unpacking")
	}
	switch {
	case bytes.Equal(data[:4], revertSelector):
		typ, err := ethabi.NewType("string")
		if err != nil {
			return "", err
		}
		var unpacked string
		if err := (ethabi.Arguments{{Type: typ}}).Unpack(&unpacked, data[4:]); err != nil {
			return "", err
		}
		return unpacked, nil
	case bytes.Equal(data[:4], panicSelector):
		typ, err := ethabi.NewType("uint256")
		if err != nil {
			return "", err
		}
		var pCode *big.Int
		if err := (ethabi.Arguments{{Type: typ}}).Unpack(&pCode, data[4:]); err != nil {
			return "", err
		}
		// uint64 safety check for future
		// but the code is not bigger than MAX(uint64) now
		if pCode.IsUint64() {
			if reason, ok := panicReasons[pCode.Uint64()]; ok {
				return reason, nil
			}
		}
		return fmt.Sprintf("unknown panic code: %#x", pCode), nil
	default:
		return "", errors.New("invalid data for unpacking")
	}
}

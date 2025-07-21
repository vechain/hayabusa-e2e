package utils

import "math/big"

var (
	VET = big.NewInt(1e18) // 1 VET in wei
)

// ScaleToVET scales a value in wei to VET.
func ScaleToVET(value *big.Int) *big.Int {
	if value == nil {
		return big.NewInt(0) // Return 0 if value is nil
	}
	return new(big.Int).Div(value, VET)
}

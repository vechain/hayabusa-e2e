// Copyright (c) 2018 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package stargate

//go:generate sh -c "rm -rf *abi *bin-runtime"
//go:generate solc --evm-version paris --overwrite --optimize --via-ir --optimize-runs 200 -o ./ --abi --bin stargate.sol
//go:generate sh -c "rm -rf IERC20* IStaker*"

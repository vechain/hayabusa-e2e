// Copyright (c) 2018 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package stargate

//go:generate sh -c "rm -rf *abi *bin-runtime"
//go:generate sh -c "rm -rf contracts && mkdir contracts && cd contracts && curl -o StargateDelegation.sol https://raw.githubusercontent.com/vechain/stargate-contracts/refs/heads/main/packages/contracts/contracts/StargateDelegation/StargateDelegation.sol"
//go:generate sh -c "rm -rf interfaces && mkdir interfaces && cd interfaces && curl -o IStargateDelegation.sol  https://raw.githubusercontent.com/vechain/stargate-contracts/refs/heads/main/packages/contracts/contracts/interfaces/IStargateDelegation.sol"
//go:generate sh -c "cd interfaces && curl -o IStargateNFT.sol  https://raw.githubusercontent.com/vechain/stargate-contracts/refs/heads/main/packages/contracts/contracts/interfaces/IStargateNFT.sol"
//go:generate sh -c "cd interfaces && curl -o ITokenAuction.sol  https://raw.githubusercontent.com/vechain/stargate-contracts/refs/heads/main/packages/contracts/contracts/interfaces/ITokenAuction.sol"
//go:generate sh -c "rm -rf StargateNFT && mkdir StargateNFT && mkdir StargateNFT/libraries && cd StargateNFT/libraries && curl -o DataTypes.sol  https://raw.githubusercontent.com/vechain/stargate-contracts/refs/heads/main/packages/contracts/contracts/StargateNFT/libraries/DataTypes.sol"

//go:generate sh -c "rm -rf contracts/openzeppelin"
//go:generate sh -c "mkdir -p contracts/openzeppelin"
//go:generate sh -c "curl -L -o openzeppelin.zip https://github.com/OpenZeppelin/openzeppelin-contracts/archive/refs/tags/v5.0.0.zip"
//go:generate sh -c "unzip -q openzeppelin.zip"
//go:generate sh -c "cp -r openzeppelin-contracts-5.0.0/contracts/* contracts/openzeppelin/"

//go:generate sh -c "rm -rf contracts/openzeppelin-upgradeable"
//go:generate sh -c "mkdir -p contracts/openzeppelin-upgradeable"
//go:generate sh -c "curl -L -o openzeppelin-upgradeable.zip https://github.com/OpenZeppelin/openzeppelin-contracts-upgradeable/archive/refs/tags/v5.0.0.zip"
//go:generate sh -c "unzip -q openzeppelin-upgradeable.zip"
//go:generate sh -c "cp -r openzeppelin-contracts-upgradeable-5.0.0/contracts/* contracts/openzeppelin-upgradeable"
//go:generate sh -c "rm -rf openzeppelin.zip openzeppelin-contracts-5.0.0 openzeppelin-upgradeable.zip openzeppelin-contracts-upgradeable-5.0.0"

//go:generate sh -c "docker run -v ./:/sources ethereum/solc:0.8.20 @openzeppelin/contracts-upgradeable=/sources/contracts/openzeppelin-upgradeable @openzeppelin/contracts=/sources/contracts/openzeppelin/  --evm-version paris --overwrite --optimize --via-ir --optimize-runs 200 -o /sources/ --abi --bin /sources/contracts/StargateDelegation.sol /sources/stargate.sol"
//go:generate sh -c "rm -rf IERC20* IStaker* contracts interfaces StargateNFT A*.abi A*.bin C*.abi C*.bin c*.abi c*.bin D*.abi D*.bin E*.abi E*.bin I*.abi I*.bin M*.abi M*.bin R*.abi R*.bin Sa*.abi Sa*.bin Sto*.abi Sto*.bin sta*.abi sta*.bin T*.abi T*.bin U*.abi U*.bin"

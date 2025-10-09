package delegatorattack

//go:generate curl -o ./staker.sol https://raw.githubusercontent.com/vechain/thor/refs/heads/release/hayabusa/builtin/gen/staker.sol
//go:generate sh -c "docker run -v ./:/sources ethereum/solc:0.8.20  --evm-version paris --overwrite --optimize --via-ir --optimize-runs 200 -o /sources/ --abi --bin /sources/ReentrancyAttacker.sol"
//go:generate rm staker.sol Staker.Bin Staker.abi StakerNative.abi StakerNative.bin

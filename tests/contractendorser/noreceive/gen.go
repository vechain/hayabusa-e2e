package noreceive

//go:generate curl https://raw.githubusercontent.com/vechain/thor/refs/heads/release/hayabusa/builtin/gen/staker.sol -o ./Staker.sol
//go:generate sh -c "docker run -v ./:/sources ethereum/solc:0.8.20  --evm-version paris --overwrite --optimize --via-ir --optimize-runs 200 -o /sources/ --abi --bin /sources/NoReceive.sol"

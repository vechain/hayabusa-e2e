package selfdestruct

//go:generate sh -c "docker run -v ./:/sources ethereum/solc:0.8.20  --evm-version paris --overwrite --optimize --via-ir --optimize-runs 200 -o /sources/ --abi --bin /sources/SelfDestruct.sol"

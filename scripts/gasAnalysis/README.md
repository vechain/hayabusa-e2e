# EVM Gas Analyzer

A tool for analyzing the gas usage of Ethereum smart contracts, including function-level breakdowns and state preservation between calls.

## Features

- Deploy contracts to a local PyEVM instance
- Analyze gas usage of all public functions
- Break down gas costs (base transaction costs vs. contract execution)
- Maintain state between function calls
- Test view/pure functions
- Export results to JSON
- Support for different Solidity versions

## Installation

Install dependencies:

```bash
pip install eth-tester py-evm web3 py-solc-x
```

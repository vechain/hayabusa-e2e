from web3 import Web3

# Read the contract source
with open('staker.sol', 'r') as file:
    contract_source = file.read()

# Create test addresses


def create_test_addresses(w3):
    return [w3.eth.account.create().address for _ in range(3)]

# Set up test parameters


def get_test_parameters(w3):
    validators = create_test_addresses(w3)
    beneficiaries = create_test_addresses(w3)

    # Current block number + valid staking periods
    current_block = 1000
    # Well within MIN_STAKING_PERIOD and MAX_STAKING_PERIOD
    expiry = current_block + 1000

    # Add parameters for view functions
    view_params = {
        'totalStake': [],
        'activeStake': [],
        'leaderGroupSize': [],
        'maxLeaderGroupSize': [],
        'previousExit': [],
        'activeHead': [],
        'activeTail': [],
        'queuedHead': [],
        'queuedTail': [],
        'validators': [validators[0]],  # Test validator lookup
        'MIN_STAKE': [],
        'MAX_STAKE': [],
        'MIN_STAKING_PERIOD': [],
        'MAX_STAKING_PERIOD': []
    }

    return {
        'addValidator': [
            validators[0], beneficiaries[0], expiry
        ],
        'withdrawStake': [
            validators[0]
        ],
        **view_params  # Add all view function parameters
    }

# Run the analysis


def run_staker_analysis():
    from evm_gas_analyzer import EVMGasAnalyzer

    # Initialize analyzer with Paris fork (The Merge)
    analyzer = EVMGasAnalyzer(contract_source, fork='paris')
    w3 = analyzer.w3
    params = get_test_parameters(w3)

    print("\nRunning Staker Contract Analysis...")

    # Group functions by type based on ABI
    functions = {
        'view': [],
        'nonpayable': [],
        'payable': []
    }

    for func in analyzer.deployed_contract.abi:
        if func['type'] != 'function':
            continue
        functions[func.get('stateMutability', 'nonpayable')
                  ].append(func['name'])

    # Analyze view functions
    print("\nAnalyzing View Functions:")
    for func_name in functions['view']:
        args = params.get(func_name, [])
        result = analyzer.get_function_gas_usage(func_name, *args)
        if result:
            print(f"{func_name}: {result['gas_estimate']} gas")

    # Analyze payable functions
    for func_name in functions['payable']:
        args = params.get(func_name, [])
        value = w3.to_wei(
            1000, 'ether') if func_name == 'addValidator' else 0
        print(value)
        result = analyzer.get_function_gas_usage(
            func_name, *args, value=value)
        if result:
            print(f"\n{func_name}:")
            print(f"  Total Gas Units: {result['gas_used']:,} gas")
            print(f"  └─ Base Transaction: {result['base_tx_cost']:,} gas")
            print(
                f"  └─ Contract Execution: {result['contract_execution_cost']:,} gas")
            if result['gas_estimate'] != result['gas_used']:
                print(f"  Estimated Gas: {result['gas_estimate']:,} gas")

    # Analyze non-payable functions
    print("\nAnalyzing Non-payable Functions:")
    for func_name in functions['nonpayable']:
        args = params.get(func_name, [])
        result = analyzer.get_function_gas_usage(func_name, *args)
        if result:
            print(f"\n{func_name}:")
            print(f"  Total Gas Units: {result['gas_used']:,} gas")
            print(f"  └─ Base Transaction: {result['base_tx_cost']:,} gas")
            print(
                f"  └─ Contract Execution: {result['contract_execution_cost']:,} gas")
            if result['gas_estimate'] != result['gas_used']:
                print(f"  Estimated Gas: {result['gas_estimate']:,} gas")

    analyzer.export_results('staker_gas_analysis.json')
    return analyzer


if __name__ == "__main__":
    analyzer = run_staker_analysis()

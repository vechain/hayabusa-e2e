from eth_tester import EthereumTester, PyEVMBackend
from eth_utils import to_wei
from web3 import Web3
from solcx import compile_source
import json


class EVMGasAnalyzer:
    def __init__(self, contract_source, fork='paris', solidity_version='0.8.20'):
        """
        Initialize EVM Gas Analyzer

        Args:
            contract_source (str): Solidity contract source code
            fork (str): EVM fork to use ('paris', 'shanghai', etc.)
        """
        import solcx
        try:
            solcx.install_solc(solidity_version)
            solcx.set_solc_version(solidity_version)
        except Exception as e:
            print(f"Error setting Solidity version: {e}")
            print("Available versions:", solcx.get_installed_solc_versions())
            raise

        SUPPORTED_FORKS = {
            'paris': 'paris',  # The Merge
            'shanghai': 'shanghai',
            'cancun': 'cancun'
        }

        if fork not in SUPPORTED_FORKS:
            raise ValueError(
                f"Unsupported fork. Choose from: {list(SUPPORTED_FORKS.keys())}")

        fork_backend = PyEVMBackend()
        fork_backend.fork = SUPPORTED_FORKS[fork]

        self.eth_tester = EthereumTester(fork_backend)
        self.w3 = Web3(Web3.EthereumTesterProvider(self.eth_tester))

        # Set default account
        self.account = self.w3.eth.accounts[0]
        self.w3.eth.default_account = self.account

        # Compile contract
        self.compiled_sol = compile_source(contract_source,
                                           output_values=['abi', 'bin'],
                                           solc_version=solidity_version)
        contract_id, contract_interface = self.compiled_sol.popitem()

        # Deploy contract
        self.contract = self.w3.eth.contract(
            abi=contract_interface['abi'],
            bytecode=contract_interface['bin']
        )
        tx_hash = self.contract.constructor().transact()
        tx_receipt = self.w3.eth.wait_for_transaction_receipt(tx_hash)
        self.deployed_contract = self.w3.eth.contract(
            address=tx_receipt.contractAddress,
            abi=contract_interface['abi']
        )

        # Store transaction history
        self.transaction_history = []

    def get_function_gas_usage(self, function_name, *args, **kwargs):
        """
        Execute a function and return its gas usage (in gas units) while preserving state.
        For view/pure functions, only estimates gas usage.

        Returns:
            dict: Gas costs in gas units (not wei/ether):
                - total_gas_used: Total gas units used
                - base_tx_cost: Base transaction gas units (21000 + data costs)
                - contract_execution_cost: Gas units used by contract execution
                - gas_estimate: Estimated gas units
        """
        try:
            # Get function from contract
            contract_function = getattr(
                self.deployed_contract.functions, function_name)

            # Get base transaction costs
            base_tx = {
                'to': self.deployed_contract.address,
                'from': self.account,
                'data': contract_function(*args)._encode_transaction_data(),
                'gas': 21000,  # minimum gas
                'gasPrice': self.w3.eth.gas_price,
                'nonce': self.w3.eth.get_transaction_count(self.account),
            }

            # Add value if specified in kwargs
            tx_params = {}
            if 'value' in kwargs:
                base_tx['value'] = kwargs['value']
                tx_params['value'] = kwargs['value']

            # Calculate base costs (21000 gas + data costs)
            base_cost = 21000  # Base transaction cost
            tx_data = base_tx['data']

            # Convert hex string to bytes for counting
            if isinstance(tx_data, str) and tx_data.startswith('0x'):
                tx_data = bytes.fromhex(tx_data[2:])

            # Count zero and non-zero bytes
            zero_bytes = sum(1 for b in tx_data if b == 0)
            non_zero_bytes = len(tx_data) - zero_bytes

            # Calculate costs: 4 gas for zero bytes, 16 gas for non-zero bytes
            base_cost += zero_bytes * 4
            vechain_base_cost = base_cost + non_zero_bytes * 68
            base_cost += non_zero_bytes * 16

            # Estimate total gas
            gas_estimate = contract_function(*args).estimate_gas(tx_params)

            # Execute transaction
            tx_hash = contract_function(*args).transact(tx_params)
            tx_receipt = self.w3.eth.wait_for_transaction_receipt(tx_hash)

            # Calculate contract execution cost
            contract_cost = tx_receipt['gasUsed'] - base_cost

            # Store transaction details
            tx_details = {
                'function': function_name,
                'args': args,
                'gas_used': contract_cost + vechain_base_cost,
                'base_tx_cost': vechain_base_cost,
                'contract_execution_cost': contract_cost,
                'gas_estimate': (gas_estimate-base_cost) + vechain_base_cost,
                'block_number': tx_receipt['blockNumber']
            }
            self.transaction_history.append(tx_details)

            return tx_details

        except Exception as e:
            print(f"Error executing {function_name}: {str(e)}")
            return None

    def analyze_all_public_functions(self, function_params=None, include_view=True):
        """
        Run all public functions with provided parameters and analyze gas usage

        Args:
            function_params (dict): Dictionary mapping function names to their parameters
            include_view (bool): Whether to include view/pure functions in analysis
        """
        if function_params is None:
            function_params = {}

        results = []

        # Get all public functions from ABI
        public_functions = [
            func for func in self.deployed_contract.abi
            if func['type'] == 'function' and
            (include_view or func['stateMutability'] not in ['view', 'pure'])
        ]

        for func in public_functions:
            func_name = func['name']
            params = function_params.get(func_name, [])

            result = self.get_function_gas_usage(func_name, *params)
            if result:
                results.append(result)

        return results

    def get_transaction_history(self):
        """
        Return the full transaction history with gas usage
        """
        return self.transaction_history

    def export_results(self, filename='gas_analysis.json'):
        """
        Export gas analysis results to a JSON file
        """
        with open(filename, 'w') as f:
            json.dump(self.transaction_history, f, indent=2)

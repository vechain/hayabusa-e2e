// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract SelfDestructible {
    constructor() payable {}

    // Allow it to receive Ether after deployment as well
    receive() external payable {}

    // Trigger selfdestruct — only the deployer can call this
    function destroy() external {
        selfdestruct(payable(address(0x00000000000000000000000000005374616B6572)));
    }

    // Helper to check the contract balance
    function getBalance() external view returns (uint) {
        return address(this).balance;
    }
}
// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract SelfDestructible {
    constructor() payable {}

    receive() external payable {}

    function destroy() external {
        selfdestruct(payable(address(0x00000000000000000000000000005374616B6572)));
    }
}
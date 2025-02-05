// SPDX-License-Identifier: MIT
pragma solidity 0.8.20;

contract Staker {
    uint256 public constant MAX_VALIDATORS = 101;
    uint256 public constant MIN_STAKE = 25000000 ether;
    uint256 public constant MAX_STAKE = 180000000 ether;

    event ValidatorAdded(address indexed beneficiary, uint256 stake);

    mapping(address => uint256) public balances;

    function addValidator(address beneficiary) public payable {
        require(msg.value >= MIN_STAKE, 'Insufficient Staked');
        require(msg.value <= MAX_STAKE, 'Exceeded Maximum Stake');

        emit ValidatorAdded(beneficiary, msg.value);
    }
}

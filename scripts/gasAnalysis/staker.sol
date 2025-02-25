// SPDX-License-Identifier: LGPL-3.0
pragma solidity ^0.8.0;

contract Staker {
    // Constants
    uint256 public constant MIN_STAKE = 10 ether;
    uint256 public constant MAX_STAKE = 400_000_000 ether;
    uint32 public constant MIN_STAKING_PERIOD = 1;
    uint32 public constant MAX_STAKING_PERIOD = 2000000000;

    enum Status {
        Unknown,
        Queued,
        Active,
        Cooldown,
        Exit
    }

    struct Validator {
        address beneficiary;
        uint32 expiry;
        uint256 stake;
        uint256 weight;
        address next;
        address prev;
        Status status;
        uint64 missedSlots;
    }

    uint256 public totalStake;
    uint256 public activeStake;
    uint256 public leaderGroupSize;
    uint256 public maxLeaderGroupSize;
    uint32 public previousExit;

    mapping(address => Validator) public validators;

    // Linked list pointers
    address public activeHead;
    address public activeTail;
    address public queuedHead;
    address public queuedTail;

    // Events
    event ValidatorAdded(
        address indexed validator,
        address beneficiary,
        uint32 expiry,
        uint256 stake
    );
    event ValidatorActivated(address indexed validator);
    event ValidatorExited(address indexed validator);
    event ValidatorWithdrawn(address indexed validator, uint256 amount);
    event MissedSlot(address indexed validator);

    error ValidatorExists();
    error ValidatorNotFound();
    error InvalidStakeAmount();
    error InvalidStakingPeriod();
    error LeaderGroupFull();
    error QueueEmpty();
    error NotInExitStatus();
    error InvalidValidatorStatus();

    // Helper functions for linked list operations
    function _addToQueue(address newTail, Validator storage validator) private {
        if (queuedHead == address(0)) {
            queuedHead = newTail;
            queuedTail = newTail;
        } else {
            validators[queuedTail].next = newTail;
            validator.prev = queuedTail;
            queuedTail = newTail;
        }
    }

    function _addToLeaderGroup(
        address newTail,
        Validator storage validator
    ) private {
        if (activeHead == address(0)) {
            activeHead = newTail;
            activeTail = newTail;
        } else {
            validators[activeTail].next = newTail;
            validator.prev = activeTail;
            activeTail = newTail;
        }
    }

    function _removeFromQueue() private returns (address validator) {
        require(queuedHead != address(0), 'Queue is empty');

        validator = queuedHead;
        Validator storage head = validators[queuedHead];

        if (head.next == address(0)) {
            queuedHead = address(0);
            queuedTail = address(0);
        } else {
            queuedHead = head.next;
            validators[queuedHead].prev = address(0);
        }

        head.next = address(0);
        head.prev = address(0);
    }

    function _removeFromLeaderGroup(address validatorAddr) private {
        Validator storage validator = validators[validatorAddr];

        if (validator.prev == address(0)) {
            activeHead = validator.next;
        } else {
            validators[validator.prev].next = validator.next;
        }

        if (validator.next == address(0)) {
            activeTail = validator.prev;
        } else {
            validators[validator.next].prev = validator.prev;
        }

        validator.next = address(0);
        validator.prev = address(0);
    }

    // Main contract functions.

    // max possible gas usage: 181489
    function addValidator(
        address validatorAddr,
        address beneficiary,
        uint32 expiry
    ) external payable {
        if (msg.value < MIN_STAKE || msg.value > MAX_STAKE) {
            revert InvalidStakeAmount();
        }

        Validator storage validator = validators[validatorAddr];
        if (validator.stake != 0) {
            revert ValidatorExists();
        }

        uint32 period = expiry - uint32(block.number);
        if (period < MIN_STAKING_PERIOD || period > MAX_STAKING_PERIOD) {
            revert InvalidStakingPeriod();
        }

        validator.beneficiary = beneficiary;
        validator.expiry = expiry;
        validator.stake = msg.value;
        validator.weight = msg.value;
        validator.status = Status.Exit; // set to Exit, to immediately withdraw after

        _addToQueue(validatorAddr, validator);
        totalStake += msg.value;

        emit ValidatorAdded(validatorAddr, beneficiary, expiry, msg.value);
    }

    // 63442 max possible gas usage
    function withdrawStake(
        address payable validatorAddr
    ) external returns (uint256) {
        Validator storage validator = validators[validatorAddr];

        if (validator.stake == 0) {
            revert NotInExitStatus();
        }

        if (validator.status != Status.Exit) {
            revert NotInExitStatus();
        }

        uint256 stake = validator.stake;
        delete validators[validatorAddr];

        (bool sent, bytes memory data) = validatorAddr.call{value: stake}('');
        require(sent, 'Failed to send Ether');

        emit ValidatorWithdrawn(validatorAddr, stake);
        return stake;
    }
}

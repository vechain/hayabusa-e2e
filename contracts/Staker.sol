// SPDX-License-Identifier: MIT
pragma solidity 0.8.20;

/**
* @title Staker
* @dev A dummy contract for staking and managing validators. Any function beginning with `protocol_` is considered a
* protocol function and is not intended to be release in the official builtin Staker
*/

contract Staker {
    uint256 public constant MAX_VALIDATORS = 101;
    uint256 public constant MIN_STAKE = 25000000 ether;
    uint256 public constant MAX_STAKE = 180000000 ether;

    enum ValidatorStatus { Queued, Active, Cooldown, Inactive }

    struct Validator {
        address beneficiary;
        uint256 stake;
        ValidatorStatus status;
        address next;
        address prev;
    }

    event ValidatorQueued(address indexed beneficiary, uint256 stake);

    mapping(address => Validator) public validators;
    address private _activeHead;
    address private _activeTail;

    address private _queuedHead;
    address private _queuedTail;


    /**
    * @dev A public function for any account to signal intent to become a validator
    * @param beneficiary - the address that will receive the rewards
    */
    function addValidator(address beneficiary) public payable {
        require(msg.value >= MIN_STAKE, 'Insufficient Staked');
        require(msg.value <= MAX_STAKE, 'Exceeded Maximum Stake');
        require(validators[msg.sender].stake == 0, 'Validator already exists');

        if (_queuedHead == address(0)) {
            _queuedHead = msg.sender;
            _queuedTail = msg.sender;
        }

        validators[_queuedTail].next = msg.sender;

        validators[msg.sender] = Validator({
            beneficiary: beneficiary,
            stake: msg.value,
            status: ValidatorStatus.Queued,
            next: address(0),
            prev: _queuedTail
        });

        _queuedTail = msg.sender;

        emit ValidatorQueued(beneficiary, msg.value);
    }

    /**
    * @dev A public function to remove a validator from the queue
    */
    function removeValidator() public {
        require(validators[msg.sender].stake > 0, 'Validator does not exist');
        require(validators[msg.sender].status == ValidatorStatus.Queued, 'Validator is not Queued');

        uint256 stake = validators[msg.sender].stake;
        delete validators[msg.sender];
        payable(msg.sender).transfer(stake);
    }

    /**
    * @dev A public function to increase the stake of a validator while in the queue
    */
    function addValidatorStake() public payable {
        require(validators[msg.sender].stake > 0, 'Validator does not exist');
        require(validators[msg.sender].status == ValidatorStatus.Queued, 'Validator is not Queued');
        require(validators[msg.sender].stake + msg.value <= MAX_STAKE, 'Exceeded Maximum Stake');

        validators[msg.sender].stake += msg.value;
    }

    /**
    * @dev A public function to decrease the stake of a validator while in the queue
    */
    function removeValidatorStake(uint256 amount) public {
        require(validators[msg.sender].stake > 0, 'Validator does not exist');
        require(validators[msg.sender].status == ValidatorStatus.Queued, 'Validator is not Queued');
        require(validators[msg.sender].stake - amount >= MIN_STAKE, 'Insufficient Staked');

        validators[msg.sender].stake -= amount;
        payable(msg.sender).transfer(amount);
    }

    /**
    * @dev A public function to get the head of the active validators
    */
    function activeHead() public view returns (Validator memory) {
        return validators[_activeHead];
    }

    function activeNext(address validator) public view returns (Validator memory) {
        return validators[validator];
    }

    /**
    * @dev A public function to get the head of the queued validators
    */
    function queuedHead() public view returns (Validator memory) {
        return validators[_queuedHead];
    }

    function queuedNext(address validator) public view returns (Validator memory) {
        return validators[validator];
    }

    /**
    * @dev A protocol function to activate a validator
    */
    function protocol_activateValidator(address validator) public {
        require(validators[validator].stake > 0, 'Validator does not exist');
        require(validators[validator].status == ValidatorStatus.Queued, 'Validator is not Queued');

        validators[validator].status = ValidatorStatus.Active;
    }
}

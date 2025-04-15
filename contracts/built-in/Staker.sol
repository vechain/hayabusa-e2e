//SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

interface Staker {
    event ValidatorQueued(
        address indexed endorsor,
        address indexed master,
        bytes32 indexed validationID,
        uint32 period,
        uint256 stake,
        bool autoRenew
    );
    event ValidatorWithdrawn(
        address indexed endorsor,
        bytes32 indexed validationID,
        uint256 stake
    );
    event ValidatorUpdatedAutoRenew(
        address indexed endorsor,
        bytes32 indexed validationID,
        bool autoRenew
    );

    event StakeIncreased(
        address indexed endorsor,
        bytes32 indexed validationID,
        uint256 added
    );
    event StakeDecreased(
        address indexed endorsor,
        bytes32 indexed validationID,
        uint256 removed
    );

    event DelegationAdded(
        bytes32 indexed validationID,
        address indexed delegator,
        uint256 stake,
        bool autoRenew,
        uint8 multiplier
    );
    event DelegationWithdrawn(
        bytes32 indexed validationID,
        address indexed delegator,
        uint256 stake
    );
    event DelegationUpdatedAutoRenew(
        bytes32 indexed validationID,
        address indexed delegator,
        bool autoRenew
    );

    function totalStake() external view returns (uint256);
    function addValidator(
        address master,
        uint32 period,
        bool autoRenew
    ) external payable;
    function increaseStake(bytes32 validationID) external payable;
    function decreaseStake(bytes32 id, uint256 amount) external;
    function withdraw(bytes32 id) external;
    function updateAutoRenew(bytes32 id, bool autoRenew) external;
    function addDelegation(
        bytes32 validationID,
        address delegator,
        bool autoRenew,
        uint8 multiplier
    ) external payable;
    function updateDelegatorAutoRenew(
        bytes32 validationID,
        address delegator,
        bool active
    ) external;
    function withdrawDelegation(
        bytes32 validationID,
        address delegator
    ) external;
    function getDelegation(
        bytes32 validationID,
        address delegator
    ) external view returns (uint256, uint8, bool);
    function get(
        bytes32 id
    ) external view returns (address, uint256, uint256, uint8, bool);
    function getWithdraw(bytes32 id) external view returns (uint256);
    function firstActive() external view returns (bytes32);
    function firstQueued() external view returns (bytes32);
    function next(bytes32 prev) external view returns (bytes32);
}

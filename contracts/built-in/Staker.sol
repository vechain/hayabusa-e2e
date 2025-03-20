//SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

interface Staker {
    // Events
    event ValidatorQueued(
        address indexed validator,
        address indexed beneficiary,
        uint32 indexed expiry,
        uint256 stake
    );
    event ValidatorWithdrawn(address indexed validator, uint256 stake);

    function totalStake() external view returns (uint256);
    function activeStake() external view returns (uint256);
    function get(
        address validator
    ) external view returns (address, uint256, uint256, uint8);
    function firstActive() external view returns (address);
    function firstQueued() external view returns (address);
    function next(address prev) external view returns (address);

    function addValidator(address beneficiary, uint32 expiry) external payable;
    function withdraw() external;
}

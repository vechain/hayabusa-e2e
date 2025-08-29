// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import {Staker} from "./compiled/Staker.sol";

interface Energy {
    function totalSupply() external view returns (uint256);

    function totalBurned() external view returns (uint256);
}

contract GetValidators {
    Staker private constant STAKER = Staker(payable(0x00000000000000000000000000005374616B6572));
    Energy private constant ENERGY = Energy(0x0000000000000000000000000000456E65726779);

    // staker stats
    function stakerBalance() public view returns (uint256) {
        return getBalance(address(STAKER));
    }

    function totalStake() public view returns (uint256, uint256) {
        return STAKER.totalStake();
    }

    function queuedStake() public view returns (uint256) {
        return STAKER.queuedStake();
    }

    function getBalance(address account) private view returns (uint256) {
        return account.balance;
    }

    // VTHO Stats
    function totalSupply() public view returns (uint256) {
        return ENERGY.totalSupply();
    }

    function totalBurned() public view returns (uint256) {
        return ENERGY.totalBurned();
    }

    function getLockedDelegators(uint256 firstID, uint256 maxID) public view returns (
        uint256[] memory locked,
        uint256[] memory withdrawable
    ) {
        // Temporary buffers (max 1000)
        uint256[1000] memory tmpLocked;
        uint256[1000] memory tmpWithdrawable;
        uint256 lockedCount = 0;
        uint256 withdrawableCount = 0;

        uint256 end = maxID;
        if (end > firstID + 999) {
            end = firstID + 999;
        }

        for (uint256 i = firstID; i <= end; i++) {
            (address validator,uint256 stake,, bool isLocked) = STAKER.getDelegation(i);
            if (validator == address(0)) {
                break; // stop when no more delegations
            }
            if (stake == 0) {
                continue;
            }
            (uint32 startPeriod, uint32 endPeriod) = STAKER.getDelegationPeriodDetails(i);
            (,,, uint32 completedPeriods) = STAKER.getValidationPeriodDetails(validator);

            if (isLocked) {
                tmpLocked[lockedCount] = i;
                lockedCount++;
            } else if (
                startPeriod > completedPeriods + 1 ||
                endPeriod <= completedPeriods
            ) {
                tmpWithdrawable[withdrawableCount] = i;
                withdrawableCount++;
            }
        }

        // Copy only the used portion into the return arrays
        locked = new uint256[](lockedCount);
        for (uint256 j = 0; j < lockedCount; j++) {
            locked[j] = tmpLocked[j];
        }

        withdrawable = new uint256[](withdrawableCount);
        for (uint256 j = 0; j < withdrawableCount; j++) {
            withdrawable[j] = tmpWithdrawable[j];
        }
    }


    function getValidators() public view returns (
        address[] memory,  // masters
        address[] memory, // endorsors
        uint8[] memory, // statuses
        bool[] memory,    // onlines
        uint32[] memory, // offlineBlocks
        uint32[] memory, // stakingPeriodLengths
        uint32[] memory, // startBlocks
        uint32[] memory, // exitBlocks
        uint32[] memory, // completedPeriods
        uint256[] memory, // validatorLockedStakes
        uint256[] memory, // validatorLockedWeights
        uint256[] memory, // delegatorsStake
        uint256[] memory, // validatorQueuedStakes
        uint256[] memory, // totalQueuedStakes
        uint256[] memory // totalExitingStakes
    ) {
        address[1000] memory idBuffer;
        uint count = 0;

        // populate active
        address first = STAKER.firstActive();
        while (first != address(0)) {
            idBuffer[count] = first;
            first = STAKER.next(first);
            count++;
        }

        // populate queued
        address next = STAKER.firstQueued();
        while (next != address(0)) {
            idBuffer[count] = next;
            next = STAKER.next(next);
            count++;
        }

        // Allocate output arrays
        address[] memory masters = new address[](count);
        address[] memory endorsors = new address[](count);
        uint8[] memory statuses = new uint8[](count);
        bool[] memory onlines = new bool[](count);
        uint32[] memory offlineBlocks = new uint32[](count);
        uint32[] memory stakingPeriodLengths = new uint32[](count);
        uint32[] memory startBlocks = new uint32[](count);
        uint32[] memory exitBlocks = new uint32[](count);
        uint32[] memory completedPeriods = new uint32[](count);

        uint256[] memory validatorLockedStakes = new uint256[](count);
        uint256[] memory validatorLockedWeights = new uint256[](count);
        uint256[] memory delegatorsStake = new uint256[](count);

        uint256[] memory validatorQueuedStakes = new uint256[](count);
        uint256[] memory totalQueuedStakes = new uint256[](count);

        uint256[] memory totalExitingStakes = new uint256[](count);


        for (uint i = 0; i < count; i++) {
            address validatorId = idBuffer[i];

            masters[i] = validatorId;

            (
                address endorsor,
                uint256 validatorStake,
                uint256 combinedWeight,
                uint256 queuedStakeAmount,
                uint8 status,
                uint32 offlineBlock
            ) = STAKER.getValidation(validatorId);
            endorsors[i] = endorsor;
            validatorLockedStakes[i] = validatorStake;
            validatorLockedWeights[i] = combinedWeight;
            validatorQueuedStakes[i] = queuedStakeAmount;
            statuses[i] = status;
            offlineBlocks[i] = offlineBlock;
            onlines[i] = offlineBlock == type(uint32).max;

            (
                uint32 period,
                uint32 start,
                uint32 exit,
                uint32 compPeriods
            ) = STAKER.getValidationPeriodDetails(validatorId);
            stakingPeriodLengths[i] = period;
            startBlocks[i] = start;
            exitBlocks[i] = exit;
            completedPeriods[i] = compPeriods;

            (
                uint256 lockedStake,
                ,
                uint256 totalQueuedStake,
                uint256 exitingStake,

            ) = STAKER.getValidationTotals(validatorId);
            delegatorsStake[i] = lockedStake- validatorStake;
            totalQueuedStakes[i] = totalQueuedStake;
            totalExitingStakes[i] = exitingStake;
        }


        return (
            masters,
            endorsors,
            statuses,
            onlines,
            offlineBlocks,
            stakingPeriodLengths,
            startBlocks,
            exitBlocks,
            completedPeriods,
            validatorLockedStakes,
            validatorLockedWeights,
            delegatorsStake,
            validatorQueuedStakes,
            totalQueuedStakes,
            totalExitingStakes
        );
    }
}

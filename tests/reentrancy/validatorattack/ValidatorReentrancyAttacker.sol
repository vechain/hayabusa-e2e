//SPDX-License-Identifier: MIT
pragma solidity 0.8.20;
import "./staker.sol";

/**
   * @title ValidatorReentrancyAttacker
   * @dev Tests reentrancy on withdrawStake() function
   * This contract acts as the endorser and attempts to reenter during withdrawal
   */
contract ValidatorReentrancyAttacker {
    Staker constant public stakerContract = Staker(payable(address(0x00000000000000000000000000005374616B6572)));

    address public validatorAddress;
    uint256 public reentryCount;
    uint256 public maxReentries = 5;
    bool public isAttacking;

    uint256 public initialBalance;
    uint256 public finalBalance;

    event AttackInitiated(address validator);
    event ReentryAttempt(uint256 attemptNumber);
    event ReentrySucceeded(uint256 attemptNumber, uint256 amount);
    event ReentryFailed(uint256 attemptNumber, string reason);
    event AttackCompleted(bool success, uint256 profit);

    /**
     * @dev Step 1: Add validation (this contract is the endorser)
       */
    function addValidation(address validator, uint32 period) external payable {
        validatorAddress = validator;
        stakerContract.addValidation{value: msg.value}(validator, period);
    }

    /**
     * @dev Step 2: Signal exit
       */
    function signalExit() external {
        stakerContract.signalExit(validatorAddress);
    }

    /**
     * @dev Step 3: Execute reentrancy attack on withdrawStake
       */
    function executeAttack() external {
        require(validatorAddress != address(0), "No validator set");

        initialBalance = address(this).balance;
        isAttacking = true;
        reentryCount = 0;

        emit AttackInitiated(validatorAddress);

        try stakerContract.withdrawStake(validatorAddress) {
            // First withdrawal initiated
        } catch Error(string memory reason) {
            emit ReentryFailed(0, reason);
        }

        isAttacking = false;
        finalBalance = address(this).balance;

        uint256 profit = finalBalance > initialBalance ? finalBalance - initialBalance : 0;
        emit AttackCompleted(profit > 0, profit);
    }

    /**
     * @dev Reentrancy point for withdrawStake
       */
    receive() external payable {
        emit ReentryAttempt(reentryCount);

        if (isAttacking && reentryCount < maxReentries) {
            reentryCount++;

            try stakerContract.withdrawStake(validatorAddress) {
                emit ReentrySucceeded(reentryCount, msg.value);
            } catch Error(string memory reason) {
                emit ReentryFailed(reentryCount, reason);
            } catch {
                emit ReentryFailed(reentryCount, "Unknown error");
            }
        }
    }

    function withdraw() external {
        payable(msg.sender).transfer(address(this).balance);
    }

    fallback() external payable {}
}
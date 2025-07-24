//SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

interface IERC20 {
    function transfer(address recipient, uint256 amount) external returns (bool);
}

interface IStaker {
    // validator functions
    function getRewards(address validationID, uint32 stakingPeriod) external view returns (uint256);
    function getCompletedPeriods(address validationID) external view returns (uint32);

    // delegator functions
    function addDelegation(address validationID, uint8 multiplier) external payable returns (bytes32);
    function signalDelegationExit(bytes32 delegationID) external;
    function withdrawDelegation(bytes32 delegationID) external;
    // validationID, stake, startPeriod, endPeriod, multiplier, active
    function getDelegation(bytes32 delegationID) external view returns (address, uint256, uint32, uint32, uint8, bool);
}

contract Stargate {

    IERC20 constant public vtho = IERC20(address(0x0000000000000000000000000000456E65726779));
    IStaker constant public staker = IStaker(address(0x00000000000000000000000000005374616B6572));

    event ClaimedRewards(bytes32 indexed delegatorId, address indexed delegator, uint256 amount, uint32 firstClaimablePeriod, uint32 lastClaimablePeriod);
    // debugging events below. Helps understand the flow of the contract

    // delegator address => delegation ID
    mapping(address => bytes32) public delegationIDs;

    // staking round weights (validation ID => staking period => weight)
    mapping(address => mapping(uint32 => uint256)) public weights;

    // populate weights (validation ID => staking period). ie. which `weights` have been populated
    mapping(address => uint32) public populatedWeights;

    // staking round reductions (validation ID => staking period => reduction in weight)
    // should be used when delegator auto-renew is false, or when the delegation disables auto-renew
    mapping(address => mapping(uint32 => uint256)) public reductions;

    // staking round claims (delegation ID => last claimed staking period). prevents double claiming
    mapping(bytes32 => uint32) public claims;

    // staking round rewards (validation ID => staking period => reward). rewards per validator per staking period
    mapping(address => mapping(uint32 => uint256)) public rewards;

    function addDelegator(address validationID, uint8 multiplier) public payable {
        // validation
        require(delegationIDs[msg.sender] == bytes32(0), "already a delegator");
        require(msg.value > 0, "must send ether");

        // create the delegation position
        bytes32 delegationID = staker.addDelegation{value: msg.value}(validationID,  multiplier);

        // store data for later use
        delegationIDs[msg.sender] = delegationID;

        uint256 weight = (msg.value * multiplier) / 100;

        (address validationID, uint256 stake, uint32 startPeriod, uint32 endPeriod, uint8 multiplier, bool active) = staker.getDelegation(delegationID);

        // so we can calculate the delegators % of total
        weights[validationID][startPeriod] += weight;
    }

    event DisabledAutoRenew(address indexed delegator, uint32 roundReduction, uint256 reductionWeight);

    function disableAutoRenew() public {
        bytes32 delegationID = delegationIDs[msg.sender];
        require(delegationID != bytes32(0), "not a delegator");

        (address validationID, uint256 stake, uint32 startPeriod, uint32 endPeriod, uint8 multiplier, bool active) = staker.getDelegation(delegationID);
        require(stake > 0, "delegation is not active");

        uint32 validatorCompleted = staker.getCompletedPeriods(validationID);

        uint256 weight = (stake * multiplier) / 100;

        staker.signalDelegationExit(delegationID);
        uint32 lastPeriod = validatorCompleted + 1; // last staking period
        uint32 reductionPeriod = lastPeriod + 1; // first round after the delegation exit, reduce the weight on this round
        reductions[validationID][reductionPeriod] += weight;
        emit DisabledAutoRenew(msg.sender, reductionPeriod, weight);
    }


    function getClaimable(address delegator) public returns (uint256, uint32, uint32) {
        bytes32 delegationID = delegationIDs[delegator];
        require(delegationID != bytes32(0), "not a delegator");
        return _getClaimableRewards(delegationID);
    }

    function claimRewards() public {
        bytes32 delegationID = delegationIDs[msg.sender];
        require(delegationID != bytes32(0), "not a delegator");

        (uint256 totalRewards, uint32 firstClaimablePeriod, uint32 maxClaimablePeriod) = _getClaimableRewards(delegationID);
        require(totalRewards > 0, "no rewards to claim");
        emit ClaimedRewards(delegationID, msg.sender, totalRewards, firstClaimablePeriod, maxClaimablePeriod);

        require(totalRewards > 0, "no rewards to claim");
        require(vtho.transfer(msg.sender, totalRewards), "transfer failed");
    }

    event ClaimParams(bytes32 indexed delegationID, address delegator, uint32 firstClaimablePeriod, uint32 lastClaimablePeriod, uint32 previouslyPopulatedPeriod, uint32 maxClaimablePeriod, uint256 delegatorWeight);
    event ClaimOutputs(bytes32 indexed delegationID, address delegator, uint256 totalRewards);

    function _getClaimableRewards(bytes32 delegationID) private returns (uint256, uint32, uint32) {
        // get the delegation
        (
            address validationID,
            uint256 stake,
            uint32 startPeriod,
            uint32 endPeriod,
            uint8 multiplier
        ) = _getDelegationInfo(delegationID);

        require(stake > 0, "delegation is not active");
        require(validationID != address(0), "delegation is not active");

        // exclude previously claimed periods
        uint32 firstClaimablePeriod = claims[delegationID];
        if (firstClaimablePeriod == 0) {
            firstClaimablePeriod = startPeriod;
        } else {
            firstClaimablePeriod++;
        }

        // max claimable period = minOf (validation completed periods, delegation end period)
        uint32 maxClaimablePeriod = staker.getCompletedPeriods(validationID);
        if (endPeriod < maxClaimablePeriod) {
            maxClaimablePeriod = endPeriod;
        }

        uint256 delegatorWeight = (stake * multiplier) / 100;

        emit ClaimParams(delegationID, msg.sender,  firstClaimablePeriod, maxClaimablePeriod, claims[delegationID], maxClaimablePeriod, delegatorWeight);

        if (maxClaimablePeriod == 0 || firstClaimablePeriod > maxClaimablePeriod) {
            return (0, firstClaimablePeriod, maxClaimablePeriod);
        }

        claims[delegationID] = maxClaimablePeriod;

        uint256 totalRewards = 0;

        uint32 previouslyPopulatedPeriod = populatedWeights[validationID];
        for (uint32 i = previouslyPopulatedPeriod + 1; i <= maxClaimablePeriod; i++) {
            // only called once per validator per staking period
            // first time callers pay the most gas
            _updateWeights(validationID, i);
        }

        for (uint32 i = previouslyPopulatedPeriod + 1; i <= maxClaimablePeriod; i++) {
            // only called once per validator per staking period
            // first time callers pay the most gas
            _updateRewards(validationID, i);
        }
        populatedWeights[validationID] = maxClaimablePeriod;

        for (uint32 stakingPeriod = firstClaimablePeriod; stakingPeriod <= maxClaimablePeriod; stakingPeriod++) {
            totalRewards += _calculatePeriodRewards(
                validationID,
                stakingPeriod,
                delegatorWeight
            );
        }

        emit ClaimOutputs(delegationID, msg.sender, totalRewards);

        return (totalRewards, firstClaimablePeriod, maxClaimablePeriod);
    }


    // Helper function to get delegation info
    function _getDelegationInfo(bytes32 delegationID) internal view returns (
        address validationID,
        uint256 stake,
        uint32 startPeriod,
        uint32 endPeriod,
        uint8 multiplier
    ) {
        bool active;
        (validationID, stake, startPeriod, endPeriod, multiplier, active) = staker.getDelegation(delegationID);
        return (validationID, stake, startPeriod, endPeriod, multiplier);
    }

    event WeightsPopulated(address indexed validationID, uint32 stakingPeriod, uint256 previousWeight, uint256 increase, uint256 reduction, uint256 newWeight);

    // Helper function to update weights for a validator
    function _updateWeights(
        address validationID,
        uint32 stakingPeriod
    ) internal {
        uint256 previousWeight = weights[validationID][stakingPeriod - 1]; // previous round weight
        uint256 increase = weights[validationID][stakingPeriod]; // for any new delegators
        uint256 reduction = reductions[validationID][stakingPeriod]; // for any exited delegators
        uint256 newWeight = previousWeight + increase - reduction;
        if (newWeight == 0) {
            // no delegators for this staking period
            return;
        }
        weights[validationID][stakingPeriod] = newWeight;
        // debugging
        emit WeightsPopulated(validationID, stakingPeriod, previousWeight, increase, reduction, newWeight);
    }

    event RewardsPopulated(address indexed validationID, uint32 stakingPeriod, uint256 blockRewards, uint256 allDelegatorsRewards, uint256 proposerRewards);

    function _updateRewards(
        address validationID,
        uint32 stakingPeriod
    ) internal {
        if (weights[validationID][stakingPeriod] == 0) {
            // no delegators for this staking period
            return;
        }
        uint256 blockRewards = staker.getRewards(validationID, stakingPeriod);
        uint256 proposerRewards = (blockRewards * 3) / 10;
        uint256 allDelegatorsRewards = blockRewards - proposerRewards;
        emit RewardsPopulated(validationID, stakingPeriod, blockRewards, allDelegatorsRewards, proposerRewards);
        rewards[validationID][stakingPeriod] = allDelegatorsRewards;
    }

    event RewardsCalculated(address indexed validationID, uint32 stakingPeriod, uint256 rewards, uint256 allDelegatorsWeight, uint256 allDelegatorsRewards);

    // Helper function to calculate rewards for a single staking period
    function _calculatePeriodRewards(
        address validationID,
        uint32 stakingPeriod,
        uint256 delegatorWeight
    ) internal returns (uint256) {
        uint256 allDelegatorsWeight = weights[validationID][stakingPeriod];
        if (allDelegatorsWeight == 0) {
            // no delegators for this staking period
            emit RewardsCalculated(validationID, stakingPeriod, 0, 0, 0);
            return 0;
        }
        uint256 allDelegatorsRewards = rewards[validationID][stakingPeriod];
        uint256 result =  (allDelegatorsRewards * delegatorWeight) / allDelegatorsWeight;
        emit RewardsCalculated(validationID, stakingPeriod, result, allDelegatorsWeight, allDelegatorsRewards);
        return result;
    }
}

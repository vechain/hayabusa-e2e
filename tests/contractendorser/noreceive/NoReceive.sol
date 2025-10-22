// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

import {Staker} from "./staker.sol";

contract NoReceive {
    Staker private constant STAKER = Staker(payable(0x00000000000000000000000000005374616B6572));
    address private validator;

    function addValidation(
        address _validator,
        uint32 _period
    ) public payable {
        validator = _validator;
        STAKER.addValidation{value: msg.value}(_validator, _period);
    }

    event WithdrawSuccess(uint256 amount);
    event CaughtError(string reason);
    event CaughtBytes(bytes lowLevelData);

    function withdraw() public {
        try STAKER.withdrawStake(validator) {
            emit WithdrawSuccess(address(this).balance);
        } catch Error(string memory reason) {
            emit CaughtError(reason);
        } catch (bytes memory lowLevelData) {
            emit CaughtBytes(lowLevelData);
        }
    }
}
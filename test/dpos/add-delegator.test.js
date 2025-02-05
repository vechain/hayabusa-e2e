import { generateAddress, ThorWallet } from '../../src/wallet'
import { Staker__factory } from '../../typechain-types'
import * as core from '@vechain/sdk-core'

const MIN_STAKE = core.VET.of(25e6, core.Units.ether)
const MAX_STAKE = core.VET.of(180e6, core.Units.ether)

describe('Staker :: Add Delegator', async () => {
    const wallet = await ThorWallet.newFunded({
        vet: MIN_STAKE.wei * 1000n,
        vtho: 1000e18,
    })
    const beneficiary = await generateAddress()
    const contract = await wallet.deployContract(
        Staker__factory.bytecode,
        Staker__factory.abi,
    )

    /**
     * @param beneficiary {string}
     * @param stakeAmount {core.VET}
     */
    const addValidator = async (beneficiary, stakeAmount) => {
        const tx = await contract.transact.addValidator(beneficiary, {
            value: `0x` + stakeAmount.wei.toString(16),
        })
        return await tx.wait()
    }

    describe('Min & Max Staking Amounts', async () => {
        const testCases = [
            {
                amount: core.VET.of(MIN_STAKE.wei - 1n, core.Units.wei),
                reverts: true,
                comment: 'should not be able to stake less than 25m VET',
            },
            {
                amount: MIN_STAKE,
                reverts: false,
                comment: 'should be able to stake the minimum amount',
            },
            {
                amount: core.VET.of(MAX_STAKE.wei + 1n, core.Units.wei),
                reverts: true,
                comment: 'should not be able to stake more than 180m VET',
            },
            {
                amount: MAX_STAKE,
                reverts: false,
                comment: 'should be able to stake the maximum amount',
            },
        ]

        testCases.forEach((testCase) => {
            it.e2eTest(
                testCase.comment,
                ['default-private', 'solo'],
                async () => {
                    const receipt = await addValidator(
                        beneficiary,
                        testCase.amount,
                    )
                    expect(receipt.reverted).toBe(testCase.reverts)
                },
            )
        })
    })
})

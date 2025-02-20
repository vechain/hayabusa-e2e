import { Client } from '../../../src/thor-client'
import { contractAddresses } from '../../../src/contracts/addresses'
import { Staker__factory } from '../../../typechain-types'
import { interfaces } from '../../../src/contracts/hardhat'
import { getBlockRef } from '../../../src/utils/block-utils'
import { revisions } from '../../../src/constants'
import { ThorWallet } from '../../../src/wallet'
import { pollReceipt } from '../../../src/transactions'

describe('POST /accounts/*', function () {
    const wallet = ThorWallet.withFunds()
    let fundedWallet

    beforeAll(async () => {
        await wallet.waitForFunding()
        fundedWallet = await ThorWallet.newFunded({ vet: '0x0', vtho: 1e18 })
        await fundedWallet.waitForFunding()
    })

    it.e2eTest('should get non-existing validator', 'all', async () => {
        const res = await Client.raw.executeAccountBatch({
            clauses: [
                {
                    to: contractAddresses.staker,
                    value: '0x0',
                    data: interfaces.staker.encodeFunctionData('get', [
                        wallet.address,
                    ]),
                },
            ],
            caller: wallet.address,
        })
        expect(res.success, 'API response should be a success').toBeTruthy()
        expect(res.httpCode, 'Expected HTTP Code').toEqual(200)
        expect(res.body, 'Expected Response Body').toEqual([
            {
                data: '0x',
                events: [],
                transfers: [],
                gasUsed: expect.any(Number),
                reverted: false,
                vmError: '',
            },
        ])
    })

    it.e2eTest('should add validators', 'all', async () => {})

    it.e2eTest('should get validator', 'all', async () => {})

    it.e2eTest('should get total stake', 'all', async () => {})
    it.e2eTest('should get active stake', 'all', async () => {})
    it.e2eTest('should get first active', 'all', async () => {})
    it.e2eTest('should get first queued', 'all', async () => {})
    it.e2eTest('should get next', 'all', async () => {})
    it.e2eTest('should withdraw', 'all', async () => {})
})

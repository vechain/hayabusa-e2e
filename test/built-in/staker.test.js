import { Client } from '../../src/thor-client'
import { contractAddresses } from '../../src/contracts/addresses'
import { interfaces } from '../../src/contracts/hardhat'
import { generateEmptyWallet } from '../../src/wallet'
import { Hex } from '@vechain/sdk-core'

describe('POST /accounts/*', function () {
    it.e2eTest('should get non-existing validator', 'all', async () => {
        // using private key as random byte[32]
        const { privateKey: id } = await generateEmptyWallet()

        console.log(Hex.of(id).toString())

        const res = await Client.raw.executeAccountBatch({
            clauses: [
                {
                    to: contractAddresses.staker,
                    value: '0x0',
                    data: interfaces.staker.encodeFunctionData('get', [id]),
                },
            ],
        })
        expect(res.success, 'API response should be a success').toBeTruthy()
        expect(res.httpCode, 'Expected HTTP Code').toEqual(200)

        const [_, stake, weight, status] =
            interfaces.staker.decodeFunctionResult('get', res.body[0].data)
        expect(stake).toEqual(0n)
        expect(weight).toEqual(0n)
        expect(status).toEqual(0n)
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

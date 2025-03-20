import { Client } from '../../src/thor-client'
import { contractAddresses } from '../../src/contracts/addresses'
import { interfaces } from '../../src/contracts/hardhat'
import { generateAddress, ThorWallet } from '../../src/wallet'

describe('POST /accounts/*', function () {
    const wallet = ThorWallet.withFunds()
    let fundedWallet

    beforeAll(async () => {
        await wallet.waitForFunding()
        fundedWallet = await ThorWallet.newFunded({ vet: '0x0', vtho: 1e18 })
        await fundedWallet.waitForFunding()
    })

    it.e2eTest('should get non-existing validator', 'all', async () => {
        const addr = await generateAddress()

        const res = await Client.raw.executeAccountBatch({
            clauses: [
                {
                    to: contractAddresses.staker,
                    value: '0x0',
                    data: interfaces.staker.encodeFunctionData('get', [
                        addr.toString(),
                    ]),
                },
            ],
            caller: addr.toString(),
        })
        expect(res.success, 'API response should be a success').toBeTruthy()
        expect(res.httpCode, 'Expected HTTP Code').toEqual(200)

        const [endorsor, stake, weight, status] =
            interfaces.staker.decodeFunctionResult('get', res.body[0].data)
        expect(endorsor).toEqual('0x0000000000000000000000000000000000000000')
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

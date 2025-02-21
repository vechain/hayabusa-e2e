import { Client } from '../../../src/thor-client'
import { contractAddresses } from '../../../src/contracts/addresses'
import { Staker__factory } from '../../../typechain-types'
import { interfaces } from '../../../src/contracts/hardhat'
import { getBlockRef } from '../../../src/utils/block-utils'
import { revisions } from '../../../src/constants'
import { ThorWallet } from '../../../src/wallet'
import { pollReceipt } from '../../../src/transactions'

function nextValidator(rawBlock) {
    return ""
}

describe('POST /accounts/*', function () {
    const wallet = ThorWallet.withFunds()
    let bestBlockNumber
    let fundedWallet

    beforeAll(async () => {
        await wallet.waitForFunding()
        fundedWallet = await ThorWallet.newFunded({ vet: '0x0', vtho: 1e18 })
        await fundedWallet.waitForFunding()

        bestBlockNumber = (await Client.raw.getBlock("best", false, false)).body?.number
    })

    it.e2eTest('validate proposers', 'all', async () => {
        const prevBlock = await Client.raw.getBlock(bestBlockNumber - 1, false, true)

        expect(
            block.success,
            'API response should be a success',
        ).toBeTruthy()
        expect(block.httpCode, 'Expected HTTP Code').toEqual(200)

        const bestBlock = await Client.raw.getBlock(bestBlockNumber, false, false)

        expect(
            block.success,
            'API response should be a success',
        ).toBeTruthy()
        expect(block.httpCode, 'Expected HTTP Code').toEqual(200)

        validator = nextValidator(prevBlock.body)

        expect(bestBlock.body.proposer, 'Proposer should be equal').toEqual(validator)
    })
})

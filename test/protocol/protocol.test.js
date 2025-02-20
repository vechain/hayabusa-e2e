
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

    it.e2eTest('validate proposers', 'all', async () => {

    })
})
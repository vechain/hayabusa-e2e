import { Hex, Transaction } from '@vechain/sdk-core'
import { generateAddress, ThorWallet } from '../../../src/wallet'
import { TransactionDataDrivenFlow } from './setup/transaction-data-driven-flow'
import {
    checkTransactionLogSuccess,
    checkTxInclusionInBlock,
    compareSentTxWithCreatedTx,
    successfulPostTx,
    successfulReceipt,
} from './setup/asserts'
import { MultipleTransactionDataDrivenFlow } from './setup/multiple-transactions-data-driven-flow'
import { Client } from '../../../src/thor-client'

/**
 * @group api
 * @group transactions
 */
describe('dependant transaction', function () {
    it.e2eTest(
        'should succeed when sending two transactions where the second one is dependant on the first one',
        'all',
        async function () {
            const initialFunds = 1000
            const transferAmount = initialFunds ** 2

            const walletA = ThorWallet.withFunds()
            const walletB = await ThorWallet.newFunded({
                vet: `0x${BigInt(initialFunds).toString(16)}`,
                vtho: 1000e18,
            })
            const thirdAddress = await generateAddress()

            // Prepare the first transaction
            const clausesA = [
                {
                    value: transferAmount,
                    data: '0x',
                    to: walletB.address.toLowerCase(),
                },
            ]
            const txBodyA = await walletA.buildTransaction(clausesA)
            const txA = new Transaction(txBodyA)
            const signedTxA = await walletA.signTransaction(txA)

            // Prepare the second transaction
            const clausesB = [
                {
                    value: transferAmount,
                    data: '0x',
                    to: thirdAddress,
                },
            ]
            const txBodyB = await walletB.buildTransaction(clausesB, {
                dependsOn: Hex.of(signedTxA.id.bytes).toString(),
            })
            const txB = new Transaction(txBodyB)
            const signedTxB = await walletB.signTransaction(txB)

            // Create the test plan
            const testPlanA = {
                postTxStep: {
                    rawTx: Hex.of(signedTxA.encoded).toString(),
                    expectedResult: successfulPostTx,
                },

                getTxStep: {
                    expectedResult: (tx) =>
                        compareSentTxWithCreatedTx(tx, signedTxA),
                },

                getTxReceiptStep: {
                    expectedResult: (receipt) =>
                        successfulReceipt(receipt, signedTxA),
                },

                getLogTransferStep: {
                    expectedResult: (input, block) =>
                        checkTransactionLogSuccess(
                            input,
                            block,
                            signedTxA,
                            signedTxA.body.clauses,
                        ),
                },

                getTxBlockStep: {
                    expectedResult: checkTxInclusionInBlock,
                },
            }

            const testPlanB = {
                postTxStep: {
                    rawTx: Hex.of(signedTxB.encoded).toString(),
                    expectedResult: successfulPostTx,
                },

                getTxStep: {
                    expectedResult: (tx) =>
                        compareSentTxWithCreatedTx(tx, signedTxB),
                },

                getTxReceiptStep: {
                    expectedResult: (receipt) =>
                        successfulReceipt(receipt, signedTxB),
                },

                getLogTransferStep: {
                    expectedResult: (input, block) =>
                        checkTransactionLogSuccess(
                            input,
                            block,
                            signedTxB,
                            signedTxB.body.clauses,
                        ),
                },

                getTxBlockStep: {
                    expectedResult: checkTxInclusionInBlock,
                },
            }

            // Run the test flow
            const ddtA = new TransactionDataDrivenFlow(testPlanA)
            const ddtB = new TransactionDataDrivenFlow(testPlanB)

            const multipleDdt = new MultipleTransactionDataDrivenFlow([
                ddtA,
                ddtB,
            ])
            await multipleDdt.runTestFlow()
        },
    )

    it.e2eTest(
        'should fail once non executable tx pool is full',
        'solo',
        async function () {
            const initialFunds = 1000
            const transferAmount = initialFunds ** 2

            const walletA = ThorWallet.withFunds()
            const thirdAddress = await generateAddress()

            const clauses = [
                {
                    value: transferAmount,
                    data: '0x',
                    to: thirdAddress.toLowerCase(),
                },
            ]
            const txBodyUnsent = await walletA.buildTransaction(clauses)
            const txUnsent = new Transaction(txBodyUnsent)
            const signedTxUnsent = await walletA.signTransaction(txUnsent)

            let errorThrown = false
            for (let i = 0; i < 201; i++) {
                const clauses = [
                    {
                        value: transferAmount,
                        data: '0x',
                        to: thirdAddress.toLowerCase(),
                    },
                ]
                const txBody = await walletA.buildTransaction(clauses, {
                    dependsOn: Hex.of(signedTxUnsent.id.bytes).toString(),
                })
                const txn = new Transaction(txBody)
                const signedTx = await walletA.signTransaction(txn)
                const response = await Client.raw.sendTransaction({
                    raw: Hex.of(signedTx.encoded).toString(),
                })
                if (
                    response.success === false &&
                    response.httpCode === 403 &&
                    response.httpMessage.includes(
                        'tx rejected: non executable pool is full',
                    )
                ) {
                    errorThrown = true
                    break
                }
            }

            expect(errorThrown).toBeTruthy()
        },
        1000 * 60 * 5,
    )
})

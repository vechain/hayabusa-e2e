import { Client } from '../src/thor-client'
import 'dotenv/config'
import { testEnv } from '../src/test-env'

it.e2eTest = (title, tags, testFunc, timeout = 60000) => {
    if (tags === 'all' || tags.includes(testEnv.type)) {
        test(title, testFunc, timeout)
    } else {
        test.skip(title, testFunc)
    }
}

jest.retryTimes(3, { logErrorsBeforeRetry: true })

afterAll(async () => {
    Client.raw.closeAllSubscriptions()
    jest.clearAllTimers()
    Client.sdk.destroy()
})

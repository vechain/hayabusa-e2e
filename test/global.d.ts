import type { it as vitestIT } from 'vitest'

const E2eTestTags = ['solo', 'default-private', 'testnet', 'mainnet'] as const
type Tag = (typeof E2eTestTags)[number]

type TestFunc = () => void | Promise<void>

interface CustomIt extends vitestIT {
    e2eTest: (
        name: string,
        tag: 'all' | Tag[],
        testFunc: TestFunc,
        timeout?: number,
    ) => void
}

declare global {
    const it: CustomIt
}

export {}

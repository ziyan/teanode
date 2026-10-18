import { act, cleanup, renderHook, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'

import { graphql } from '../api'
import { announceHerdrAsking, useHerdrPresence } from './herdrPresence'

vi.mock('../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../api')>()),
  graphql: vi.fn(),
}))

const execute = vi.mocked(graphql)

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

it("counts what asks, takes the Coding page's count at once, and the next answer over it", async () => {
  execute.mockResolvedValue({
    ListAgentHerdrSessions: { computerNames: ['laptop'], sessions: [{ herdrSessionState: 'idle' }] },
  })
  const { result } = renderHook(() => useHerdrPresence(true))
  await waitFor(() => expect(result.current.isPresent).toBe(true))
  expect(result.current.askingCount).toBe(0)

  act(() => announceHerdrAsking(2))
  expect(result.current.askingCount).toBe(2)

  // The rail asks again on coming back into view after a while; its answer
  // is the same as before, and still it replaces the announced count.
  vi.spyOn(Date, 'now').mockReturnValue(Date.now() + 60_000)
  act(() => {
    window.dispatchEvent(new Event('focus'))
  })
  await waitFor(() => expect(result.current.askingCount).toBe(0))
  expect(execute).toHaveBeenCalledTimes(2)
})

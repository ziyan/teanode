import { afterEach, expect, it, vi } from 'vitest'

const graphql = vi.fn()
vi.mock('../api', () => ({
  APIError: class APIError extends Error {},
  graphql: (...args: unknown[]) => graphql(...args),
}))

import { waitForRestart } from './serverRestart'

afterEach(() => {
  graphql.mockReset()
  vi.useRealTimers()
})

const startedAt = (value: string) => ({ GetServerStatus: { startedAt: value } })

it('is not back while the old process still answers, only once a new one does', async () => {
  vi.useFakeTimers()
  graphql
    .mockResolvedValueOnce(startedAt('2026-01-01T08:00:00Z'))
    .mockRejectedValueOnce(new TypeError('Failed to fetch'))
    .mockResolvedValueOnce(startedAt('2026-01-01T08:05:00Z'))
  const isBack = waitForRestart('2026-01-01T08:00:00Z')
  await vi.advanceTimersByTimeAsync(1500 * 3)
  await expect(isBack).resolves.toBe(true)
  expect(graphql).toHaveBeenCalledTimes(3)
})

it('says it did not come back when only the old process ever answers', async () => {
  vi.useFakeTimers()
  graphql.mockResolvedValue(startedAt('2026-01-01T08:00:00Z'))
  const isBack = waitForRestart('2026-01-01T08:00:00Z')
  await vi.advanceTimersByTimeAsync(95_000)
  await expect(isBack).resolves.toBe(false)
})

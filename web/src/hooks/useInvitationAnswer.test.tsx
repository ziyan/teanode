import { act, cleanup, renderHook } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { graphql } from '../api'
import { useInvitationAnswer } from './useInvitationAnswer'

vi.mock('../api', () => ({ graphql: vi.fn() }))
const execute = vi.mocked(graphql)
beforeEach(() => {
  sessionStorage.clear()
  execute.mockReset()
})
afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

describe('retained invitation answers', () => {
  it('looks up an accepted answer after reload without sending again, even if the event is deleted', async () => {
    execute.mockRejectedValueOnce(new Error('lost response'))
    const first = renderHook(() => useInvitationAnswer('owner', 'item'))
    await act(async () => {
      await expect(first.result.current.send('ACCEPTED')).rejects.toThrow('lost response')
    })
    const request = execute.mock.calls[0][1]
    expect(request).toMatchObject({ itemId: 'item', answer: 'ACCEPTED', requestId: expect.any(String) })
    first.unmount()
    const resumed = renderHook(() => useInvitationAnswer('owner', 'item'))
    expect(resumed.result.current.pending?.answer).toBe('ACCEPTED')
    execute.mockResolvedValueOnce({
      GetCalendarRequest: { requestId: resumed.result.current.pending?.requestId, isMissing: true },
    })
    await act(async () => {
      await resumed.result.current.send('ACCEPTED')
    })
    expect(execute.mock.calls).toHaveLength(2)
    expect(execute.mock.calls[1][0]).toContain('GetCalendarRequest')
    expect(resumed.result.current.pending).toBeNull()
    expect(sessionStorage.length).toBe(0)
  })

  it('retries the identical request and blocks another answer until completion', async () => {
    execute.mockRejectedValueOnce(new Error('lost response'))
    const hook = renderHook(() => useInvitationAnswer('owner', 'item'))
    await act(async () => {
      await expect(hook.result.current.send('ACCEPTED')).rejects.toThrow()
    })
    const original = execute.mock.calls[0][1]
    await act(async () => {
      await expect(hook.result.current.send('DECLINED')).rejects.toThrow('pending answer')
    })
    expect(execute).toHaveBeenCalledTimes(1)
    execute
      .mockResolvedValueOnce({ GetCalendarRequest: null })
      .mockResolvedValueOnce({ AnswerMailInvitation: { id: 'invitation' } })
    await act(async () => {
      await hook.result.current.send('ACCEPTED')
    })
    expect(execute.mock.calls[2][1]).toEqual(original)
    execute.mockResolvedValueOnce({ AnswerMailInvitation: { id: 'invitation' } })
    await act(async () => {
      await hook.result.current.send('DECLINED')
    })
    expect(execute.mock.calls[3][1]).toMatchObject({ answer: 'DECLINED' })
    expect(execute.mock.calls[3][1]?.requestId).not.toEqual(original?.requestId)
  })

  it('does not send when pending storage fails, and does not cross account or item boundaries', async () => {
    const hook = renderHook(() => useInvitationAnswer('owner', 'item'))
    const write = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('storage full')
    })
    await act(async () => {
      await expect(hook.result.current.send('ACCEPTED')).rejects.toThrow('storage full')
    })
    expect(execute).not.toHaveBeenCalled()
    write.mockRestore()
    execute.mockRejectedValueOnce(new Error('lost response'))
    await act(async () => {
      await expect(hook.result.current.send('ACCEPTED')).rejects.toThrow()
    })
    const otherOwner = renderHook(() => useInvitationAnswer('other-owner', 'item'))
    const otherItem = renderHook(() => useInvitationAnswer('owner', 'other-item'))
    expect(otherOwner.result.current.pending).toBeNull()
    expect(otherItem.result.current.pending).toBeNull()
  })

  it('retains the request after lookup failure without resending', async () => {
    execute.mockRejectedValueOnce(new Error('lost response'))
    const hook = renderHook(() => useInvitationAnswer('owner', 'item'))
    await act(async () => {
      await expect(hook.result.current.send('TENTATIVE')).rejects.toThrow()
    })
    const identity = hook.result.current.pending?.requestId
    execute.mockRejectedValueOnce(new Error('lookup failed'))
    await act(async () => {
      await expect(hook.result.current.send('TENTATIVE')).rejects.toThrow('lookup failed')
    })
    expect(hook.result.current.pending?.requestId).toBe(identity)
    expect(execute.mock.calls.filter(([document]) => document.includes('AnswerMailInvitation'))).toHaveLength(1)
  })
  it('does not erase a newer pending answer when an older reader finishes', async () => {
    let finishFirst!: (response: unknown) => void
    execute.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finishFirst = resolve
        }),
    )
    const first = renderHook(() => useInvitationAnswer('owner', 'item'))
    let firstRequest!: Promise<void>
    act(() => {
      firstRequest = first.result.current.send('ACCEPTED')
    })
    const requestId = first.result.current.pending?.requestId
    const second = renderHook(() => useInvitationAnswer('owner', 'item'))
    execute.mockResolvedValueOnce({ GetCalendarRequest: { requestId, isMissing: false } })
    await act(async () => {
      await second.result.current.send('ACCEPTED')
    })
    execute.mockRejectedValueOnce(new Error('later response lost'))
    await act(async () => {
      await expect(second.result.current.send('DECLINED')).rejects.toThrow()
    })
    const laterId = second.result.current.pending?.requestId
    await act(async () => {
      finishFirst({ AnswerMailInvitation: { id: 'invitation' } })
      await firstRequest
    })
    expect(first.result.current.pending?.requestId).toBe(laterId)
    const restored = renderHook(() => useInvitationAnswer('owner', 'item'))
    expect(restored.result.current.pending?.answer).toBe('DECLINED')
  })
})

it.each([false, true])('clears pending only after cancellation resolves, completed=%s', async (isCompleted) => {
  const hook = renderHook(() => useInvitationAnswer('owner', 'item'))
  execute.mockRejectedValueOnce(new Error('lost response'))
  await act(async () => {
    await expect(hook.result.current.send('ACCEPTED')).rejects.toThrow()
  })
  const requestId = hook.result.current.pending?.requestId
  execute.mockRejectedValueOnce(new Error('cancellation response lost'))
  await act(async () => {
    await expect(hook.result.current.cancel()).rejects.toThrow('cancellation response lost')
  })
  expect(hook.result.current.pending?.requestId).toBe(requestId)
  execute.mockResolvedValueOnce({ CancelCalendarRequest: isCompleted ? { requestId } : null })
  await act(async () => {
    await expect(hook.result.current.cancel()).resolves.toBe(isCompleted)
  })
  expect(hook.result.current.pending).toBeNull()
  expect(execute.mock.calls[1][0]).toContain('CancelCalendarRequest')
  expect(execute.mock.calls[2][1]).toEqual({ requestId })
})

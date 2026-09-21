import { act, cleanup, renderHook } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { useAgentConversation } from './useAgentConversation'

function pendingSnapshot() {
  let resolve!: (snapshot: { conversation: { id: string } }) => void
  let reject!: (failure: Error) => void
  const promise = new Promise<{ conversation: { id: string } }>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise
    reject = rejectPromise
  })
  return { promise, resolve, reject }
}

afterEach(cleanup)

describe('agent conversation reads', () => {
  it('keeps the latest selection when an aborted transport still returns', async () => {
    const older = pendingSnapshot()
    const newer = pendingSnapshot()
    const readSnapshot = vi.fn().mockReturnValueOnce(older.promise).mockReturnValueOnce(newer.promise)
    const applySnapshot = vi.fn()
    const { result } = renderHook(() => useAgentConversation('first', readSnapshot, applySnapshot))

    act(() => void result.current.readConversation('first', true))
    await act(async () => undefined)
    act(() => void result.current.readConversation('second', true))
    expect(result.current.selectedConversation.current).toBe('second')
    await act(async () => newer.resolve({ conversation: { id: 'second' } }))
    await act(async () => older.resolve({ conversation: { id: 'first' } }))

    expect(readSnapshot.mock.calls[0][1].aborted).toBe(true)
    expect(applySnapshot).toHaveBeenCalledTimes(1)
    expect(applySnapshot.mock.calls[0][0].conversation.id).toBe('second')
    expect(result.current.isLoading).toBe(false)
  })

  it('ignores a reconnect for the conversation being left', async () => {
    const pending = pendingSnapshot()
    const readSnapshot = vi.fn().mockReturnValue(pending.promise)
    const { result } = renderHook(() => useAgentConversation('first', readSnapshot, vi.fn()))

    act(() => void result.current.readConversation('second', true))
    await act(async () => result.current.readConversation('first'))
    expect(readSnapshot).toHaveBeenCalledTimes(1)
    expect(result.current.selectedConversation.current).toBe('second')
    expect(result.current.isLoading).toBe(true)
    await act(async () => pending.resolve({ conversation: { id: 'second' } }))
  })

  it('does not clear loading or report an error from a superseded read', async () => {
    const older = pendingSnapshot()
    const newer = pendingSnapshot()
    const readSnapshot = vi.fn().mockReturnValueOnce(older.promise).mockReturnValueOnce(newer.promise)
    const { result } = renderHook(() => useAgentConversation('first', readSnapshot, vi.fn()))

    act(() => void result.current.readConversation('first', true))
    await act(async () => undefined)
    act(() => void result.current.readConversation('second', true))
    await act(async () => older.reject(new Error('old failure')))
    expect(result.current.isLoading).toBe(true)
    expect(result.current.readFailure).toBeNull()
    await act(async () => newer.resolve({ conversation: { id: 'second' } }))
  })

  it('retains the displayed selection after a failed switch', async () => {
    const failure = new Error('read failed')
    const readSnapshot = vi.fn().mockRejectedValue(failure)
    const { result } = renderHook(() => useAgentConversation('first', readSnapshot, vi.fn()))

    await act(async () => {
      await expect(result.current.readConversation('second', true)).rejects.toBe(failure)
    })
    expect(result.current.selectedConversation.current).toBe('first')
    expect(result.current.isLoading).toBe(false)
    expect(result.current.readFailure).toBe(failure)
  })

  it('adopts the resolved main conversation identity', async () => {
    const readSnapshot = vi.fn().mockResolvedValue({ conversation: { id: 'main' } })
    const { result } = renderHook(() => useAgentConversation('', readSnapshot, vi.fn()))

    await act(async () => result.current.readConversation('', true))
    expect(result.current.selectedConversation.current).toBe('main')
    expect(result.current.currentRead.current).toBeNull()
  })

  it('ignores outstanding reads when adopting a newly created conversation', async () => {
    const pending = pendingSnapshot()
    const applySnapshot = vi.fn()
    const { result } = renderHook(() => useAgentConversation('', () => pending.promise, applySnapshot))

    act(() => void result.current.readConversation('', true))
    act(() => result.current.adoptConversation('created'))
    await act(async () => pending.resolve({ conversation: { id: 'old' } }))
    expect(result.current.selectedConversation.current).toBe('created')
    expect(applySnapshot).not.toHaveBeenCalled()
  })

  it('aborts and ignores outstanding reads on unmount', async () => {
    const pending = pendingSnapshot()
    const readSnapshot = vi.fn().mockReturnValue(pending.promise)
    const applySnapshot = vi.fn()
    const { result, unmount } = renderHook(() => useAgentConversation('first', readSnapshot, applySnapshot))

    act(() => void result.current.readConversation('first', true))
    await act(async () => undefined)
    unmount()
    expect(readSnapshot.mock.calls[0][1].aborted).toBe(true)
    await act(async () => pending.resolve({ conversation: { id: 'first' } }))
    expect(applySnapshot).not.toHaveBeenCalled()
  })
})

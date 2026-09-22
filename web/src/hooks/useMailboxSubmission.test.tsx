import { act, cleanup, renderHook } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { graphql } from '../api'
import { MailboxSendMessage, useMailboxSubmission } from './useMailboxSubmission'

vi.mock('../api', () => ({ graphql: vi.fn() }))
const execute = vi.mocked(graphql)
const message: MailboxSendMessage = {
  from: 'sender@example.com',
  to: ['recipient@example.net'],
  cc: [],
  bcc: [],
  subject: 'Fixture',
  htmlContent: '',
  textContent: 'Content',
  replyToItemId: null,
  forwardItemId: null,
  forwardAttachments: [],
  draftItemId: 'original-draft',
  keepAttachments: [2],
}
const request = () => ({ mailboxId: 'owned-mailbox', message: structuredClone(message), kept: [], carried: [] })

beforeEach(() => {
  sessionStorage.clear()
  execute.mockReset()
})
afterEach(cleanup)

describe('mailbox submission recovery', () => {
  it('retains the exact request and identifier across errors and remounts', async () => {
    execute.mockRejectedValueOnce(new Error('response lost'))
    const first = renderHook(() => useMailboxSubmission('owner'))
    const original = request()
    await act(async () => {
      await expect(first.result.current.send(original)).rejects.toThrow('response lost')
    })
    const firstVariables = execute.mock.calls[0][1]
    original.message.subject = 'Changed editor'
    original.message.draftItemId = 'new-draft'
    first.unmount()
    execute
      .mockResolvedValueOnce({ GetMailboxSubmission: null })
      .mockResolvedValueOnce({ SendMailboxMessage: { mail: { id: 'accepted' }, item: null } })
    const resumed = renderHook(() => useMailboxSubmission('owner'))
    expect(resumed.result.current.pending?.message.subject).toBe('Fixture')
    await act(async () => {
      await resumed.result.current.send(original)
    })
    expect(execute.mock.calls[2][1]).toEqual(firstVariables)
  })

  it('recovers an accepted send without resending its removed draft', async () => {
    execute.mockRejectedValueOnce(new Error('response lost'))
    const { result } = renderHook(() => useMailboxSubmission('owner'))
    await act(async () => {
      await expect(result.current.send(request())).rejects.toThrow()
    })
    execute.mockResolvedValueOnce({ GetMailboxSubmission: { mailId: 'original-mail', sentItemId: 'original-item' } })
    await act(async () => {
      expect(await result.current.send()).toEqual({ mailId: 'original-mail', sentItemId: 'original-item' })
    })
    expect(execute.mock.calls.filter(([document]) => document.includes('SendMailboxMessage'))).toHaveLength(1)
  })

  it('keeps uncertainty on cancellation failure and allows editing only after committed cancellation', async () => {
    execute.mockRejectedValueOnce(new Error('send lost'))
    const { result } = renderHook(() => useMailboxSubmission('owner'))
    await act(async () => {
      await expect(result.current.send(request())).rejects.toThrow()
    })
    const originalId = result.current.pending?.submissionId
    execute.mockRejectedValueOnce(new Error('cancel lost'))
    await act(async () => {
      await expect(result.current.cancel()).rejects.toThrow('cancel lost')
    })
    expect(result.current.pending?.submissionId).toBe(originalId)
    execute.mockResolvedValueOnce({ CancelMailboxSubmission: null })
    await act(async () => {
      expect(await result.current.cancel()).toBeNull()
      result.current.clear()
    })
    execute.mockResolvedValueOnce({ SendMailboxMessage: { mail: { id: 'edited-mail' }, item: null } })
    await act(async () => {
      await result.current.send(request())
    })
    expect(result.current.pending?.submissionId).not.toBe(originalId)
  })

  it('returns acceptance when cancellation loses the race', async () => {
    execute.mockRejectedValueOnce(new Error('response lost'))
    const { result } = renderHook(() => useMailboxSubmission('owner'))
    await act(async () => {
      await expect(result.current.send(request())).rejects.toThrow()
    })
    execute.mockResolvedValueOnce({ CancelMailboxSubmission: { mailId: 'already-accepted' } })
    await act(async () => {
      expect(await result.current.cancel()).toEqual({ mailId: 'already-accepted' })
    })
  })

  it('does not send when durable browser storage is unavailable', async () => {
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('storage full')
    })
    const { result } = renderHook(() => useMailboxSubmission('owner'))
    await act(async () => {
      await expect(result.current.send(request())).rejects.toThrow('storage full')
    })
    expect(execute).not.toHaveBeenCalled()
  })

  it('does not expose another account’s pending content', async () => {
    execute.mockRejectedValueOnce(new Error('response lost'))
    const first = renderHook(() => useMailboxSubmission('first-owner'))
    await act(async () => {
      await expect(first.result.current.send(request())).rejects.toThrow()
    })
    const second = renderHook(() => useMailboxSubmission('second-owner'))
    expect(second.result.current.pending).toBeNull()
  })
  it('does not clear a newer request when an older composer completes', async () => {
    execute.mockRejectedValue(new Error('response lost'))
    const older = renderHook(() => useMailboxSubmission('owner'))
    await act(async () => {
      await expect(older.result.current.send(request())).rejects.toThrow()
    })
    const sameRequest = renderHook(() => useMailboxSubmission('owner'))
    act(() => sameRequest.result.current.clear())
    await act(async () => {
      await expect(sameRequest.result.current.send(request())).rejects.toThrow()
    })
    const newerId = sameRequest.result.current.pending?.submissionId
    act(() => older.result.current.clear())
    const restored = renderHook(() => useMailboxSubmission('owner'))
    expect(restored.result.current.pending?.submissionId).toBe(newerId)
  })

  it('prevents overlapping send clicks before React renders the disabled button', async () => {
    let finish!: (response: unknown) => void
    execute.mockReturnValueOnce(
      new Promise((resolve) => {
        finish = resolve
      }),
    )
    const { result } = renderHook(() => useMailboxSubmission('owner'))
    let sending!: ReturnType<typeof result.current.send>
    act(() => {
      sending = result.current.send(request())
    })
    await act(async () => {
      await expect(result.current.send(request())).rejects.toThrow('already in progress')
    })
    await act(async () => {
      finish({ SendMailboxMessage: { mail: { id: 'accepted' }, item: null } })
      await sending
    })
    expect(execute).toHaveBeenCalledOnce()
  })
  it('does not silently send another composer’s request', async () => {
    execute.mockRejectedValue(new Error('response lost'))
    const first = renderHook(() => useMailboxSubmission('owner'))
    const second = renderHook(() => useMailboxSubmission('owner'))
    await act(async () => {
      await expect(first.result.current.send(request())).rejects.toThrow()
    })
    await act(async () => {
      await expect(second.result.current.send(request())).rejects.toThrow()
    })
    expect(execute).toHaveBeenCalledOnce()
    expect(second.result.current.pending).toBeNull()
  })
})

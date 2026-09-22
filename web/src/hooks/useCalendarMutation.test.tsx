import { act, cleanup, renderHook } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { graphql } from '../api'
import { CalendarMutation, useCalendarMutation } from './useCalendarMutation'

vi.mock('../api', () => ({ graphql: vi.fn() }))
const execute = vi.mocked(graphql)
const save = (): CalendarMutation => ({
  operation: 'save',
  summary: 'Planning',
  variables: {
    calendarId: 'calendar',
    id: null,
    summary: 'Planning',
    startsAt: '2030-01-02T10:00:00Z',
    endsAt: '2030-01-02T11:00:00Z',
    timezone: 'UTC',
    attendees: ['guest@example.net'],
  },
})
beforeEach(() => {
  sessionStorage.clear()
  execute.mockReset()
})
afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

describe('calendar mutation recovery', () => {
  it('restores a committed create after reload without creating another event', async () => {
    execute.mockRejectedValueOnce(new Error('lost response'))
    const first = renderHook(() => useCalendarMutation('owner'))
    await act(async () => {
      await expect(first.result.current.execute(save())).rejects.toThrow('lost response')
    })
    const pending = first.result.current.pending!
    first.unmount()
    const restored = renderHook(() => useCalendarMutation('owner'))
    expect(restored.result.current.pending).toEqual(pending)
    execute.mockResolvedValueOnce({ GetCalendarRequest: { requestId: pending.requestId, isMissing: true } })
    await act(async () => {
      await restored.result.current.execute()
    })
    expect(execute).toHaveBeenCalledTimes(2)
    expect(execute.mock.calls[1][0]).toContain('GetCalendarRequest')
    expect(restored.result.current.pending).toBeNull()
  })

  it.each(['save', 'delete'] as const)('resends the identical %s only after an absent receipt', async (operation) => {
    const request =
      operation === 'save'
        ? save()
        : { operation, summary: 'Planning', variables: { calendarId: 'calendar', id: 'event' } }
    const hook = renderHook(() => useCalendarMutation('owner'))
    execute.mockRejectedValueOnce(new Error('lost response'))
    await act(async () => {
      await expect(hook.result.current.execute(request)).rejects.toThrow()
    })
    const original = structuredClone(execute.mock.calls[0][1])
    request.variables.calendarId = 'changed-after-request'
    await act(async () => {
      await expect(hook.result.current.execute(save())).rejects.toThrow('pending calendar change')
    })
    expect(execute).toHaveBeenCalledTimes(1)
    execute.mockResolvedValueOnce({ GetCalendarRequest: null }).mockResolvedValueOnce({})
    await act(async () => {
      await hook.result.current.execute()
    })
    expect(execute.mock.calls[2][1]).toEqual(original)
    expect(execute.mock.calls[2][0]).toContain(operation === 'save' ? 'SaveCalendarEvent' : 'DeleteCalendarEvent')
  })

  it('fails closed on storage and lookup errors and isolates accounts', async () => {
    const hook = renderHook(() => useCalendarMutation('owner'))
    const write = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('storage unavailable')
    })
    await act(async () => {
      await expect(hook.result.current.execute(save())).rejects.toThrow('storage unavailable')
    })
    expect(execute).not.toHaveBeenCalled()
    write.mockRestore()
    execute.mockRejectedValueOnce(new Error('lost response'))
    await act(async () => {
      await expect(hook.result.current.execute(save())).rejects.toThrow()
    })
    const pendingId = hook.result.current.pending?.requestId
    const other = renderHook(() => useCalendarMutation('other-owner'))
    expect(other.result.current.pending).toBeNull()
    execute.mockRejectedValueOnce(new Error('lookup unavailable'))
    await act(async () => {
      await expect(hook.result.current.execute()).rejects.toThrow('lookup unavailable')
    })
    expect(hook.result.current.pending?.requestId).toBe(pendingId)
    expect(execute.mock.calls.filter(([document]) => document.includes('SaveCalendarEvent'))).toHaveLength(1)
  })

  it('does not clear a newer pending change when an older reader finishes', async () => {
    let finishFirst!: (response: unknown) => void
    execute.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finishFirst = resolve
        }),
    )
    const first = renderHook(() => useCalendarMutation('owner'))
    let inFlight!: Promise<unknown>
    act(() => {
      inFlight = first.result.current.execute(save())
    })
    const requestId = first.result.current.pending?.requestId
    const second = renderHook(() => useCalendarMutation('owner'))
    execute.mockResolvedValueOnce({ GetCalendarRequest: { requestId, isMissing: false } })
    await act(async () => {
      await second.result.current.execute()
    })
    execute.mockRejectedValueOnce(new Error('later response lost'))
    await act(async () => {
      await expect(
        second.result.current.execute({
          operation: 'delete',
          summary: 'Planning',
          variables: { calendarId: 'calendar', id: 'event' },
        }),
      ).rejects.toThrow()
    })
    await act(async () => {
      finishFirst({})
      await inFlight
    })
    expect(first.result.current.pending?.operation).toBe('delete')
    expect(first.result.current.pending?.requestId).toBe(second.result.current.pending?.requestId)
  })
})

it.each([false, true])('clears pending only after cancellation resolves, completed=%s', async (isCompleted) => {
  const hook = renderHook(() => useCalendarMutation('owner'))
  execute.mockRejectedValueOnce(new Error('lost response'))
  await act(async () => {
    await expect(hook.result.current.execute(save())).rejects.toThrow()
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

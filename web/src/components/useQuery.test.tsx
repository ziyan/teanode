import { act, cleanup, renderHook, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'

import { useQuery } from './useQuery'

function pendingRead<T>() {
  let resolve!: (answer: T) => void
  let reject!: (failure: Error) => void
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise
    reject = rejectPromise
  })
  return { promise, resolve, reject }
}

afterEach(cleanup)

describe('query request ownership', () => {
  it('keeps a new selection when an older read completes last', async () => {
    const first = pendingRead<string>()
    const second = pendingRead<string>()
    const { result, rerender } = renderHook(
      ({ selected }) =>
        useQuery(() => (selected === 'first' ? first.promise : second.promise), [selected], { refresh: false }),
      { initialProps: { selected: 'first' } },
    )
    rerender({ selected: 'second' })
    await act(async () => second.resolve('second answer'))
    await waitFor(() => expect(result.current.data).toBe('second answer'))
    await act(async () => first.resolve('old answer'))
    expect(result.current.data).toBe('second answer')
  })

  it('ignores an old failure after the current request succeeds', async () => {
    const first = pendingRead<string>()
    const second = pendingRead<string>()
    const { result, rerender } = renderHook(
      ({ selected }) =>
        useQuery(() => (selected === 'first' ? first.promise : second.promise), [selected], { refresh: false }),
      { initialProps: { selected: 'first' } },
    )
    rerender({ selected: 'second' })
    await act(async () => second.resolve('current answer'))
    await act(async () => first.reject(new Error('old failure')))
    expect(result.current.error).toBeNull()
    expect(result.current.data).toBe('current answer')
  })

  it('preserves object identity when a refresh contains no changes', async () => {
    const { result } = renderHook(() => useQuery(async () => ({ subject: 'unchanged' }), [], { refresh: false }))
    await waitFor(() => expect(result.current.loading).toBe(false))
    const original = result.current.data
    await act(async () => result.current.reload())
    expect(result.current.data).toBe(original)
  })
})

import { cleanup, renderHook } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { useEdgeSwipe } from './edgeSwipe'

afterEach(cleanup)

function swipe(points: [number, number][]) {
  const touchAt = ([x, y]: [number, number]) => ({ clientX: x, clientY: y }) as Touch
  document.dispatchEvent(new TouchEvent('touchstart', { touches: [touchAt(points[0])] }))
  for (const point of points.slice(1))
    document.dispatchEvent(new TouchEvent('touchmove', { touches: [touchAt(point)] }))
  document.dispatchEvent(new TouchEvent('touchend'))
}

it('opens the menu on a swipe in from the left edge, and only from there', () => {
  const onOpen = vi.fn()
  renderHook(() => useEdgeSwipe(true, false, onOpen, vi.fn()))
  swipe([
    [120, 300],
    [240, 300],
  ])
  expect(onOpen).not.toHaveBeenCalled()
  swipe([
    [10, 300],
    [30, 304],
    [80, 306],
  ])
  expect(onOpen).toHaveBeenCalledTimes(1)
})

it('leaves a swipe that goes more down than across to the page', () => {
  const onOpen = vi.fn()
  renderHook(() => useEdgeSwipe(true, false, onOpen, vi.fn()))
  swipe([
    [10, 300],
    [40, 400],
    [90, 480],
  ])
  expect(onOpen).not.toHaveBeenCalled()
})

it('closes the open menu on a swipe to the left, and does nothing on a desktop', () => {
  const onClose = vi.fn()
  renderHook(() => useEdgeSwipe(true, true, vi.fn(), onClose))
  swipe([
    [200, 300],
    [120, 302],
  ])
  expect(onClose).toHaveBeenCalledTimes(1)
  cleanup()
  const onOpen = vi.fn()
  renderHook(() => useEdgeSwipe(false, false, onOpen, vi.fn()))
  swipe([
    [10, 300],
    [90, 300],
  ])
  expect(onOpen).not.toHaveBeenCalled()
})

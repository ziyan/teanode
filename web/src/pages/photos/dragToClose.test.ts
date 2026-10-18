import { describe, expect, it } from 'vitest'
import {
  CLOSE_DISTANCE_PIXELS,
  FLICK_MINIMUM_PIXELS,
  FLICK_SPEED_PIXELS_PER_MS,
  dragDirection,
  dragProgress,
  shouldCloseAfterDrag,
} from './dragToClose'

describe('drag to close', () => {
  it('closes from far enough down, whatever the speed', () => {
    expect(shouldCloseAfterDrag(CLOSE_DISTANCE_PIXELS, 0)).toBe(true)
    expect(shouldCloseAfterDrag(CLOSE_DISTANCE_PIXELS - 1, 0)).toBe(false)
  })

  it('closes from a shorter way when the finger was flicking', () => {
    expect(shouldCloseAfterDrag(FLICK_MINIMUM_PIXELS, FLICK_SPEED_PIXELS_PER_MS)).toBe(true)
    expect(shouldCloseAfterDrag(FLICK_MINIMUM_PIXELS, FLICK_SPEED_PIXELS_PER_MS - 0.01)).toBe(false)
  })

  it('does not close from a flick that hardly moved', () => {
    expect(shouldCloseAfterDrag(FLICK_MINIMUM_PIXELS - 1, 5)).toBe(false)
  })

  it('waits to know which way a finger is going', () => {
    expect(dragDirection(3, 4)).toBe('undecided')
  })

  it('takes a drag that is mostly down for a close, and any other for something else', () => {
    expect(dragDirection(2, 30)).toBe('down')
    expect(dragDirection(30, 5)).toBe('other')
    expect(dragDirection(0, -30)).toBe('other')
    // Down, but as much across as down: the photo is being turned, not dropped.
    expect(dragDirection(30, 32)).toBe('other')
  })

  it('measures how far along a close is against the height of the screen', () => {
    expect(dragProgress(0, 800)).toBe(0)
    expect(dragProgress(160, 800)).toBe(0.5)
    expect(dragProgress(5000, 800)).toBe(1)
    expect(dragProgress(-50, 800)).toBe(0)
    expect(dragProgress(100, 0)).toBe(0)
  })
})

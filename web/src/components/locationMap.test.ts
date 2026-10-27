import { describe, expect, it } from 'vitest'

import { placeMenu } from './locationMap'

// A rectangle from its edges, as getBoundingClientRect gives one.
function rectangle(left: number, top: number, right: number, bottom: number): DOMRect {
  return { left, top, right, bottom, width: right - left, height: bottom - top, x: left, y: top } as DOMRect
}

const map = rectangle(0, 0, 600, 360)

describe("a group's list on the map", () => {
  it('opens below pictures near the top, as tall as the room left', () => {
    expect(placeMenu(rectangle(280, 40, 320, 80), map, 180)).toEqual({
      isAbove: false,
      maxHeightPixels: 264,
      shiftPixels: 0,
    })
  })

  it('opens above pictures near the bottom', () => {
    expect(placeMenu(rectangle(280, 280, 320, 320), map, 180)).toMatchObject({ isAbove: true, maxHeightPixels: 264 })
  })

  it('moves in from the edge it would go past', () => {
    expect(placeMenu(rectangle(0, 40, 40, 80), map, 180).shiftPixels).toBe(78)
    expect(placeMenu(rectangle(560, 40, 600, 80), map, 180).shiftPixels).toBe(-78)
  })
})

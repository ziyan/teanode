import { describe, expect, it } from 'vitest'
import {
  DOUBLE_TAP_SCALE,
  FITTED_VIEW,
  MAXIMUM_SCALE,
  clampView,
  fittedSize,
  panBy,
  pinchView,
  scaleAfterWheel,
  viewTransform,
  zoomAround,
  type Size,
} from './zoom'

// A box 800 wide and 600 tall holding a picture twice as wide as it is tall,
// which fitted is 800 by 400.
const box: Size = { width: 800, height: 600 }
const picture = fittedSize(box, 2)

// Where a point of the picture (given from the picture's center, unzoomed) is
// drawn, from the box's center, once the view is applied.
const drawnAt = (view: { scale: number; offsetX: number; offsetY: number }, point: { x: number; y: number }) => ({
  x: view.offsetX + point.x * view.scale,
  y: view.offsetY + point.y * view.scale,
})

describe('fittedSize', () => {
  it('fits a wide picture to the width and a tall one to the height', () => {
    expect(fittedSize(box, 2)).toEqual({ width: 800, height: 400 })
    expect(fittedSize(box, 0.5)).toEqual({ width: 300, height: 600 })
  })

  it('takes the whole box for a picture whose shape is not known', () => {
    expect(fittedSize(box, 0)).toEqual(box)
  })
})

describe('zoomAround', () => {
  it('keeps the spot under the pointer under the pointer', () => {
    const pointer = { x: 150, y: -60 }
    const zoomed = zoomAround(FITTED_VIEW, 3, pointer, box, picture)
    // The spot of the picture that was under the pointer when fitted is the
    // pointer's own place, and it is drawn there again after the zoom.
    expect(drawnAt(zoomed, pointer)).toEqual(pointer)
    expect(zoomed.scale).toBe(3)
  })

  it('keeps it there again when zooming further from a view that has moved', () => {
    const first = zoomAround(FITTED_VIEW, 2, { x: 100, y: 50 }, box, picture)
    const pointer = { x: -120, y: 30 }
    // The spot of the picture under the pointer now.
    const spot = { x: (pointer.x - first.offsetX) / first.scale, y: (pointer.y - first.offsetY) / first.scale }
    const second = zoomAround(first, 4, pointer, box, picture)
    const drawn = drawnAt(second, spot)
    expect(drawn.x).toBeCloseTo(pointer.x, 6)
    expect(drawn.y).toBeCloseTo(pointer.y, 6)
  })

  it('zooms about the center when the pointer is there, without moving the picture', () => {
    expect(zoomAround(FITTED_VIEW, DOUBLE_TAP_SCALE, { x: 0, y: 0 }, box, picture)).toEqual({
      scale: DOUBLE_TAP_SCALE,
      offsetX: 0,
      offsetY: 0,
    })
  })

  it('does not go past the largest scale, nor below fitted', () => {
    expect(zoomAround(FITTED_VIEW, 100, { x: 0, y: 0 }, box, picture).scale).toBe(MAXIMUM_SCALE)
    expect(zoomAround({ scale: 2, offsetX: 0, offsetY: 0 }, 0.2, { x: 10, y: 10 }, box, picture)).toEqual(FITTED_VIEW)
  })

  it('is fitted again, with no offset, when it comes back down to fitted', () => {
    const zoomed = zoomAround(FITTED_VIEW, 3, { x: 300, y: 200 }, box, picture)
    expect(zoomed.offsetX).not.toBe(0)
    expect(zoomAround(zoomed, 1, { x: 300, y: 200 }, box, picture)).toEqual(FITTED_VIEW)
  })
})

describe('clampView and panBy', () => {
  it('stops a pan where the picture meets the edge of the box', () => {
    // At 3 times, the picture is 2400 by 1200: 800 to spare either side of the
    // box across, 300 up and down.
    const zoomed = { scale: 3, offsetX: 0, offsetY: 0 }
    expect(panBy(zoomed, 5000, 5000, box, picture)).toEqual({ scale: 3, offsetX: 800, offsetY: 300 })
    expect(panBy(zoomed, -5000, -5000, box, picture)).toEqual({ scale: 3, offsetX: -800, offsetY: -300 })
    expect(panBy(zoomed, 120, -40, box, picture)).toEqual({ scale: 3, offsetX: 120, offsetY: -40 })
  })

  it('keeps a picture that is smaller than the box on one side centered on that side', () => {
    // At 1.2 times, the picture is 960 by 480: wider than the box, shorter than it.
    const view = clampView({ scale: 1.2, offsetX: 500, offsetY: 500 }, box, picture)
    expect(view.offsetX).toBe(80)
    expect(view.offsetY).toBe(0)
  })

  it('does not pan a picture that is fitted', () => {
    expect(panBy(FITTED_VIEW, 100, 100, box, picture)).toEqual(FITTED_VIEW)
  })

  it('pulls a view back inside the picture when the box has grown', () => {
    const bigger = { width: 2000, height: 1500 }
    // At 1.2 times the picture is 2400 wide in a box 2000 wide: 200 to spare
    // either side, and the offset of 700 that fitted a smaller box is too much.
    expect(clampView({ scale: 1.2, offsetX: 700, offsetY: 0 }, bigger, fittedSize(bigger, 2))).toEqual({
      scale: 1.2,
      offsetX: 200,
      offsetY: 0,
    })
  })
})

describe('scaleAfterWheel', () => {
  it('zooms in when scrolling up and out when scrolling down', () => {
    expect(scaleAfterWheel(2, -100, 0, false)).toBeGreaterThan(2)
    expect(scaleAfterWheel(2, 100, 0, false)).toBeLessThan(2)
  })

  it('zooms further for the same distance when it is a trackpad pinch', () => {
    expect(scaleAfterWheel(2, -10, 0, true)).toBeGreaterThan(scaleAfterWheel(2, -10, 0, false))
  })

  it('counts a wheel that scrolls by lines for more than one of pixels', () => {
    expect(scaleAfterWheel(2, -3, 1, false)).toBeGreaterThan(scaleAfterWheel(2, -3, 0, false))
  })

  it('never zooms by more than a fixed share in one event, and stays between fitted and the largest', () => {
    expect(scaleAfterWheel(2, -100000, 0, false)).toBeCloseTo(2 * 1.6, 6)
    expect(scaleAfterWheel(1, 500, 0, false)).toBe(1)
    expect(scaleAfterWheel(MAXIMUM_SCALE, -500, 0, false)).toBe(MAXIMUM_SCALE)
  })
})

describe('pinchView', () => {
  const start = { distancePixels: 100, midpoint: { x: 0, y: 0 }, view: FITTED_VIEW }

  it('scales with the distance between the fingers', () => {
    const view = pinchView(start, { x: -100, y: 0 }, { x: 100, y: 0 }, box, picture)
    expect(view.scale).toBe(2)
    expect(view.offsetX).toBe(0)
  })

  it('zooms around the fingers: the spot between them stays between them', () => {
    const fingerStart = { distancePixels: 100, midpoint: { x: 200, y: 100 }, view: FITTED_VIEW }
    const view = pinchView(fingerStart, { x: 100, y: 100 }, { x: 400, y: 100 }, box, picture)
    expect(view.scale).toBe(3)
    // The spot that was at (200, 100) is drawn at the new midpoint (250, 100).
    const drawn = drawnAt(view, { x: 200, y: 100 })
    expect(drawn.x).toBeCloseTo(250, 6)
    expect(drawn.y).toBeCloseTo(100, 6)
  })

  it('pans when both fingers move together without changing the distance', () => {
    const zoomedStart = { distancePixels: 100, midpoint: { x: 0, y: 0 }, view: { scale: 3, offsetX: 0, offsetY: 0 } }
    const view = pinchView(zoomedStart, { x: -20, y: 30 }, { x: 80, y: 30 }, box, picture)
    expect(view).toEqual({ scale: 3, offsetX: 30, offsetY: 30 })
  })

  it('does not zoom out past fitted', () => {
    expect(pinchView(start, { x: -10, y: 0 }, { x: 10, y: 0 }, box, picture)).toEqual(FITTED_VIEW)
  })

  it('stays inside the picture', () => {
    const view = pinchView(
      { distancePixels: 100, midpoint: { x: 0, y: 0 }, view: { scale: 2, offsetX: 0, offsetY: 0 } },
      { x: 700, y: 500 },
      { x: 900, y: 500 },
      box,
      picture,
    )
    expect(Math.abs(view.offsetX)).toBeLessThanOrEqual((picture.width * view.scale - box.width) / 2)
  })
})

describe('viewTransform', () => {
  it('draws nothing for a picture that is fitted, and the view otherwise', () => {
    expect(viewTransform(FITTED_VIEW)).toBeUndefined()
    expect(viewTransform({ scale: 2, offsetX: 10, offsetY: -5 })).toBe('translate(10px, -5px) scale(2)')
  })
})

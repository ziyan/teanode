// The arithmetic of zooming and panning the photo in the viewer, with no
// element or event in it, so that it can be checked on its own.
//
// A view is how the picture sits in its box: scale 1 is the picture fitted (all
// of it, as large as the box allows), larger is zoomed in, and the offset is how
// far the picture's center has moved from the box's center, in pixels of the
// box. The picture is drawn as translate(offset) scale(scale) from the center.

export interface ZoomView {
  scale: number
  offsetX: number
  offsetY: number
}

export interface Size {
  width: number
  height: number
}

export interface Point {
  x: number
  y: number
}

// Fitted, and not moved.
export const FITTED_VIEW: ZoomView = { scale: 1, offsetX: 0, offsetY: 0 }

// How far in a picture can be zoomed, as a multiple of its fitted size.
export const MAXIMUM_SCALE = 8
// How far the zoom button and a double click or tap go in.
export const DOUBLE_TAP_SCALE = 2.5
// Closer to fitted than this is fitted: a pinch that ends at 1.004 is not a zoom.
const FITTED_TOLERANCE = 0.01

// How much one turn of a wheel zooms: the scale is multiplied by e to the power
// of the distance scrolled times this. A trackpad pinch arrives as a wheel with
// ctrlKey set and small distances, so it needs a larger share of each.
const WHEEL_ZOOM_PER_PIXEL = 0.0015
const PINCH_ZOOM_PER_PIXEL = 0.01
// A wheel that scrolls by lines (some mice) or pages: pixels per line.
const PIXELS_PER_LINE = 16
const PIXELS_PER_PAGE = 400
// No single wheel event zooms by more than this factor either way.
const MOST_ZOOM_PER_EVENT = 1.6

// The size a picture is drawn at when fitted into a box: all of it, as large as
// the box allows. An aspect ratio that is not known (0) takes the whole box.
export function fittedSize(box: Size, aspectRatio: number): Size {
  if (!(aspectRatio > 0) || box.width <= 0 || box.height <= 0) return { width: box.width, height: box.height }
  const boxAspectRatio = box.width / box.height
  return boxAspectRatio > aspectRatio
    ? { width: box.height * aspectRatio, height: box.height }
    : { width: box.width, height: box.width / aspectRatio }
}

function clamp(value: number, minimum: number, maximum: number): number {
  return Math.min(maximum, Math.max(minimum, value))
}

export function clampScale(scale: number): number {
  if (!Number.isFinite(scale)) return 1
  return clamp(scale, 1, MAXIMUM_SCALE)
}

// The view moved back inside what the picture covers: no edge of the picture
// is pulled past the edge of the box while it is larger than the box, and a
// picture that is smaller than the box on one side stays centered on it.
export function clampView(view: ZoomView, box: Size, picture: Size): ZoomView {
  const scale = clampScale(view.scale)
  if (scale <= 1 + FITTED_TOLERANCE) return FITTED_VIEW
  const farthestX = Math.max(0, (picture.width * scale - box.width) / 2)
  const farthestY = Math.max(0, (picture.height * scale - box.height) / 2)
  return {
    scale,
    offsetX: clamp(view.offsetX, -farthestX, farthestX),
    offsetY: clamp(view.offsetY, -farthestY, farthestY),
  }
}

// The view at another scale in which the point under the anchor, given from the
// center of the box, is still under it: the spot of the picture the pointer is
// on stays there while it grows or shrinks around it.
export function zoomAround(view: ZoomView, nextScale: number, anchor: Point, box: Size, picture: Size): ZoomView {
  const scale = clampScale(nextScale)
  const ratio = scale / view.scale
  return clampView(
    {
      scale,
      offsetX: anchor.x - (anchor.x - view.offsetX) * ratio,
      offsetY: anchor.y - (anchor.y - view.offsetY) * ratio,
    },
    box,
    picture,
  )
}

// The view moved by a drag.
export function panBy(view: ZoomView, acrossPixels: number, downPixels: number, box: Size, picture: Size): ZoomView {
  if (view.scale <= 1 + FITTED_TOLERANCE) return FITTED_VIEW
  return clampView(
    { scale: view.scale, offsetX: view.offsetX + acrossPixels, offsetY: view.offsetY + downPixels },
    box,
    picture,
  )
}

// The scale after one wheel event. Scrolling up (negative distance) zooms in.
// A pinch on a trackpad is told by isPinch.
export function scaleAfterWheel(scale: number, deltaY: number, deltaMode: number, isPinch: boolean): number {
  const pixels = deltaY * (deltaMode === 1 ? PIXELS_PER_LINE : deltaMode === 2 ? PIXELS_PER_PAGE : 1)
  const factor = Math.exp(-pixels * (isPinch ? PINCH_ZOOM_PER_PIXEL : WHEEL_ZOOM_PER_PIXEL))
  return clampScale(scale * clamp(factor, 1 / MOST_ZOOM_PER_EVENT, MOST_ZOOM_PER_EVENT))
}

export function distanceBetween(first: Point, second: Point): number {
  return Math.hypot(second.x - first.x, second.y - first.y)
}

export function midpointOf(first: Point, second: Point): Point {
  return { x: (first.x + second.x) / 2, y: (first.y + second.y) / 2 }
}

// Where two fingers started: how far apart, where between them, and the view
// that was showing.
export interface PinchStart {
  distancePixels: number
  midpoint: Point
  view: ZoomView
}

// The view while two fingers that started as `start` are now at `first` and
// `second` (all points from the center of the box). The picture scales with the
// distance between the fingers, and the spot that was between them when they
// went down stays between them as they move, so a pinch zooms around the
// fingers and moving both fingers together pans.
export function pinchView(start: PinchStart, first: Point, second: Point, box: Size, picture: Size): ZoomView {
  if (start.distancePixels <= 0) return start.view
  const scale = clampScale((start.view.scale * distanceBetween(first, second)) / start.distancePixels)
  const ratio = scale / start.view.scale
  const midpoint = midpointOf(first, second)
  return clampView(
    {
      scale,
      offsetX: midpoint.x - (start.midpoint.x - start.view.offsetX) * ratio,
      offsetY: midpoint.y - (start.midpoint.y - start.view.offsetY) * ratio,
    },
    box,
    picture,
  )
}

// The CSS that draws a view. Fitted draws nothing, so a photo that is not
// zoomed has no transform of its own.
export function viewTransform(view: ZoomView): string | undefined {
  if (view.scale <= 1 + FITTED_TOLERANCE) return undefined
  return `translate(${view.offsetX}px, ${view.offsetY}px) scale(${view.scale})`
}

import { expect, it } from 'vitest'

import {
  buttonOf,
  clampView,
  clickCountOf,
  keyMessage,
  modifiersOf,
  MOST_ZOOM,
  panView,
  piecesOf,
  pinchView,
  pointToTab,
  UNZOOMED,
} from './browserInput'

const noModifiers = { altKey: false, ctrlKey: false, metaKey: false, shiftKey: false }

// A picture drawn 640 pixels wide and 360 tall at (100, 50), of a page that is
// 1280 by 720 CSS pixels.
const pictureRect = { left: 100, top: 50, width: 640, height: 360 }

it('maps a click at the middle of the picture to the middle of the page', () => {
  expect(pointToTab(420, 230, pictureRect, 1280, 720)).toEqual({ x: 640, y: 360 })
})

it('maps the picture scale, not the screen pixel', () => {
  expect(pointToTab(100, 50, pictureRect, 1280, 720)).toEqual({ x: 0, y: 0 })
  expect(pointToTab(260, 140, pictureRect, 1280, 720)).toEqual({ x: 320, y: 180 })
})

it('keeps a point outside the picture inside the page', () => {
  expect(pointToTab(10, 5, pictureRect, 1280, 720)).toEqual({ x: 0, y: 0 })
  expect(pointToTab(5000, 5000, pictureRect, 1280, 720)).toEqual({ x: 1280, y: 720 })
})

it('maps to the origin while the picture has no size yet', () => {
  expect(pointToTab(10, 10, { left: 0, top: 0, width: 0, height: 0 }, 1280, 720)).toEqual({ x: 0, y: 0 })
})

it('gives the DevTools modifier bits', () => {
  expect(modifiersOf(noModifiers)).toBe(0)
  expect(modifiersOf({ ...noModifiers, altKey: true })).toBe(1)
  expect(modifiersOf({ ...noModifiers, ctrlKey: true })).toBe(2)
  expect(modifiersOf({ ...noModifiers, metaKey: true })).toBe(4)
  expect(modifiersOf({ ...noModifiers, shiftKey: true })).toBe(8)
  expect(modifiersOf({ altKey: true, ctrlKey: true, metaKey: true, shiftKey: true })).toBe(15)
})

it('sends Enter as key code 13 with a carriage return on the way down only', () => {
  const event = { ...noModifiers, key: 'Enter', code: 'Enter' }
  expect(keyMessage(event, 'down')).toEqual({
    type: 'key',
    event: 'down',
    key: 'Enter',
    code: 'Enter',
    keyCode: 13,
    text: '\r',
    modifiers: 0,
  })
  expect(keyMessage(event, 'up')).toMatchObject({ event: 'up', keyCode: 13, text: '' })
})

it('sends the editing and arrow keys with their key codes and no text', () => {
  const codes: [string, number][] = [
    ['Backspace', 8],
    ['Tab', 9],
    ['Escape', 27],
    ['ArrowLeft', 37],
    ['ArrowUp', 38],
    ['ArrowRight', 39],
    ['ArrowDown', 40],
    ['Delete', 46],
    ['Home', 36],
    ['End', 35],
    ['PageUp', 33],
    ['PageDown', 34],
  ]
  for (const [key, keyCode] of codes) {
    expect(keyMessage({ ...noModifiers, key, code: key }, 'down')).toMatchObject({ keyCode, text: '' })
  }
})

it('sends a printable key with its character and its letter key code', () => {
  expect(keyMessage({ ...noModifiers, key: 'a', code: 'KeyA' }, 'down')).toMatchObject({
    key: 'a',
    keyCode: 65,
    text: 'a',
  })
  // The code is optional for a key from a phone's keyboard.
  expect(keyMessage({ ...noModifiers, key: 'a', code: '' }, 'down')).toMatchObject({ keyCode: 65, text: 'a' })
  expect(keyMessage({ ...noModifiers, key: 'a', code: 'KeyA' }, 'up')).toMatchObject({ keyCode: 65, text: '' })
})

it('sends a digit and a space by their character codes', () => {
  expect(keyMessage({ ...noModifiers, key: '7', code: 'Digit7' }, 'down')).toMatchObject({ keyCode: 55, text: '7' })
  expect(keyMessage({ ...noModifiers, key: ' ', code: 'Space' }, 'down')).toMatchObject({ keyCode: 32, text: ' ' })
})

it('keeps the key under a shifted character', () => {
  expect(keyMessage({ ...noModifiers, shiftKey: true, key: '!', code: 'Digit1' }, 'down')).toMatchObject({
    keyCode: 49,
    text: '!',
    modifiers: 8,
  })
})

it('types nothing for a shortcut', () => {
  expect(keyMessage({ ...noModifiers, ctrlKey: true, key: 'a', code: 'KeyA' }, 'down')).toMatchObject({
    keyCode: 65,
    text: '',
    modifiers: 2,
  })
})

it('falls back to the browser key code for a key it does not know', () => {
  expect(keyMessage({ ...noModifiers, key: 'F5', code: 'F5', keyCode: 116 }, 'down')).toMatchObject({
    keyCode: 116,
    text: '',
  })
})

it('names the pointer buttons', () => {
  expect(buttonOf(0)).toBe('left')
  expect(buttonOf(1)).toBe('middle')
  expect(buttonOf(2)).toBe('right')
})

it('counts a second press nearby and soon as a double click', () => {
  const first = { timeMS: 1000, x: 100, y: 100, button: 'left' as const, clickCount: 1 }
  expect(clickCountOf(null, 1000, 100, 100, 'left')).toBe(1)
  expect(clickCountOf(first, 1300, 102, 99, 'left')).toBe(2)
  expect(clickCountOf({ ...first, clickCount: 2 }, 1400, 100, 100, 'left')).toBe(3)
  // Too late, too far, or another button starts again.
  expect(clickCountOf(first, 1800, 100, 100, 'left')).toBe(1)
  expect(clickCountOf(first, 1300, 160, 100, 'left')).toBe(1)
  expect(clickCountOf(first, 1300, 100, 100, 'right')).toBe(1)
})

it('cuts pasted text into pieces without splitting an emoji', () => {
  expect(piecesOf('abcdef', 4)).toEqual(['abcd', 'ef'])
  // An emoji is two code units; a piece that would end on its first half
  // ends before it instead.
  const pieces = piecesOf('abc\u{1F600}def', 4)
  expect(pieces).toEqual(['abc', '\u{1F600}de', 'f'])
  expect(pieces.join('')).toBe('abc\u{1F600}def')
  expect(piecesOf('', 4)).toEqual([])
})

it('keeps the zoomed picture covering its box', () => {
  expect(clampView({ scale: 0.5, x: 10, y: 10 }, 400, 300)).toEqual({ scale: 1, x: 0, y: 0 })
  expect(clampView({ scale: 9, x: 0, y: 0 }, 400, 300).scale).toBe(MOST_ZOOM)
  // Zoomed twice, it may move up to its box's size to the left and up.
  expect(clampView({ scale: 2, x: -900, y: 50 }, 400, 300)).toEqual({ scale: 2, x: -400, y: 0 })
})

it('zooms around the point between two fingers', () => {
  // Fingers 100 apart around (200, 150) spread to 200 apart around the
  // same point: twice the zoom, and that point stays where it was.
  const view = pinchView(
    UNZOOMED,
    [
      { x: 150, y: 150 },
      { x: 250, y: 150 },
    ],
    [
      { x: 100, y: 150 },
      { x: 300, y: 150 },
    ],
    400,
    300,
  )
  expect(view.scale).toBe(2)
  expect(view.x).toBe(-200)
  expect(view.y).toBe(-150)
  // Moved together, they move the picture with them.
  const moved = panView(view, 50, 0, 400, 300)
  expect(moved.x).toBe(-150)
})

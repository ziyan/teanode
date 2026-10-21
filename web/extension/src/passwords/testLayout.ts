// Helpers for the tests that need a page to have a layout. jsdom does no
// layout, so every rectangle is empty; these give elements a size, and let a
// test say which ones have none.

import { vi } from 'vitest'

export interface Layout {
  width: number
  height: number
  left: number
  top: number
}

const DEFAULT_LAYOUT: Layout = { width: 200, height: 30, left: 10, top: 10 }

// giveLayout makes every element 200 by 30 at (10, 10) unless a test
// overrode it with setLayout, which it does by a data attribute.
export function giveLayout(): void {
  vi.spyOn(Element.prototype, 'getBoundingClientRect').mockImplementation(function (this: Element) {
    const text = this.getAttribute('data-layout')
    const layout = text ? { ...DEFAULT_LAYOUT, ...(JSON.parse(text) as Partial<Layout>) } : DEFAULT_LAYOUT
    return {
      x: layout.left,
      y: layout.top,
      left: layout.left,
      top: layout.top,
      width: layout.width,
      height: layout.height,
      right: layout.left + layout.width,
      bottom: layout.top + layout.height,
      toJSON: () => ({}),
    } as DOMRect
  })
}

export function setLayout(element: Element, layout: Partial<Layout>): void {
  element.setAttribute('data-layout', JSON.stringify(layout))
}

import { cleanup, render } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { Tabs } from './tabs'

// The labels as the language in use writes them: a test switches language
// by changing this.
let labelByKey: Record<string, string> = {}
vi.mock('../i18n/i18n', () => ({
  useTranslation: () => ({ t: (key: string) => labelByKey[key] ?? key }),
}))

// jsdom lays nothing out, so the row's widths are the test's: the row is
// 300 pixels wide, and its content as wide as rowContentWidth.
let rowContentWidth = 300
const observed: Element[] = []

beforeEach(() => {
  labelByKey = { 'tabs.first': 'One', 'tabs.second': 'Two' }
  rowContentWidth = 300
  observed.length = 0
  vi.spyOn(HTMLElement.prototype, 'clientWidth', 'get').mockReturnValue(300)
  vi.spyOn(HTMLElement.prototype, 'scrollWidth', 'get').mockImplementation(() => rowContentWidth)
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe(element: Element) {
        observed.push(element)
      }
      disconnect() {}
    },
  )
})

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

const items = [
  { id: 'first', label: 'tabs.first' as never },
  { id: 'second', label: 'tabs.second' as never },
]

// A change of language widens the labels, and the row with it, without the
// number of tabs changing: the fade that says the row scrolls appears.
it('measures the row again when the labels change', () => {
  const { container, rerender } = render(<Tabs items={items} active="first" onSelect={() => {}} />)
  const row = container.querySelector('.tabs')
  expect(row?.classList.contains('tabs-more-after')).toBe(false)
  labelByKey = { 'tabs.first': 'A much longer first label', 'tabs.second': 'A much longer second label' }
  rowContentWidth = 520
  rerender(<Tabs items={items} active="first" onSelect={() => {}} />)
  expect(row?.classList.contains('tabs-more-after')).toBe(true)
})

// A web font arriving widens the tabs and nothing else, so each tab is
// watched as well as the row.
it('watches each tab for a change of width', () => {
  const { container } = render(<Tabs items={items} active="first" onSelect={() => {}} />)
  for (const tab of Array.from(container.querySelectorAll('.tabs button'))) {
    expect(observed).toContain(tab)
  }
})

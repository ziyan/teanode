import { act, cleanup, render } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { afterEach, expect, it, vi } from 'vitest'

import { memoryPath, shownPath, useShowPage } from './dashboardPath'

afterEach(cleanup)

it('reads a memory link as a page the graph could hold and nothing else', () => {
  expect(memoryPath('people/some-person')).toBe('/knowledge/people/some-person')
  expect(memoryPath('projects/example-app#12')).toBe('/knowledge/projects/example-app')
  expect(memoryPath('people/zoë')).toBe('/knowledge/people/zoë')
  for (const target of ['', '../x', 'a//b', 'A/b', 'a b', 'a?b', 'a/b#c', 'a#1#2', 'a--b', '-a']) {
    expect(memoryPath(target), target).toBeNull()
  }
})

it('shows only the parts of the dashboard the agent may take the person to', () => {
  expect(shownPath('/knowledge/people/some-person')).toBe('/knowledge/people/some-person')
  expect(shownPath('/mailbox/starred/item42/')).toBe('/mailbox/starred/item42')
  expect(shownPath('/settings/agent')).toBe('/settings/agent')
  expect(shownPath('/finance/budgets')).toBe('/finance/budgets')
  for (const path of [
    '',
    '/',
    'https://example.net/settings',
    '//example.net/settings',
    'javascript:alert(1)',
    '/manage/server/about',
    '/settingsx',
    '/finance-link',
    '/finance/link',
    '/finance/link/',
    '/settings/../server',
    '/mailbox?search=x',
    '/settings/agent/a b',
  ]) {
    expect(shownPath(path), path).toBeNull()
  }
})

// Where the router is, as text.
function Where() {
  return <p data-testid="where">{useLocation().pathname}</p>
}

let handle: ReturnType<typeof useShowPage> = () => undefined

function Drawer({ leaving }: { leaving: () => void }) {
  handle = useShowPage(leaving)
  return null
}

it('moves the dashboard on a navigate event once, and puts the drawer away as a link does', () => {
  const leaving = vi.fn()
  const { getByTestId } = render(
    <MemoryRouter initialEntries={['/mailbox']}>
      <Drawer leaving={leaving} />
      <Routes>
        <Route path="*" element={<Where />} />
      </Routes>
    </MemoryRouter>,
  )
  const event = { kind: 'navigate', runId: 'run1', sequence: 4, text: '/knowledge/people/some-person' }
  act(() => handle(event))
  expect(getByTestId('where').textContent).toBe('/knowledge/people/some-person')
  expect(leaving).toHaveBeenCalledTimes(1)

  // The person moves on; the same event replayed when the socket comes
  // back does not take them back.
  act(() => handle({ kind: 'navigate', runId: 'run1', sequence: 5, text: '/mailbox' }))
  expect(getByTestId('where').textContent).toBe('/mailbox')
  act(() => handle(event))
  expect(getByTestId('where').textContent).toBe('/mailbox')

  // Nor anywhere off the allow list.
  act(() => handle({ kind: 'navigate', runId: 'run1', sequence: 6, text: '/manage/server/about' }))
  act(() => handle({ kind: 'navigate', runId: 'run1', sequence: 7, text: 'https://example.net/' }))
  expect(getByTestId('where').textContent).toBe('/mailbox')
  expect(leaving).toHaveBeenCalledTimes(2)
})

function setVisibility(visibility: 'visible' | 'hidden') {
  Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => visibility })
  document.dispatchEvent(new Event('visibilitychange'))
}

it('waits in a hidden tab and moves when the person comes back soon enough', () => {
  vi.useFakeTimers()
  try {
    const leaving = vi.fn()
    const { getByTestId } = render(
      <MemoryRouter initialEntries={['/mailbox']}>
        <Drawer leaving={leaving} />
        <Routes>
          <Route path="*" element={<Where />} />
        </Routes>
      </MemoryRouter>,
    )
    setVisibility('hidden')
    act(() => handle({ kind: 'navigate', runId: 'run2', sequence: 1, text: '/knowledge/people/some-person' }))
    expect(getByTestId('where').textContent).toBe('/mailbox')
    act(() => setVisibility('visible'))
    expect(getByTestId('where').textContent).toBe('/knowledge/people/some-person')
    expect(leaving).toHaveBeenCalledTimes(1)

    // Back after too long, the page they are on stays.
    act(() => handle({ kind: 'navigate', runId: 'run2', sequence: 2, text: '/mailbox' }))
    setVisibility('hidden')
    act(() => handle({ kind: 'navigate', runId: 'run2', sequence: 3, text: '/settings/agent/alerts' }))
    vi.advanceTimersByTime(3 * 60 * 1000)
    act(() => setVisibility('visible'))
    expect(getByTestId('where').textContent).toBe('/mailbox')
  } finally {
    setVisibility('visible')
    vi.useRealTimers()
  }
})

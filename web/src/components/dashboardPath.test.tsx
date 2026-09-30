import { act, cleanup, render } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { afterEach, expect, it, vi } from 'vitest'

import { memoryPath, shownPath, useShowPage } from './dashboardPath'

afterEach(cleanup)

it('reads a memory link as a page the graph could hold and nothing else', () => {
  expect(memoryPath('people/some-person')).toBe('/settings/knowledge/people/some-person')
  expect(memoryPath('projects/example-app#12')).toBe('/settings/knowledge/projects/example-app')
  expect(memoryPath('people/zoë')).toBe('/settings/knowledge/people/zoë')
  for (const target of ['', '../x', 'a//b', 'A/b', 'a b', 'a?b', 'a/b#c', 'a#1#2', 'a--b', '-a']) {
    expect(memoryPath(target), target).toBeNull()
  }
})

it('shows only the parts of the dashboard the agent may take the person to', () => {
  expect(shownPath('/settings/knowledge/people/some-person')).toBe('/settings/knowledge/people/some-person')
  expect(shownPath('/mailbox/starred/item42/')).toBe('/mailbox/starred/item42')
  expect(shownPath('/settings/agent')).toBe('/settings/agent')
  for (const path of [
    '',
    '/',
    'https://example.net/settings',
    '//example.net/settings',
    'javascript:alert(1)',
    '/server/about',
    '/settingsx',
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
  const event = { kind: 'navigate', runId: 'run1', sequence: 4, text: '/settings/knowledge/people/some-person' }
  act(() => handle(event))
  expect(getByTestId('where').textContent).toBe('/settings/knowledge/people/some-person')
  expect(leaving).toHaveBeenCalledTimes(1)

  // The person moves on; the same event replayed when the socket comes
  // back does not take them back.
  act(() => handle({ kind: 'navigate', runId: 'run1', sequence: 5, text: '/mailbox' }))
  expect(getByTestId('where').textContent).toBe('/mailbox')
  act(() => handle(event))
  expect(getByTestId('where').textContent).toBe('/mailbox')

  // Nor anywhere off the allow list.
  act(() => handle({ kind: 'navigate', runId: 'run1', sequence: 6, text: '/server/about' }))
  act(() => handle({ kind: 'navigate', runId: 'run1', sequence: 7, text: 'https://example.net/' }))
  expect(getByTestId('where').textContent).toBe('/mailbox')
  expect(leaving).toHaveBeenCalledTimes(2)
})

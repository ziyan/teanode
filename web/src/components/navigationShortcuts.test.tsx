import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { useMemo } from 'react'
import { afterEach, expect, it, vi } from 'vitest'
import { MemoryRouter, Route, Routes } from 'react-router-dom'

import { TranslationProvider } from '../i18n/i18n'
import { FOCUS_SEARCH_EVENT } from '../searchEverything'
import { SessionProvider } from '../session'
import { Shortcut, useShortcuts } from '../shortcuts'
import { NavigationShortcuts } from './navigationShortcuts'
import { ToastProvider } from './toast'

afterEach(cleanup)

// A page with a key of its own, r, which the second key of g then r must not
// reach.
function PageWithKeys({ onReply }: { onReply: () => void }) {
  useShortcuts(useMemo<Shortcut[]>(() => [{ key: 'r', label: 'Reply', run: onReply }], [onReply]))
  return (
    <main className="content">
      <p>page</p>
      <input type="search" aria-label="Find" />
    </main>
  )
}

function renderAt(path: string, onReply = vi.fn()) {
  render(
    <TranslationProvider>
      <SessionProvider
        value={{
          userId: 'fixture-owner',
          authenticated: true,
          authenticationRequired: true,
          username: 'fixture',
          passkeysEnabled: false,
          permissions: { everywhere: ['agent:use'], byDomain: [] },
        }}
      >
        <ToastProvider>
          <MemoryRouter initialEntries={[path]}>
            <Routes>
              <Route path="/page" element={<PageWithKeys onReply={onReply} />} />
              <Route path="/recordings" element={<p>recordings page</p>} />
              <Route path="/settings" element={<p>settings page</p>} />
            </Routes>
            <NavigationShortcuts onToggleSidebar={() => {}} />
          </MemoryRouter>
        </ToastProvider>
      </SessionProvider>
    </TranslationProvider>,
  )
  return onReply
}

it('goes to a place on g then its letter, without the page hearing the letter', () => {
  const onReply = renderAt('/page')
  fireEvent.keyDown(document.body, { key: 'g' })
  fireEvent.keyDown(document.body, { key: 'r' })
  expect(screen.getByText('recordings page')).toBeTruthy()
  expect(onReply).not.toHaveBeenCalled()
})

it('leaves the page its own letter when no g came before it', () => {
  const onReply = renderAt('/page')
  fireEvent.keyDown(document.body, { key: 'r' })
  expect(onReply).toHaveBeenCalledTimes(1)
  expect(screen.getByText('page')).toBeTruthy()
})

it('ignores the keys while typing in a box', () => {
  renderAt('/page')
  const box = screen.getByLabelText('Find')
  fireEvent.keyDown(box, { key: 'g' })
  fireEvent.keyDown(box, { key: ',' })
  expect(screen.queryByText('settings page')).toBeNull()
})

it('lists the page keys first, then where g goes, on ?', () => {
  renderAt('/page')
  fireEvent.keyDown(document.body, { key: '?', shift: true, shiftKey: true })
  const headings = screen.getAllByRole('heading', { level: 4 }).map((heading) => heading.textContent)
  expect(headings).toEqual(['On this page', 'Go to', 'Anywhere'])
  expect(screen.getByText('Reply')).toBeTruthy()
  expect(screen.getByText('Recordings')).toBeTruthy()
})

// Counts the times the rail's search box is asked to take focus.
function listenForSearch() {
  const asked = vi.fn()
  window.addEventListener(FOCUS_SEARCH_EVENT, asked)
  return { asked, stop: () => window.removeEventListener(FOCUS_SEARCH_EVENT, asked) }
}

it.each([
  ['Control', { ctrlKey: true }],
  ['Command', { metaKey: true }],
])('focuses the search with %s-K, even from a box', (_name, modifier) => {
  const search = listenForSearch()
  renderAt('/page')
  fireEvent.keyDown(screen.getByLabelText('Find'), { key: 'k', ...modifier })
  expect(search.asked).toHaveBeenCalledTimes(1)
  fireEvent.keyDown(document.body, { key: 'K', ...modifier })
  expect(search.asked).toHaveBeenCalledTimes(2)
  search.stop()
})

it('leaves other chords with K to the browser', () => {
  const search = listenForSearch()
  renderAt('/page')
  fireEvent.keyDown(document.body, { key: 'k', ctrlKey: true, shiftKey: true })
  fireEvent.keyDown(document.body, { key: 'k', ctrlKey: true, altKey: true })
  fireEvent.keyDown(document.body, { key: 'k' })
  expect(search.asked).not.toHaveBeenCalled()
  search.stop()
})

it('does not take Control-K from a dialog that is asking something', () => {
  const search = listenForSearch()
  renderAt('/page')
  const scrim = document.createElement('div')
  scrim.className = 'dialog-scrim'
  document.body.appendChild(scrim)
  fireEvent.keyDown(document.body, { key: 'k', ctrlKey: true })
  scrim.remove()
  expect(search.asked).not.toHaveBeenCalled()
  search.stop()
})

it('searches the page on /, and everything through the rail when the page has no box', () => {
  const search = listenForSearch()
  // jsdom has no layout, so a box counts as visible once it says it has a
  // parent to be laid out in.
  const offsetParent = vi.spyOn(HTMLElement.prototype, 'offsetParent', 'get').mockImplementation(() => document.body)
  renderAt('/page')
  fireEvent.keyDown(document.body, { key: '/' })
  expect(document.activeElement).toBe(screen.getByLabelText('Find'))
  expect(search.asked).not.toHaveBeenCalled()
  offsetParent.mockRestore()

  // No box that can be seen: the rail's takes the key.
  ;(document.activeElement as HTMLElement).blur()
  fireEvent.keyDown(document.body, { key: '/' })
  expect(search.asked).toHaveBeenCalledTimes(1)
  search.stop()
})

it('lists the search key under anywhere', () => {
  renderAt('/page')
  fireEvent.keyDown(document.body, { key: '?', shift: true, shiftKey: true })
  expect(screen.getByText('Go to a page, or search everything')).toBeTruthy()
})

import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { useMemo } from 'react'
import { afterEach, expect, it, vi } from 'vitest'
import { MemoryRouter, Route, Routes } from 'react-router-dom'

import { TranslationProvider } from '../i18n/i18n'
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

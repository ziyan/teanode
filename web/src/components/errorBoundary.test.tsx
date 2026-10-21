import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { Link, MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, expect, it, vi } from 'vitest'

import { TranslationProvider } from '../i18n/i18n'
import { RouteErrorBoundary } from './errorBoundary'

afterEach(cleanup)

function Fails(): never {
  throw new Error('invented failure with a decrypted value')
}

it('shows a message with a reload button instead of a blank page, without the error text', () => {
  const errors = vi.spyOn(console, 'error').mockImplementation(() => undefined)
  try {
    render(
      <TranslationProvider>
        <MemoryRouter>
          <RouteErrorBoundary>
            <Fails />
          </RouteErrorBoundary>
        </MemoryRouter>
      </TranslationProvider>,
    )
    expect(screen.getByRole('alert').textContent).toContain('Something went wrong on this page')
    expect(screen.getByRole('button', { name: 'Reload page' })).toBeTruthy()
    expect(document.body.textContent).not.toContain('decrypted value')
    // What went wrong is in the console for whoever debugs it.
    expect(errors.mock.calls.some((call) => call.includes('A page failed to draw'))).toBe(true)
  } finally {
    errors.mockRestore()
  }
})

it('draws the next page when the address changes after a failure, and leaves the rest of the app alone', () => {
  const errors = vi.spyOn(console, 'error').mockImplementation(() => undefined)
  try {
    render(
      <TranslationProvider>
        <MemoryRouter initialEntries={['/broken']}>
          <nav>
            <Link to="/fine">Go to the fine page</Link>
          </nav>
          <RouteErrorBoundary>
            <Routes>
              <Route path="/broken" element={<Fails />} />
              <Route path="/fine" element={<p>The fine page</p>} />
            </Routes>
          </RouteErrorBoundary>
        </MemoryRouter>
      </TranslationProvider>,
    )
    expect(screen.getByRole('alert')).toBeTruthy()
    fireEvent.click(screen.getByRole('link', { name: 'Go to the fine page' }))
    expect(screen.getByText('The fine page')).toBeTruthy()
    expect(screen.queryByRole('alert')).toBeNull()
  } finally {
    errors.mockRestore()
  }
})

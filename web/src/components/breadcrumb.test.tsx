import { cleanup, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { TranslationProvider } from '../i18n/i18n'
import { Breadcrumb, BreadcrumbProvider, PageHeading, useBreadcrumbDetail, useBreadcrumbNote } from './breadcrumb'

beforeEach(() => {
  // A wide window.
  vi.stubGlobal('matchMedia', () => ({ matches: true, addEventListener() {}, removeEventListener() {} }))
})
afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

function Page({ note }: { note: string | null }) {
  useBreadcrumbDetail('Sample Reader')
  useBreadcrumbNote(note)
  return null
}

const show = (note: string | null) =>
  render(
    <TranslationProvider>
      <MemoryRouter initialEntries={['/photos/people/group-1']}>
        <BreadcrumbProvider>
          <PageHeading />
          <Breadcrumb current />
          <Page note={note} />
        </BreadcrumbProvider>
      </MemoryRouter>
    </TranslationProvider>,
  )

it('puts a note after the name in the heading and on the phone bar, muted, and not in the tab title', () => {
  show('You')
  const heading = screen.getByRole('heading', { level: 1 })
  expect(heading.textContent).toBe('Sample Reader You')
  expect(heading.querySelector('.muted')?.textContent).toBe(' You')
  expect(document.querySelector('.crumb-note')?.textContent).toBe('You')
  expect(document.title).not.toContain('You')
})

it('has no note unless the page says one', () => {
  show(null)
  expect(screen.getByRole('heading', { level: 1 }).textContent).toBe('Sample Reader')
  expect(document.querySelector('.crumb-note')).toBeNull()
})

import { cleanup, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { Breadcrumb, BreadcrumbProvider, PageHeading, useBreadcrumbDetail } from '../../components/breadcrumb'
import { TranslationProvider } from '../../i18n/i18n'

// Where a page of the journal says you are: the timeline, then the larger
// period the page belongs to, then the page itself.

beforeEach(() => {
  vi.stubGlobal('matchMedia', () => ({ matches: true, addEventListener() {}, removeEventListener() {} }))
})
afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

function Page({ detail, parent }: { detail: string | null; parent: string | null }) {
  useBreadcrumbDetail(detail, parent)
  return null
}

const show = (address: string, detail: string | null, parent: string | null) =>
  render(
    <TranslationProvider>
      <MemoryRouter initialEntries={[address]}>
        <BreadcrumbProvider>
          <Breadcrumb />
          <PageHeading />
          <Page detail={detail} parent={parent} />
        </BreadcrumbProvider>
      </MemoryRouter>
    </TranslationProvider>,
  )

const crumbs = () =>
  [...document.querySelectorAll('.breadcrumb .crumb')].map((crumb) => ({
    text: crumb.textContent,
    href: crumb.querySelector('a')?.getAttribute('href') ?? null,
  }))

it('puts a day under its week, which is under the journal', () => {
  show('/journal/2026-10-22', 'Thursday, October 22', 'Week of October 19')
  expect(crumbs()).toEqual([
    { text: 'Journal', href: '/journal' },
    { text: 'Week of October 19', href: '/journal/week/2026-10-22' },
  ])
  expect(screen.getByRole('heading', { level: 1 }).textContent).toBe('Thursday, October 22')
})

it('puts a week under its month, and a month under its year', () => {
  show('/journal/week/2026-10-19', 'Week of October 19', 'October 2026')
  expect(crumbs()[1]).toEqual({ text: 'October 2026', href: '/journal/month/2026-10' })
  cleanup()
  show('/journal/month/2026-10', 'October 2026', '2026')
  expect(crumbs()[1]).toEqual({ text: '2026', href: '/journal/year/2026' })
})

it('has nothing above a year but the journal', () => {
  show('/journal/year/2026', '2026', null)
  expect(crumbs()).toEqual([{ text: 'Journal', href: '/journal' }])
  expect(screen.getByRole('heading', { level: 1 }).textContent).toBe('2026')
})

it('leaves the heading blank until the page has its name', () => {
  show('/journal/2026-10-22', null, null)
  expect(screen.getByRole('heading', { level: 1 }).textContent).toBe(' ')
})

it('is just the journal on the timeline', () => {
  show('/journal', null, null)
  expect(screen.getByRole('heading', { level: 1 }).textContent).toBe('Journal')
})

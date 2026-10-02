import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, useLocation } from 'react-router-dom'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { graphql } from '../../api'
import { AuditTab } from './audit'

vi.mock('../../api', () => ({ graphql: vi.fn() }))
vi.mock('../../i18n/i18n', () => ({
  useTranslation: () => ({
    t: (key: string, values?: Record<string, unknown>) => (values ? `${key} ${JSON.stringify(values)}` : key),
    plural: (count: number, forms: { one: string; other: string }) =>
      `${count === 1 ? forms.one : forms.other} ${count}`,
    language: 'en',
  }),
}))
const execute = vi.mocked(graphql)

// An invented change to an invented domain, its label saying where it is.
function event(index: number) {
  return {
    id: `event-${index}`,
    createdAt: '2026-09-15T12:00:00Z',
    actorKind: 'user',
    actorLabel: 'operator',
    resourceType: 'domain',
    resourceId: `domain-${index}`,
    resourceLabel: `invented-${index}.example.com`,
    action: 'update',
  }
}

function Address() {
  return <output data-testid="address">{useLocation().search}</output>
}
const address = () => new URLSearchParams(screen.getByTestId('address').textContent ?? '')

const eventReads = () => execute.mock.calls.map(([, variables]) => [variables?.offset, variables?.resourceType])

beforeEach(() => {
  window.matchMedia = ((query: string) => ({
    matches: true,
    media: query,
    addEventListener: () => {},
    removeEventListener: () => {},
  })) as unknown as typeof window.matchMedia
  execute.mockImplementation(async (_document: string, variables?: Record<string, unknown>) => {
    const offset = Number(variables?.offset ?? 0)
    const total = variables?.resourceType ? 3 : 75
    const count = Math.max(0, Math.min(50, total - offset))
    return { ListAuditEvents: { total, events: Array.from({ length: count }, (_, index) => event(offset + index)) } }
  })
})
afterEach(() => {
  cleanup()
  execute.mockReset()
})

// The log pages with the page in the address, and narrowing it to one
// kind of thing starts again at the first page.
it('pages the audit log and starts a narrowed log at the first page', async () => {
  render(
    <MemoryRouter>
      <AuditTab />
      <Address />
    </MemoryRouter>,
  )
  expect(await screen.findByText('invented-0.example.com')).toBeTruthy()
  expect(screen.getByText('table.range {"first":"1","last":"50","total":"75"}')).toBeTruthy()

  fireEvent.click(screen.getByRole('button', { name: 'table.next' }))
  expect(await screen.findByText('invented-50.example.com')).toBeTruthy()
  expect(screen.queryByText('invented-0.example.com')).toBeNull()
  expect(address().get('page')).toBe('2')

  fireEvent.click(screen.getByRole('combobox'))
  fireEvent.click(screen.getByRole('option', { name: 'domain' }))
  await waitFor(() => expect(address().get('resource')).toBe('domain'))
  expect(address().get('page')).toBeNull()
  expect(await screen.findByText('table.range {"first":"1","last":"3","total":"3"}')).toBeTruthy()
  expect(eventReads()).toEqual([
    [0, null],
    [50, null],
    [0, 'domain'],
  ])
})

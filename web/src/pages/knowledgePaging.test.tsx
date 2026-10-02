import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { graphql } from '../api'
import { KnowledgePage } from './knowledge'

vi.hoisted(() => {
  // Read when the modules load, before any test runs.
  window.matchMedia = ((query: string) => ({
    matches: false,
    media: query,
    addEventListener: () => {},
    removeEventListener: () => {},
    addListener: () => {},
    removeListener: () => {},
  })) as unknown as typeof window.matchMedia
  window.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  } as unknown as typeof window.ResizeObserver
})
vi.mock('../api', () => ({ graphql: vi.fn(), askAgentAbout: vi.fn(), setAgentViewing: vi.fn() }))
vi.mock('../session', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../session')>()),
  useSession: () => ({ name: 'reader', permissions: null }),
}))
vi.mock('./knowledgeExplore', () => ({ KnowledgeGraph: () => null }))
vi.mock('../i18n/i18n', () => ({
  useTranslation: () => ({
    t: (key: string, values?: Record<string, unknown>) => (values ? `${key} ${JSON.stringify(values)}` : key),
    plural: (count: number, forms: { one: string; other: string }) =>
      `${count === 1 ? forms.one : forms.other} ${count}`,
    language: 'en',
  }),
}))
vi.mock('../components/toast', () => ({ useToast: () => ({ done: vi.fn(), failed: vi.fn(), failure: vi.fn() }) }))
const execute = vi.mocked(graphql)

const node = (path: string, kind: string) => ({
  id: `node-${path}`,
  path,
  kind,
  name: `Invented ${path.split('/').pop()}`,
  summary: '',
})

// An invented folder of seventy projects, and a project with sixty facts.
function page(path: string) {
  if (path === 'projects') {
    return { node: node('projects', 'folder'), facts: [], folded: [], children: [], attachments: [], contact: null }
  }
  return {
    node: node(path, 'page'),
    facts: Array.from({ length: 60 }, (_, index) => ({
      id: `fact-${index + 1}`,
      number: index + 1,
      kind: 'fact',
      text: `Invented fact ${index + 1}`,
      evidence: [],
      audiences: [],
      createdAt: '2026-09-15T12:00:00Z',
    })),
    folded: [],
    children: [],
    attachments: [],
    contact: null,
  }
}

function Address() {
  const location = useLocation()
  return <output data-testid="address">{location.pathname + location.search}</output>
}
const address = () => screen.getByTestId('address').textContent ?? ''

const childrenReads = () =>
  execute.mock.calls
    .filter(([document]) => document.includes('AgentGraphChildren('))
    .map(([, variables]) => [variables?.path, variables?.offset])

beforeEach(() => {
  execute.mockImplementation(async (document: string, variables?: Record<string, unknown>) => {
    if (document.includes('AgentGraphChildren(')) {
      const offset = Number(variables?.offset ?? 0)
      const count = Math.max(0, Math.min(50, 70 - offset))
      return {
        AgentGraphChildren: {
          rows: Array.from({ length: count }, (_, index) => ({
            node: node(`projects/project-${String(offset + index).padStart(2, '0')}`, 'page'),
            hint: '',
            children: 0,
          })),
          total: 70,
        },
      }
    }
    if (document.includes('AgentGraphPage(')) return { AgentGraphPage: page(String(variables?.path)) }
    return {}
  })
})
afterEach(() => {
  cleanup()
  execute.mockReset()
})

function renderAt(entry: string) {
  render(
    <MemoryRouter initialEntries={[entry]}>
      <Routes>
        <Route path="/knowledge/*" element={<KnowledgePage />} />
      </Routes>
      <Address />
    </MemoryRouter>,
  )
}

// A folder pages with the page in the address, previous and next between
// its pages.
it('pages a folder with the page in the address', async () => {
  renderAt('/knowledge/projects')
  expect(await screen.findByText('Invented project-00')).toBeTruthy()
  expect(await screen.findByText('table.range {"first":"1","last":"50","total":"70"}')).toBeTruthy()

  fireEvent.click(screen.getByRole('button', { name: 'table.next' }))
  expect(await screen.findByText('Invented project-50')).toBeTruthy()
  expect(screen.queryByText('Invented project-00')).toBeNull()
  expect(address()).toBe('/knowledge/projects?page=2')

  // The buttons wait while a page is on its way.
  await waitFor(() =>
    expect((screen.getByRole('button', { name: 'table.previous' }) as HTMLButtonElement).disabled).toBe(false),
  )
  expect((screen.getByRole('button', { name: 'table.next' }) as HTMLButtonElement).disabled).toBe(true)
  fireEvent.click(screen.getByRole('button', { name: 'table.previous' }))
  expect(await screen.findByText('Invented project-00')).toBeTruthy()
  expect(address()).toBe('/knowledge/projects')
  await waitFor(() =>
    expect(childrenReads()).toEqual([
      ['projects', 0],
      ['projects', 50],
      ['projects', 0],
    ]),
  )
})

// A page's facts page too, under a name of their own in the address.
it('pages the facts of a page', async () => {
  renderAt('/knowledge/projects/project-07')
  expect(await screen.findByText('#1 Invented fact 1')).toBeTruthy()
  expect(screen.queryByText('#51 Invented fact 51')).toBeNull()
  expect(screen.getByText('table.range {"first":"1","last":"50","total":"60"}')).toBeTruthy()

  fireEvent.click(screen.getByRole('button', { name: 'table.next' }))
  expect(await screen.findByText('#51 Invented fact 51')).toBeTruthy()
  expect(screen.queryByText('#1 Invented fact 1')).toBeNull()
  expect(address()).toBe('/knowledge/projects/project-07?facts=2')
})

import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { graphql } from '../api'
import { ToastProvider } from '../components/toast'
import { TranslationProvider } from '../i18n/i18n'
import { KnowledgeGraph } from './knowledgeExplore'

vi.mock('../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../api')>()),
  graphql: vi.fn(),
}))
vi.mock('../session', () => ({ useSession: () => ({ name: 'Fixture Owner' }) }))

beforeEach(() => {
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      disconnect() {}
      unobserve() {}
    },
  )
  vi.mocked(graphql).mockReset()
})
afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const page = (path: string, name: string, kind = 'person') => ({ id: path, path, kind, name, summary: '' })

const recordLink = (overrides: Record<string, unknown>) => ({
  id: 'link',
  nodePath: 'people/fixture-guest',
  nodeName: 'Fixture Guest',
  nodeKind: 'person',
  recordKind: 'mail',
  recordId: 'item-1',
  recordRelation: 'wrote',
  linkStatus: 'stated',
  passageStartMS: 0,
  passageEndMS: 0,
  transcriptRunId: '',
  note: '',
  createdAt: '2026-01-01T00:00:00Z',
  recordTitle: 'Fixture subject',
  recordHappenedAt: '2026-03-04T10:00:00Z',
  dashboardPath: '/mailbox/starred/item-1',
  isUnavailable: false,
  ...overrides,
})

function show() {
  vi.mocked(graphql).mockImplementation(async (document: string) => {
    if (document.includes('AgentGraphNeighbours'))
      return {
        AgentGraphNeighbours: {
          node: page('people/fixture-guest', 'Fixture Guest'),
          parent: null,
          neighbours: [],
          children: 0,
          links: 0,
        },
      } as never
    if (document.includes('ListAgentRecordLinks'))
      return {
        ListAgentRecordLinks: {
          recordLinks: [
            recordLink({}),
            recordLink({
              id: 'guess',
              recordKind: 'calendar_event',
              recordId: 'event-1',
              recordRelation: 'attended',
              linkStatus: 'proposed',
              recordTitle: 'Fixture planning',
              dashboardPath: '/calendar?event=event-1',
            }),
            recordLink({
              id: 'gone',
              recordKind: 'finance_receipt',
              recordId: 'receipt-1',
              recordRelation: 'receipt_for',
              recordTitle: '',
              isUnavailable: true,
              dashboardPath: '',
            }),
          ],
          recordLinkCount: 14,
          nextOffset: 0,
        },
      } as never
    if (document.includes('AgentGraphPage')) return { AgentGraphPage: { facts: [] } } as never
    return {} as never
  })
  render(
    <TranslationProvider>
      <ToastProvider>
        <MemoryRouter>
          <KnowledgeGraph from="people/fixture-guest" />
        </MemoryRouter>
      </ToastProvider>
    </TranslationProvider>,
  )
}

it('draws the records a page is linked to as squares with their kind, apart from the page, the guess dashed', async () => {
  show()
  await waitFor(() => expect(document.querySelectorAll('.graph-explore-record').length).toBe(3))
  // The page is a circle; the records are squares, with the kind's own mark.
  expect(document.querySelectorAll('.graph-explore-node:not(.graph-explore-record) > circle').length).toBe(1)
  for (const square of document.querySelectorAll('.graph-explore-record')) {
    expect(square.querySelector('rect')).toBeTruthy()
    expect(square.querySelector('.graph-explore-record-icon svg')).toBeTruthy()
  }
  const titles = [...document.querySelectorAll('.graph-explore-record title')].map((each) => each.textContent)
  expect(titles).toEqual([
    expect.stringContaining('Mail · Fixture subject'),
    expect.stringContaining('Calendar event · Fixture planning'),
    expect.stringContaining('Receipt · No longer available'),
  ])
  // Lines name the relation in the person's words; the guess is marked.
  const relations = [...document.querySelectorAll('.graph-explore-relation')].map((each) => each.textContent)
  expect(relations).toEqual(['Wrote', 'Attended (proposed)', 'Receipt for'])
  expect(document.querySelectorAll('.graph-explore-edge.proposed').length).toBe(1)
  // The page says how many records were left out of the drawing.
  const pageTitle = document.querySelector('.graph-explore-node:not(.graph-explore-record) title')?.textContent ?? ''
  expect(pageTitle).toContain('11')
  const asked = vi.mocked(graphql).mock.calls.find(([document]) => document.includes('ListAgentRecordLinks'))
  expect(asked?.[1]).toEqual({ path: 'people/fixture-guest', first: 10 })
})

it('opens a record where it lives from the square, and never asks the graph for its neighbours', async () => {
  show()
  await waitFor(() => expect(document.querySelectorAll('.graph-explore-record').length).toBe(3))
  const square = document.querySelector('[data-path="record|mail|item-1|0-0"]') as SVGGElement
  expect(square).toBeTruthy()
  const canvas = document.querySelector('svg.graph-explore-canvas') as SVGSVGElement
  canvas.setPointerCapture = vi.fn()
  canvas.hasPointerCapture = vi.fn(() => false)
  canvas.releasePointerCapture = vi.fn()
  // Pressed twice, which expands a page: a record has nothing to expand.
  for (let press = 0; press < 2; press++) {
    fireEvent.pointerDown(square, { pointerId: 1, clientX: 5, clientY: 5 })
    fireEvent.pointerUp(square, { pointerId: 1, clientX: 5, clientY: 5 })
  }
  const neighbourCalls = vi.mocked(graphql).mock.calls.filter(([document]) => document.includes('AgentGraphNeighbours'))
  expect(neighbourCalls).toHaveLength(1)
  expect(neighbourCalls[0][1]).toEqual({ path: 'people/fixture-guest' })
  expect(screen.queryByText('Open record')).toBeNull()
})

it('has its own entry in the legend', async () => {
  show()
  expect(await screen.findByText('Your records')).toBeTruthy()
})

import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { graphql, openAgentConversation } from '../api'
import { AgentAdminPage } from './agentAdmin'
import { DEFAULT_OVERSIGHT_RANGE, rangeOf, sinceOf } from './agentOversight'

vi.mock('../api', () => ({ graphql: vi.fn(), openAgentConversation: vi.fn(() => true) }))
vi.mock('../i18n/i18n', () => ({
  useTranslation: () => ({
    t: (key: string, values?: Record<string, unknown>) => (values ? `${key} ${JSON.stringify(values)}` : key),
    plural: (count: number) => String(count),
    language: 'en',
  }),
}))
vi.mock('../components/toast', () => ({ useToast: () => ({ done: vi.fn(), failed: vi.fn(), failure: vi.fn() }) }))
vi.mock('../components/usageChart', () => ({ UsageChart: () => null }))
vi.mock('./settings/integrations', () => ({ IntegrationsSection: () => null }))
vi.mock('../session', () => ({
  useSession: () => ({ permissions: ['agent:observe'] }),
  hasPermission: (permissions: string[], permission: string) => permissions.includes(permission),
}))
const execute = vi.mocked(graphql)

// Invented people and conversations.
function run(index: number, isRunning: boolean) {
  return {
    id: `conversation-${index}`,
    agentId: 'agent-one',
    kind: index % 2 === 0 ? 'main' : 'named',
    title: `Planning a trip ${index}`,
    jobKind: '',
    lastAt: '2030-05-01T12:00:00Z',
    isRunning,
    ownerUsername: 'sample-owner',
    ownerName: 'Sample Owner',
    usage: { promptTokens: 0, cacheReadTokens: 0, completionTokens: 0, cost: 0 },
  }
}

function Address() {
  return <output data-testid="address">{useLocation().search}</output>
}
const address = () => new URLSearchParams(screen.getByTestId('address').textContent ?? '')

const runReads = () =>
  execute.mock.calls
    .filter(([document]) => String(document).includes('ListAllAgentRuns'))
    .map(([, variables]) => variables as Record<string, unknown>)

beforeEach(() => {
  window.matchMedia = ((query: string) => ({
    matches: true,
    media: query,
    addEventListener: () => {},
    removeEventListener: () => {},
  })) as unknown as typeof window.matchMedia
  execute.mockImplementation(async (document: string, variables?: Record<string, unknown>) => {
    if (document.includes('ListFailedAgentTurns')) {
      return {
        ListFailedAgentTurns: {
          totalCount: 1,
          failedTurns: [
            {
              conversationId: 'conversation-9',
              conversationKind: 'main',
              conversationTitle: 'Main talk',
              ownerUsername: 'sample-owner',
              ownerName: 'Sample Owner',
              failedAt: '2030-05-01T12:30:00Z',
              errorText: 'the provider could not be reached',
              agentId: 'agent-one',
            },
          ],
        },
      }
    }
    if (document.includes('ListAllAgentRuns')) {
      const isRunningOnly = variables?.isRunningOnly === true
      const offset = Number(variables?.offset ?? 0)
      const total = isRunningOnly ? 1 : 60
      const count = Math.max(0, Math.min(Number(variables?.first ?? 25), total - offset))
      return {
        ListAllAgentRuns: {
          totalCount: total,
          runs: Array.from({ length: count }, (_, index) => run(offset + index, isRunningOnly)),
        },
      }
    }
    return { ListAgents: [], AgentServerUsage: [], ListAgentDeadLetters: [] }
  })
})
afterEach(() => {
  cleanup()
  execute.mockReset()
})

function renderOversight(address = '/manage/agent/oversight') {
  render(
    <MemoryRouter initialEntries={[address]}>
      <Routes>
        <Route
          path="/manage/agent/:tab"
          element={
            <>
              <AgentAdminPage />
              <Address />
            </>
          }
        />
      </Routes>
    </MemoryRouter>,
  )
}

it('reads the range and kind from the address and says so to the server', async () => {
  renderOversight('/manage/agent/oversight?range=24h&kind=named')
  await screen.findByText('agentOversight.failed')
  const reads = runReads()
  expect(reads.length).toBeGreaterThan(0)
  const recent = reads.find((variables) => variables.isRunningOnly === false)!
  expect(recent.kinds).toEqual(['named'])
  const sinceAge = Date.now() - new Date(String(recent.since)).getTime()
  expect(Math.abs(sinceAge - 24 * 60 * 60 * 1000)).toBeLessThan(60 * 1000)
  // What is running now is not cut off by the range.
  const running = reads.find((variables) => variables.isRunningOnly === true)!
  expect(running.since).toBeNull()
  expect(running.kinds).toEqual(['named'])
})

it('shows the three sections, a running tag, and the failed turn with its error', async () => {
  renderOversight()
  expect(await screen.findByText('agentOversight.running')).toBeTruthy()
  expect(screen.getByText('agentOversight.recent')).toBeTruthy()
  expect(screen.getByText('agentOversight.failed')).toBeTruthy()
  expect((await screen.findAllByText('agentOversight.isRunning')).length).toBeGreaterThan(0)
  expect(await screen.findByText(/the provider could not be reached/)).toBeTruthy()
})

it('pages with the page in the address, not by loading more', async () => {
  renderOversight()
  await screen.findAllByText('Planning a trip 0')
  expect(screen.queryByText('Planning a trip 25')).toBeNull()
  // Only the Recent list has a page after this one.
  const next = (await screen.findAllByLabelText('table.next')).find(
    (button) => !(button.closest('button') ?? button).hasAttribute('disabled'),
  )!
  fireEvent.click(next.closest('button') ?? next)
  await waitFor(() => expect(address().get('page')).toBe('2'))
  await screen.findByText('Planning a trip 25')
  // The rows of the first page are gone: a page, not more under it.
  expect(screen.queryByText('Planning a trip 3')).toBeNull()
  expect(runReads().some((variables) => variables.offset === 25)).toBe(true)
})

it('opens a conversation with a button, not a link', async () => {
  renderOversight()
  const buttons = await screen.findAllByRole('button', { name: 'agent.open' })
  fireEvent.click(buttons[0])
  expect(vi.mocked(openAgentConversation)).toHaveBeenCalled()
  expect(screen.queryAllByRole('link', { name: 'agent.open' })).toHaveLength(0)
})

it('sends a link to the old Runs tab to Oversight', async () => {
  renderOversight('/manage/agent/runs')
  await waitFor(() => expect(screen.getByTestId('address')).toBeTruthy())
  expect(await screen.findByText('agentOversight.recent')).toBeTruthy()
})

it('takes a range it does not know as the default', () => {
  expect(rangeOf('24h')).toBe('24h')
  expect(rangeOf('30d')).toBe('30d')
  expect(rangeOf('1y')).toBe(DEFAULT_OVERSIGHT_RANGE)
  expect(rangeOf(null)).toBe(DEFAULT_OVERSIGHT_RANGE)
  const now = Date.parse('2030-05-08T00:00:00Z')
  expect(sinceOf('7d', now)).toBe('2030-05-01T00:00:00.000Z')
})

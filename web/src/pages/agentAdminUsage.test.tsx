import { cleanup, render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, expect, it, vi } from 'vitest'

import { graphql } from '../api'
import { AgentAdminPage } from './agentAdmin'

vi.mock('../api', () => ({ graphql: vi.fn(), openAgentConversation: vi.fn(() => true) }))
vi.mock('../i18n/i18n', () => ({
  useTranslation: () => ({ t: (key: string) => key, plural: (count: number) => String(count), language: 'en' }),
}))
vi.mock('../components/toast', () => ({ useToast: () => ({ done: vi.fn(), failed: vi.fn(), failure: vi.fn() }) }))
vi.mock('../components/usageChart', () => ({ UsageChart: () => null }))
vi.mock('./settings/integrations', () => ({ IntegrationsSection: () => null }))
vi.mock('../session', () => ({
  useSession: () => ({ permissions: ['agent:audit'] }),
  hasPermission: (permissions: string[], permission: string) => permissions.includes(permission),
}))
const execute = vi.mocked(graphql)
afterEach(cleanup)

const totals = {
  promptTokens: 10,
  completionTokens: 5,
  cacheReadTokens: 0,
  cacheWriteTokens: 0,
  audioInputTokens: 0,
  calls: 2,
}

function renderUsage(rows: object[]) {
  execute.mockResolvedValue({ ListAgents: [], AgentServerUsage: rows, ListAgentDeadLetters: [] })
  render(
    <MemoryRouter initialEntries={['/manage/agent/usage']}>
      <Routes>
        <Route path="/manage/agent/:tab" element={<AgentAdminPage />} />
      </Routes>
    </MemoryRouter>,
  )
}

it('shows what the provider billed beside the estimate when a bill was read', async () => {
  renderUsage([
    { key: '2030-05-01', cost: 1.5, currency: 'USD', billedAmount: 1.75, billedCurrencyCode: 'usd', totals },
    { key: '2030-05-02', cost: 0.5, currency: 'USD', billedAmount: null, billedCurrencyCode: '', totals },
  ])
  expect(await screen.findByText('agentAdmin.billed')).toBeTruthy()
  expect(screen.getByText('agentAdmin.estimated')).toBeTruthy()
  expect(screen.getByText('$1.75')).toBeTruthy()
})

it('leaves the table as it was when no bill was read', async () => {
  renderUsage([{ key: '2030-05-01', cost: 1.5, currency: 'USD', billedAmount: null, billedCurrencyCode: '', totals }])
  expect(await screen.findByText('agentAdmin.cost')).toBeTruthy()
  expect(screen.queryByText('agentAdmin.billed')).toBeNull()
})

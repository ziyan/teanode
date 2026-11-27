import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, expect, it, vi } from 'vitest'
import { graphql } from '../../api'
import { ToastProvider } from '../../components/toast'
import { TranslationProvider } from '../../i18n/i18n'
import { AGENT_SELECTION, Agent, LimitsForm } from './agentSettings'

vi.mock('../../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../api')>()),
  graphql: vi.fn(),
}))
afterEach(() => {
  cleanup()
  vi.resetAllMocks()
})

const removed = [
  'maxRoundsPerAsk',
  'maxRoundsPerResearch',
  'maxRoundsPerReply',
  'maxRoundsPerDream',
  'maxRoundsPerSubagent',
  'maxToolCallsPerRun',
  'dreamShare',
  'embeddingTokensPerDay',
  'goalShare',
  'goalTurnsAlone',
  'backgroundWakesAlone',
  'maxBodyCharacters',
  'maxRecordingMediaBytes',
  'maxRecordingAccountBytes',
  'requestTimeout',
  'concurrency',
  'scanConcurrency',
  'rewriteConcurrency',
  'ingestChunksPerRun',
]

const settings = {
  limits: {
    dailyTokensPerAgent: 0,
    dailyCostPerAgent: 30,
    monthlyCostPerServer: 500,
    monthlyTokensPerServer: 0,
  },
  retention: { runs: '30d', corrections: '90d' },
  currency: 'USD',
} as unknown as Agent

it('does not ask for the removed limits', () => {
  for (const name of removed) expect(AGENT_SELECTION).not.toContain(name)
})

it('does not send the removed limits when saved', async () => {
  vi.mocked(graphql).mockResolvedValue({} as never)
  const { container } = render(
    <MemoryRouter>
      <TranslationProvider>
        <ToastProvider>
          <LimitsForm settings={settings} onSaved={() => undefined} />
        </ToastProvider>
      </TranslationProvider>
    </MemoryRouter>,
  )
  fireEvent.submit(container.querySelector('form') as HTMLFormElement)
  await waitFor(() =>
    expect(vi.mocked(graphql).mock.calls.some((entry) => JSON.stringify(entry).includes('dailyCostPerAgent'))).toBe(
      true,
    ),
  )
  const call = vi.mocked(graphql).mock.calls.find((entry) => JSON.stringify(entry).includes('dailyCostPerAgent'))
  const sent = JSON.stringify(call)
  for (const name of removed) expect(sent).not.toContain(name)
  expect(sent).toContain('dailyCostPerAgent')
  expect(sent).toContain('monthlyCostPerServer')
  expect(screen.queryByText(/Share of the day/)).toBeNull()
})

it('shows only the token and money budget', () => {
  const { container } = render(
    <MemoryRouter>
      <TranslationProvider>
        <ToastProvider>
          <LimitsForm settings={settings} onSaved={() => undefined} />
        </ToastProvider>
      </TranslationProvider>
    </MemoryRouter>,
  )
  expect(container.querySelectorAll('input[inputmode="numeric"]')).toHaveLength(4)
  expect(screen.queryByText(/Timeout per call/)).toBeNull()
})

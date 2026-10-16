import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, expect, it, vi } from 'vitest'
import { graphql } from '../../api'
import { ToastProvider } from '../../components/toast'
import { TranslationProvider } from '../../i18n/i18n'
import { Agent, ProvidersSection } from './agentSettings'

vi.mock('../../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../api')>()),
  graphql: vi.fn(),
}))
afterEach(() => {
  cleanup()
  vi.resetAllMocks()
})

const provider = {
  name: 'fixture-provider',
  kind: 'openai',
  baseUrl: '',
  hasApiKey: true,
  hasBillingApiKey: true,
  hasRefreshToken: false,
  account: '',
  enabled: true,
  allow: [],
  deny: [],
  pricingInput: 0,
  pricingOutput: 0,
  pricingCacheRead: 0,
  pricingCacheWrite: 0,
  pricingAudioInput: 0,
  modelPricing: [],
}

const checked = {
  providerName: 'fixture-provider',
  checkedAt: new Date().toISOString(),
  errorMessage: '',
  compared: [
    {
      model: 'fixture-model',
      tokenKind: 'cacheWrite',
      configuredPerMillion: 0,
      billedPerMillion: 0.25,
      dayCount: 6,
      isPattern: false,
    },
    {
      model: 'fixture-*',
      tokenKind: 'output',
      configuredPerMillion: 1,
      billedPerMillion: 1.2,
      dayCount: 5,
      isPattern: true,
    },
  ],
  changed: [{ model: 'fixture-model', tokenKind: 'cacheWrite', beforePerMillion: 0, afterPerMillion: 0.25 }],
}

function renderProviders(onSaved = vi.fn()) {
  render(
    <MemoryRouter>
      <TranslationProvider>
        <ToastProvider>
          <ProvidersSection
            settings={{ providers: [provider], kinds: ['openai'] } as unknown as Agent}
            onSaved={onSaved}
            onModels={() => undefined}
          />
        </ToastProvider>
      </TranslationProvider>
    </MemoryRouter>,
  )
  return onSaved
}

it('says when the prices were last checked against the bill, or that they never were', async () => {
  vi.mocked(graphql).mockResolvedValue({ AgentBillingChecks: [checked] } as never)
  renderProviders()
  expect(await screen.findByText(/Prices checked against the bill .*: 1 changed/)).toBeTruthy()
})

it('says so when a provider with a billing key has never been checked', async () => {
  vi.mocked(graphql).mockResolvedValue({
    AgentBillingChecks: [{ ...checked, checkedAt: '0001-01-01T00:00:00Z', compared: [], changed: [] }],
  } as never)
  renderProviders()
  expect(await screen.findByText('Prices not checked against the bill yet')).toBeTruthy()
})

it('runs the check on the button, reads the providers again for the changed price, and shows what was compared', async () => {
  vi.mocked(graphql).mockImplementation(async (document) => {
    if (document.includes('CheckAgentProviderPrices')) return { CheckAgentProviderPrices: [checked] } as never
    return {
      AgentBillingChecks: [{ ...checked, checkedAt: '0001-01-01T00:00:00Z', compared: [], changed: [] }],
    } as never
  })
  const onSaved = renderProviders()
  fireEvent.click(await screen.findByRole('button', { name: 'Check prices now' }))

  const dialog = await screen.findByRole('dialog', { name: 'Prices from the bill: fixture-provider' })
  expect(dialog.textContent).toContain('fixture-model')
  expect(dialog.textContent).toContain('Cache writes')
  expect(dialog.textContent).toContain('pattern, not changed')
  expect(dialog.textContent).toContain('fixture-model, Cache writes: not set → 0.25')
  expect(vi.mocked(graphql).mock.calls.some(([document]) => document.includes('mutation'))).toBe(true)
  // A changed price changed the provider list, so the rows are read again.
  expect(onSaved).toHaveBeenCalled()
})

it('shows a refused key in the danger tone, without a table', async () => {
  vi.mocked(graphql).mockResolvedValue({
    AgentBillingChecks: [{ ...checked, errorMessage: 'the billing key was refused', compared: [], changed: [] }],
  } as never)
  renderProviders()
  const message = await screen.findByText('the billing key was refused')
  expect(message.className).toBe('error')
})

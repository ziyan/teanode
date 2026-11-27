import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, expect, it, vi } from 'vitest'
import { graphql } from '../../api'
import { ToastProvider } from '../../components/toast'
import { TranslationProvider } from '../../i18n/i18n'
import { Agent, ModelsForm, ProvidersSection } from './agentSettings'
import { AgentModelEntry, modelOptions } from './providerModels'

vi.mock('../../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../api')>()),
  graphql: vi.fn(),
}))
afterEach(() => {
  cleanup()
  vi.resetAllMocks()
})

const noPricing = { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, audioInput: 0 }

const named: AgentModelEntry = {
  model: 'alpha-large-1',
  name: 'Alpha Large',
  contextWindow: 0,
  reportedContextWindow: 200000,
  pricing: { input: 2, output: 8, cacheRead: 0.5, cacheWrite: 2.5, audioInput: 0 },
  isEnabled: true,
  usedFor: ['default', 'embedding'],
}
const unnamedOff: AgentModelEntry = {
  model: 'alpha-small-2',
  name: '',
  contextWindow: 32000,
  reportedContextWindow: 0,
  pricing: noPricing,
  isEnabled: false,
  usedFor: [],
}

const provider = {
  name: 'fixture-provider',
  kind: 'openai',
  baseUrl: '',
  hasApiKey: true,
  hasBillingApiKey: false,
  hasRefreshToken: false,
  account: '',
  enabled: true,
  pricingInput: 0,
  pricingOutput: 0,
  pricingCacheRead: 0,
  pricingCacheWrite: 0,
  pricingAudioInput: 0,
  models: [named, unnamedOff],
}

function renderProviders(onSaved = vi.fn()) {
  render(
    <MemoryRouter>
      <TranslationProvider>
        <ToastProvider>
          <ProvidersSection
            settings={{ providers: [provider], kinds: ['openai'] } as unknown as Agent}
            onSaved={onSaved}
          />
        </ToastProvider>
      </TranslationProvider>
    </MemoryRouter>,
  )
  return onSaved
}

const offered = {
  ListAgentModels: [
    {
      provider: 'fixture-provider',
      model: 'alpha-large-1',
      name: 'fixture-provider:alpha-large-1',
      contextLength: 200000,
    },
    {
      provider: 'fixture-provider',
      model: 'alpha-medium-3',
      name: 'fixture-provider:alpha-medium-3',
      contextLength: 64000,
    },
    { provider: 'other-provider', model: 'beta-1', name: 'other-provider:beta-1', contextLength: 8000 },
  ],
}

function mutationCalls() {
  return vi.mocked(graphql).mock.calls.filter(([document]) => document.includes('mutation'))
}

it('lists the models under the provider, name over id, with a muted Off for a disabled one', () => {
  renderProviders()
  expect(screen.getByText('Alpha Large')).toBeTruthy()
  expect(screen.getByText('alpha-large-1')).toBeTruthy()
  // A model with no name reads as its id, and says it is off.
  const off = screen.getByText('Off')
  expect(off.className).toBe('muted')
  expect(screen.getAllByText('alpha-small-2').length).toBeGreaterThan(0)
  expect(screen.getByRole('button', { name: 'Add model' })).toBeTruthy()
})

it('adds a model from the provider’s own list, filtered to this provider and without ones already added', async () => {
  vi.mocked(graphql).mockImplementation(
    async (document) =>
      (document.includes('ListAgentModels') ? offered : { SaveAgentModelEntry: { model: 'alpha-medium-3' } }) as never,
  )
  const onSaved = renderProviders()
  fireEvent.click(screen.getByRole('button', { name: 'Add model' }))
  const dialog = await screen.findByRole('dialog', { name: 'Add a model to fixture-provider' })
  const choice = await within(dialog).findByText('alpha-medium-3')
  expect(within(dialog).queryByText('beta-1')).toBeNull()
  expect(within(dialog).queryByText('alpha-large-1')).toBeNull()
  fireEvent.click(choice)
  fireEvent.click(within(dialog).getByRole('button', { name: 'Add model' }))

  await waitFor(() => expect(onSaved).toHaveBeenCalled())
  const [, variables] = mutationCalls()[0]
  expect(variables).toEqual({
    providerName: 'fixture-provider',
    entry: { model: 'alpha-medium-3', name: '', contextWindow: 0, pricing: noPricing, isEnabled: true },
  })
})

it('adds a typed id the list does not have', async () => {
  vi.mocked(graphql).mockImplementation(
    async (document) =>
      (document.includes('ListAgentModels') ? offered : { SaveAgentModelEntry: { model: 'typed-id' } }) as never,
  )
  const onSaved = renderProviders()
  fireEvent.click(screen.getByRole('button', { name: 'Add model' }))
  const dialog = await screen.findByRole('dialog', { name: 'Add a model to fixture-provider' })
  await within(dialog).findByText('alpha-medium-3')
  fireEvent.change(within(dialog).getByRole('textbox'), { target: { value: ' typed-id ' } })
  // Nothing in the list matches what was typed.
  expect(within(dialog).queryByText('alpha-medium-3')).toBeNull()
  fireEvent.click(within(dialog).getByRole('button', { name: 'Add model' }))

  await waitFor(() => expect(onSaved).toHaveBeenCalled())
  expect(mutationCalls()[0][1]).toMatchObject({ providerName: 'fixture-provider', entry: { model: 'typed-id' } })
})

it('does not add an id the provider already has', async () => {
  vi.mocked(graphql).mockResolvedValue(offered as never)
  renderProviders()
  fireEvent.click(screen.getByRole('button', { name: 'Add model' }))
  const dialog = await screen.findByRole('dialog', { name: 'Add a model to fixture-provider' })
  fireEvent.change(within(dialog).getByRole('textbox'), { target: { value: 'alpha-large-1' } })
  expect(within(dialog).getByRole('button', { name: 'Add model' }).hasAttribute('disabled')).toBe(true)
})

it('shows a model’s settings and saves them with the entry the server takes', async () => {
  vi.mocked(graphql).mockResolvedValue({ SaveAgentModelEntry: { model: 'alpha-large-1' } } as never)
  const onSaved = renderProviders()
  fireEvent.click(screen.getByRole('button', { name: 'Alpha Large: Edit' }))
  const dialog = await screen.findByRole('dialog', { name: 'Model: Alpha Large' })

  // The id is read-only, the window shows the provider's figure, and the
  // roles that use the model are named.
  const id = within(dialog).getByDisplayValue('alpha-large-1') as HTMLInputElement
  expect(id.readOnly).toBe(true)
  expect(within(dialog).getByPlaceholderText('200000')).toBeTruthy()
  expect(within(dialog).getByText('Default')).toBeTruthy()
  expect(within(dialog).getByText('Embedding')).toBeTruthy()

  fireEvent.change(within(dialog).getByDisplayValue('Alpha Large'), { target: { value: 'Alpha Big' } })
  fireEvent.change(within(dialog).getByPlaceholderText('200000'), { target: { value: '150,000' } })
  fireEvent.change(within(dialog).getByDisplayValue('2'), { target: { value: '2,75' } })
  fireEvent.change(within(dialog).getByDisplayValue('0.5'), { target: { value: '' } })
  fireEvent.click(within(dialog).getByRole('checkbox', { name: 'On' }))
  fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }))

  await waitFor(() => expect(onSaved).toHaveBeenCalled())
  expect(mutationCalls()[0][1]).toEqual({
    providerName: 'fixture-provider',
    entry: {
      model: 'alpha-large-1',
      name: 'Alpha Big',
      contextWindow: 150000,
      pricing: { input: 2.75, output: 8, cacheRead: 0, cacheWrite: 2.5, audioInput: 0 },
      isEnabled: false,
    },
  })
})

it('says the window is not reported when the provider gives none', async () => {
  renderProviders()
  fireEvent.click(screen.getByRole('button', { name: 'alpha-small-2: Edit' }))
  const dialog = await screen.findByRole('dialog', { name: 'Model: alpha-small-2' })
  expect(within(dialog).getByPlaceholderText('not reported')).toBeTruthy()
  expect(within(dialog).getByText('No role uses this model.')).toBeTruthy()
})

it('removes a model after asking', async () => {
  vi.mocked(graphql).mockResolvedValue({ DeleteAgentModelEntry: true } as never)
  const onSaved = renderProviders()
  fireEvent.click(screen.getByRole('button', { name: 'alpha-small-2: Edit' }))
  const dialog = await screen.findByRole('dialog', { name: 'Model: alpha-small-2' })
  fireEvent.click(within(dialog).getByRole('button', { name: 'Remove' }))
  const confirm = await screen.findByRole('alertdialog')
  fireEvent.click(within(confirm).getByRole('button', { name: 'Remove' }))

  await waitFor(() => expect(onSaved).toHaveBeenCalled())
  expect(mutationCalls()[0][1]).toEqual({ providerName: 'fixture-provider', model: 'alpha-small-2' })
})

it('shows the server’s words in a toast when a remove is refused, and keeps the model', async () => {
  vi.mocked(graphql).mockRejectedValue(new Error('alpha-large-1 is used by default and embedding') as never)
  const onSaved = renderProviders()
  fireEvent.click(screen.getByRole('button', { name: 'Alpha Large: Edit' }))
  const dialog = await screen.findByRole('dialog', { name: 'Model: Alpha Large' })
  fireEvent.click(within(dialog).getByRole('button', { name: 'Remove' }))
  const confirm = await screen.findByRole('alertdialog')
  fireEvent.click(within(confirm).getByRole('button', { name: 'Remove' }))

  expect(await screen.findByText('alpha-large-1 is used by default and embedding')).toBeTruthy()
  expect(onSaved).not.toHaveBeenCalled()
  // The form is back, as it was.
  expect(await screen.findByRole('dialog', { name: 'Model: Alpha Large' })).toBeTruthy()
})

it('offers models to the role pickers as "Provider · Model", grouped by provider, storing provider:model', () => {
  const options = modelOptions([
    { name: 'fixture-provider', kind: 'openai', enabled: true, models: [named, unnamedOff] },
    { name: 'second-provider', kind: 'openai', enabled: true, models: [{ ...named, model: 'gamma-1', name: '' }] },
    { name: 'switched-off', kind: 'openai', enabled: false, models: [named] },
  ])
  // A model that is off, and every model of a provider that is off, are not offered.
  expect(options.map(({ value, label, group }) => ({ value, label, group }))).toEqual([
    { value: 'fixture-provider:alpha-large-1', label: 'fixture-provider · Alpha Large', group: 'fixture-provider' },
    { value: 'second-provider:gamma-1', label: 'second-provider · gamma-1', group: 'second-provider' },
  ])
})

it('shows the stored role as "Provider · Model" in the picker', async () => {
  vi.mocked(graphql).mockResolvedValue({ ListAgentModels: [] } as never)
  const models = {
    default: 'fixture-provider:alpha-large-1',
    fast: '',
    quick: '',
    embedding: '',
    transcription: '',
    choices: [],
  }
  render(
    <MemoryRouter>
      <TranslationProvider>
        <ToastProvider>
          <ModelsForm
            settings={{ providers: [provider], models } as unknown as Agent}
            onSaved={vi.fn()}
            knownModels={[]}
          />
        </ToastProvider>
      </TranslationProvider>
    </MemoryRouter>,
  )
  const picker = screen.getByRole('combobox', { name: 'Default' })
  expect(picker.textContent).toContain('fixture-provider · Alpha Large')
})

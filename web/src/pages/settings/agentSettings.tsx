import { useEffect, useMemo, useState } from 'react'

import { graphql } from '../../api'
import { SaveRow } from '../../components/common'
import { ConfirmDialog, FormDialog } from '../../components/dialog'
import { PencilIcon, TrashIcon } from '../../components/icons'
import { Select } from '../../components/select'
import { SettingsEmpty, SettingsRow, SettingsSection } from '../../components/settingsList'
import { Tag } from '../../components/common'
import { useToast } from '../../components/toast'
import { ToolPolicyAccordion } from '../../components/toolPolicy'
import { Tooltip } from '../../components/tooltip'
import { useTranslation } from '../../i18n/i18n'
import { UPDATE, useSaver } from './integrations'

// The agent's settings, as the operator sees them: which model services
// this server may call, which model does which work, what the deployment
// offers people at all, and the limits. Every secret comes back as "set"
// or not; a blank on save keeps what is stored, so a form can be saved
// without re-entering a key.
//
// One card per subject, each with its own Save, and the API takes a
// section at a time: changing the timeout should not mean re-sending the
// providers. Lists — providers, connected servers — are rows, edited one
// at a time in a dialog and saved as they are closed, the way tokens and
// mailbox rules are.

export type AgentProvider = {
  name: string
  kind: string
  baseUrl: string
  hasApiKey: boolean
  enabled: boolean
  allow: string[]
  deny: string[]
  pricingInput: number
  modelPricing: { model: string; input: number; output: number; cacheRead: number }[]
  pricingOutput: number
  pricingCacheRead: number
}

export type AgentMCPServer = {
  name: string
  transport: string
  effectiveTransport: string
  url: string
  command: string
  args: string[]
  envNames: string[]
  workingDir: string
  auth: string
  effectiveAuth: string
  hasAuthorization: boolean
  oauthClientId: string
  hasOauthClientSecret: boolean
  oauthScopes: string[]
  oauthAuthorizationUrl: string
  oauthTokenUrl: string
  headless: boolean
  readOnly: string[]
  disabled: string[]
  timeout: string
  enabled: boolean
}

export type AgentTool = { name: string; family: string; risk: string; description: string; confirms: boolean; core: boolean }

export type Agent = {
  enabled: boolean
  instructions: string
  providers: AgentProvider[]
  models: {
    default: string
    fast: string
    embedding: string
    triage: string
    research: string
    summarize: string
    reply: string
    ask: string
    schedule: string
    compact: string
    choices: string[]
  }
  features: Record<string, boolean>
  limits: {
    maxBodyCharacters: number
    dailyTokensPerAgent: number
    dailyCostPerAgent: number
    monthlyCostPerServer: number
    monthlyTokensPerServer: number
    maxRoundsPerAsk: number
    maxRoundsPerResearch: number
    maxRoundsPerReply: number
    maxToolCallsPerRun: number
    requestTimeout: string
    concurrency: number
  }
  retention: { runs: string; corrections: string }
  currency: string
  search: { kind: string; hasApiKey: boolean }
  tools: { disabled: string[]; confirm: string[]; catalog: AgentTool[] }
  browser: {
    enabled: boolean
    cdpEndpoint: string
    attachTabs: boolean
    allowPrivateAddresses: string[]
    idleTimeout: string
    maxContexts: number
  }
  mcpServers: AgentMCPServer[]
  works: string[]
  families: string[]
  kinds: string[]
}

export const AGENT_SELECTION = `agent {
  enabled instructions
  providers { name kind baseUrl hasApiKey enabled allow deny pricingInput pricingOutput pricingCacheRead modelPricing { model input output cacheRead } }
  models { default fast embedding triage research summarize reply ask schedule compact choices }
  features { triage summaries draftReplies search research autoReply ask schedules browser connectedServers computer chatApps }
  limits { maxBodyCharacters dailyTokensPerAgent monthlyTokensPerServer dailyCostPerAgent monthlyCostPerServer maxRoundsPerAsk maxRoundsPerResearch maxRoundsPerReply maxToolCallsPerRun requestTimeout concurrency }
  retention { runs corrections }
  currency
  search { kind hasApiKey }
  tools { disabled confirm catalog { name family risk description confirms core } }
  browser { enabled cdpEndpoint attachTabs allowPrivateAddresses idleTimeout maxContexts }
  mcpServers { name transport effectiveTransport url command args envNames workingDir auth effectiveAuth hasAuthorization oauthClientId hasOauthClientSecret oauthScopes oauthAuthorizationUrl oauthTokenUrl headless readOnly disabled timeout enabled }
  works families kinds
}`

const LIST_MODELS = `{ ListAgentModels { name contextLength } }`
const TEST_PROVIDER = `query ($name: String!) { TestAgentProvider(name: $name) { name contextLength } }`

const FEATURES = [
  'triage',
  'summaries',
  'draftReplies',
  'search',
  'research',
  'autoReply',
  'ask',
  'schedules',
  'browser',
  'connectedServers',
  'computer',
  'chatApps',
] as const

const BASE_MODELS = ['default', 'fast', 'embedding'] as const
const WORK_MODELS = ['triage', 'research', 'summarize', 'reply', 'ask', 'schedule', 'compact'] as const

function list(values: string[]): string {
  return values.join(', ')
}

function split(value: string): string[] {
  return value
    .split(/[,\n]/)
    .map((item) => item.trim())
    .filter((item) => item !== '')
}

function messageOf(caught: unknown): string {
  return caught instanceof Error ? caught.message : String(caught)
}

// useSectionSave saves one section of the agent settings and says how it
// went through the toast, for the lists that save as a dialog closes and
// have no Save button of their own.
function useSectionSave(onSaved: () => Promise<unknown> | unknown) {
  const { t } = useTranslation()
  const toast = useToast()
  const [busy, setBusy] = useState(false)
  async function save(values: Record<string, unknown>): Promise<boolean> {
    setBusy(true)
    try {
      await graphql(UPDATE, { agent: values })
      toast.done(t('integrations.savedNeedsRestart'))
      await onSaved()
      return true
    } catch (caught) {
      toast.failed(messageOf(caught))
      return false
    } finally {
      setBusy(false)
    }
  }
  return { busy, save }
}

type Props = { settings: Agent; onSaved: () => Promise<unknown> | unknown }

export function AgentForm({ settings, onSaved }: Props) {
  const [known, setKnown] = useState<string[]>([])

  // The model pickers are fed by what the enabled providers offer, read
  // once per visit; a provider that cannot be reached simply contributes
  // nothing, and Test on its row says why.
  useEffect(() => {
    let cancelled = false
    graphql<{ ListAgentModels: { name: string }[] }>(LIST_MODELS)
      .then((data) => {
        if (!cancelled) setKnown(data.ListAgentModels.map((model) => model.name))
      })
      .catch(() => undefined)
    return () => {
      cancelled = true
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [JSON.stringify(settings.providers)])

  return (
    <>
      <GeneralForm settings={settings} onSaved={onSaved} />
      <ProvidersSection
        settings={settings}
        onSaved={onSaved}
        onModels={(names) => setKnown((previous) => Array.from(new Set([...previous, ...names])).sort())}
      />
      <ModelsForm settings={settings} onSaved={onSaved} known={known} />
      <FeaturesForm settings={settings} onSaved={onSaved} />
      <LimitsForm settings={settings} onSaved={onSaved} />
      <ToolsForm settings={settings} onSaved={onSaved} />
      <SearchForm settings={settings} onSaved={onSaved} />
      <BrowserForm settings={settings} onSaved={onSaved} />
      <ServersSection settings={settings} onSaved={onSaved} />
    </>
  )
}

// GeneralForm: whether people may have an agent at all, and the house
// instructions every agent on this server is given.
function GeneralForm({ settings, onSaved }: Props) {
  const { t } = useTranslation()
  const { busy, problem, saved, save } = useSaver(onSaved)
  const [enabled, setEnabled] = useState(settings.enabled)
  const [instructions, setInstructions] = useState(settings.instructions)
  useEffect(() => {
    setEnabled(settings.enabled)
    setInstructions(settings.instructions)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [settings.enabled, settings.instructions])

  return (
    <form
      className="card"
      onSubmit={(event) => {
        event.preventDefault()
        void save({ agent: { enabled, instructions } })
      }}
    >
      <h3>{t('agentSettings.title')}</h3>
      <p className="muted">{t('agentSettings.description')}</p>
      <label className="checkbox">
        <input type="checkbox" checked={enabled} onChange={(event) => setEnabled(event.target.checked)} />
        {t('agentSettings.enabled')}
      </label>
      <label>
        <span>{t('agentSettings.instructions')}</span>
        <textarea
          rows={3}
          value={instructions}
          placeholder={t('agentSettings.instructionsPlaceholder')}
          onChange={(event) => setInstructions(event.target.value)}
        />
      </label>
      <SaveRow busy={busy} saved={saved} problem={problem} note={t('integrations.savedNeedsRestart')} />
    </form>
  )
}

type ProviderDraft = {
  name: string
  kind: string
  baseUrl: string
  apiKey: string
  hasApiKey: boolean
  enabled: boolean
  allow: string
  deny: string
  pricingInput: string
  pricingOutput: string
  pricingCacheRead: string
  modelPricing: { model: string; input: string; output: string; cacheRead: string }[]
}

function providerDraft(provider?: AgentProvider): ProviderDraft {
  return provider
    ? {
        name: provider.name,
        kind: provider.kind,
        baseUrl: provider.baseUrl,
        apiKey: '',
        hasApiKey: provider.hasApiKey,
        enabled: provider.enabled,
        allow: list(provider.allow),
        deny: list(provider.deny),
        pricingInput: provider.pricingInput ? String(provider.pricingInput) : '',
        pricingOutput: provider.pricingOutput ? String(provider.pricingOutput) : '',
        pricingCacheRead: provider.pricingCacheRead ? String(provider.pricingCacheRead) : '',
        modelPricing: (provider.modelPricing ?? []).map((priced) => ({
          model: priced.model,
          input: priced.input ? String(priced.input) : '',
          output: priced.output ? String(priced.output) : '',
          cacheRead: priced.cacheRead ? String(priced.cacheRead) : '',
        })),
      }
    : {
        name: '',
        kind: 'openai',
        baseUrl: '',
        apiKey: '',
        hasApiKey: false,
        enabled: true,
        allow: '',
        deny: '',
        pricingInput: '',
        pricingOutput: '',
        pricingCacheRead: '',
        modelPricing: [],
      }
}

// providerValues is a stored provider as the API takes it back: the key
// blank, which keeps the one stored.
function providerValues(provider: AgentProvider) {
  return {
    name: provider.name,
    kind: provider.kind,
    baseUrl: provider.baseUrl,
    apiKey: '',
    enabled: provider.enabled,
    allow: provider.allow,
    deny: provider.deny,
    pricingInput: provider.pricingInput,
    pricingOutput: provider.pricingOutput,
    pricingCacheRead: provider.pricingCacheRead,
    modelPricing: provider.modelPricing ?? [],
  }
}

function draftValues(draft: ProviderDraft) {
  return {
    name: draft.name.trim(),
    kind: draft.kind,
    baseUrl: draft.baseUrl.trim(),
    apiKey: draft.apiKey,
    enabled: draft.enabled,
    allow: split(draft.allow),
    deny: split(draft.deny),
    pricingInput: Number(draft.pricingInput) || 0,
    pricingOutput: Number(draft.pricingOutput) || 0,
    pricingCacheRead: Number(draft.pricingCacheRead) || 0,
    modelPricing: draft.modelPricing
      .filter((priced) => priced.model.trim())
      .map((priced) => ({
        model: priced.model.trim(),
        input: Number(priced.input) || 0,
        output: Number(priced.output) || 0,
        cacheRead: Number(priced.cacheRead) || 0,
      })),
  }
}

// ProvidersSection: the model services, a row each. The list is sent
// whole whenever a row changes, because a list is what it is; the row
// being edited is the one in the dialog.
function ProvidersSection({ settings, onSaved, onModels }: Props & { onModels: (names: string[]) => void }) {
  const { t } = useTranslation()
  const toast = useToast()
  const { busy, save } = useSectionSave(onSaved)
  const [editing, setEditing] = useState<{ index: number; draft: ProviderDraft } | null>(null)
  const [removing, setRemoving] = useState<number | null>(null)
  const [testing, setTesting] = useState<string | null>(null)

  async function test(name: string) {
    setTesting(name)
    try {
      const data = await graphql<{ TestAgentProvider: { name: string }[] }>(TEST_PROVIDER, { name })
      const names = data.TestAgentProvider.map((model) => model.name)
      toast.done(t('agentSettings.testResult', { count: names.length, sample: names.slice(0, 5).join(', ') }))
      onModels(names)
    } catch (caught) {
      toast.failed(messageOf(caught))
    } finally {
      setTesting(null)
    }
  }

  async function saveList(providers: ReturnType<typeof providerValues>[]) {
    return save({ providers })
  }

  return (
    <SettingsSection
      card
      title={t('agentSettings.providers')}
      description={t('agentSettings.providersDescription')}
      action={
        <button type="button" className="primary" onClick={() => setEditing({ index: -1, draft: providerDraft() })}>
          {t('agentSettings.addProvider')}
        </button>
      }
    >
      {settings.providers.length === 0 ? <SettingsEmpty>{t('agentSettings.noProviders')}</SettingsEmpty> : null}
      {settings.providers.map((provider, index) => {
        const detail = [
          provider.baseUrl || t('agentSettings.publicService'),
          provider.hasApiKey ? t('agentSettings.keySet') : t('agentSettings.keyMissing'),
        ]
        if (provider.allow.length > 0) detail.push(t('agentSettings.offers', { patterns: list(provider.allow) }))
        if (provider.deny.length > 0) detail.push(t('agentSettings.hides', { patterns: list(provider.deny) }))
        if (provider.pricingInput || provider.pricingOutput) {
          detail.push(
            t('agentSettings.priced', { input: String(provider.pricingInput), output: String(provider.pricingOutput) }),
          )
        }
        // A provider whose models are priced apart says so, since the
        // prices above are only what is left when none of them matches.
        if ((provider.modelPricing ?? []).length > 0) {
          detail.push(t('agentSettings.pricedModels', { count: String(provider.modelPricing.length) }))
        }
        return (
          <SettingsRow
            key={provider.name || index}
            title={provider.name}
            badge={
              <>
                <Tag value={provider.kind} />
                {!provider.enabled && <Tag value={t('agentSettings.disabled')} />}
              </>
            }
            subtitle={detail.join(' · ')}
            actions={
              <>
                <button
                  type="button"
                  disabled={busy || testing !== null || (!provider.hasApiKey && provider.kind !== 'openai')}
                  onClick={() => void test(provider.name)}
                >
                  {testing === provider.name ? t('agentSettings.testing') : t('agentSettings.test')}
                </button>
                <Tooltip label={t('common.edit')}>
                  <button
                    type="button"
                    className="icon-action"
                    aria-label={`${provider.name}: ${t('common.edit')}`}
                    disabled={busy}
                    onClick={() => setEditing({ index, draft: providerDraft(provider) })}
                  >
                    <PencilIcon size={16} />
                  </button>
                </Tooltip>
                <Tooltip label={t('agentSettings.remove')}>
                  <button
                    type="button"
                    className="icon-action danger"
                    aria-label={`${provider.name}: ${t('agentSettings.remove')}`}
                    disabled={busy}
                    onClick={() => setRemoving(index)}
                  >
                    <TrashIcon size={16} />
                  </button>
                </Tooltip>
              </>
            }
          />
        )
      })}
      {editing ? (
        <ProviderDialog
          draft={editing.draft}
          kinds={settings.kinds}
          adding={editing.index < 0}
          busy={busy}
          onChange={(draft) => setEditing({ ...editing, draft })}
          onClose={() => setEditing(null)}
          onSubmit={() => {
            const values: (ReturnType<typeof providerValues> & { previousName?: string })[] = settings.providers.map(providerValues)
            const section: Record<string, unknown> = {}
            if (editing.index < 0) {
              values.push(draftValues(editing.draft))
            } else {
              values[editing.index] = { ...draftValues(editing.draft), previousName: settings.providers[editing.index].name }
              // A renamed provider takes its models with it: every
              // assignment written provider:model is rewritten, or the
              // server would refuse the settings for naming a provider
              // that no longer exists.
              const before = settings.providers[editing.index].name
              const after = editing.draft.name.trim()
              if (before !== after) {
                const rename = (model: string) => (model.startsWith(before + ':') ? after + model.slice(before.length) : model)
                const models = settings.models
                section.models = {
                  ...Object.fromEntries(
                    (['default', 'fast', 'embedding', 'triage', 'research', 'summarize', 'reply', 'ask', 'schedule', 'compact'] as const).map(
                      (field) => [field, rename(models[field])],
                    ),
                  ),
                  choices: models.choices.map(rename),
                }
              }
            }
            void save({ providers: values, ...section }).then((ok) => ok && setEditing(null))
          }}
        />
      ) : null}
      {removing !== null ? (
        <ConfirmDialog
          title={t('agentSettings.removeProvider')}
          body={t('agentSettings.removeProviderConfirm', { name: settings.providers[removing]?.name ?? '' })}
          confirmLabel={t('agentSettings.remove')}
          destructive
          busy={busy}
          onClose={() => setRemoving(null)}
          onConfirm={() => {
            const values = settings.providers.filter((_, at) => at !== removing).map(providerValues)
            void saveList(values).then((ok) => ok && setRemoving(null))
          }}
        />
      ) : null}
    </SettingsSection>
  )
}

function ProviderDialog({
  draft,
  kinds,
  adding,
  busy,
  onChange,
  onClose,
  onSubmit,
}: {
  draft: ProviderDraft
  kinds: string[]
  adding: boolean
  busy: boolean
  onChange: (draft: ProviderDraft) => void
  onClose: () => void
  onSubmit: () => void
}) {
  const { t } = useTranslation()
  const set = (change: Partial<ProviderDraft>) => onChange({ ...draft, ...change })
  return (
    <FormDialog
      title={adding ? t('agentSettings.addProvider') : t('agentSettings.editProvider')}
      submitLabel={t('common.save')}
      busy={busy}
      canSubmit={draft.name.trim() !== ''}
      wide
      onClose={onClose}
      onSubmit={onSubmit}
    >
      <div className="row">
        <label>
          <span>{t('agentSettings.providerName')}</span>
          <input value={draft.name} onChange={(event) => set({ name: event.target.value })} />
        </label>
        <label className="shrink">
          <span>{t('agentSettings.providerKind')}</span>
          <Select
            block
            value={draft.kind}
            label={t('agentSettings.providerKind')}
            options={kinds.map((kind) => ({ value: kind, label: kind }))}
            onChange={(kind) => set({ kind })}
          />
        </label>
      </div>
      <label>
        <span>{t('agentSettings.providerBaseUrl')}</span>
        <input
          value={draft.baseUrl}
          placeholder={t('agentSettings.providerBaseUrlPlaceholder')}
          onChange={(event) => set({ baseUrl: event.target.value })}
        />
      </label>
      <label>
        <span>
          {t('agentSettings.providerApiKey')}
          {draft.hasApiKey ? <span className="muted"> {t('agentSettings.secretKept')}</span> : null}
        </span>
        <input
          type="password"
          autoComplete="off"
          value={draft.apiKey}
          placeholder={draft.hasApiKey ? '••••••••' : ''}
          onChange={(event) => set({ apiKey: event.target.value })}
        />
      </label>
      <div className="row">
        <label>
          <span>{t('agentSettings.providerAllow')}</span>
          <input value={draft.allow} onChange={(event) => set({ allow: event.target.value })} />
        </label>
        <label>
          <span>{t('agentSettings.providerDeny')}</span>
          <input value={draft.deny} onChange={(event) => set({ deny: event.target.value })} />
        </label>
      </div>
      <div className="row">
        <label>
          <span>{t('agentSettings.pricingInput')}</span>
          <input
            value={draft.pricingInput}
            inputMode="decimal"
            onChange={(event) => set({ pricingInput: event.target.value })}
          />
        </label>
        <label>
          <span>{t('agentSettings.pricingOutput')}</span>
          <input
            value={draft.pricingOutput}
            inputMode="decimal"
            onChange={(event) => set({ pricingOutput: event.target.value })}
          />
        </label>
        <label>
          <span>{t('agentSettings.pricingCacheRead')}</span>
          <input
            value={draft.pricingCacheRead}
            inputMode="decimal"
            onChange={(event) => set({ pricingCacheRead: event.target.value })}
          />
        </label>
      </div>
      <p className="field-label">{t('agentSettings.modelPricing')}</p>
      <p className="muted field-hint">{t('agentSettings.modelPricingHint')}</p>
      {draft.modelPricing.length > 0 && (
        <div className="priced-models">
          <div className="priced-models-head">
            <span>{t('agentSettings.pricingModel')}</span>
            <span>{t('agentSettings.pricingIn')}</span>
            <span>{t('agentSettings.pricingOut')}</span>
            <span>{t('agentSettings.pricingCached')}</span>
            <span />
          </div>
          {draft.modelPricing.map((priced, index) => {
            const change = (fields: Partial<(typeof draft.modelPricing)[number]>) =>
              set({ modelPricing: draft.modelPricing.map((row, at) => (at === index ? { ...row, ...fields } : row)) })
            const named = priced.model.trim() || t('agentSettings.pricingModel')
            return (
              <div className="priced-models-row" key={index}>
                <input
                  value={priced.model}
                  placeholder="gpt-5*"
                  aria-label={t('agentSettings.pricingModel')}
                  onChange={(event) => change({ model: event.target.value })}
                />
                <input
                  value={priced.input}
                  inputMode="decimal"
                  placeholder={t('agentSettings.pricingIn')}
                  aria-label={`${named}: ${t('agentSettings.pricingIn')}`}
                  onChange={(event) => change({ input: event.target.value })}
                />
                <input
                  value={priced.output}
                  inputMode="decimal"
                  placeholder={t('agentSettings.pricingOut')}
                  aria-label={`${named}: ${t('agentSettings.pricingOut')}`}
                  onChange={(event) => change({ output: event.target.value })}
                />
                <input
                  value={priced.cacheRead}
                  inputMode="decimal"
                  placeholder={t('agentSettings.pricingCached')}
                  aria-label={`${named}: ${t('agentSettings.pricingCached')}`}
                  onChange={(event) => change({ cacheRead: event.target.value })}
                />
                <button
                  type="button"
                  className="icon-action danger"
                  aria-label={`${named}: ${t('common.remove')}`}
                  title={t('common.remove')}
                  onClick={() => set({ modelPricing: draft.modelPricing.filter((_, at) => at !== index) })}
                >
                  <TrashIcon size={14} />
                </button>
              </div>
            )
          })}
        </div>
      )}
      <div className="priced-models-add">
        <button
          type="button"
          onClick={() => set({ modelPricing: [...draft.modelPricing, { model: '', input: '', output: '', cacheRead: '' }] })}
        >
          {t('agentSettings.addModelPricing')}
        </button>
      </div>
      <label className="checkbox">
        <input type="checkbox" checked={draft.enabled} onChange={(event) => set({ enabled: event.target.checked })} />
        {t('integrations.enabled')}
      </label>
    </FormDialog>
  )
}

// ModelPicker is one model assignment: a list of what the providers offer,
// a box to narrow it, and room for a name the list does not know yet.
function ModelPicker({
  value,
  known,
  label,
  placeholder,
  onChange,
}: {
  value: string
  known: string[]
  label: string
  placeholder: string
  onChange: (value: string) => void
}) {
  const names = value && !known.includes(value) ? [value, ...known] : known
  return (
    <label>
      <span>{label}</span>
      <Select
        block
        allowCustom
        value={value}
        label={label}
        placeholder={placeholder}
        options={[{ value: '', label: placeholder }, ...names.map((name) => ({ value: name, label: name }))]}
        onChange={onChange}
      />
    </label>
  )
}

// ModelsForm assigns work to models: three every deployment needs, then
// one per kind of work for whoever wants to spend differently on sorting
// than on conversations, and the models a person may pick for their own.
function ModelsForm({ settings, onSaved, known }: Props & { known: string[] }) {
  const { t } = useTranslation()
  const { busy, problem, saved, save } = useSaver(onSaved)
  const [models, setModels] = useState(settings.models)
  const [more, setMore] = useState(false)
  useEffect(() => {
    setModels(settings.models)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [JSON.stringify(settings.models)])

  const choosable = known.filter((name) => !models.choices.includes(name))

  return (
    <form
      className="card"
      onSubmit={(event) => {
        event.preventDefault()
        void save({ agent: { models } })
      }}
    >
      <h3>{t('agentSettings.models')}</h3>
      <p className="muted">{t('agentSettings.modelsDescription')}</p>
      <div className="form-narrow">
        {BASE_MODELS.map((field) => (
          <ModelPicker
            key={field}
            value={models[field]}
            known={known}
            label={t(`agentSettings.model.${field}`)}
            placeholder={t('agentSettings.modelNone')}
            onChange={(value) => setModels({ ...models, [field]: value })}
          />
        ))}
        <div className="page-actions">
          <button type="button" onClick={() => setMore((previous) => !previous)}>
            {more ? t('agentSettings.fewerModels') : t('agentSettings.modelOverrides')}
          </button>
        </div>
        {more ? (
          <>
            {WORK_MODELS.map((field) => (
              <ModelPicker
                key={field}
                value={models[field]}
                known={known}
                label={t(`agentSettings.work.${field}`)}
                placeholder={t('agentSettings.modelInherit')}
                onChange={(value) => setModels({ ...models, [field]: value })}
              />
            ))}
            <label>
              <span>{t('agentSettings.modelChoices')}</span>
              {models.choices.length > 0 ? (
                <div className="chip-list">
                  {models.choices.map((choice) => (
                    <span key={choice} className="chip">
                      {choice}
                      <button
                        type="button"
                        className="chip-remove"
                        aria-label={`${choice}: ${t('agentSettings.remove')}`}
                        onClick={() =>
                          setModels({ ...models, choices: models.choices.filter((item) => item !== choice) })
                        }
                      >
                        ×
                      </button>
                    </span>
                  ))}
                </div>
              ) : null}
              <Select
                block
                allowCustom
                value=""
                label={t('agentSettings.modelChoices')}
                placeholder={t('agentSettings.addChoice')}
                options={[
                  { value: '', label: t('agentSettings.addChoice') },
                  ...choosable.map((name) => ({ value: name, label: name })),
                ]}
                onChange={(value) => {
                  if (value && !models.choices.includes(value)) {
                    setModels({ ...models, choices: [...models.choices, value] })
                  }
                }}
              />
            </label>
          </>
        ) : null}
      </div>
      <SaveRow busy={busy} saved={saved} problem={problem} note={t('integrations.savedNeedsRestart')} />
    </form>
  )
}

function FeaturesForm({ settings, onSaved }: Props) {
  const { t } = useTranslation()
  const { busy, problem, saved, save } = useSaver(onSaved)
  const [features, setFeatures] = useState<Record<string, boolean>>(settings.features)
  useEffect(() => {
    setFeatures(settings.features)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [JSON.stringify(settings.features)])

  return (
    <form
      className="card"
      onSubmit={(event) => {
        event.preventDefault()
        void save({ agent: { features } })
      }}
    >
      <h3>{t('agentSettings.features')}</h3>
      <p className="muted">{t('agentSettings.featuresDescription')}</p>
      {FEATURES.map((feature) => (
        <label className="checkbox" key={feature}>
          <input
            type="checkbox"
            checked={features[feature] !== false}
            onChange={(event) => setFeatures({ ...features, [feature]: event.target.checked })}
          />
          {t(`agentSettings.feature.${feature}`)}
        </label>
      ))}
      <SaveRow busy={busy} saved={saved} problem={problem} note={t('integrations.savedNeedsRestart')} />
    </form>
  )
}

function limitFields(settings: Agent) {
  return {
    maxBodyCharacters: String(settings.limits.maxBodyCharacters),
    dailyTokensPerAgent: String(settings.limits.dailyTokensPerAgent),
    monthlyTokensPerServer: String(settings.limits.monthlyTokensPerServer),
    dailyCostPerAgent: String(settings.limits.dailyCostPerAgent),
    monthlyCostPerServer: String(settings.limits.monthlyCostPerServer),
    maxRoundsPerAsk: String(settings.limits.maxRoundsPerAsk),
    maxRoundsPerResearch: String(settings.limits.maxRoundsPerResearch),
    maxRoundsPerReply: String(settings.limits.maxRoundsPerReply),
    maxToolCallsPerRun: String(settings.limits.maxToolCallsPerRun),
    requestTimeout: settings.limits.requestTimeout,
    concurrency: String(settings.limits.concurrency),
  }
}

function LimitsForm({ settings, onSaved }: Props) {
  const { t } = useTranslation()
  const { busy, problem, saved, save } = useSaver(onSaved)
  const [limits, setLimits] = useState(limitFields(settings))
  const [retention, setRetention] = useState(settings.retention)
  const [currency, setCurrency] = useState(settings.currency || '')
  useEffect(() => {
    setLimits(limitFields(settings))
    setRetention(settings.retention)
    setCurrency(settings.currency || '')
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [JSON.stringify(settings.limits), JSON.stringify(settings.retention)])

  const number = (value: string) => Number(value) || 0
  const numeric = (field: keyof typeof limits) => (
    <label key={field} className="shrink">
      <span>{t(`agentSettings.limit.${field}`)}</span>
      <input
        value={limits[field]}
        inputMode={field === 'requestTimeout' ? undefined : 'numeric'}
        onChange={(event) => setLimits({ ...limits, [field]: event.target.value })}
      />
    </label>
  )

  return (
    <form
      className="card"
      onSubmit={(event) => {
        event.preventDefault()
        void save({
          agent: {
            limits: {
              maxBodyCharacters: number(limits.maxBodyCharacters),
              dailyTokensPerAgent: number(limits.dailyTokensPerAgent),
              monthlyTokensPerServer: number(limits.monthlyTokensPerServer),
              dailyCostPerAgent: number(limits.dailyCostPerAgent),
              monthlyCostPerServer: number(limits.monthlyCostPerServer),
              maxRoundsPerAsk: number(limits.maxRoundsPerAsk),
              maxRoundsPerResearch: number(limits.maxRoundsPerResearch),
              maxRoundsPerReply: number(limits.maxRoundsPerReply),
              maxToolCallsPerRun: number(limits.maxToolCallsPerRun),
              requestTimeout: limits.requestTimeout,
              concurrency: number(limits.concurrency),
            },
            retention,
            currency,
          },
        })
      }}
    >
      <h3>{t('agentSettings.limits')}</h3>
      <p className="muted">{t('agentSettings.limitsDescription')}</p>
      <div className="row">{(['dailyTokensPerAgent', 'monthlyTokensPerServer', 'maxBodyCharacters'] as const).map(numeric)}</div>
      <div className="row">
        {(['dailyCostPerAgent', 'monthlyCostPerServer'] as const).map(numeric)}
        <label className="shrink">
          <span>{t('agentSettings.currency')}</span>
          <input
            value={currency}
            maxLength={3}
            placeholder="USD"
            onChange={(event) => setCurrency(event.target.value.toUpperCase())}
          />
        </label>
      </div>
      <div className="row">
        {(['maxRoundsPerAsk', 'maxRoundsPerResearch', 'maxRoundsPerReply', 'maxToolCallsPerRun'] as const).map(numeric)}
      </div>
      <div className="row">
        {(['requestTimeout', 'concurrency'] as const).map(numeric)}
        <label className="shrink">
          <span>{t('agentSettings.retentionRuns')}</span>
          <input value={retention.runs} onChange={(event) => setRetention({ ...retention, runs: event.target.value })} />
        </label>
        <label className="shrink">
          <span>{t('agentSettings.retentionCorrections')}</span>
          <input
            value={retention.corrections}
            onChange={(event) => setRetention({ ...retention, corrections: event.target.value })}
          />
        </label>
      </div>
      <SaveRow busy={busy} saved={saved} problem={problem} note={t('integrations.savedNeedsRestart')} />
    </form>
  )
}

// policyOf reads the two lists as one word per name.
function policyOf(name: string, tools: Agent['tools']): string {
  if (tools.disabled.includes(name)) return 'off'
  if (tools.confirm.includes(name)) return 'confirm'
  return 'allow'
}

// ToolsForm: every tool the agent can be given, by family, and one word
// for each — allowed, ask first, off — with a word for the whole family
// that the tools inherit unless they say otherwise.
function ToolsForm({ settings, onSaved }: Props) {
  const { t } = useTranslation()
  const { busy, problem, saved, save } = useSaver(onSaved)
  const names = useMemo(
    () => [...settings.families, ...settings.tools.catalog.map((tool) => tool.name)],
    [settings.families, settings.tools.catalog],
  )
  const [policy, setPolicy] = useState<Record<string, string>>({})
  useEffect(() => {
    setPolicy(Object.fromEntries(names.map((name) => [name, policyOf(name, settings.tools)])))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [JSON.stringify(settings.tools), names.join(',')])

  const options = [
    { value: 'allow', label: t('agentSettings.policyAllow') },
    { value: 'confirm', label: t('agentSettings.policyConfirm') },
    { value: 'off', label: t('agentSettings.policyOff') },
  ]
  const wordFor = (name: string) => policy[name] ?? 'allow'

  return (
    <form
      className="card"
      onSubmit={(event) => {
        event.preventDefault()
        void save({
          agent: {
            tools: {
              disabled: names.filter((name) => wordFor(name) === 'off'),
              confirm: names.filter((name) => wordFor(name) === 'confirm'),
            },
          },
        })
      }}
    >
      <h3>{t('agentSettings.tools')}</h3>
      <p className="muted">{t('agentSettings.toolsDescription')}</p>
      <ToolPolicyAccordion
        families={settings.families}
        tools={settings.tools.catalog}
        options={options}
        policy={policy}
        defaultWord="allow"
        onChange={(name, word) => setPolicy({ ...policy, [name]: word })}
      />
      <SaveRow busy={busy} saved={saved} problem={problem} note={t('integrations.savedNeedsRestart')} />
    </form>
  )
}

function SearchForm({ settings, onSaved }: Props) {
  const { t } = useTranslation()
  const { busy, problem, saved, save } = useSaver(onSaved)
  const [kind, setKind] = useState(settings.search.kind)
  const [apiKey, setApiKey] = useState('')
  useEffect(() => {
    setKind(settings.search.kind)
    setApiKey('')
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [JSON.stringify(settings.search)])

  return (
    <form
      className="card"
      onSubmit={(event) => {
        event.preventDefault()
        void save({ agent: { search: { kind, apiKey: apiKey === '' ? undefined : apiKey } } })
      }}
    >
      <h3>{t('agentSettings.search')}</h3>
      <p className="muted">{t('agentSettings.searchDescription')}</p>
      <div className="row">
        <label>
          <span>{t('agentSettings.searchKind')}</span>
          <Select
            block
            value={kind}
            label={t('agentSettings.searchKind')}
            options={[
              { value: '', label: t('agentSettings.searchNone') },
              { value: 'brave', label: 'Brave' },
            ]}
            onChange={setKind}
          />
        </label>
        <label>
          <span>
            {t('agentSettings.searchApiKey')}
            {settings.search.hasApiKey ? <span className="muted"> {t('agentSettings.secretKept')}</span> : null}
          </span>
          <input
            type="password"
            autoComplete="off"
            value={apiKey}
            placeholder={settings.search.hasApiKey ? '••••••••' : ''}
            onChange={(event) => setApiKey(event.target.value)}
          />
        </label>
      </div>
      <SaveRow busy={busy} saved={saved} problem={problem} note={t('integrations.savedNeedsRestart')} />
    </form>
  )
}

function browserFields(settings: Agent) {
  return {
    ...settings.browser,
    allowPrivateAddresses: list(settings.browser.allowPrivateAddresses),
    maxContexts: String(settings.browser.maxContexts),
  }
}

function BrowserForm({ settings, onSaved }: Props) {
  const { t } = useTranslation()
  const { busy, problem, saved, save } = useSaver(onSaved)
  const [browser, setBrowser] = useState(browserFields(settings))
  useEffect(() => {
    setBrowser(browserFields(settings))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [JSON.stringify(settings.browser)])

  return (
    <form
      className="card"
      onSubmit={(event) => {
        event.preventDefault()
        void save({
          agent: {
            browser: {
              enabled: browser.enabled,
              cdpEndpoint: browser.cdpEndpoint,
              attachTabs: browser.attachTabs,
              allowPrivateAddresses: split(browser.allowPrivateAddresses),
              idleTimeout: browser.idleTimeout,
              maxContexts: Number(browser.maxContexts) || 0,
            },
          },
        })
      }}
    >
      <h3>{t('agentSettings.browser')}</h3>
      <p className="muted">{t('agentSettings.browserDescription')}</p>
      <label className="checkbox">
        <input
          type="checkbox"
          checked={browser.enabled}
          onChange={(event) => setBrowser({ ...browser, enabled: event.target.checked })}
        />
        {t('integrations.enabled')}
      </label>
      <div className="row">
        <label>
          <span>{t('agentSettings.browserEndpoint')}</span>
          <input
            value={browser.cdpEndpoint}
            placeholder="chrome:9222"
            onChange={(event) => setBrowser({ ...browser, cdpEndpoint: event.target.value })}
          />
        </label>
        <label className="shrink">
          <span>{t('agentSettings.browserIdleTimeout')}</span>
          <input
            value={browser.idleTimeout}
            onChange={(event) => setBrowser({ ...browser, idleTimeout: event.target.value })}
          />
        </label>
        <label className="shrink">
          <span>{t('agentSettings.browserMaxContexts')}</span>
          <input
            value={browser.maxContexts}
            inputMode="numeric"
            onChange={(event) => setBrowser({ ...browser, maxContexts: event.target.value })}
          />
        </label>
      </div>
      <label>
        <span>{t('agentSettings.browserAllowPrivate')}</span>
        <input
          value={browser.allowPrivateAddresses}
          onChange={(event) => setBrowser({ ...browser, allowPrivateAddresses: event.target.value })}
        />
      </label>
      <label className="checkbox">
        <input
          type="checkbox"
          checked={browser.attachTabs}
          onChange={(event) => setBrowser({ ...browser, attachTabs: event.target.checked })}
        />
        {t('agentSettings.browserAttachTabs')}
      </label>
      <SaveRow busy={busy} saved={saved} problem={problem} note={t('integrations.savedNeedsRestart')} />
    </form>
  )
}

type ServerDraft = {
  name: string
  transport: string
  url: string
  command: string
  args: string
  env: string
  workingDir: string
  auth: string
  authorization: string
  hasAuthorization: boolean
  oauthClientId: string
  oauthClientSecret: string
  hasOauthClientSecret: boolean
  oauthScopes: string
  oauthAuthorizationUrl: string
  oauthTokenUrl: string
  headless: boolean
  readOnly: string
  disabled: string
  timeout: string
  enabled: boolean
}

function serverDraft(server?: AgentMCPServer): ServerDraft {
  return server
    ? {
        name: server.name,
        transport: server.transport,
        url: server.url,
        command: server.command,
        args: server.args.join(' '),
        env: server.envNames.map((name) => `${name}=`).join('\n'),
        workingDir: server.workingDir,
        auth: server.auth,
        authorization: '',
        hasAuthorization: server.hasAuthorization,
        oauthClientId: server.oauthClientId,
        oauthClientSecret: '',
        hasOauthClientSecret: server.hasOauthClientSecret,
        oauthScopes: list(server.oauthScopes),
        oauthAuthorizationUrl: server.oauthAuthorizationUrl,
        oauthTokenUrl: server.oauthTokenUrl,
        headless: server.headless,
        readOnly: list(server.readOnly),
        disabled: list(server.disabled),
        timeout: server.timeout,
        enabled: server.enabled,
      }
    : {
        name: '',
        transport: '',
        url: '',
        command: '',
        args: '',
        env: '',
        workingDir: '',
        auth: '',
        authorization: '',
        hasAuthorization: false,
        oauthClientId: '',
        oauthClientSecret: '',
        hasOauthClientSecret: false,
        oauthScopes: '',
        oauthAuthorizationUrl: '',
        oauthTokenUrl: '',
        headless: false,
        readOnly: '',
        disabled: '',
        timeout: '',
        enabled: true,
      }
}

// serverValues is a stored server as the API takes it back: secrets blank,
// which keeps them; the environment as names with blank values, which
// keeps those too.
function serverValues(server: AgentMCPServer) {
  return {
    name: server.name,
    transport: server.transport,
    url: server.url,
    command: server.command,
    args: server.args,
    env: server.envNames.map((name) => `${name}=`),
    workingDir: server.workingDir,
    auth: server.auth,
    authorization: '',
    oauthClientId: server.oauthClientId,
    oauthClientSecret: '',
    oauthScopes: server.oauthScopes,
    oauthAuthorizationUrl: server.oauthAuthorizationUrl,
    oauthTokenUrl: server.oauthTokenUrl,
    headless: server.headless,
    readOnly: server.readOnly,
    disabled: server.disabled,
    timeout: server.timeout,
    enabled: server.enabled,
  }
}

function serverDraftValues(draft: ServerDraft) {
  return {
    name: draft.name.trim(),
    transport: draft.transport,
    url: draft.url.trim(),
    command: draft.command.trim(),
    args: draft.args.split(/\s+/).filter((item) => item !== ''),
    env: draft.env.split('\n').filter((line) => line.trim() !== ''),
    workingDir: draft.workingDir.trim(),
    auth: draft.auth,
    authorization: draft.authorization,
    oauthClientId: draft.oauthClientId.trim(),
    oauthClientSecret: draft.oauthClientSecret,
    oauthScopes: split(draft.oauthScopes),
    oauthAuthorizationUrl: draft.oauthAuthorizationUrl.trim(),
    oauthTokenUrl: draft.oauthTokenUrl.trim(),
    headless: draft.headless,
    readOnly: split(draft.readOnly),
    disabled: split(draft.disabled),
    timeout: draft.timeout.trim(),
    enabled: draft.enabled,
  }
}

// ServersSection: the connected servers the operator declares, a row
// each, edited in a dialog because a server has a dozen fields and most
// of them are read once.
function ServersSection({ settings, onSaved }: Props) {
  const { t } = useTranslation()
  const { busy, save } = useSectionSave(onSaved)
  const [editing, setEditing] = useState<{ index: number; draft: ServerDraft } | null>(null)
  const [removing, setRemoving] = useState<number | null>(null)

  async function saveList(servers: ReturnType<typeof serverValues>[]) {
    return save({ mcpServers: servers })
  }

  return (
    <SettingsSection
      card
      title={t('agentSettings.servers')}
      description={t('agentSettings.serversDescription')}
      action={
        <button type="button" className="primary" onClick={() => setEditing({ index: -1, draft: serverDraft() })}>
          {t('agentSettings.addServer')}
        </button>
      }
    >
      {settings.mcpServers.length === 0 ? <SettingsEmpty>{t('agentSettings.noServers')}</SettingsEmpty> : null}
      {settings.mcpServers.map((server, index) => {
        const detail = [
          server.effectiveTransport === 'stdio' ? [server.command, ...server.args].join(' ') : server.url,
          t('agentSettings.authIs', { auth: server.effectiveAuth || 'none' }),
        ]
        if (server.timeout) detail.push(t('agentSettings.timeoutIs', { timeout: server.timeout }))
        if (server.readOnly.length > 0) detail.push(t('agentSettings.readOnlyAre', { tools: list(server.readOnly) }))
        if (server.disabled.length > 0) detail.push(t('agentSettings.disabledAre', { tools: list(server.disabled) }))
        return (
          <SettingsRow
            key={server.name || index}
            title={server.name}
            badge={
              <>
                <Tag value={server.effectiveTransport} />
                {server.headless && <Tag value={t('agentSettings.headless')} />}
                {!server.enabled && <Tag value={t('agentSettings.disabled')} />}
              </>
            }
            subtitle={detail.join(' · ')}
            actions={
              <>
                <Tooltip label={t('common.edit')}>
                  <button
                    type="button"
                    className="icon-action"
                    aria-label={`${server.name}: ${t('common.edit')}`}
                    disabled={busy}
                    onClick={() => setEditing({ index, draft: serverDraft(server) })}
                  >
                    <PencilIcon size={16} />
                  </button>
                </Tooltip>
                <Tooltip label={t('agentSettings.remove')}>
                  <button
                    type="button"
                    className="icon-action danger"
                    aria-label={`${server.name}: ${t('agentSettings.remove')}`}
                    disabled={busy}
                    onClick={() => setRemoving(index)}
                  >
                    <TrashIcon size={16} />
                  </button>
                </Tooltip>
              </>
            }
          />
        )
      })}
      {editing ? (
        <ServerDialog
          draft={editing.draft}
          adding={editing.index < 0}
          busy={busy}
          onChange={(draft) => setEditing({ ...editing, draft })}
          onClose={() => setEditing(null)}
          onSubmit={() => {
            const values: (ReturnType<typeof serverValues> & { previousName?: string })[] = settings.mcpServers.map(serverValues)
            if (editing.index < 0) {
              values.push(serverDraftValues(editing.draft))
            } else {
              values[editing.index] = { ...serverDraftValues(editing.draft), previousName: settings.mcpServers[editing.index].name }
            }
            void saveList(values).then((ok) => ok && setEditing(null))
          }}
        />
      ) : null}
      {removing !== null ? (
        <ConfirmDialog
          title={t('agentSettings.removeServer')}
          body={t('agentSettings.removeServerConfirm', { name: settings.mcpServers[removing]?.name ?? '' })}
          confirmLabel={t('agentSettings.remove')}
          destructive
          busy={busy}
          onClose={() => setRemoving(null)}
          onConfirm={() => {
            const values = settings.mcpServers.filter((_, at) => at !== removing).map(serverValues)
            void saveList(values).then((ok) => ok && setRemoving(null))
          }}
        />
      ) : null}
    </SettingsSection>
  )
}

function ServerDialog({
  draft,
  adding,
  busy,
  onChange,
  onClose,
  onSubmit,
}: {
  draft: ServerDraft
  adding: boolean
  busy: boolean
  onChange: (draft: ServerDraft) => void
  onClose: () => void
  onSubmit: () => void
}) {
  const { t } = useTranslation()
  const set = (change: Partial<ServerDraft>) => onChange({ ...draft, ...change })
  const stdio = draft.transport === 'stdio' || (draft.transport === '' && draft.command.trim() !== '' && draft.url.trim() === '')
  const oauth = draft.auth === 'oauth'
  return (
    <FormDialog
      title={adding ? t('agentSettings.addServer') : t('agentSettings.editServer')}
      submitLabel={t('common.save')}
      busy={busy}
      canSubmit={draft.name.trim() !== '' && (draft.url.trim() !== '' || draft.command.trim() !== '')}
      wide
      onClose={onClose}
      onSubmit={onSubmit}
    >
      <div className="row">
        <label>
          <span>{t('agentSettings.serverName')}</span>
          <input value={draft.name} onChange={(event) => set({ name: event.target.value })} />
        </label>
        <label className="shrink">
          <span>{t('agentSettings.serverTransport')}</span>
          <Select
            block
            value={draft.transport}
            label={t('agentSettings.serverTransport')}
            options={[
              { value: '', label: t('agentSettings.inferred') },
              { value: 'http', label: 'http' },
              { value: 'stdio', label: 'stdio' },
            ]}
            onChange={(transport) => set({ transport })}
          />
        </label>
      </div>
      {draft.transport !== 'stdio' ? (
        <label>
          <span>{t('agentSettings.serverUrl')}</span>
          <input value={draft.url} placeholder="https://" onChange={(event) => set({ url: event.target.value })} />
        </label>
      ) : null}
      {draft.transport !== 'http' ? (
        <>
          <div className="row">
            <label>
              <span>{t('agentSettings.serverCommand')}</span>
              <input value={draft.command} onChange={(event) => set({ command: event.target.value })} />
            </label>
            <label>
              <span>{t('agentSettings.serverArgs')}</span>
              <input value={draft.args} onChange={(event) => set({ args: event.target.value })} />
            </label>
          </div>
          {stdio ? (
            <>
              <label>
                <span>{t('agentSettings.serverWorkingDir')}</span>
                <input value={draft.workingDir} onChange={(event) => set({ workingDir: event.target.value })} />
              </label>
              <label>
                <span>{t('agentSettings.serverEnv')}</span>
                <textarea rows={2} value={draft.env} onChange={(event) => set({ env: event.target.value })} />
              </label>
            </>
          ) : null}
        </>
      ) : null}
      <div className="row">
        <label className="shrink">
          <span>{t('agentSettings.serverAuth')}</span>
          <Select
            block
            value={draft.auth}
            label={t('agentSettings.serverAuth')}
            options={[
              { value: '', label: t('agentSettings.inferred') },
              { value: 'none', label: t('agentSettings.authNone') },
              { value: 'static', label: t('agentSettings.authStatic') },
              { value: 'user', label: t('agentSettings.authUser') },
              { value: 'oauth', label: t('agentSettings.authOauth') },
            ]}
            onChange={(auth) => set({ auth })}
          />
        </label>
        <label className="shrink">
          <span>{t('agentSettings.serverTimeout')}</span>
          <input value={draft.timeout} placeholder="30s" onChange={(event) => set({ timeout: event.target.value })} />
        </label>
      </div>
      {draft.auth === 'static' || draft.auth === '' ? (
        <label>
          <span>
            {t('agentSettings.serverAuthorization')}
            {draft.hasAuthorization ? <span className="muted"> {t('agentSettings.secretKept')}</span> : null}
          </span>
          <input
            type="password"
            autoComplete="off"
            value={draft.authorization}
            placeholder={draft.hasAuthorization ? '••••••••' : ''}
            onChange={(event) => set({ authorization: event.target.value })}
          />
        </label>
      ) : null}
      {oauth ? (
        <>
          <div className="row">
            <label>
              <span>{t('agentSettings.serverOauthClientId')}</span>
              <input value={draft.oauthClientId} onChange={(event) => set({ oauthClientId: event.target.value })} />
            </label>
            <label>
              <span>
                {t('agentSettings.serverOauthClientSecret')}
                {draft.hasOauthClientSecret ? <span className="muted"> {t('agentSettings.secretKept')}</span> : null}
              </span>
              <input
                type="password"
                autoComplete="off"
                value={draft.oauthClientSecret}
                placeholder={draft.hasOauthClientSecret ? '••••••••' : ''}
                onChange={(event) => set({ oauthClientSecret: event.target.value })}
              />
            </label>
          </div>
          <label>
            <span>{t('agentSettings.serverOauthScopes')}</span>
            <input value={draft.oauthScopes} onChange={(event) => set({ oauthScopes: event.target.value })} />
          </label>
          <div className="row">
            <label>
              <span>{t('agentSettings.serverOauthAuthorizationUrl')}</span>
              <input
                value={draft.oauthAuthorizationUrl}
                placeholder={t('agentSettings.discovered')}
                onChange={(event) => set({ oauthAuthorizationUrl: event.target.value })}
              />
            </label>
            <label>
              <span>{t('agentSettings.serverOauthTokenUrl')}</span>
              <input
                value={draft.oauthTokenUrl}
                placeholder={t('agentSettings.discovered')}
                onChange={(event) => set({ oauthTokenUrl: event.target.value })}
              />
            </label>
          </div>
        </>
      ) : null}
      <div className="row">
        <label>
          <span>{t('agentSettings.serverReadOnly')}</span>
          <input value={draft.readOnly} onChange={(event) => set({ readOnly: event.target.value })} />
        </label>
        <label>
          <span>{t('agentSettings.serverDisabled')}</span>
          <input value={draft.disabled} onChange={(event) => set({ disabled: event.target.value })} />
        </label>
      </div>
      <label className="checkbox">
        <input type="checkbox" checked={draft.headless} onChange={(event) => set({ headless: event.target.checked })} />
        {t('agentSettings.serverHeadless')}
      </label>
      <label className="checkbox">
        <input type="checkbox" checked={draft.enabled} onChange={(event) => set({ enabled: event.target.checked })} />
        {t('integrations.enabled')}
      </label>
    </FormDialog>
  )
}

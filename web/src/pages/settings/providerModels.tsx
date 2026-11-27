import { useEffect, useState } from 'react'

import { graphql } from '../../api'
import { ConfirmDialog, FormDialog } from '../../components/dialog'
import { PencilIcon } from '../../components/icons'
import { SelectOption } from '../../components/select'
import { SettingsRow } from '../../components/settingsList'
import { useToast } from '../../components/toast'
import { Tooltip } from '../../components/tooltip'
import { Key, useTranslation } from '../../i18n/i18n'

// The models a provider offers, listed under its credential: each a row
// with its name over its id, a dialog for one model's own settings, and
// "Add model", which offers what the provider itself lists or takes a typed
// id. A role still names a model as provider:model, where model is the id;
// the entries are what the role pickers offer.

export type ModelPricing = {
  input: number
  output: number
  cacheRead: number
  cacheWrite: number
  audioInput: number
}

export type AgentModelEntry = {
  // The id the service knows the model by.
  model: string
  // What people read; empty reads as the id.
  name: string
  // The override of the provider's window, in tokens; zero takes the
  // provider's figure.
  contextWindow: number
  // The provider's own figure, read-only; zero when it reports none.
  reportedContextWindow: number
  pricing: ModelPricing
  isEnabled: boolean
  // The roles that point at this model: default, quick, embedding, ...
  usedFor: string[]
}

export const MODEL_ENTRY_FIELDS =
  'model name contextWindow reportedContextWindow pricing { input output cacheRead cacheWrite audioInput } isEnabled usedFor'

const SAVE_ENTRY = `mutation ($providerName: String!, $entry: AgentModelEntryInput!) {
  SaveAgentModelEntry(providerName: $providerName, entry: $entry) { model }
}`
const DELETE_ENTRY = `mutation ($providerName: String!, $model: String!) {
  DeleteAgentModelEntry(providerName: $providerName, model: $model)
}`
const LIST_PROVIDER_MODELS = `{ ListAgentModels { provider model name contextLength } }`

type OfferedModel = { provider: string; model: string; name: string; contextLength: number }

function messageOf(caught: unknown): string {
  return caught instanceof Error ? caught.message : String(caught)
}

// asNumber keeps a number as it is typed. A comma for a decimal point is
// what half the world writes, and Number("1,50") is NaN.
function asNumber(typed: string): string {
  const digits = typed.replace(/,/g, '.').replace(/[^0-9.]/g, '')
  const [whole, ...rest] = digits.split('.')
  return rest.length > 0 ? `${whole}.${rest.join('')}` : whole
}

function asWholeNumber(typed: string): string {
  return typed.replace(/[^0-9]/g, '')
}

// modelLabel is what a model is called: its name, or its id when it has none.
export function modelLabel(entry: { model: string; name: string }): string {
  return entry.name.trim() || entry.model
}

// The roles, as the page words them. A role the page has no word for is
// shown as the server names it.
const ROLE_LABELS: Record<string, Key> = {
  default: 'agentSettings.model.default',
  fast: 'agentSettings.model.fast',
  quick: 'agentSettings.model.quick',
  embedding: 'agentSettings.model.embedding',
  transcription: 'agentSettings.recordingTranscription',
  triage: 'agentSettings.work.triage',
  research: 'agentSettings.work.research',
  summarize: 'agentSettings.work.summarize',
  reply: 'agentSettings.work.reply',
  ask: 'agentSettings.work.ask',
  schedule: 'agentSettings.work.schedule',
  compact: 'agentSettings.work.compact',
  scan: 'agentSettings.work.scan',
  synthesize: 'agentSettings.work.synthesize',
  decide: 'agentSettings.work.decide',
  categorize: 'agentSettings.work.categorize',
}

// ModelOptionSource is the part of a provider the role pickers read.
export type ModelOptionSource = {
  name: string
  kind: string
  enabled: boolean
  models?: AgentModelEntry[]
}

export type ModelOption = SelectOption & { provider: string; kind: string; id: string }

// modelOptions lists the models of the enabled providers for a role picker,
// "Provider · Model", grouped by provider. The value is what a role stores:
// provider:model, with the model's id.
export function modelOptions(providers: ModelOptionSource[]): ModelOption[] {
  return providers
    .filter((provider) => provider.enabled)
    .flatMap((provider) =>
      (provider.models ?? [])
        .filter((entry) => entry.isEnabled)
        .map((entry) => ({
          value: `${provider.name}:${entry.model}`,
          label: `${provider.name} · ${modelLabel(entry)}`,
          group: provider.name,
          provider: provider.name,
          kind: provider.kind,
          id: entry.model,
        })),
    )
}

// ProviderModels is what sits under a provider's credential.
export function ProviderModels({
  providerName,
  models,
  busy,
  onSaved,
}: {
  providerName: string
  models: AgentModelEntry[]
  busy: boolean
  onSaved: () => Promise<unknown> | unknown
}) {
  const { t } = useTranslation()
  const [adding, setAdding] = useState(false)
  const [editing, setEditing] = useState<string | null>(null)
  const editedEntry = models.find((entry) => entry.model === editing)
  return (
    <div className="provider-models">
      {models.length === 0 ? <p className="muted provider-models-empty">{t('agentSettings.noModels')}</p> : null}
      {models.map((entry) => (
        <SettingsRow
          key={entry.model}
          title={modelLabel(entry)}
          badge={entry.isEnabled ? null : <span className="muted">{t('agentSettings.modelOff')}</span>}
          subtitle={<span className="mono">{entry.model}</span>}
          actions={
            <Tooltip label={t('common.edit')}>
              <button
                type="button"
                className="icon-action"
                aria-label={`${modelLabel(entry)}: ${t('common.edit')}`}
                disabled={busy}
                onClick={() => setEditing(entry.model)}
              >
                <PencilIcon size={16} />
              </button>
            </Tooltip>
          }
        />
      ))}
      <div className="provider-models-add">
        <button type="button" disabled={busy} onClick={() => setAdding(true)}>
          {t('agentSettings.addModel')}
        </button>
      </div>
      {adding ? (
        <AddModelDialog
          providerName={providerName}
          existing={models.map((entry) => entry.model)}
          onClose={() => setAdding(false)}
          onSaved={onSaved}
        />
      ) : null}
      {editedEntry ? (
        <ModelDialog
          providerName={providerName}
          entry={editedEntry}
          onClose={() => setEditing(null)}
          onSaved={onSaved}
        />
      ) : null}
    </div>
  )
}

// AddModelDialog offers the provider's own list, narrowed by what is typed,
// and takes what is typed as the id when the list does not have it (or the
// provider cannot be reached).
function AddModelDialog({
  providerName,
  existing,
  onClose,
  onSaved,
}: {
  providerName: string
  existing: string[]
  onClose: () => void
  onSaved: () => Promise<unknown> | unknown
}) {
  const { t } = useTranslation()
  const toast = useToast()
  const [offered, setOffered] = useState<OfferedModel[] | null>(null)
  const [typed, setTyped] = useState('')
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    let isCurrent = true
    graphql<{ ListAgentModels: OfferedModel[] }>(LIST_PROVIDER_MODELS)
      .then((data) => {
        if (isCurrent) setOffered(data.ListAgentModels.filter((listed) => listed.provider === providerName))
      })
      .catch(() => {
        if (isCurrent) setOffered([])
      })
    return () => {
      isCurrent = false
    }
  }, [providerName])

  const words = typed.trim().toLowerCase()
  const shown = (offered ?? [])
    .filter((listed) => !existing.includes(listed.model))
    .filter((listed) => !words || listed.model.toLowerCase().includes(words))
  const model = typed.trim()
  const isDuplicate = existing.includes(model)

  async function add() {
    setBusy(true)
    try {
      await graphql(SAVE_ENTRY, {
        providerName,
        entry: {
          model,
          name: '',
          contextWindow: 0,
          pricing: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, audioInput: 0 },
          isEnabled: true,
        },
      })
      toast.done(t('agentSettings.modelAdded', { model }))
      await onSaved()
      onClose()
    } catch (caught) {
      toast.failed(messageOf(caught))
    } finally {
      setBusy(false)
    }
  }

  return (
    <FormDialog
      title={t('agentSettings.addModelTitle', { name: providerName })}
      submitLabel={t('agentSettings.addModel')}
      busy={busy}
      canSubmit={model !== '' && !isDuplicate}
      wide
      onClose={onClose}
      onSubmit={() => void add()}
    >
      <label>
        <span>{t('agentSettings.addModelFilter')}</span>
        <input value={typed} autoComplete="off" onChange={(event) => setTyped(event.target.value)} />
      </label>
      {offered === null ? <p className="muted">{t('agentSettings.addModelLoading')}</p> : null}
      {offered !== null && shown.length === 0 ? <p className="muted">{t('agentSettings.addModelNone')}</p> : null}
      {shown.length > 0 ? (
        <div className="model-choices" role="list">
          {shown.map((listed) => (
            <button
              key={listed.model}
              type="button"
              role="listitem"
              className={listed.model === model ? 'model-choice selected' : 'model-choice'}
              onClick={() => setTyped(listed.model)}
            >
              <span className="mono">{listed.model}</span>
              {listed.contextLength > 0 ? (
                <small className="muted">
                  {t('agentSettings.modelTokens', { count: listed.contextLength.toLocaleString() })}
                </small>
              ) : null}
            </button>
          ))}
        </div>
      ) : null}
    </FormDialog>
  )
}

type PriceField = keyof ModelPricing

const PRICE_FIELDS: { field: PriceField; label: Key }[] = [
  { field: 'input', label: 'agentSettings.modelPriceInput' },
  { field: 'output', label: 'agentSettings.modelPriceOutput' },
  { field: 'cacheRead', label: 'agentSettings.modelPriceCacheRead' },
  { field: 'cacheWrite', label: 'agentSettings.modelPriceCacheWrite' },
  { field: 'audioInput', label: 'agentSettings.modelPriceAudioInput' },
]

// ModelDialog is one model's own settings. The id is read-only once added;
// Remove is refused by the server while a role uses the model, and the
// server's words are the toast.
function ModelDialog({
  providerName,
  entry,
  onClose,
  onSaved,
}: {
  providerName: string
  entry: AgentModelEntry
  onClose: () => void
  onSaved: () => Promise<unknown> | unknown
}) {
  const { t } = useTranslation()
  const toast = useToast()
  const [name, setName] = useState(entry.name)
  const [contextWindow, setContextWindow] = useState(entry.contextWindow ? String(entry.contextWindow) : '')
  const [prices, setPrices] = useState<Record<PriceField, string>>(() => ({
    input: entry.pricing.input ? String(entry.pricing.input) : '',
    output: entry.pricing.output ? String(entry.pricing.output) : '',
    cacheRead: entry.pricing.cacheRead ? String(entry.pricing.cacheRead) : '',
    cacheWrite: entry.pricing.cacheWrite ? String(entry.pricing.cacheWrite) : '',
    audioInput: entry.pricing.audioInput ? String(entry.pricing.audioInput) : '',
  }))
  const [isEnabled, setIsEnabled] = useState(entry.isEnabled)
  const [busy, setBusy] = useState(false)
  const [isRemoving, setIsRemoving] = useState(false)

  async function save() {
    setBusy(true)
    try {
      await graphql(SAVE_ENTRY, {
        providerName,
        entry: {
          model: entry.model,
          name: name.trim(),
          contextWindow: Number(contextWindow) || 0,
          pricing: {
            input: Number(prices.input) || 0,
            output: Number(prices.output) || 0,
            cacheRead: Number(prices.cacheRead) || 0,
            cacheWrite: Number(prices.cacheWrite) || 0,
            audioInput: Number(prices.audioInput) || 0,
          },
          isEnabled,
        },
      })
      toast.done(t('agentSettings.modelSaved', { model: entry.model }))
      await onSaved()
      onClose()
    } catch (caught) {
      toast.failed(messageOf(caught))
    } finally {
      setBusy(false)
    }
  }

  async function remove() {
    setBusy(true)
    try {
      await graphql(DELETE_ENTRY, { providerName, model: entry.model })
      toast.done(t('agentSettings.modelRemoved', { model: entry.model }))
      await onSaved()
      onClose()
    } catch (caught) {
      // A model a role uses is refused, naming the role; the dialog stays.
      toast.failed(messageOf(caught))
      setIsRemoving(false)
    } finally {
      setBusy(false)
    }
  }

  const roles = entry.usedFor.map((role) => (ROLE_LABELS[role] ? t(ROLE_LABELS[role]) : role))
  // The confirmation replaces the form while it is asked, so Escape closes
  // one dialog; what was typed is kept in this component meanwhile.
  if (isRemoving) {
    return (
      <ConfirmDialog
        title={t('agentSettings.removeModel')}
        body={t('agentSettings.removeModelConfirm', { model: entry.model })}
        confirmLabel={t('agentSettings.remove')}
        destructive
        busy={busy}
        onClose={() => setIsRemoving(false)}
        onConfirm={() => void remove()}
      />
    )
  }
  return (
    <FormDialog
      title={t('agentSettings.modelDialogTitle', { name: modelLabel(entry) })}
      submitLabel={t('common.save')}
      busy={busy}
      wide
      otherAction={
        <button type="button" className="danger" disabled={busy} onClick={() => setIsRemoving(true)}>
          {t('agentSettings.remove')}
        </button>
      }
      onClose={onClose}
      onSubmit={() => void save()}
    >
      <label>
        <span>{t('agentSettings.modelName')}</span>
        <input value={name} placeholder={entry.model} onChange={(event) => setName(event.target.value)} />
      </label>
      <label>
        <span>{t('agentSettings.modelId')}</span>
        <input value={entry.model} readOnly className="mono" />
      </label>
      <label>
        <span>{t('agentSettings.modelContextWindow')}</span>
        <input
          value={contextWindow}
          inputMode="numeric"
          placeholder={
            entry.reportedContextWindow > 0
              ? String(entry.reportedContextWindow)
              : t('agentSettings.modelContextWindowNotReported')
          }
          onChange={(event) => setContextWindow(asWholeNumber(event.target.value))}
        />
        <small className="muted">{t('agentSettings.modelContextWindowHint')}</small>
      </label>
      <p className="field-label">{t('agentSettings.modelPrices')}</p>
      <div className="row">
        {PRICE_FIELDS.map(({ field, label }) => (
          <label key={field}>
            <span>{t(label)}</span>
            <input
              value={prices[field]}
              inputMode="decimal"
              onChange={(event) => setPrices({ ...prices, [field]: asNumber(event.target.value) })}
            />
          </label>
        ))}
      </div>
      <label className="checkbox">
        <input type="checkbox" checked={isEnabled} onChange={(event) => setIsEnabled(event.target.checked)} />
        {t('agentSettings.modelIsOn')}
      </label>
      <p className="field-label">{t('agentSettings.modelUsedFor')}</p>
      {roles.length > 0 ? (
        <div className="chip-list">
          {roles.map((role) => (
            <span key={role} className="chip">
              {role}
            </span>
          ))}
        </div>
      ) : (
        <p className="muted">{t('agentSettings.modelUsedForNone')}</p>
      )}
    </FormDialog>
  )
}

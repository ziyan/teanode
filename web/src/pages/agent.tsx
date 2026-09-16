import { useEffect, useMemo, useState } from 'react'

import { AgentReply, graphql, openAgentConversation } from '../api'
import {
  ErrorMessage,
  Loading,
  SaveRow,
  Tag,
  budgetNearness,
  formatClock,
  formatCount,
  formatMoney,
  formatTime,
} from '../components/common'
import { Column, DataTable } from '../components/dataTable'
import { ConfirmDialog, FormDialog } from '../components/dialog'
import { PencilIcon, RefreshIcon, ToggleOffIcon, ToggleOnIcon, TrashIcon } from '../components/icons'
import { SettingsEmpty, SettingsRow, SettingsSection } from '../components/settingsList'
import { useToast } from '../components/toast'
import { useQuery } from '../components/useQuery'
import { Link } from 'react-router-dom'

import { useTranslation } from '../i18n/i18n'
import { Select } from '../components/select'
import { PolicyTool, ToolPolicyAccordion } from '../components/toolPolicy'

// A person's agent: the page where they turn it on, name it, tell it about
// themselves, choose what it may reach and what it does there, and see what
// it costs. Grouped the way a person thinks of it — about me, tell me when,
// then one card per mailbox — rather than the way it is stored.

export type AgentVoice = { tone?: string; length?: string; greeting?: string; signoff?: string }
export type AgentCategory = { name: string; description?: string }
export type AgentNotifications = { heldReply?: string; highPriority?: string; runFailed?: string }
export type AgentHours = { from: string; until: string; days: number[] }
export type AgentAutoReply = {
  enabled: boolean
  guidance?: string
  scope?: string
  allow?: string[]
  never?: string[]
  categories?: string[]
  when?: string
  hours?: AgentHours | null
  holdMinutes?: number
  dailyLimit?: number
}
export type AgentMailboxPolicy = {
  granted: boolean
  triage?: { enabled: boolean; backfill?: string; replyExpectation?: string } | null
  summaries?: { enabled: boolean; minimumMessages?: number; style?: string } | null
  draftReplies: boolean
  search: boolean
  research: boolean
  autoReply?: AgentAutoReply | null
}
export type Agent = {
  id: string
  name: string
  enabled: boolean
  instructions?: string
  language?: string
  voice?: AgentVoice | null
  categories: AgentCategory[]
  notifications?: AgentNotifications | null
  confirm: string[]
  askModel?: string
  dreamFrom?: string
  dreamUntil?: string
  dailyTokens: number
  operatorDisabledAt?: string | null
}
export type AgentSource = { mailboxId: string; name: string; addresses: string[]; policy?: AgentMailboxPolicy | null }
// A calendar or an address book as a source: a switch, and how much is in it.
export type AgentCollection = {
  id: string
  name: string
  kind: 'calendar' | 'addressBook'
  granted: boolean
  items: number
}
export type AgentView = {
  agent: Agent | null
  sources: AgentSource[]
  collections: AgentCollection[]
  allowed: Record<string, boolean>
  budget: { used: number; limit: number; resetsAt: string; cost: number; costLimit: number; currency: string } | null
  choices: string[]
  timezone: string
  language: string
  categories: string[]
}

const VIEW = `{
  agent { id name enabled instructions language askModel dreamFrom dreamUntil dailyTokens operatorDisabledAt confirm
    voice { tone length greeting signoff }
    categories { name description }
    notifications { heldReply highPriority runFailed } }
  sources { mailboxId name addresses policy { granted draftReplies search research
    triage { enabled backfill replyExpectation }
    summaries { enabled minimumMessages style }
    autoReply { enabled guidance scope allow never categories when hours { from until days } holdMinutes dailyLimit } } }
  collections { id name kind granted items }
  allowed { enabled triage summaries draftReplies search research autoReply ask schedules browser connectedServers }
  budget { used limit resetsAt cost costLimit currency }
  choices timezone language categories
}`

export const READ_AGENT = `query { ReadAgent ${VIEW} }`
const UPDATE_AGENT = `
  mutation ($enabled: Boolean, $name: String, $instructions: String, $language: String, $voice: AgentVoiceInput,
    $categories: [AgentCategoryInput!], $notifications: AgentNotificationsInput, $confirm: [String!], $askModel: String,
    $dreamFrom: String, $dreamUntil: String, $forget: Boolean) {
    UpdateAgent(enabled: $enabled, name: $name, instructions: $instructions, language: $language, voice: $voice,
      categories: $categories, notifications: $notifications, confirm: $confirm, askModel: $askModel,
      dreamFrom: $dreamFrom, dreamUntil: $dreamUntil, forget: $forget) ${VIEW}
  }`
const GRANT = `mutation ($mailboxId: String!, $policy: AgentMailboxInput) { GrantAgentMailbox(mailboxId: $mailboxId, policy: $policy) ${VIEW} }`
const REVOKE = `mutation ($mailboxId: String!) { RevokeAgentMailbox(mailboxId: $mailboxId) ${VIEW} }`
const GRANT_SOURCE = `
  mutation ($kind: String!, $id: String!, $granted: Boolean!) {
    GrantAgentSource(kind: $kind, id: $id, granted: $granted) ${VIEW}
  }`

export function useAgent(options: { refresh?: boolean } = {}) {
  return useQuery(() => graphql<{ ReadAgent: AgentView }>(READ_AGENT), [], options)
}

function joinList(values?: string[] | null): string {
  return (values ?? []).join(', ')
}

function splitList(value: string): string[] {
  return value
    .split(/[,\n]/)
    .map((item) => item.trim())
    .filter((item) => item !== '')
}

function messageOf(caught: unknown): string {
  return caught instanceof Error ? caught.message : String(caught)
}

export function AgentPage() {
  const { t } = useTranslation()
  const { data, error, loading, reload } = useAgent()
  const toast = useToast()
  const [busy, setBusy] = useState(false)
  const [forgetting, setForgetting] = useState(false)

  if (loading && !data) return <Loading />
  if (error) return <ErrorMessage error={error} />
  const view = data!.ReadAgent

  async function update(variables: Record<string, unknown>, done: string) {
    setBusy(true)
    try {
      await graphql(UPDATE_AGENT, variables)
      toast.done(done)
      await reload()
    } catch (caught) {
      toast.failed(messageOf(caught))
    } finally {
      setBusy(false)
    }
  }

  if (!view.allowed.enabled) {
    return (
      <div className="card">
        <h3>{t('agent.title')}</h3>
        <p className="muted">{t('agent.notOffered')}</p>
      </div>
    )
  }

  if (!view.agent) {
    return (
      <div className="card">
        <h3>{t('agent.title')}</h3>
        <p className="muted">{t('agent.intro')}</p>
        <div className="page-actions">
          <button
            type="button"
            className="primary"
            disabled={busy}
            onClick={() => void update({ enabled: true }, t('agent.turnedOn'))}
          >
            {t('agent.turnOn')}
          </button>
        </div>
      </div>
    )
  }

  const agent = view.agent
  return (
    <>
      <div className="card">
        <h3>{t('agent.title')}</h3>
        {agent.operatorDisabledAt ? <p className="warning">{t('agent.operatorDisabled')}</p> : null}
        <label className="checkbox">
          <input
            type="checkbox"
            checked={agent.enabled}
            disabled={busy || !!agent.operatorDisabledAt}
            onChange={(event) =>
              void update(
                { enabled: event.target.checked },
                event.target.checked ? t('agent.turnedOn') : t('agent.turnedOff'),
              )
            }
          />
          {t('agent.enabled')}
        </label>
        <BudgetBar budget={view.budget} zone={view.timezone} />
        <p className="muted">
          {t('agent.timezone', { zone: view.timezone })} · {t('agent.language', { language: view.language || '—' })}
        </p>
      </div>
      <div className="card">
        <AboutForm agent={agent} view={view} busy={busy} onSave={update} />
        <VoiceForm agent={agent} busy={busy} onSave={update} />
      </div>
      <CategoriesSection agent={agent} view={view} busy={busy} onSave={update} />
      <SettingsSection card title={t('agent.sources')} description={t('agent.sourcesHint')}>
        {view.sources.map((source) => (
          <SourceCard key={source.mailboxId} source={source} view={view} onChanged={reload} />
        ))}
        {view.collections.map((collection) => (
          <CollectionRow key={collection.id} collection={collection} view={view} onChanged={reload} />
        ))}
      </SettingsSection>
      <LearnedCard />
      <KnowledgeSourcesCard />
      <DreamCard agent={agent} busy={busy} onChange={update} />
      <BriefCard />
      <SchedulesCard />
      <ServersCard />
      <SkillSecretsCard />
      <ChatAppsCard />
      <RepliesCard />
      <ActivityCard />
      <CorrectionsCard />
      <div className="card">
        <h3>{t('agent.advanced')}</h3>
        <div className="form-narrow">
          {view.choices.length > 0 ? (
            <label>
              <span>{t('agent.askModel')}</span>
              <Select
                block
                value={agent.askModel ?? ''}
                disabled={busy}
                label={t('agent.askModel')}
                options={[
                  { value: '', label: t('agent.askModelDefault') },
                  ...view.choices.map((choice) => ({ value: choice, label: choice })),
                ]}
                onChange={(value) => void update({ askModel: value }, t('agent.saved'))}
              />
            </label>
          ) : null}
        </div>
        <ConfirmForm agent={agent} busy={busy} onSave={update} />
        <div className="settings-subform">
          <h4>{t('agent.forget')}</h4>
          <p className="muted">{t('agent.forgetHint')}</p>
          <div className="page-actions">
            <button type="button" className="danger" disabled={busy} onClick={() => setForgetting(true)}>
              {t('agent.forget')}
            </button>
          </div>
        </div>
      </div>
      {forgetting ? (
        <ConfirmDialog
          title={t('agent.forget')}
          body={t('agent.forgetConfirm')}
          confirmLabel={t('agent.forget')}
          destructive
          onClose={() => setForgetting(false)}
          onConfirm={() => {
            setForgetting(false)
            void update({ forget: true }, t('agent.forgotten'))
          }}
        />
      ) : null}
    </>
  )
}

type SaveProps = {
  agent: Agent
  busy: boolean
  onSave: (variables: Record<string, unknown>, done: string) => Promise<void>
}

// AboutForm: what the agent is called, what it writes in, and the standing
// instructions — the three things a person writes once and then leaves.
// BudgetBar is the day against its budget: what has gone of it as a bar
// in the colour of how near the end it is, the numbers beside it, and
// when the day starts again. A budget can be set in tokens or in money;
// where both are, the one nearer its end is the one drawn, since that is
// the one that will stop the day.
function BudgetBar({ budget, zone }: { budget: AgentView['budget']; zone: string }) {
  const { t } = useTranslation()
  if (!budget) return null
  const tokens = budget.limit > 0 ? budget.used / budget.limit : -1
  const money = budget.costLimit > 0 ? budget.cost / budget.costLimit : -1
  if (tokens < 0 && money < 0) {
    return (
      <p className="muted">
        {t('agent.budgetNone')}{' '}
        {t('agent.budgetMoney', { used: formatMoney(budget.cost, budget.currency), limit: t('agent.unlimited') })}
      </p>
    )
  }
  const byMoney = money >= tokens
  const fraction = Math.max(0, Math.min(1, byMoney ? money : tokens))
  const said = byMoney
    ? t('agent.budgetMoney', {
        used: formatMoney(budget.cost, budget.currency),
        limit: formatMoney(budget.costLimit, budget.currency),
      })
    : t('agent.budgetTokens', { used: formatCount(budget.used), limit: formatCount(budget.limit) })
  return (
    <div className="agent-budget">
      <div className="agent-budget-said">
        <span>
          {said}
          {/* What it came to, whichever way the budget is counted: a
              number of tokens is not something anybody can act on. */}
          {!byMoney && budget.cost > 0 ? (
            <span className="muted"> · {formatMoney(budget.cost, budget.currency)}</span>
          ) : null}
        </span>
        <span className="muted">{t('agent.budgetResets', { at: formatClock(budget.resetsAt, zone) })}</span>
      </div>
      <div
        className="agent-budget-bar"
        role="progressbar"
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={Math.round(fraction * 100)}
        aria-label={said}
      >
        <span
          className={`agent-budget-bar-fill ${budgetNearness(fraction, 1)}`}
          style={{ width: `${Math.round(fraction * 100)}%` }}
        />
      </div>
    </div>
  )
}

// The languages the server has a name for, each written in itself. A tag
// it does not know is still allowed — the prompt says "the language with
// the code xx" and the model does the rest — which is what allowCustom
// on the control is for.
const AGENT_LANGUAGES = [
  { value: 'en', label: 'English' },
  { value: 'de', label: 'Deutsch' },
  { value: 'es', label: 'Español' },
  { value: 'fr', label: 'Français' },
  { value: 'it', label: 'Italiano' },
  { value: 'ja', label: '日本語' },
  { value: 'ko', label: '한국어' },
  { value: 'nl', label: 'Nederlands' },
  { value: 'pt', label: 'Português' },
  { value: 'ru', label: 'Русский' },
  { value: 'zh', label: '中文' },
]

function AboutForm({ agent, view, busy, onSave }: SaveProps & { view: AgentView }) {
  const { t } = useTranslation()
  const [name, setName] = useState(agent.name)
  const [instructions, setInstructions] = useState(agent.instructions ?? '')
  const [language, setLanguage] = useState(agent.language ?? '')
  useEffect(() => {
    setName(agent.name)
    setInstructions(agent.instructions ?? '')
    setLanguage(agent.language ?? '')
    // On the values, not the object: the page reads the agent again every
    // few seconds, and a fresh copy of the same values must not put a
    // field back while somebody is typing into it.
  }, [agent.name, agent.instructions, agent.language])

  return (
    <form
      onSubmit={(event) => {
        event.preventDefault()
        void onSave({ name: name.trim(), instructions, language: language.trim() }, t('agent.saved'))
      }}
    >
      <h3>{t('agent.aboutMe')}</h3>
      <div className="form-narrow">
        <div className="row">
          <label>
            <span>{t('agent.name')}</span>
            <input value={name} maxLength={64} onChange={(event) => setName(event.target.value)} />
          </label>
          <label className="shrink">
            <span>{t('agent.languageField')}</span>
            <Select
              value={language}
              label={t('agent.languageField')}
              options={[{ value: '', label: t('agent.languageFollows') }, ...AGENT_LANGUAGES]}
              onChange={setLanguage}
              placeholder={view.language || 'en'}
              allowCustom
              block
            />
          </label>
        </div>
        <label>
          <span>{t('agent.instructions')}</span>
          <textarea
            rows={5}
            value={instructions}
            placeholder={t('agent.instructionsPlaceholder')}
            onChange={(event) => setInstructions(event.target.value)}
          />
        </label>
      </div>
      <SaveRow busy={busy} saved={false} note={t('agent.saved')} />
    </form>
  )
}

// VoiceForm: how it sounds when it writes for the person.
function VoiceForm({ agent, busy, onSave }: SaveProps) {
  const { t } = useTranslation()
  const [voice, setVoice] = useState<AgentVoice>(agent.voice ?? {})
  useEffect(() => {
    setVoice(agent.voice ?? {})
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [JSON.stringify(agent.voice)])

  return (
    <form
      className="settings-subform"
      onSubmit={(event) => {
        event.preventDefault()
        void onSave(
          {
            voice: {
              tone: voice.tone ?? '',
              length: voice.length ?? '',
              greeting: voice.greeting ?? '',
              signoff: voice.signoff ?? '',
            },
          },
          t('agent.saved'),
        )
      }}
    >
      <h4>{t('agent.voice')}</h4>
      <p className="muted">{t('agent.voiceHint')}</p>
      <div className="form-narrow">
        <div className="row">
          <label>
            <span>{t('agent.voiceTone')}</span>
            <Select
              block
              value={voice.tone ?? ''}
              label={t('agent.voiceTone')}
              options={[
                { value: '', label: t('agent.voiceNeutral') },
                { value: 'formal', label: t('agent.voiceFormal') },
                { value: 'casual', label: t('agent.voiceCasual') },
              ]}
              onChange={(value) => setVoice({ ...voice, tone: value })}
            />
          </label>
          <label>
            <span>{t('agent.voiceLength')}</span>
            <Select
              block
              value={voice.length ?? ''}
              label={t('agent.voiceLength')}
              options={[
                { value: '', label: t('agent.voiceMedium') },
                { value: 'short', label: t('agent.voiceShort') },
                { value: 'long', label: t('agent.voiceLong') },
              ]}
              onChange={(value) => setVoice({ ...voice, length: value })}
            />
          </label>
        </div>
        <div className="row">
          <label>
            <span>{t('agent.voiceGreeting')}</span>
            <input
              value={voice.greeting ?? ''}
              onChange={(event) => setVoice({ ...voice, greeting: event.target.value })}
            />
          </label>
          <label>
            <span>{t('agent.voiceSignoff')}</span>
            <input
              value={voice.signoff ?? ''}
              onChange={(event) => setVoice({ ...voice, signoff: event.target.value })}
            />
          </label>
        </div>
      </div>
      <SaveRow busy={busy} saved={false} note={t('agent.saved')} />
    </form>
  )
}

// CategoriesSection: the person's own categories beside the fixed ones, a
// row each, added and changed in a dialog and saved as it closes.
function CategoriesSection({ agent, view, busy, onSave }: SaveProps & { view: AgentView }) {
  const { t } = useTranslation()
  const [editing, setEditing] = useState<{ index: number; category: AgentCategory } | null>(null)
  const [removing, setRemoving] = useState<number | null>(null)

  const save = (categories: AgentCategory[]) =>
    onSave({ categories: categories.filter((category) => category.name.trim() !== '') }, t('agent.saved'))

  return (
    <SettingsSection
      card
      title={t('agent.categories')}
      description={t('agent.categoriesHint', { fixed: view.categories.slice(0, 9).join(', ') })}
      action={
        <button
          type="button"
          className="primary"
          disabled={busy}
          onClick={() => setEditing({ index: -1, category: { name: '', description: '' } })}
        >
          {t('agent.addCategory')}
        </button>
      }
    >
      {agent.categories.length === 0 ? <SettingsEmpty>{t('agent.noCategories')}</SettingsEmpty> : null}
      {agent.categories.map((category, index) => (
        <SettingsRow
          key={`${category.name}-${index}`}
          title={category.name}
          subtitle={category.description}
          actions={
            <>
              <button
                type="button"
                className="icon-action"
                title={t('common.edit')}
                aria-label={`${category.name}: ${t('common.edit')}`}
                disabled={busy}
                onClick={() => setEditing({ index, category: { ...category } })}
              >
                <PencilIcon size={16} />
              </button>
              <button
                type="button"
                className="icon-action danger"
                title={t('common.remove')}
                aria-label={`${category.name}: ${t('common.remove')}`}
                disabled={busy}
                onClick={() => setRemoving(index)}
              >
                <TrashIcon size={16} />
              </button>
            </>
          }
        />
      ))}
      {editing ? (
        <FormDialog
          title={editing.index < 0 ? t('agent.addCategory') : t('agent.editCategory')}
          submitLabel={t('common.save')}
          busy={busy}
          canSubmit={editing.category.name.trim() !== ''}
          onClose={() => setEditing(null)}
          onSubmit={() => {
            const categories = [...agent.categories]
            if (editing.index < 0) {
              categories.push(editing.category)
            } else {
              categories[editing.index] = editing.category
            }
            void save(categories).then(() => setEditing(null))
          }}
        >
          <label>
            <span>{t('agent.categoryName')}</span>
            <input
              value={editing.category.name}
              maxLength={40}
              onChange={(event) =>
                setEditing({ ...editing, category: { ...editing.category, name: event.target.value } })
              }
            />
          </label>
          <label>
            <span>{t('agent.categoryDescription')}</span>
            <input
              value={editing.category.description ?? ''}
              onChange={(event) =>
                setEditing({ ...editing, category: { ...editing.category, description: event.target.value } })
              }
            />
          </label>
        </FormDialog>
      ) : null}
      {removing !== null ? (
        <ConfirmDialog
          title={t('agent.removeCategory')}
          body={t('agent.removeCategoryConfirm', { name: agent.categories[removing]?.name ?? '' })}
          confirmLabel={t('common.remove')}
          destructive
          busy={busy}
          onClose={() => setRemoving(null)}
          onConfirm={() => {
            void save(agent.categories.filter((_, at) => at !== removing)).then(() => setRemoving(null))
          }}
        />
      ) : null}
    </SettingsSection>
  )
}

// What the agent filed lately. The whole graph is its own page; this is
// the last day of it, which is the part somebody checks.
const LEARNED = `
  query ($days: Int, $first: Int) {
    ListAgentLearned(days: $days, first: $first) {
      fact { id number text inferred evidence { kind quote } }
      path
      name
    }
  }`

const STRIKE_FACT = `
  mutation ($path: String!, $number: Int!) {
    DeleteAgentFact(path: $path, number: $number)
  }`

// The places the agent reads, and what the nightly run did.
const KNOWLEDGE_SOURCES = `
  query {
    ListAgentKnowledgeSources {
      id kind name specification { computer path format }
      enabled cron lastRunAt lastError documentCount chunkCount refusedCount more sensitive allowed
      unknownAuthors
    }
  }`

const SAVE_KNOWLEDGE_SOURCE = `
  mutation ($sourceId: String, $kind: String, $name: String, $computer: String, $path: String, $format: String, $enabled: Boolean, $mailboxId: String) {
    SaveAgentKnowledgeSource(sourceId: $sourceId, kind: $kind, name: $name, computer: $computer, path: $path, format: $format, enabled: $enabled, mailboxId: $mailboxId) { id name }
  }`

const DELETE_KNOWLEDGE_SOURCE = `mutation ($sourceId: String!) { DeleteAgentKnowledgeSource(sourceId: $sourceId) }`
const SYNC_KNOWLEDGE_SOURCE = `mutation ($sourceId: String!) { SyncAgentKnowledgeSource(sourceId: $sourceId) }`
const ALLOW_KNOWLEDGE_DIRECTORY = `
  mutation ($sourceId: String!, $name: String!) {
    AllowAgentKnowledgeDirectory(sourceId: $sourceId, name: $name) { id }
  }`

const DREAMS = `
  query {
    ListAgentDreams(first: 7) {
      id startedAt finishedAt digested filed merged rewritten moved dormant embedded backlog coarse
      strengthened associated rehearsed gaps revised notes lastError
      proposals { kind path to reason }
    }
  }`

const DREAM_NOW = `mutation { DreamAgentNow }`

type LearnedFact = {
  fact: { id: string; number: number; text: string; inferred: boolean; evidence: { kind: string; quote: string }[] }
  path: string
  name: string
}

type KnowledgeSource = {
  id: string
  kind: string
  name: string
  specification: { computer: string; path: string; format: string }
  enabled: boolean
  cron: string
  lastRunAt: string | null
  lastError: string
  documentCount: number
  chunkCount: number
  refusedCount: number
  more: boolean
  sensitive: string[]
  unknownAuthors: string[]
  allowed: string[]
}

type Dream = {
  id: string
  startedAt: string
  finishedAt: string | null
  digested: number
  filed: number
  merged: number
  rewritten: number
  moved: number
  dormant: number
  embedded: number
  backlog: number
  coarse: boolean
  strengthened: number
  associated: number
  rehearsed: number
  gaps: number
  revised: number
  // What the night wrote down about the questions it asked itself, one
  // line each. Prose rather than counts: "answered: ..." and "gap: ..."
  // are the sentences a person reads to see whether the night was any
  // use, which no number above can say.
  notes: string
  lastError: string
  proposals: { kind: string; path: string; to: string; reason: string }[]
}

const SCHEDULES = `
  query {
    ListAgentSchedules { id name cron prompt deliver enabled lastRunAt nextRunAt }
  }`

const SAVE_SCHEDULE = `
  mutation ($scheduleId: String, $name: String, $cron: String, $prompt: String, $deliver: String, $enabled: Boolean) {
    SaveAgentSchedule(scheduleId: $scheduleId, name: $name, cron: $cron, prompt: $prompt, deliver: $deliver, enabled: $enabled) { id }
  }`

const SET_BRIEF = `
  mutation ($enabled: Boolean!, $at: String, $days: [Int!]) {
    SetAgentBrief(enabled: $enabled, at: $at, days: $days) { id name cron prompt deliver enabled nextRunAt }
  }`
const RUN_BRIEF = `mutation { RunAgentBriefNow { id name } }`
const DELETE_SCHEDULE = `
  mutation ($scheduleId: String!) {
    DeleteAgentSchedule(scheduleId: $scheduleId)
  }`

const RUN_SCHEDULE = `
  mutation ($scheduleId: String!) {
    RunAgentSchedule(scheduleId: $scheduleId)
  }`

const SERVERS = `
  query {
    ListAgentServers { name transport auth headless enabled status lastError lastConnectedAt tools }
  }`

const CONNECT_SERVER = `
  mutation ($server: String!, $credential: String) {
    ConnectAgentServer(server: $server, credential: $credential) { name status lastError tools }
  }`

const DISCONNECT_SERVER = `
  mutation ($server: String!) {
    DisconnectAgentServer(server: $server) { name status }
  }`

const BEGIN_OAUTH = `
  mutation ($server: String!, $redirectUrl: String!) {
    BeginAgentServerOAuth(server: $server, redirectUrl: $redirectUrl)
  }`

const FINISH_OAUTH = `
  mutation ($server: String!, $code: String!, $state: String!) {
    FinishAgentServerOAuth(server: $server, code: $code, state: $state) { name status lastError }
  }`

interface AgentServer {
  name: string
  transport: string
  auth: string
  headless: boolean
  enabled: boolean
  status: string
  lastError?: string
  tools: number
}

// ServersCard is the connected servers the operator declared, and the
// person's way into each: a credential, or an authorization that leaves
// the page and comes back to it with a code.
type ChatApp = {
  kind: string
  hasToken: boolean
  botName?: string
  linked: boolean
  linkedName?: string
  linkCode?: string
  enabled: boolean
  running: boolean
  lastError?: string
}

const CHAT_APPS = `
  query {
    ListAgentChannels { kind hasToken botName linked linkedName linkCode enabled running lastError }
  }`
const SET_CHAT_APP = `
  mutation ($kind: String!, $token: String, $enabled: Boolean) {
    SetAgentChannel(kind: $kind, token: $token, enabled: $enabled) { kind hasToken botName linked linkedName linkCode enabled running lastError }
  }`
const UNLINK_CHAT_APP = `mutation ($kind: String!) { UnlinkAgentChannel(kind: $kind) { kind } }`
const REMOVE_CHAT_APP = `mutation ($kind: String!) { RemoveAgentChannel(kind: $kind) }`
const CHAT_APP_KINDS = ['telegram', 'discord'] as const

// The chat apps: the person's own bot in each, its token handed over
// once, the code a chat sends to link itself, and whether it runs.
function ChatAppsCard() {
  const { t } = useTranslation()
  const toast = useToast()
  const { data, error, reload } = useQuery(() => graphql<{ ListAgentChannels: ChatApp[] }>(CHAT_APPS, {}), [], {
    refresh: false,
  })
  const [editing, setEditing] = useState<string | null>(null)
  const [token, setToken] = useState('')
  const [busy, setBusy] = useState<string | null>(null)
  const [problem, setProblem] = useState<string | null>(null)
  const apps = data?.ListAgentChannels ?? []
  const byKind = new Map(apps.map((app) => [app.kind, app]))
  const nameOf = (kind: string) => (kind === 'discord' ? t('agent.chatApp.discord') : t('agent.chatApp.telegram'))
  const howOf = (kind: string) => (kind === 'discord' ? t('agent.chatAppHow.discord') : t('agent.chatAppHow.telegram'))
  const setApp = async (kind: string, secret: string, enabled?: boolean) => {
    setBusy(kind)
    setProblem(null)
    try {
      await graphql(SET_CHAT_APP, { kind, token: secret || undefined, enabled })
      setEditing(null)
      setToken('')
      toast.done(t('agent.chatAppSet', { name: nameOf(kind) }))
      await reload()
    } catch (caught) {
      if (editing) {
        setProblem(messageOf(caught))
      } else {
        toast.failed(messageOf(caught))
      }
    } finally {
      setBusy(null)
    }
  }
  const unlink = async (kind: string) => {
    setBusy(kind)
    try {
      await graphql(UNLINK_CHAT_APP, { kind })
      await reload()
    } catch (caught) {
      toast.failed(messageOf(caught))
    } finally {
      setBusy(null)
    }
  }
  const remove = async (kind: string) => {
    setBusy(kind)
    try {
      await graphql(REMOVE_CHAT_APP, { kind })
      await reload()
    } catch (caught) {
      toast.failed(messageOf(caught))
    } finally {
      setBusy(null)
    }
  }
  if (error) return null
  const stateOf = (app: ChatApp | undefined) => {
    if (!app) return <Tag value={t('agent.chatAppNotSet')} />
    if (!app.enabled) return <Tag value={t('agent.scheduleOff')} />
    if (app.lastError) return <Tag value={t('agent.serverError')} tone="bad" />
    if (app.running) return <Tag value={t('agent.chatAppRunning')} tone="good" />
    return <Tag value={t('agent.chatAppStarting')} tone="warn" />
  }
  return (
    <>
      <SettingsSection card title={t('agent.chatApps')} description={t('agent.chatAppsHint')}>
        {CHAT_APP_KINDS.map((kind) => {
          const app = byKind.get(kind)
          const detail: string[] = []
          if (app?.botName) detail.push(app.botName)
          if (app?.linked) detail.push(t('agent.chatAppLinked', { name: app.linkedName || '' }))
          else if (app?.linkCode) detail.push(t('agent.chatAppLink', { code: app.linkCode }))
          if (app?.lastError) detail.push(app.lastError)
          return (
            <SettingsRow
              key={kind}
              title={nameOf(kind)}
              badge={stateOf(app)}
              subtitle={detail.length > 0 ? detail.join(' · ') : howOf(kind)}
              actions={
                <>
                  {app && (
                    <button type="button" disabled={busy === kind} onClick={() => void setApp(kind, '', !app.enabled)}>
                      {app.enabled ? t('agent.chatAppTurnOff') : t('agent.chatAppTurnOn')}
                    </button>
                  )}
                  {app?.linked && (
                    <button type="button" disabled={busy === kind} onClick={() => void unlink(kind)}>
                      {t('agent.chatAppUnlink')}
                    </button>
                  )}
                  <button type="button" disabled={busy === kind} onClick={() => setEditing(kind)}>
                    {app ? t('agent.chatAppReplaceToken') : t('agent.chatAppSetToken')}
                  </button>
                  {app && (
                    <button type="button" className="danger" disabled={busy === kind} onClick={() => void remove(kind)}>
                      {t('common.remove')}
                    </button>
                  )}
                </>
              }
            />
          )
        })}
      </SettingsSection>
      {editing && (
        <FormDialog
          title={t('agent.chatAppTokenTitle', { name: nameOf(editing) })}
          submitLabel={t('common.save')}
          busy={busy === editing}
          error={problem}
          canSubmit={token.trim() !== ''}
          onClose={() => {
            setEditing(null)
            setToken('')
            setProblem(null)
          }}
          onSubmit={() => void setApp(editing, token.trim())}
        >
          <p className="muted">{howOf(editing)}</p>
          <label>
            <span>{t('agent.chatAppToken')}</span>
            <input
              type="password"
              value={token}
              autoComplete="off"
              onChange={(event) => setToken(event.target.value)}
            />
          </label>
        </FormDialog>
      )}
    </>
  )
}

function ServersCard() {
  const { t } = useTranslation()
  const toast = useToast()
  const { data, error, reload } = useQuery(() => graphql<{ ListAgentServers: AgentServer[] }>(SERVERS, {}), [], {
    refresh: false,
  })
  const [connecting, setConnecting] = useState<AgentServer | null>(null)
  const [credential, setCredential] = useState('')
  const [problem, setProblem] = useState<string | null>(null)
  const [busy, setBusy] = useState<string | null>(null)

  // Back from an authorization: the code and state are in the address.
  useEffect(() => {
    const parameters = new URLSearchParams(window.location.search)
    const server = parameters.get('connect')
    const code = parameters.get('code')
    const state = parameters.get('state')
    if (!server || !code || !state) return
    window.history.replaceState(null, '', window.location.pathname)
    graphql<{ FinishAgentServerOAuth: AgentServer }>(FINISH_OAUTH, { server, code, state })
      .then((response) => {
        if (response.FinishAgentServerOAuth.status === 'connected') {
          toast.done(t('agent.serverConnected', { name: server }))
        } else {
          toast.failed(response.FinishAgentServerOAuth.lastError ?? t('agent.serverFailed'))
        }
        void reload()
      })
      .catch((caught) => toast.failed(messageOf(caught)))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const authorize = async (server: AgentServer) => {
    setBusy(server.name)
    try {
      const redirect = `${window.location.origin}/settings/agent?connect=${encodeURIComponent(server.name)}`
      const response = await graphql<{ BeginAgentServerOAuth: string }>(BEGIN_OAUTH, {
        server: server.name,
        redirectUrl: redirect,
      })
      window.location.assign(response.BeginAgentServerOAuth)
    } catch (caught) {
      toast.failed(messageOf(caught))
      setBusy(null)
    }
  }
  const connect = async (server: AgentServer, secret: string) => {
    setBusy(server.name)
    setProblem(null)
    try {
      const response = await graphql<{ ConnectAgentServer: AgentServer }>(CONNECT_SERVER, {
        server: server.name,
        credential: secret || undefined,
      })
      if (response.ConnectAgentServer.status === 'error') {
        setProblem(response.ConnectAgentServer.lastError ?? t('agent.serverFailed'))
        return
      }
      setConnecting(null)
      setCredential('')
      toast.done(t('agent.serverConnected', { name: server.name }))
      await reload()
    } catch (caught) {
      setProblem(messageOf(caught))
    } finally {
      setBusy(null)
    }
  }
  const disconnect = async (server: AgentServer) => {
    setBusy(server.name)
    try {
      await graphql(DISCONNECT_SERVER, { server: server.name })
      await reload()
    } catch (caught) {
      toast.failed(messageOf(caught))
    } finally {
      setBusy(null)
    }
  }
  const servers = data?.ListAgentServers ?? []
  if (error || servers.length === 0) {
    return null
  }
  const stateOf = (server: AgentServer, needsPerson: boolean) => {
    if (!server.enabled) return <Tag value={t('agent.scheduleOff')} />
    if (!needsPerson) return <Tag value={t('agent.serverShared')} tone="good" />
    switch (server.status) {
      case 'connected':
        return <Tag value={t('agent.serverConnectedState')} tone="good" />
      case 'error':
        return <Tag value={t('agent.serverError')} tone="bad" />
      case 'pending':
        return <Tag value={t('agent.serverPending')} tone="warn" />
      default:
        return <Tag value={t('agent.serverNotConnected')} />
    }
  }
  return (
    <>
      <SettingsSection card title={t('agent.servers')} description={t('agent.serversHint')}>
        {servers.map((server) => {
          const needsPerson = server.auth === 'user' || server.auth === 'oauth'
          const connected = server.status === 'connected'
          const detail = [server.transport]
          // The state beside the name is one word, so a server nobody has to
          // connect to says why here rather than in a tag the width of the
          // column.
          if (!needsPerson) detail.push(t('agent.serverSharedHint'))
          if (server.headless) detail.push(t('agent.serverHeadless'))
          if (server.status === 'error' && server.lastError) detail.push(server.lastError)
          return (
            <SettingsRow
              key={server.name}
              title={server.name}
              badge={stateOf(server, needsPerson)}
              subtitle={detail.join(' · ')}
              actions={
                needsPerson ? (
                  connected ? (
                    <button
                      type="button"
                      className="danger"
                      disabled={busy === server.name}
                      onClick={() => void disconnect(server)}
                    >
                      {t('agent.serverDisconnect')}
                    </button>
                  ) : (
                    <button
                      type="button"
                      disabled={busy === server.name || !server.enabled}
                      onClick={() => {
                        if (server.auth === 'oauth') {
                          void authorize(server)
                        } else {
                          setCredential('')
                          setProblem(null)
                          setConnecting(server)
                        }
                      }}
                    >
                      {server.auth === 'oauth' ? t('agent.serverAuthorize') : t('agent.serverConnect')}
                    </button>
                  )
                ) : undefined
              }
            />
          )
        })}
      </SettingsSection>
      {connecting ? (
        <FormDialog
          title={`${t('agent.serverConnect')} · ${connecting.name}`}
          submitLabel={t('agent.serverConnect')}
          busy={busy === connecting.name}
          error={problem}
          canSubmit={credential.trim() !== ''}
          onClose={() => setConnecting(null)}
          onSubmit={() => void connect(connecting, credential.trim())}
        >
          <label>
            <span>{t('agent.serverCredential')}</span>
            <input
              autoFocus
              type="password"
              value={credential}
              onChange={(event) => setCredential(event.target.value)}
            />
          </label>
        </FormDialog>
      ) : null}
    </>
  )
}

// SkillSecretsCard is what the installed skills ask this person for. A
// skill's author says which of its secrets are the deployment's and which
// are each person's own; the operator fills in the first kind in the
// settings, and this is where somebody fills in the second.
interface SkillSecret {
  skill: string
  key: string
  description: string
  set: boolean
}

const SKILL_SECRETS = `query { ListAgentSkillSecrets { skill key description set } }`

const SET_SKILL_SECRET = `
  mutation ($skill: String!, $key: String!, $value: String!) {
    SetAgentSkillSecret(skill: $skill, key: $key, value: $value) { skill key set }
  }`

const CLEAR_SKILL_SECRET = `mutation ($skill: String!, $key: String!) { ClearAgentSkillSecret(skill: $skill, key: $key) }`

function SkillSecretsCard() {
  const { t } = useTranslation()
  const toast = useToast()
  const { data, error, reload } = useQuery(
    () => graphql<{ ListAgentSkillSecrets: SkillSecret[] }>(SKILL_SECRETS, {}),
    [],
    { refresh: false },
  )
  const [filling, setFilling] = useState<SkillSecret | null>(null)
  const [value, setValue] = useState('')
  const [busy, setBusy] = useState<string | null>(null)

  const asked = data?.ListAgentSkillSecrets ?? []
  // Nothing installed asks for anything of theirs: no card at all, rather
  // than an empty one to wonder about.
  if (error || asked.length === 0) return null

  const keep = async (secret: SkillSecret, given: string) => {
    setBusy(secret.key)
    try {
      await graphql(SET_SKILL_SECRET, { skill: secret.skill, key: secret.key, value: given })
      toast.done(t('agent.skillSecretKept', { key: secret.key, skill: secret.skill }))
      setFilling(null)
      void reload()
    } catch (caught) {
      toast.failure(caught, t('agent.skillSecretFailed'))
    } finally {
      setBusy(null)
    }
  }

  const forget = async (secret: SkillSecret) => {
    setBusy(secret.key)
    try {
      await graphql(CLEAR_SKILL_SECRET, { skill: secret.skill, key: secret.key })
      toast.done(t('agent.skillSecretForgotten', { key: secret.key }))
      void reload()
    } catch (caught) {
      toast.failure(caught, t('agent.skillSecretFailed'))
    } finally {
      setBusy(null)
    }
  }

  return (
    <>
      <SettingsSection card title={t('agent.skillSecrets')} description={t('agent.skillSecretsHint')}>
        {asked.map((secret) => (
          <SettingsRow
            key={`${secret.skill}.${secret.key}`}
            title={secret.key}
            badge={<Tag value={secret.skill} />}
            subtitle={secret.description || undefined}
            actions={
              <>
                <button
                  type="button"
                  className={secret.set ? '' : 'primary'}
                  disabled={busy === secret.key}
                  onClick={() => {
                    setValue('')
                    setFilling(secret)
                  }}
                >
                  {secret.set ? t('agent.skillSecretReplace') : t('agent.skillSecretSet')}
                </button>
                {secret.set ? (
                  <button
                    type="button"
                    className="danger"
                    disabled={busy === secret.key}
                    onClick={() => void forget(secret)}
                  >
                    {t('agent.skillSecretForget')}
                  </button>
                ) : null}
              </>
            }
          />
        ))}
      </SettingsSection>
      {filling ? (
        <FormDialog
          title={`${filling.key} · ${filling.skill}`}
          submitLabel={t('agent.skillSecretSet')}
          busy={busy === filling.key}
          canSubmit={value.trim() !== ''}
          onClose={() => setFilling(null)}
          onSubmit={() => void keep(filling, value.trim())}
        >
          <label>
            <span>{filling.description || filling.key}</span>
            <input autoFocus type="password" value={value} onChange={(event) => setValue(event.target.value)} />
          </label>
        </FormDialog>
      ) : null}
    </>
  )
}

const CORRECTIONS = `
  query {
    ListAgentCorrections(first: 50) { id createdAt kind said }
  }`

// LearnedCard is what the agent filed after the person's recent
// conversations, newest first, with a way to strike anything it should
// not have kept.
//
// The whole of what it knows is its own page; this is the window onto the
// last day of it, because that is the part somebody actually wants to
// check. And striking is the only correction the filing run ever gets:
// it is shown what was struck as an example of what not to keep.
function LearnedCard() {
  const { t } = useTranslation()
  const toast = useToast()
  const { data, error, loading, reload } = useQuery(
    () => graphql<{ ListAgentLearned: LearnedFact[] }>(LEARNED, { days: 2, first: 40 }),
    [],
    { refresh: false },
  )
  const [striking, setStriking] = useState<LearnedFact | null>(null)
  const [busy, setBusy] = useState(false)
  const facts = data?.ListAgentLearned ?? []

  const strike = async (row: LearnedFact) => {
    setBusy(true)
    try {
      await graphql(STRIKE_FACT, { path: row.path, number: row.fact.number })
      toast.done(t('knowledge.struck'))
      await reload()
    } catch (caught) {
      toast.failed(messageOf(caught))
    } finally {
      setBusy(false)
      setStriking(null)
    }
  }

  return (
    <>
      <SettingsSection
        card
        title={t('agent.learned')}
        description={t('agent.learnedHint')}
        action={
          <Link className="button" to="/settings/knowledge">
            {t('agent.learnedOpen')}
          </Link>
        }
      >
        {error ? <ErrorMessage error={error} /> : null}
        {loading && !data ? <Loading /> : null}
        {data && facts.length === 0 ? <SettingsEmpty>{t('agent.noLearned')}</SettingsEmpty> : null}
        {facts.map((row) => (
          <SettingsRow
            key={row.fact.id}
            title={row.fact.text}
            badge={
              <>
                <Link className="tag knowledge-path" to={`/settings/knowledge/${row.path}`}>
                  {row.path}#{row.fact.number}
                </Link>
                {row.fact.inferred ? <Tag value={t('knowledge.inferred')} tone="warn" /> : null}
              </>
            }
            subtitle={row.fact.evidence[0]?.quote ? `\u201c${row.fact.evidence[0].quote}\u201d` : undefined}
            actions={
              <button type="button" className="danger" onClick={() => setStriking(row)}>
                {t('knowledge.strike')}
              </button>
            }
          />
        ))}
      </SettingsSection>
      {striking ? (
        <ConfirmDialog
          title={t('knowledge.strike')}
          body={t('knowledge.strikeBody', { text: striking.fact.text })}
          confirmLabel={t('knowledge.strike')}
          busy={busy}
          onClose={() => setStriking(null)}
          onConfirm={() => void strike(striking)}
        />
      ) : null}
    </>
  )
}

interface Schedule {
  id: string
  name: string
  cron: string
  prompt: string
  deliver: string
  enabled: boolean
  lastRunAt?: string | null
  nextRunAt?: string | null
}

// KnowledgeSourcesCard is the places the person has pointed their agent
// at, and how far each has got.
//
// The state of a source is worth as much as the list of them: a first
// pass over a checkout is a night's work, and a source that is waiting
// for a laptop to come back says so rather than looking broken.
function KnowledgeSourcesCard() {
  const { t } = useTranslation()
  const toast = useToast()
  const { data, error, loading, reload } = useQuery(
    () => graphql<{ ListAgentKnowledgeSources: KnowledgeSource[] }>(KNOWLEDGE_SOURCES, {}),
    [],
    { refresh: true },
  )
  const [adding, setAdding] = useState(false)
  const [removing, setRemoving] = useState<KnowledgeSource | null>(null)
  const [busy, setBusy] = useState(false)
  const [problem, setProblem] = useState<string | null>(null)
  const [name, setName] = useState('')
  const [kind, setKind] = useState('computer')
  const [computer, setComputer] = useState('')
  const [path, setPath] = useState('')
  const [format, setFormat] = useState('files')

  const sources = data?.ListAgentKnowledgeSources ?? []

  const run = async (document: string, variables: Record<string, unknown>, said: string) => {
    setBusy(true)
    setProblem(null)
    try {
      await graphql(document, variables)
      toast.done(said)
      await reload()
      return true
    } catch (caught) {
      setProblem(messageOf(caught))
      toast.failed(messageOf(caught))
      return false
    } finally {
      setBusy(false)
    }
  }

  return (
    <>
      <SettingsSection
        card
        title={t('agent.sourcesKnowledge')}
        description={t('agent.sourcesKnowledgeHint')}
        action={
          <button
            type="button"
            className="primary"
            onClick={() => {
              setName('')
              setPath('')
              setComputer('')
              setKind('computer')
              setFormat('files')
              setProblem(null)
              setAdding(true)
            }}
          >
            {t('agent.addKnowledgeSource')}
          </button>
        }
      >
        {error ? <ErrorMessage error={error} /> : null}
        {loading && !data ? <Loading /> : null}
        {data && sources.length === 0 ? <SettingsEmpty>{t('agent.noKnowledgeSources')}</SettingsEmpty> : null}
        {sources.map((source) => (
          <SettingsRow
            key={source.id}
            title={source.name}
            badge={
              <>
                <Tag value={t(`agent.knowledgeKind.${source.kind}` as 'agent.knowledgeKind.computer')} />
                {source.more ? <Tag value={t('agent.knowledgeReading')} tone="good" /> : null}
                {!source.enabled ? <Tag value={t('agent.knowledgePausedBadge')} tone="warn" /> : null}
              </>
            }
            subtitle={
              <>
                {source.specification.path}
                {source.specification.computer ? ` · ${source.specification.computer}` : ''}
                <br />
                {t('agent.knowledgeCounts', { documents: source.documentCount, chunks: source.chunkCount })}
                {source.refusedCount > 0 ? ` · ${t('agent.knowledgeRefused', { count: source.refusedCount })}` : ''}
                {/* What pausing means, said where the pause is: the
                    index is kept, so resuming does not start the first
                    pass over again. */}
                {!source.enabled ? (
                  <>
                    <br />
                    <span className="muted">{t('agent.knowledgePaused')}</span>
                  </>
                ) : null}
                {source.lastError ? (
                  <>
                    <br />
                    <span className="muted">{source.lastError}</span>
                  </>
                ) : null}
                {/* Whose commits it could not place. Shown before the
                    directories held back, because this one makes every
                    answer built on the source quietly empty and takes a
                    minute to fix. */}
                {source.unknownAuthors.length > 0 ? (
                  <>
                    <br />
                    <span className="muted">
                      {t('agent.knowledgeUnknownAuthors', { names: source.unknownAuthors.join(', ') })}
                    </span>{' '}
                    <Link className="link" to="/mailbox/contacts">
                      {t('agent.knowledgeWhichIsYou')}
                    </Link>
                  </>
                ) : null}
                {source.sensitive.filter((held) => !source.allowed.includes(held)).length > 0 ? (
                  <>
                    <br />
                    {t('agent.knowledgeHeldBack', {
                      names: source.sensitive.filter((held) => !source.allowed.includes(held)).join(', '),
                    })}{' '}
                    {source.sensitive
                      .filter((held) => !source.allowed.includes(held))
                      .map((held) => (
                        <button
                          key={held}
                          type="button"
                          className="link"
                          onClick={() =>
                            void run(
                              ALLOW_KNOWLEDGE_DIRECTORY,
                              { sourceId: source.id, name: held },
                              t('agent.knowledgeAllowed', { name: held }),
                            )
                          }
                        >
                          {t('agent.knowledgeAllow')} {held}
                        </button>
                      ))}
                  </>
                ) : null}
              </>
            }
            actions={
              <div className="row-actions">
                <button
                  type="button"
                  className="icon-action"
                  title={t('agent.knowledgeSync')}
                  aria-label={`${source.name}: ${t('agent.knowledgeSync')}`}
                  onClick={() => void run(SYNC_KNOWLEDGE_SOURCE, { sourceId: source.id }, t('agent.knowledgeSyncing'))}
                >
                  <RefreshIcon size={16} />
                </button>
                <button
                  type="button"
                  className="icon-action"
                  title={source.enabled ? t('agent.knowledgePause') : t('agent.knowledgeResume')}
                  aria-label={`${source.name}: ${source.enabled ? t('agent.knowledgePause') : t('agent.knowledgeResume')}`}
                  onClick={() =>
                    void run(
                      SAVE_KNOWLEDGE_SOURCE,
                      { sourceId: source.id, enabled: !source.enabled },
                      source.enabled ? t('agent.knowledgePaused') : t('agent.knowledgeResumed'),
                    )
                  }
                >
                  {source.enabled ? <ToggleOnIcon size={16} /> : <ToggleOffIcon size={16} />}
                </button>
                <button
                  type="button"
                  className="icon-action danger"
                  title={t('agent.knowledgeRemove')}
                  aria-label={`${source.name}: ${t('agent.knowledgeRemove')}`}
                  onClick={() => setRemoving(source)}
                >
                  <TrashIcon size={16} />
                </button>
              </div>
            }
          />
        ))}
      </SettingsSection>
      {adding ? (
        <FormDialog
          title={t('agent.addKnowledgeSource')}
          submitLabel={t('agent.addKnowledgeSource')}
          busy={busy}
          error={problem}
          canSubmit={name.trim() !== '' && (kind === 'sent' || (path.trim() !== '' && computer.trim() !== ''))}
          onClose={() => setAdding(false)}
          onSubmit={() => {
            void (async () => {
              if (
                await run(
                  SAVE_KNOWLEDGE_SOURCE,
                  { name: name.trim(), kind, computer: computer.trim(), path: path.trim(), format },
                  t('agent.knowledgeSaved'),
                )
              ) {
                setAdding(false)
              }
            })()
          }}
        >
          <label>
            <span>{t('agent.knowledgeName')}</span>
            <input value={name} onChange={(event) => setName(event.target.value)} />
          </label>
          <label>
            <span>{t('agent.knowledgeKind')}</span>
            <select value={kind} onChange={(event) => setKind(event.target.value)}>
              {['computer', 'archive', 'sent'].map((value) => (
                <option key={value} value={value}>
                  {t(`agent.knowledgeKind.${value}` as 'agent.knowledgeKind.computer')}
                </option>
              ))}
            </select>
          </label>
          {kind !== 'sent' ? (
            <>
              <label>
                <span>{t('agent.knowledgeComputer')}</span>
                <input value={computer} onChange={(event) => setComputer(event.target.value)} />
              </label>
              <label>
                <span>{t('agent.knowledgePath')}</span>
                <input value={path} placeholder="~/projects" onChange={(event) => setPath(event.target.value)} />
              </label>
              <label>
                <span>{t('agent.knowledgeFormat')}</span>
                <select value={format} onChange={(event) => setFormat(event.target.value)}>
                  {['files', 'mattermost', 'journal'].map((value) => (
                    <option key={value} value={value}>
                      {t(`agent.knowledgeFormat.${value}` as 'agent.knowledgeFormat.files')}
                    </option>
                  ))}
                </select>
              </label>
              <p className="muted">{t('agent.knowledgeAllowFirst', { path: path.trim() || '~/projects' })}</p>
            </>
          ) : null}
        </FormDialog>
      ) : null}
      {removing ? (
        <ConfirmDialog
          title={t('agent.knowledgeRemove')}
          body={t('agent.knowledgeRemoveBody', { name: removing.name, documents: removing.documentCount })}
          confirmLabel={t('agent.knowledgeRemove')}
          busy={busy}
          onClose={() => setRemoving(null)}
          onConfirm={() => {
            void (async () => {
              await run(DELETE_KNOWLEDGE_SOURCE, { sourceId: removing.id }, t('agent.knowledgeRemoved'))
              setRemoving(null)
            })()
          }}
        />
      ) : null}
    </>
  )
}

// DreamCard is what the agent did overnight.
//
// The backlog is the number that matters: a night that could not get
// through everything says how much is left and roughly how long it will
// take, so the person can decide whether to give it more of the day
// rather than being quietly a fortnight behind.
function DreamCard({
  agent,
  busy,
  onChange,
}: {
  agent: NonNullable<AgentView['agent']>
  busy: boolean
  onChange: (variables: Record<string, unknown>, done: string) => Promise<void>
}) {
  const { t } = useTranslation()
  const toast = useToast()
  const { data, error, loading, reload } = useQuery(() => graphql<{ ListAgentDreams: Dream[] }>(DREAMS, {}), [], {
    refresh: false,
  })
  const dreams = data?.ListAgentDreams ?? []
  const [starting, setStarting] = useState(false)

  // Asking for the night now only moves it to the next tick, and only
  // within the hours below: a run that rewrites pages while somebody is
  // reading them is the thing those hours exist to prevent.
  const startNow = async () => {
    setStarting(true)
    try {
      await graphql(DREAM_NOW, {})
      toast.done(t('agent.dreamNowAsked'))
      await reload()
    } catch (caught) {
      toast.failed(messageOf(caught))
    } finally {
      setStarting(false)
    }
  }

  return (
    <SettingsSection
      card
      title={t('agent.dream')}
      description={t('agent.dreamHint')}
      action={
        <button type="button" disabled={starting} onClick={() => void startNow()}>
          {t('agent.dreamNow')}
        </button>
      }
    >
      {/* When the night is. A person who works at two in the morning
          should be able to move it rather than have a page rewritten
          under them while they read it. */}
      <SettingsRow title={t('agent.dreamWindow')} subtitle={t('agent.dreamWindowHint')} />
      <div className="row">
        <label className="shrink">
          <span>{t('agent.dreamFrom')}</span>
          <input
            className="narrow"
            type="time"
            disabled={busy}
            value={agent.dreamFrom || '01:00'}
            onChange={(event) => void onChange({ dreamFrom: event.target.value }, t('agent.saved'))}
          />
        </label>
        <label className="shrink">
          <span>{t('agent.dreamUntil')}</span>
          <input
            className="narrow"
            type="time"
            disabled={busy}
            value={agent.dreamUntil || '06:00'}
            onChange={(event) => void onChange({ dreamUntil: event.target.value }, t('agent.saved'))}
          />
        </label>
      </div>
      {error ? <ErrorMessage error={error} /> : null}
      {loading && !data ? <Loading /> : null}
      {data && dreams.length === 0 ? <SettingsEmpty>{t('agent.noDreams')}</SettingsEmpty> : null}
      {dreams.map((dream) => {
        const did = [
          dream.digested > 0 ? t('agent.dreamRead', { count: dream.digested }) : '',
          dream.filed > 0 ? t('agent.dreamFiled', { count: dream.filed }) : '',
          dream.rewritten > 0 ? t('agent.dreamRewritten', { count: dream.rewritten }) : '',
          dream.merged > 0 ? t('agent.dreamMerged', { count: dream.merged }) : '',
          dream.moved > 0 ? t('agent.dreamMoved', { count: dream.moved }) : '',
          dream.dormant > 0 ? t('agent.dreamRetired', { count: dream.dormant }) : '',
          dream.embedded > 0 ? t('agent.dreamEmbedded', { count: dream.embedded }) : '',
          dream.strengthened > 0 ? t('agent.dreamStrengthened', { count: dream.strengthened }) : '',
          dream.associated > 0 ? t('agent.dreamAssociated', { count: dream.associated }) : '',
          dream.rehearsed > 0 ? t('agent.dreamRehearsed', { count: dream.rehearsed }) : '',
          dream.revised > 0 ? t('agent.dreamRevised', { count: dream.revised }) : '',
        ].filter(Boolean)
        return (
          <SettingsRow
            key={dream.id}
            title={formatTime(dream.startedAt)}
            badge={dream.coarse ? <Tag value={t('agent.knowledgeReading')} tone="warn" /> : null}
            subtitle={
              <>
                {did.length > 0 ? did.join(' · ') : t('agent.dreamNothing')}
                {dream.backlog > 0 ? (
                  <>
                    <br />
                    {t('agent.dreamBacklog', {
                      count: dream.backlog,
                      nights: Math.max(1, Math.ceil(dream.backlog / 400)),
                    })}
                  </>
                ) : null}
                {dream.coarse ? (
                  <>
                    <br />
                    {t('agent.dreamCoarse')}
                  </>
                ) : null}
                {dream.proposals.map((proposal, index) => (
                  <span key={`${proposal.kind}-${proposal.path}-${proposal.to}-${index}`}>
                    <br />
                    {proposal.kind === 'gap'
                      ? t('agent.dreamGap', { question: proposal.reason })
                      : proposal.kind === 'linked'
                        ? t('agent.dreamLinked', {
                            path: proposal.path,
                            to: proposal.to,
                            reason: proposal.reason,
                          })
                        : t('agent.dreamSuggests', { path: proposal.path, under: proposal.to })}
                  </span>
                ))}
                {/* What it asked itself and what it could not answer, in
                    its own words. Not translated: the night wrote them,
                    and it writes in the language the agent is set to. */}
                {(dream.notes || '')
                  .split('\n')
                  .map((line) => line.trim())
                  .filter(Boolean)
                  .map((line, index) => (
                    <span key={`note-${index}`} className="muted">
                      <br />
                      {line}
                    </span>
                  ))}
                {dream.lastError ? (
                  <>
                    <br />
                    <span className="muted">{dream.lastError}</span>
                  </>
                ) : null}
              </>
            }
          />
        )
      })}
    </SettingsSection>
  )
}

function BriefCard() {
  const { t } = useTranslation()
  const toast = useToast()
  const { data, error, loading, reload } = useQuery(
    () => graphql<{ ListAgentSchedules: Schedule[] }>(SCHEDULES, {}),
    [],
  )
  const [busy, setBusy] = useState(false)
  const brief = (data?.ListAgentSchedules ?? []).find((schedule) => schedule.name === 'Daily brief')
  const [at, setAt] = useState('')
  const [days, setDays] = useState('')

  // What the cron line says, for the two fields: "30 7 * * 1-5".
  const fields = (brief?.cron ?? '30 7 * * 1-5').split(/\s+/)
  const time = at || `${(fields[1] ?? '7').padStart(2, '0')}:${(fields[0] ?? '30').padStart(2, '0')}`
  const chosenDays = days || (fields[4] ?? '1-5')

  const save = async (enabled: boolean) => {
    setBusy(true)
    try {
      const parsed = chosenDays
        .split(',')
        .flatMap((part) => {
          const range = part.split('-')
          if (range.length === 2) {
            const from = Number(range[0])
            const until = Number(range[1])
            return Number.isNaN(from) || Number.isNaN(until)
              ? []
              : Array.from({ length: until - from + 1 }, (_, index) => from + index)
          }
          const one = Number(part)
          return Number.isNaN(one) ? [] : [one === 0 ? 7 : one]
        })
        .filter((day) => day >= 1 && day <= 7)
      await graphql(SET_BRIEF, { enabled, at: time, days: parsed.length > 0 ? parsed : undefined })
      toast.done(enabled ? t('agent.briefOn') : t('agent.briefOff'))
      await reload()
    } catch (caught) {
      toast.failed(messageOf(caught))
    } finally {
      setBusy(false)
    }
  }

  const sendNow = async () => {
    setBusy(true)
    try {
      await graphql(RUN_BRIEF, {})
      toast.done(t('agent.briefSending'))
    } catch (caught) {
      toast.failed(messageOf(caught))
    } finally {
      setBusy(false)
    }
  }

  return (
    <SettingsSection card title={t('agent.brief')} description={t('agent.briefHint')}>
      {error ? <ErrorMessage error={error} /> : null}
      {loading && !data ? <Loading /> : null}
      <div className="form-narrow">
        <div className="row">
          <label>
            <span>{t('agent.briefAt')}</span>
            <input type="time" value={time} disabled={busy} onChange={(change) => setAt(change.target.value)} />
          </label>
          <label>
            <span>{t('agent.briefDays')}</span>
            <input
              value={chosenDays}
              disabled={busy}
              placeholder="1-5"
              onChange={(change) => setDays(change.target.value)}
            />
          </label>
        </div>
        <div className="page-actions page-actions-end">
          {brief?.enabled ? (
            <>
              <button type="button" disabled={busy} onClick={() => void sendNow()}>
                {t('agent.briefNow')}
              </button>
              <button type="button" disabled={busy} onClick={() => void save(false)}>
                {t('agent.briefStop')}
              </button>
              <button type="button" className="primary" disabled={busy} onClick={() => void save(true)}>
                {t('agent.briefSave')}
              </button>
            </>
          ) : (
            <button type="button" className="primary" disabled={busy} onClick={() => void save(true)}>
              {t('agent.briefStart')}
            </button>
          )}
        </div>
        <p className="muted">{brief ? t('agent.briefWhere') : t('agent.briefNotYet')}</p>
      </div>
    </SettingsSection>
  )
}

function SchedulesCard() {
  const { t } = useTranslation()
  const toast = useToast()
  const { data, error, loading, reload } = useQuery(
    () => graphql<{ ListAgentSchedules: Schedule[] }>(SCHEDULES, {}),
    [],
  )
  const [adding, setAdding] = useState(false)
  const [editing, setEditing] = useState<string | null>(null)
  const [name, setName] = useState('')
  const [cron, setCron] = useState('0 8 * * 1-5')
  const [prompt, setPrompt] = useState('')
  const [deliver, setDeliver] = useState('drawer')
  const [busy, setBusy] = useState(false)
  const [problem, setProblem] = useState<string | null>(null)
  const failed = (caught: unknown) => toast.failed(messageOf(caught))
  const open = (schedule?: Schedule) => {
    setEditing(schedule?.id ?? null)
    setName(schedule?.name ?? '')
    setCron(schedule?.cron ?? '0 8 * * 1-5')
    setPrompt(schedule?.prompt ?? '')
    setDeliver(schedule?.deliver ?? 'drawer')
    setProblem(null)
    setAdding(true)
  }
  const add = async () => {
    setBusy(true)
    setProblem(null)
    try {
      await graphql(SAVE_SCHEDULE, {
        scheduleId: editing ?? undefined,
        name: name.trim(),
        cron: cron.trim(),
        prompt: prompt.trim(),
        deliver,
        ...(editing ? {} : { enabled: true }),
      })
      setAdding(false)
      setName('')
      setPrompt('')
      await reload()
    } catch (caught) {
      setProblem(messageOf(caught))
    } finally {
      setBusy(false)
    }
  }
  const toggle = async (schedule: Schedule) => {
    try {
      await graphql(SAVE_SCHEDULE, { scheduleId: schedule.id, enabled: !schedule.enabled })
      await reload()
    } catch (caught) {
      failed(caught)
    }
  }
  const remove = async (schedule: Schedule) => {
    try {
      await graphql(DELETE_SCHEDULE, { scheduleId: schedule.id })
      await reload()
    } catch (caught) {
      failed(caught)
    }
  }
  const run = async (schedule: Schedule) => {
    try {
      await graphql(RUN_SCHEDULE, { scheduleId: schedule.id })
      toast.done(t('agent.scheduleQueued'))
    } catch (caught) {
      failed(caught)
    }
  }
  const schedules = data?.ListAgentSchedules ?? []
  return (
    <>
      <SettingsSection
        card
        title={t('agent.schedules')}
        description={t('agent.schedulesHint')}
        action={
          <button type="button" className="primary" onClick={() => open()}>
            {t('agent.addSchedule')}
          </button>
        }
      >
        {error ? <ErrorMessage error={error} /> : null}
        {loading && !data ? <Loading /> : null}
        {data && schedules.length === 0 ? <SettingsEmpty>{t('agent.noSchedules')}</SettingsEmpty> : null}
        {schedules.map((schedule) => (
          <SettingsRow
            key={schedule.id}
            title={schedule.name}
            badge={
              <>
                <code className="tag">{schedule.cron}</code>
                {!schedule.enabled ? <Tag value={t('agent.scheduleOff')} tone="warn" /> : null}
              </>
            }
            subtitle={
              <>
                {schedule.prompt}
                <br />
                {schedule.deliver === 'mail' ? t('agent.deliverMail') : t('agent.deliverDrawer')}
                {schedule.nextRunAt ? ` · ${t('agent.scheduleNext', { time: formatTime(schedule.nextRunAt) })}` : ''}
              </>
            }
            actions={
              <div className="row-actions">
                <button
                  type="button"
                  className="icon-action"
                  title={t('agent.editSchedule')}
                  aria-label={`${schedule.name}: ${t('agent.editSchedule')}`}
                  onClick={() => open(schedule)}
                >
                  <PencilIcon size={16} />
                </button>
                <button
                  type="button"
                  className="icon-action"
                  title={t('agent.scheduleRun')}
                  aria-label={`${schedule.name}: ${t('agent.scheduleRun')}`}
                  onClick={() => void run(schedule)}
                >
                  <RefreshIcon size={16} />
                </button>
                <button
                  type="button"
                  className="icon-action"
                  title={schedule.enabled ? t('agent.scheduleDisable') : t('agent.scheduleEnable')}
                  aria-label={`${schedule.name}: ${schedule.enabled ? t('agent.scheduleDisable') : t('agent.scheduleEnable')}`}
                  onClick={() => void toggle(schedule)}
                >
                  {schedule.enabled ? <ToggleOnIcon size={16} /> : <ToggleOffIcon size={16} />}
                </button>
                <button
                  type="button"
                  className="icon-action danger"
                  title={t('common.remove')}
                  aria-label={`${schedule.name}: ${t('common.remove')}`}
                  onClick={() => void remove(schedule)}
                >
                  <TrashIcon size={16} />
                </button>
              </div>
            }
          />
        ))}
      </SettingsSection>
      {adding ? (
        <FormDialog
          title={editing ? t('agent.editSchedule') : t('agent.addSchedule')}
          submitLabel={editing ? t('common.save') : t('agent.scheduleAdd')}
          busy={busy}
          error={problem}
          canSubmit={name.trim() !== '' && cron.trim() !== '' && prompt.trim() !== ''}
          onClose={() => setAdding(false)}
          onSubmit={() => void add()}
        >
          <label>
            <span>{t('agent.scheduleName')}</span>
            <input autoFocus value={name} onChange={(event) => setName(event.target.value)} />
          </label>
          <div className="row">
            <label>
              <span>{t('agent.scheduleCron')}</span>
              <input value={cron} placeholder="0 8 * * 1-5" onChange={(event) => setCron(event.target.value)} />
            </label>
            <label>
              <span>{t('agent.scheduleDeliver')}</span>
              <Select
                block
                value={deliver}
                label={t('agent.scheduleDeliver')}
                options={[
                  { value: 'drawer', label: t('agent.deliverDrawer') },
                  { value: 'mail', label: t('agent.deliverMail') },
                ]}
                onChange={setDeliver}
              />
            </label>
          </div>
          <p className="muted">{t('agent.scheduleCronHint')}</p>
          <label>
            <span>{t('agent.schedulePrompt')}</span>
            <textarea rows={3} value={prompt} onChange={(event) => setPrompt(event.target.value)} />
          </label>
        </FormDialog>
      ) : null}
    </>
  )
}

const RUNS = `
  query ($first: Int) {
    ListAgentRuns(first: $first) { id title jobKind lastAt }
  }`

// ActivityCard is what the agent did on its own: every run kept, newest
// first, as a table that pages and filters, each row a transcript the
// drawer opens. Runs are swept by the operator's retention, so what the
// query returns is the whole of what there is.
type Run = { id: string; title: string; jobKind: string; lastAt: string }

function ActivityCard() {
  const { t, plural } = useTranslation()
  const { data, error, loading } = useQuery(() => graphql<{ ListAgentRuns: Run[] }>(RUNS, { first: 1000 }), [], {
    refresh: false,
  })
  const runs = data?.ListAgentRuns ?? []
  if (error) {
    return null
  }
  const columns: Column<Run>[] = [
    {
      key: 'lastAt',
      header: t('agent.when'),
      width: '11rem',
      value: (run) => formatTime(run.lastAt),
      sort: (first, second) => first.lastAt.localeCompare(second.lastAt),
    },
    {
      key: 'jobKind',
      header: t('agent.runKind'),
      width: '8rem',
      filter: 'select',
      value: (run) => run.jobKind,
      render: (run) => <Tag value={run.jobKind} />,
    },
    { key: 'title', header: t('agent.runWhat'), filter: 'text', truncate: true, value: (run) => run.title },
    {
      key: 'open',
      header: '',
      width: '6rem',
      render: (run) => (
        <button
          type="button"
          onClick={() => {
            if (!openAgentConversation(run.id)) window.scrollTo(0, 0)
          }}
        >
          {t('agent.open')}
        </button>
      ),
    },
  ]
  return (
    <SettingsSection card title={t('agent.activity')} description={t('agent.activityHint')}>
      <DataTable
        columns={columns}
        rows={runs}
        rowKey={(run) => run.id}
        loading={loading && !data}
        emptyMessage={t('agent.noActivity')}
        countLabel={(count, filtering) =>
          filtering
            ? plural(count, { one: 'agent.runsFilteredOne', other: 'agent.runsFilteredOther' }, { total: runs.length })
            : plural(count, { one: 'agent.runsOne', other: 'agent.runsOther' })
        }
      />
    </SettingsSection>
  )
}

// CorrectionsCard is what the agent learned from the person's own hands.
function CorrectionsCard() {
  const { t } = useTranslation()
  const { data, error } = useQuery(
    () =>
      graphql<{ ListAgentCorrections: { id: string; createdAt: string; kind: string; said: string }[] }>(
        CORRECTIONS,
        {},
      ),
    [],
  )
  const corrections = data?.ListAgentCorrections ?? []
  if (error || corrections.length === 0) {
    return null
  }
  return (
    <SettingsSection card title={t('agent.corrections')} description={t('agent.correctionsHint')}>
      {corrections.map((correction) => (
        <SettingsRow key={correction.id} title={correction.said} subtitle={formatTime(correction.createdAt)} />
      ))}
    </SettingsSection>
  )
}

const REPLIES = `
  query ($first: Int) {
    ListAgentReplies(first: $first) {
      total
      replies { id createdAt mailboxId mailId status reason subject from to text sendAfter sentAt draftItemId }
    }
  }`

const CANCEL_REPLY = `
  mutation ($replyId: String!) {
    CancelAgentReply(replyId: $replyId) { id status }
  }`

// RepliesCard is what the agent answered for the person, and what it
// declined to: the one place every refusal and its reason is shown, so a
// message that went unanswered is never a mystery.
function RepliesCard() {
  const { t } = useTranslation()
  const toast = useToast()
  const { data, error, loading, reload } = useQuery(
    () => graphql<{ ListAgentReplies: { total: number; replies: AgentReply[] } }>(REPLIES, { first: 20 }),
    [],
  )
  const [busy, setBusy] = useState<string | null>(null)
  const cancel = async (replyId: string) => {
    setBusy(replyId)
    try {
      await graphql(CANCEL_REPLY, { replyId })
      toast.done(t('mailbox.heldReplyCancelled'))
      await reload()
    } catch (caught) {
      toast.failed(messageOf(caught))
    } finally {
      setBusy(null)
    }
  }
  const replies = data?.ListAgentReplies.replies ?? []
  const statusOf = (status: AgentReply['status']) => {
    switch (status) {
      case 'held':
        return <Tag value={t('agent.replyHeld')} tone="warn" />
      case 'sent':
        return <Tag value={t('agent.replySent')} tone="good" />
      case 'cancelled':
        return <Tag value={t('agent.replyCancelled')} />
      case 'refused':
        return <Tag value={t('agent.replyRefused')} />
      default:
        return <Tag value={t('agent.replyFailed')} tone="bad" />
    }
  }
  return (
    <SettingsSection card title={t('agent.answered')} description={t('agent.repliesHint')}>
      {error ? <ErrorMessage error={error} /> : null}
      {loading && !data ? <Loading /> : null}
      {data && replies.length === 0 ? <SettingsEmpty>{t('agent.noReplies')}</SettingsEmpty> : null}
      {replies.map((reply) => {
        const detail = [reply.to, formatTime(reply.sentAt ?? reply.createdAt)]
        if (reply.reason) detail.push(reply.reason)
        if (reply.status === 'held' && reply.sendAfter)
          detail.push(t('agent.replySends', { time: formatTime(reply.sendAfter) }))
        return (
          <SettingsRow
            key={reply.id}
            title={reply.subject}
            badge={statusOf(reply.status)}
            subtitle={detail.join(' · ')}
            actions={
              reply.status === 'held' ? (
                <button
                  type="button"
                  className="danger"
                  disabled={busy === reply.id}
                  onClick={() => void cancel(reply.id)}
                >
                  {t('mailbox.heldReplyCancel')}
                </button>
              ) : undefined
            }
          />
        )
      })}
    </SettingsSection>
  )
}

const TOOLS = `{ ListAgentTools { name family risk description confirms core actions } }`

// ConfirmForm: the tools the agent must always ask about first, by
// family, each with a word — as usual, or ask me first — and a word for
// the whole family its tools inherit.
function ConfirmForm({ agent, busy, onSave }: SaveProps) {
  const { t } = useTranslation()
  const { data } = useQuery(() => graphql<{ ListAgentTools: PolicyTool[] }>(TOOLS), [], { refresh: false })
  const tools = data?.ListAgentTools ?? []
  const families = useMemo(() => Array.from(new Set(tools.map((tool) => tool.family))), [tools])
  const [policy, setPolicy] = useState<Record<string, string>>({})
  useEffect(() => {
    setPolicy(Object.fromEntries(agent.confirm.map((name) => [name, 'confirm'])))
  }, [agent.confirm.join(',')])
  const options = [
    { value: 'allow', label: t('agent.policyUsual') },
    { value: 'confirm', label: t('agent.policyAsk') },
  ]
  return (
    <form
      className="settings-subform"
      onSubmit={(event) => {
        event.preventDefault()
        void onSave(
          {
            confirm: Object.entries(policy)
              .filter(([, word]) => word === 'confirm')
              .map(([name]) => name),
          },
          t('agent.saved'),
        )
      }}
    >
      <h4>{t('agent.confirmTools')}</h4>
      <p className="muted">{t('agent.confirmToolsHint')}</p>
      <ToolPolicyAccordion
        families={families}
        tools={tools}
        options={options}
        policy={policy}
        defaultWord="allow"
        onChange={(name, word) => setPolicy({ ...policy, [name]: word })}
      />
      <SaveRow busy={busy} saved={false} note={t('agent.saved')} />
    </form>
  )
}

const DEFAULT_POLICY: AgentMailboxPolicy = { granted: false, draftReplies: true, search: true, research: false }

// cleanedPolicy is a policy as the API takes it: granted, and the
// answering policy dropped when nothing in it is set.
function cleanedPolicy(policy: AgentMailboxPolicy): AgentMailboxPolicy {
  const reply = policy.autoReply ?? { enabled: false }
  return {
    ...policy,
    granted: true,
    autoReply:
      reply.enabled || reply.guidance
        ? {
            ...reply,
            hours:
              reply.when === 'outsideHours'
                ? (reply.hours ?? { from: '09:00', until: '17:30', days: [1, 2, 3, 4, 5] })
                : null,
          }
        : null,
  }
}

type PolicyProps = {
  policy: AgentMailboxPolicy
  allowed: Record<string, boolean>
  busy: boolean
  onSave: (change: Partial<AgentMailboxPolicy>) => Promise<unknown>
}

function Check({
  checked,
  disabled,
  label,
  onChange,
}: {
  checked: boolean
  disabled?: boolean
  label: string
  onChange: (checked: boolean) => void
}) {
  return (
    <label className="checkbox">
      <input
        type="checkbox"
        checked={checked}
        disabled={disabled}
        onChange={(event) => onChange(event.target.checked)}
      />
      {label}
    </label>
  )
}

// SourceCard is one mailbox as a source: whether the agent may reach it, and
// what it does there, one subject at a time with a Save for each. Also shown
// on the mailbox's own settings page.
function SourceCard({
  source,
  view,
  onChanged,
}: {
  source: AgentSource
  view: AgentView
  onChanged: () => Promise<unknown>
}) {
  const { t } = useTranslation()
  const toast = useToast()
  const [busy, setBusy] = useState(false)
  const [revoking, setRevoking] = useState(false)
  const stored = source.policy
  const granted = !!stored?.granted
  const policy = stored ?? DEFAULT_POLICY

  async function run(work: () => Promise<unknown>, done: string) {
    setBusy(true)
    try {
      await work()
      toast.done(done)
      await onChanged()
    } catch (caught) {
      toast.failed(messageOf(caught))
    } finally {
      setBusy(false)
    }
  }

  const savePolicy = (change: Partial<AgentMailboxPolicy>) =>
    run(
      () => graphql(GRANT, { mailboxId: source.mailboxId, policy: cleanedPolicy({ ...policy, ...change }) }),
      t('agent.saved'),
    )

  return (
    <div className="agent-source">
      <div className="settings-section-head">
        <div>
          <h4>{source.name}</h4>
          <p className="muted">{source.addresses.join(', ')}</p>
        </div>
        {granted ? (
          <button type="button" className="danger" disabled={busy} onClick={() => setRevoking(true)}>
            {t('agent.revoke')}
          </button>
        ) : (
          <button
            type="button"
            className="primary"
            disabled={busy || !view.agent}
            onClick={() => void run(() => graphql(GRANT, { mailboxId: source.mailboxId }), t('agent.granted'))}
          >
            {t('agent.grant')}
          </button>
        )}
      </div>
      {granted ? (
        <>
          <SortingForm policy={policy} allowed={view.allowed} busy={busy} onSave={savePolicy} />
          <SummariesForm policy={policy} allowed={view.allowed} busy={busy} onSave={savePolicy} />
          <HelpForm policy={policy} allowed={view.allowed} busy={busy} onSave={savePolicy} />
          <AnsweringForm policy={policy} allowed={view.allowed} busy={busy} onSave={savePolicy} />
        </>
      ) : (
        <p className="muted">{t('agent.notGranted')}</p>
      )}
      {revoking ? (
        <ConfirmDialog
          title={t('agent.revoke')}
          body={t('agent.revokeConfirm', { name: source.name })}
          confirmLabel={t('agent.revoke')}
          destructive
          busy={busy}
          onClose={() => setRevoking(false)}
          onConfirm={() => {
            void run(() => graphql(REVOKE, { mailboxId: source.mailboxId }), t('agent.revoked')).then(() =>
              setRevoking(false),
            )
          }}
        />
      ) : null}
    </div>
  )
}

// A calendar or an address book as a source: one switch, no policy. A
// mailbox carries what to do with what arrives in it; these carry nothing,
// because reading a diary is reading a diary.
export function CollectionRow({
  collection,
  view,
  onChanged,
}: {
  collection: AgentCollection
  view: AgentView
  onChanged: () => Promise<unknown>
}) {
  const { t } = useTranslation()
  const toast = useToast()
  const [busy, setBusy] = useState(false)

  const set = async (granted: boolean) => {
    setBusy(true)
    try {
      await graphql(GRANT_SOURCE, { kind: collection.kind, id: collection.id, granted })
      toast.done(granted ? t('agent.grantedCollection') : t('agent.revokedCollection'))
      await onChanged()
    } catch (caught) {
      toast.failed(messageOf(caught))
    } finally {
      setBusy(false)
    }
  }

  const what =
    collection.kind === 'calendar'
      ? t('agent.collection.events', { count: collection.items })
      : t('agent.collection.people', { count: collection.items })

  return (
    <div className="agent-source">
      <div className="settings-section-head">
        <div>
          <h4>{collection.name}</h4>
          <p className="muted">
            {collection.kind === 'calendar' ? t('agent.collection.calendar') : t('agent.collection.addressBook')} ·{' '}
            {what}
          </p>
        </div>
        {collection.granted ? (
          <button type="button" className="danger" disabled={busy} onClick={() => void set(false)}>
            {t('agent.revoke')}
          </button>
        ) : (
          <button type="button" className="primary" disabled={busy || !view.agent} onClick={() => void set(true)}>
            {t('agent.grantCollection')}
          </button>
        )}
      </div>
      {collection.granted ? null : <p className="muted">{t('agent.notGrantedCollection')}</p>}
    </div>
  )
}

function SortingForm({ policy, allowed, busy, onSave }: PolicyProps) {
  const { t } = useTranslation()
  const stored = policy.triage ?? { enabled: false }
  const [triage, setTriage] = useState(stored)
  useEffect(() => {
    setTriage(policy.triage ?? { enabled: false })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [JSON.stringify(policy.triage)])

  return (
    <form
      className="settings-subform"
      onSubmit={(event) => {
        event.preventDefault()
        void onSave({ triage })
      }}
    >
      <h4>{t('agent.sorting')}</h4>
      <Check
        checked={!!triage.enabled}
        disabled={!allowed.triage}
        label={t('agent.triage')}
        onChange={(enabled) => setTriage({ ...triage, enabled })}
      />
      <div className="form-narrow">
        <div className="row">
          <label>
            <span>{t('agent.replyExpectation')}</span>
            <Select
              block
              value={triage.replyExpectation || 'direct'}
              label={t('agent.replyExpectation')}
              options={[
                { value: 'direct', label: t('agent.replyExpectationDirect') },
                { value: 'any', label: t('agent.replyExpectationAny') },
              ]}
              onChange={(replyExpectation) => setTriage({ ...triage, replyExpectation })}
            />
          </label>
          <label>
            <span>{t('agent.backfill')}</span>
            <Select
              block
              value={triage.backfill || 'recent'}
              label={t('agent.backfill')}
              options={[
                { value: 'recent', label: t('agent.backfillRecent') },
                { value: 'none', label: t('agent.backfillNone') },
                { value: 'all', label: t('agent.backfillAll') },
              ]}
              onChange={(backfill) => setTriage({ ...triage, backfill })}
            />
          </label>
        </div>
      </div>
      <SaveRow busy={busy} saved={false} note={t('agent.saved')} />
    </form>
  )
}

function SummariesForm({ policy, allowed, busy, onSave }: PolicyProps) {
  const { t } = useTranslation()
  const [summaries, setSummaries] = useState(policy.summaries ?? { enabled: false })
  useEffect(() => {
    setSummaries(policy.summaries ?? { enabled: false })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [JSON.stringify(policy.summaries)])

  return (
    <form
      className="settings-subform"
      onSubmit={(event) => {
        event.preventDefault()
        void onSave({ summaries })
      }}
    >
      <h4>{t('agent.summaries')}</h4>
      <Check
        checked={!!summaries.enabled}
        disabled={!allowed.summaries}
        label={t('agent.summariesOn')}
        onChange={(enabled) => setSummaries({ ...summaries, enabled })}
      />
      <div className="form-narrow">
        <div className="row">
          <label className="shrink">
            <span>{t('agent.minimumMessages')}</span>
            <input
              className="narrow"
              value={summaries.minimumMessages || ''}
              inputMode="numeric"
              placeholder="3"
              onChange={(event) => setSummaries({ ...summaries, minimumMessages: Number(event.target.value) || 0 })}
            />
          </label>
          <label>
            <span>{t('agent.summaryStyle')}</span>
            <Select
              block
              value={summaries.style || 'brief'}
              label={t('agent.summaryStyle')}
              options={[
                { value: 'brief', label: t('agent.styleBrief') },
                { value: 'detailed', label: t('agent.styleDetailed') },
              ]}
              onChange={(style) => setSummaries({ ...summaries, style })}
            />
          </label>
        </div>
      </div>
      <SaveRow busy={busy} saved={false} note={t('agent.saved')} />
    </form>
  )
}

// HelpForm: the three things it does when asked or when sorting says so.
function HelpForm({ policy, allowed, busy, onSave }: PolicyProps) {
  const { t } = useTranslation()
  const [help, setHelp] = useState({
    draftReplies: policy.draftReplies,
    search: policy.search,
    research: policy.research,
  })
  useEffect(() => {
    setHelp({ draftReplies: policy.draftReplies, search: policy.search, research: policy.research })
  }, [policy.draftReplies, policy.search, policy.research])

  return (
    <form
      className="settings-subform"
      onSubmit={(event) => {
        event.preventDefault()
        void onSave(help)
      }}
    >
      <h4>{t('agent.replies')}</h4>
      <Check
        checked={help.draftReplies}
        disabled={!allowed.draftReplies}
        label={t('agent.draftReplies')}
        onChange={(draftReplies) => setHelp({ ...help, draftReplies })}
      />
      <Check
        checked={help.search}
        disabled={!allowed.search}
        label={t('agent.search')}
        onChange={(search) => setHelp({ ...help, search })}
      />
      <Check
        checked={help.research}
        disabled={!allowed.research}
        label={t('agent.research')}
        onChange={(research) => setHelp({ ...help, research })}
      />
      <SaveRow busy={busy} saved={false} note={t('agent.saved')} />
    </form>
  )
}

function AnsweringForm({ policy, allowed, busy, onSave }: PolicyProps) {
  const { t } = useTranslation()
  const [reply, setReplyState] = useState<AgentAutoReply>(policy.autoReply ?? { enabled: false })
  useEffect(() => {
    setReplyState(policy.autoReply ?? { enabled: false })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [JSON.stringify(policy.autoReply)])
  const setReply = (change: Partial<AgentAutoReply>) => setReplyState({ ...reply, ...change })
  // The three lists as typed, commas and all; split when saved, or a
  // comma would vanish under the cursor.
  const [lists, setLists] = useState({
    allow: joinList(policy.autoReply?.allow),
    never: joinList(policy.autoReply?.never),
    categories: joinList(policy.autoReply?.categories),
  })
  useEffect(() => {
    setLists({
      allow: joinList(policy.autoReply?.allow),
      never: joinList(policy.autoReply?.never),
      categories: joinList(policy.autoReply?.categories),
    })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [JSON.stringify(policy.autoReply)])

  return (
    <form
      className="settings-subform"
      onSubmit={(event) => {
        event.preventDefault()
        void onSave({
          autoReply: {
            ...reply,
            allow: splitList(lists.allow),
            never: splitList(lists.never),
            categories: splitList(lists.categories),
          },
        })
      }}
    >
      <h4>{t('agent.answering')}</h4>
      <p className="muted">{t('agent.answeringHint')}</p>
      <Check
        checked={reply.enabled}
        disabled={!allowed.autoReply}
        label={t('agent.autoReply')}
        onChange={(enabled) => setReply({ enabled })}
      />
      <div className="form-narrow">
        <label>
          <span>{t('agent.guidance')}</span>
          <textarea
            rows={3}
            value={reply.guidance ?? ''}
            placeholder={t('agent.guidancePlaceholder')}
            onChange={(event) => setReply({ guidance: event.target.value })}
          />
        </label>
        <div className="row">
          <label>
            <span>{t('agent.scope')}</span>
            <Select
              block
              value={reply.scope || 'known'}
              label={t('agent.scope')}
              options={[
                { value: 'known', label: t('agent.scopeKnown') },
                { value: 'everyone', label: t('agent.scopeEveryone') },
                { value: 'list', label: t('agent.scopeList') },
              ]}
              onChange={(scope) => setReply({ scope })}
            />
          </label>
          <label>
            <span>{t('agent.when')}</span>
            <Select
              block
              value={reply.when || 'always'}
              label={t('agent.when')}
              options={[
                { value: 'always', label: t('agent.whenAlways') },
                { value: 'outsideHours', label: t('agent.whenOutsideHours') },
                { value: 'whenAway', label: t('agent.whenAway') },
              ]}
              onChange={(when) => setReply({ when })}
            />
          </label>
        </div>
        {reply.when === 'outsideHours' ? (
          <div className="row">
            <label className="shrink">
              <span>{t('agent.hoursFrom')}</span>
              <input
                className="narrow"
                type="time"
                value={reply.hours?.from ?? '09:00'}
                onChange={(event) =>
                  setReply({
                    hours: { ...(reply.hours ?? { until: '17:30', days: [1, 2, 3, 4, 5] }), from: event.target.value },
                  })
                }
              />
            </label>
            <label className="shrink">
              <span>{t('agent.hoursUntil')}</span>
              <input
                className="narrow"
                type="time"
                value={reply.hours?.until ?? '17:30'}
                onChange={(event) =>
                  setReply({
                    hours: { ...(reply.hours ?? { from: '09:00', days: [1, 2, 3, 4, 5] }), until: event.target.value },
                  })
                }
              />
            </label>
          </div>
        ) : null}
        <label>
          <span>{t('agent.allow')}</span>
          <input value={lists.allow} onChange={(event) => setLists({ ...lists, allow: event.target.value })} />
        </label>
        <label>
          <span>{t('agent.never')}</span>
          <input value={lists.never} onChange={(event) => setLists({ ...lists, never: event.target.value })} />
        </label>
        <label>
          <span>{t('agent.replyCategories')}</span>
          <input
            value={lists.categories}
            placeholder={t('agent.replyCategoriesAny')}
            onChange={(event) => setLists({ ...lists, categories: event.target.value })}
          />
        </label>
        <div className="row">
          <label className="shrink">
            <span>{t('agent.hold')}</span>
            <input
              className="narrow"
              value={reply.holdMinutes || ''}
              inputMode="numeric"
              placeholder="10"
              onChange={(event) => setReply({ holdMinutes: Number(event.target.value) || 0 })}
            />
          </label>
          <label className="shrink">
            <span>{t('agent.dailyLimit')}</span>
            <input
              className="narrow"
              value={reply.dailyLimit || ''}
              inputMode="numeric"
              placeholder="20"
              onChange={(event) => setReply({ dailyLimit: Number(event.target.value) || 0 })}
            />
          </label>
        </div>
      </div>
      <SaveRow busy={busy} saved={false} note={t('agent.saved')} />
    </form>
  )
}

import { useState } from 'react'

import { graphql } from '../api'
import { ErrorMessage, Loading, Tag, formatCount, formatMoney, formatTime } from '../components/common'
import { FormDialog } from '../components/dialog'
import { PencilIcon, ToggleOffIcon, ToggleOnIcon } from '../components/icons'
import { SettingsEmpty, SettingsRow, SettingsSection } from '../components/settingsList'
import { useToast } from '../components/toast'
import { useQuery } from '../components/useQuery'
import { useTranslation } from '../i18n/i18n'
import { Select } from '../components/select'

// Everybody's agents, for an operator: who has one, what it may reach, what
// it cost, and the two controls — a limit and a switch. Tokens and kinds,
// never content: a transcript belongs to the person.

type Summary = {
  agentId: string
  userId: string
  username: string
  name: string
  enabled: boolean
  operatorDisabledAt?: string | null
  dailyTokens: number
  dailyCost: number
  sources: { mailboxId: string; name: string; policy?: { granted: boolean } | null }[]
  today: { used: number; limit: number; resetsAt: string; cost: number; costLimit: number; currency: string } | null
  lastRunAt?: string | null
  dead: number
  queued: number
  totals: {
    promptTokens: number
    completionTokens: number
    cacheReadTokens: number
    cacheWriteTokens: number
    calls: number
  }
}
type UsageRow = {
  key: string
  cost: number
  currency: string
  totals: {
    promptTokens: number
    completionTokens: number
    cacheReadTokens: number
    cacheWriteTokens: number
    calls: number
  }
}
type Job = {
  id: string
  agentId: string
  mailboxId: string
  kind: string
  attempts: number
  error: string
  finishedAt?: string | null
}

const SUMMARY = `{ agentId userId username name enabled operatorDisabledAt dailyTokens dailyCost dead queued lastRunAt
  sources { mailboxId name policy { granted } } today { used limit resetsAt cost costLimit currency }
  totals { promptTokens completionTokens cacheReadTokens cacheWriteTokens calls } }`
const ADMIN = `query ($by: String, $since: DateTime, $until: DateTime) {
  ListAgents ${SUMMARY}
  AgentServerUsage(by: $by, since: $since, until: $until) { key cost currency totals { promptTokens completionTokens cacheReadTokens cacheWriteTokens calls } }
  ListAgentDeadLetters { id agentId mailboxId kind attempts error finishedAt }
}`
const SET_LIMIT = `mutation ($agentId: String!, $dailyTokens: Int!, $dailyCost: Float) { SetAgentLimit(agentId: $agentId, dailyTokens: $dailyTokens, dailyCost: $dailyCost) ${SUMMARY} }`
const SET_DISABLED = `mutation ($agentId: String!, $disabled: Boolean!) { SetAgentDisabled(agentId: $agentId, disabled: $disabled) ${SUMMARY} }`
const RETRY = `mutation ($jobId: String!) { RetryAgentJob(jobId: $jobId) { id } }`

// dayOf is a date as a date input wants it, in the person's own zone.
function dayOf(date: Date): string {
  const pad = (value: number) => String(value).padStart(2, '0')
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`
}

function total(totals: UsageRow['totals']): number {
  return totals.promptTokens + totals.completionTokens + totals.cacheReadTokens + totals.cacheWriteTokens
}

export function AgentAdminPage() {
  const { t } = useTranslation()
  const toast = useToast()
  const [by, setBy] = useState('day')
  // The range, as dates the person picks; thirty days back by default,
  // and the end date inclusive, so "to today" means through tonight.
  const [since, setSince] = useState(() => dayOf(new Date(Date.now() - 30 * 24 * 60 * 60 * 1000)))
  const [until, setUntil] = useState(() => dayOf(new Date()))
  const { data, error, loading, reload } = useQuery(
    () =>
      graphql<{ ListAgents: Summary[]; AgentServerUsage: UsageRow[]; ListAgentDeadLetters: Job[] }>(ADMIN, {
        by,
        since: since ? new Date(since + 'T00:00:00').toISOString() : undefined,
        until: until ? new Date(new Date(until + 'T00:00:00').getTime() + 24 * 60 * 60 * 1000).toISOString() : undefined,
      }),
    [by, since, until],
  )
  // The agent whose limit is being set, and the number typed for it.
  const [limiting, setLimiting] = useState<Summary | null>(null)
  const [limit, setLimit] = useState('')
  const [cost, setCost] = useState('')
  const [busy, setBusy] = useState(false)
  const [problem, setProblem] = useState<string | null>(null)

  if (loading && !data) return <Loading />
  if (error) return <ErrorMessage error={error} />
  const agents = data!.ListAgents
  const usage = data!.AgentServerUsage
  const dead = data!.ListAgentDeadLetters

  // keyLabel is a row's key as a person reads it: an agent by whose it
  // is, a mailbox by its name, never an id.
  const keyLabel = (key: string): string => {
    if (!key) return t('agentAdmin.total')
    if (by === 'agent') {
      const agent = agents.find((candidate) => candidate.agentId === key)
      return agent ? agent.name || agent.username : key
    }
    if (by === 'mailbox') {
      for (const agent of agents) {
        const source = agent.sources.find((candidate) => candidate.mailboxId === key)
        if (source) return `${source.name} (${agent.username})`
      }
    }
    return key
  }

  async function act(work: () => Promise<unknown>, done: string) {
    try {
      await work()
      toast.done(done)
      await reload()
    } catch (caught) {
      toast.failed(caught instanceof Error ? caught.message : String(caught))
    }
  }

  const stateOf = (summary: Summary) =>
    summary.operatorDisabledAt ? (
      <Tag value={t('agentAdmin.switchedOff')} tone="warn" />
    ) : summary.enabled ? (
      <Tag value={t('agentAdmin.on')} tone="good" />
    ) : (
      <Tag value={t('agentAdmin.off')} />
    )

  const detailOf = (summary: Summary) => {
    const granted = summary.sources.filter((source) => source.policy?.granted).length
    const parts = [
      summary.name,
      t('agentAdmin.mailboxes', { granted: String(granted), total: String(summary.sources.length) }),
      t('agentAdmin.tokensToday', {
        used: formatCount(summary.today?.used ?? 0),
        limit: summary.today && summary.today.limit > 0 ? formatCount(summary.today.limit) : t('agentAdmin.unlimited'),
      }),
    ]
    if (summary.lastRunAt) parts.push(t('agentAdmin.lastRunAt', { time: formatTime(summary.lastRunAt) }))
    if (summary.queued > 0) parts.push(t('agentAdmin.queuedCount', { count: String(summary.queued) }))
    if (summary.dead > 0) parts.push(t('agentAdmin.deadCount', { count: String(summary.dead) }))
    return parts.join(' · ')
  }

  return (
    <>
      <SettingsSection
        card
        title={t('agentAdmin.usage')}
        action={
          <div className="usage-range">
            <label className="shrink">
              <span>{t('agentAdmin.since')}</span>
              <input type="date" value={since} max={until || undefined} onChange={(event) => setSince(event.target.value)} />
            </label>
            <label className="shrink">
              <span>{t('agentAdmin.until')}</span>
              <input type="date" value={until} min={since || undefined} onChange={(event) => setUntil(event.target.value)} />
            </label>
          <label className="shrink">
            <span>{t('agentAdmin.by')}</span>
            <Select
              value={by}
              label={t('agentAdmin.by')}
              options={[
                { value: 'day', label: t('agentAdmin.byDay') },
                { value: 'kind', label: t('agentAdmin.byKind') },
                { value: 'model', label: t('agentAdmin.byModel') },
                { value: 'agent', label: t('agentAdmin.byAgent') },
                { value: 'mailbox', label: t('agentAdmin.byMailbox') },
              ]}
              onChange={setBy}
            />
          </label>
          </div>
        }
      >
        {usage.length === 0 ? (
          <SettingsEmpty>{t('agentAdmin.noUsage')}</SettingsEmpty>
        ) : (
          <div className="table-wrap">
            <table className="numbers-table">
              <thead>
                <tr>
                  <th>{{ day: t('agentAdmin.byDay'), kind: t('agentAdmin.byKind'), model: t('agentAdmin.byModel'), agent: t('agentAdmin.byAgent'), mailbox: t('agentAdmin.byMailbox') }[by as 'day' | 'kind' | 'model' | 'agent' | 'mailbox'] ?? t('agentAdmin.key')}</th>
                  <th className="numeric">{t('agentAdmin.prompt')}</th>
                  <th className="numeric">{t('agentAdmin.completion')}</th>
                  <th className="numeric">{t('agentAdmin.cached')}</th>
                  <th className="numeric">{t('agentAdmin.total')}</th>
                  <th className="numeric">{t('agentAdmin.cost')}</th>
                  <th className="numeric">{t('agentAdmin.calls')}</th>
                </tr>
              </thead>
              <tbody>
                {usage.map((row) => (
                  <tr key={row.key}>
                    <td>{keyLabel(row.key)}</td>
                    <td className="numeric">{formatCount(row.totals.promptTokens)}</td>
                    <td className="numeric">{formatCount(row.totals.completionTokens)}</td>
                    <td className="numeric">{formatCount(row.totals.cacheReadTokens)}</td>
                    <td className="numeric">{formatCount(total(row.totals))}</td>
                    <td className="numeric">{formatMoney(row.cost, row.currency)}</td>
                    <td className="numeric">{row.totals.calls}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </SettingsSection>

      <SettingsSection card title={t('agentAdmin.agents')} description={t('agentAdmin.agentsHint')}>
        {agents.length === 0 ? (
          <SettingsEmpty>{t('agentAdmin.noAgents')}</SettingsEmpty>
        ) : (
          agents.map((summary) => (
            <SettingsRow
              key={summary.agentId}
              avatar={summary.username}
              title={summary.username}
              badge={stateOf(summary)}
              subtitle={detailOf(summary)}
              actions={
                <div className="row-actions">
                  <button
                    type="button"
                    className="icon-action"
                    title={t('agentAdmin.setLimit')}
                    aria-label={`${summary.username}: ${t('agentAdmin.setLimit')}`}
                    onClick={() => {
                      setLimit(summary.dailyTokens > 0 ? String(summary.dailyTokens) : '')
                      setCost(summary.dailyCost > 0 ? String(summary.dailyCost) : '')
                      setProblem(null)
                      setLimiting(summary)
                    }}
                  >
                    <PencilIcon size={16} />
                  </button>
                  <button
                    type="button"
                    className={summary.operatorDisabledAt ? 'icon-action' : 'icon-action danger'}
                    title={summary.operatorDisabledAt ? t('agentAdmin.switchOn') : t('agentAdmin.switchOff')}
                    aria-label={`${summary.username}: ${summary.operatorDisabledAt ? t('agentAdmin.switchOn') : t('agentAdmin.switchOff')}`}
                    onClick={() =>
                      void act(
                        () =>
                          graphql(SET_DISABLED, {
                            agentId: summary.agentId,
                            disabled: !summary.operatorDisabledAt,
                          }),
                        t('agentAdmin.switched'),
                      )
                    }
                  >
                    {summary.operatorDisabledAt ? <ToggleOffIcon size={16} /> : <ToggleOnIcon size={16} />}
                  </button>
                </div>
              }
            />
          ))
        )}
      </SettingsSection>

      <SettingsSection card title={t('agentAdmin.deadLetters')}>
        {dead.length === 0 ? (
          <SettingsEmpty>{t('agentAdmin.noDeadLetters')}</SettingsEmpty>
        ) : (
          dead.map((job) => (
            <SettingsRow
              key={job.id}
              title={t('agentAdmin.gaveUp', {
                kind: job.kind,
                user: agents.find((summary) => summary.agentId === job.agentId)?.username ?? job.agentId,
              })}
              subtitle={
                <>
                  {t('agentAdmin.after', { count: String(job.attempts), time: formatTime(job.finishedAt ?? undefined) })}
                  {job.error ? <> · {job.error}</> : null}
                </>
              }
              actions={
                <button
                  type="button"
                  onClick={() => void act(() => graphql(RETRY, { jobId: job.id }), t('agentAdmin.retried'))}
                >
                  {t('agentAdmin.retry')}
                </button>
              }
            />
          ))
        )}
      </SettingsSection>

      {limiting ? (
        <FormDialog
          title={t('agentAdmin.setLimit')}
          submitLabel={t('common.save')}
          busy={busy}
          error={problem}
          onClose={() => setLimiting(null)}
          onSubmit={() => {
            setBusy(true)
            setProblem(null)
            graphql(SET_LIMIT, { agentId: limiting.agentId, dailyTokens: Number(limit) || 0, dailyCost: Number(cost) || 0 })
              .then(async () => {
                setLimiting(null)
                toast.done(t('agentAdmin.limitSet'))
                await reload()
              })
              .catch((caught) => setProblem(caught instanceof Error ? caught.message : String(caught)))
              .finally(() => setBusy(false))
          }}
        >
          <p className="muted">{t('agentAdmin.limitHint', { user: limiting.username })}</p>
          <label>
            <span>{t('agentAdmin.limit')}</span>
            <input
              autoFocus
              inputMode="numeric"
              value={limit}
              placeholder={
                limiting.today && limiting.today.limit > 0 ? String(limiting.today.limit) : t('agentAdmin.unlimited')
              }
              onChange={(event) => setLimit(event.target.value.replace(/[^0-9]/g, ''))}
            />
          </label>
          <label>
            <span>{t('agentAdmin.costLimit', { currency: limiting.today?.currency || 'USD' })}</span>
            <input
              inputMode="decimal"
              value={cost}
              placeholder={
                limiting.today && limiting.today.costLimit > 0 ? String(limiting.today.costLimit) : t('agentAdmin.unlimited')
              }
              onChange={(event) => setCost(event.target.value.replace(/[^0-9.]/g, ''))}
            />
          </label>
        </FormDialog>
      ) : null}
    </>
  )
}

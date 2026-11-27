import { useSearchParams } from 'react-router-dom'

import { RUN_KINDS, runKindLabel } from '../agentRuns'
import { graphql, openAgentConversation } from '../api'
import { ErrorMessage, Loading, Tag, formatCount, formatMoney, formatTime } from '../components/common'
import { Pager, useKeepPageInRange, usePageInAddress } from '../components/pager'
import { Select } from '../components/select'
import { SettingsEmpty, SettingsRow, SettingsSection } from '../components/settingsList'
import { useQuery } from '../components/useQuery'
import { Key, useTranslation } from '../i18n/i18n'

// Oversight is what every person's agent is doing and has done, for an
// operator who may observe: running now, recent, and the turns that failed.
// Reading only, and every conversation opened from here is written in the
// audit log. The filters and the pages are in the address, so a view can be
// sent to somebody and Back goes to the page before.

export const OVERSIGHT_RANGES = ['24h', '7d', '30d'] as const
export type OversightRange = (typeof OVERSIGHT_RANGES)[number]
export const DEFAULT_OVERSIGHT_RANGE: OversightRange = '7d'

const RANGE_MILLISECONDS: Record<OversightRange, number> = {
  '24h': 24 * 60 * 60 * 1000,
  '7d': 7 * 24 * 60 * 60 * 1000,
  '30d': 30 * 24 * 60 * 60 * 1000,
}

const RANGE_LABELS: Record<OversightRange, Key> = {
  '24h': 'agentOversight.range24h',
  '7d': 'agentOversight.range7d',
  '30d': 'agentOversight.range30d',
}

// rangeOf is the range in the address, or the default for one that is not
// one of the three.
export function rangeOf(text: string | null): OversightRange {
  return (OVERSIGHT_RANGES as readonly string[]).includes(text ?? '')
    ? (text as OversightRange)
    : DEFAULT_OVERSIGHT_RANGE
}

// sinceOf is the start of a range, as the server reads it.
export function sinceOf(range: OversightRange, now: number = Date.now()): string {
  return new Date(now - RANGE_MILLISECONDS[range]).toISOString()
}

type OversightRun = {
  id: string
  agentId: string
  kind: string
  title: string
  jobKind: string
  lastAt: string
  isRunning: boolean
  ownerUsername: string
  ownerName: string
  usage: { promptTokens: number; cacheReadTokens: number; completionTokens: number; cost: number }
}

type FailedTurn = {
  conversationId: string
  conversationKind: string
  conversationTitle: string
  ownerUsername: string
  ownerName: string
  failedAt: string
  errorText: string
  agentId: string
}

const OVERSIGHT_RUNS = `
  query ($first: Int, $offset: Int, $jobId: String, $kinds: [String!], $since: String, $isRunningOnly: Boolean) {
    ListAllAgentRuns(first: $first, offset: $offset, jobId: $jobId, kinds: $kinds, since: $since, isRunningOnly: $isRunningOnly) {
      totalCount
      runs { id agentId kind title jobKind lastAt isRunning ownerUsername ownerName usage { promptTokens cacheReadTokens completionTokens cost } }
    }
  }`

const FAILED_TURNS = `
  query ($first: Int, $offset: Int, $since: String) {
    ListFailedAgentTurns(first: $first, offset: $offset, since: $since) {
      totalCount
      failedTurns { conversationId conversationKind conversationTitle ownerUsername ownerName failedAt errorText agentId }
    }
  }`

// kindLabel is what a conversation is, in the reader's words: a run by the
// kind of run, the others by their own kind.
function useKindLabel() {
  const { t } = useTranslation()
  return (kind: string, jobKind: string): string => {
    if (kind === 'run') return runKindLabel(t, jobKind)
    if (kind === 'main') return t('agentOversight.kindMain')
    if (kind === 'named') return t('agentOversight.kindNamed')
    if (kind === 'goal') return t('agentOversight.kindGoal')
    return kind
  }
}

// OversightTab is the whole tab: the filters, and the three sections. job
// narrows Recent to the runs of one job given up on, as the dead letters ask.
export function OversightTab({ job, onAllRuns }: { job: string | null; onAllRuns: () => void }) {
  const { t } = useTranslation()
  const [parameters, setParameters] = useSearchParams()
  const range = rangeOf(parameters.get('range'))
  const kind = parameters.get('kind') ?? ''

  // Choosing another range or kind starts every list at its first page.
  const choose = (name: 'range' | 'kind', value: string) =>
    setParameters((previous) => {
      const written = new URLSearchParams(previous)
      if (value === '' || (name === 'range' && value === DEFAULT_OVERSIGHT_RANGE)) written.delete(name)
      else written.set(name, value)
      for (const pageName of ['runningPage', 'page', 'failedPage']) written.delete(pageName)
      return written
    })

  const kindOptions = [
    { value: '', label: t('agentOversight.kindAll') },
    { value: 'main', label: t('agentOversight.kindMain') },
    { value: 'named', label: t('agentOversight.kindNamed') },
    ...RUN_KINDS.map((runKind) => ({ value: runKind, label: runKindLabel(t, runKind) })),
  ]

  return (
    <>
      <div className="usage-range oversight-filters">
        <label className="shrink">
          <span>{t('agentOversight.range')}</span>
          <Select
            value={range}
            label={t('agentOversight.range')}
            options={OVERSIGHT_RANGES.map((each) => ({ value: each, label: t(RANGE_LABELS[each]) }))}
            onChange={(value) => choose('range', value)}
          />
        </label>
        <label className="shrink">
          <span>{t('agentOversight.kind')}</span>
          <Select
            value={kind}
            label={t('agentOversight.kind')}
            options={kindOptions}
            searchable
            onChange={(value) => choose('kind', value)}
          />
        </label>
      </div>
      <OversightRuns
        pageParameter="runningPage"
        pageSizeParameter="runningRows"
        title={t('agentOversight.running')}
        description={t('agentOversight.runningHint')}
        empty={t('agentOversight.noRunning')}
        isRunningOnly
        job={null}
        range={range}
        kind={kind}
        onAllRuns={onAllRuns}
      />
      <OversightRuns
        pageParameter="page"
        pageSizeParameter="rows"
        title={t('agentOversight.recent')}
        description={job ? t('agentAdmin.runsOfJob') : t('agentOversight.recentHint')}
        empty={t('agentOversight.noRecent')}
        isRunningOnly={false}
        job={job}
        range={range}
        kind={kind}
        onAllRuns={onAllRuns}
      />
      <OversightFailed range={range} />
    </>
  )
}

function OversightRuns({
  pageParameter,
  pageSizeParameter,
  title,
  description,
  empty,
  isRunningOnly,
  job,
  range,
  kind,
  onAllRuns,
}: {
  pageParameter: string
  pageSizeParameter: string
  title: string
  description: string
  empty: string
  isRunningOnly: boolean
  job: string | null
  range: OversightRange
  kind: string
  onAllRuns: () => void
}) {
  const { t } = useTranslation()
  const kindLabel = useKindLabel()
  const { pageIndex, pageSize, offset, setPageIndex, setPageSize } = usePageInAddress({
    pageParameter,
    pageSizeParameter,
    defaultPageSize: 25,
  })
  const { data, error, loading } = useQuery(
    () =>
      graphql<{ ListAllAgentRuns: { totalCount: number; runs: OversightRun[] } }>(OVERSIGHT_RUNS, {
        first: pageSize,
        offset,
        jobId: job,
        kinds: kind ? [kind] : null,
        // Running now is whatever is in flight, however long ago it began.
        since: isRunningOnly ? null : sinceOf(range),
        isRunningOnly,
      }),
    [pageSize, offset, job, kind, range, isRunningOnly],
    // Refreshed: a conversation that just spoke belongs at the top while the
    // person watches, and a turn that ended leaves Running now.
    { refresh: true },
  )
  const totalCount = data?.ListAllAgentRuns.totalCount ?? null
  useKeepPageInRange(pageIndex, pageSize, totalCount, setPageIndex)
  if (error) return <ErrorMessage error={error} />
  if (loading && !data) return <Loading />
  const runs = data?.ListAllAgentRuns.runs ?? []
  return (
    <SettingsSection
      card
      title={title}
      description={description}
      action={
        job && !isRunningOnly ? (
          <button type="button" onClick={onAllRuns}>
            {t('agent.allRuns')}
          </button>
        ) : null
      }
    >
      {runs.length === 0 ? (
        <SettingsEmpty>{empty}</SettingsEmpty>
      ) : (
        runs.map((run) => (
          <SettingsRow
            key={run.id}
            avatar={run.ownerUsername}
            title={run.title || kindLabel(run.kind, run.jobKind)}
            badge={
              <>
                <Tag value={kindLabel(run.kind, run.jobKind)} />
                {run.isRunning ? <Tag value={t('agentOversight.isRunning')} tone="good" /> : null}
              </>
            }
            subtitle={
              <>
                {run.ownerName || run.ownerUsername} · {formatTime(run.lastAt)}
                {run.usage.promptTokens + run.usage.cacheReadTokens + run.usage.completionTokens > 0 ? (
                  <>
                    {' · '}
                    {formatCount(run.usage.promptTokens + run.usage.cacheReadTokens + run.usage.completionTokens)}
                    {run.usage.cost ? ` · ${formatMoney(run.usage.cost)}` : ''}
                  </>
                ) : null}
              </>
            }
            actions={
              <div className="row-actions">
                <button
                  type="button"
                  onClick={() => {
                    if (!openAgentConversation(run.id)) window.scrollTo(0, 0)
                  }}
                >
                  {t('agent.open')}
                </button>
              </div>
            }
          />
        ))
      )}
      {totalCount !== null && totalCount > 0 ? (
        <div className="table-bar">
          <Pager
            pageIndex={pageIndex}
            pageSize={pageSize}
            shownCount={runs.length}
            totalCount={totalCount}
            isLoading={loading}
            onPageIndex={setPageIndex}
            onPageSize={setPageSize}
          />
        </div>
      ) : null}
    </SettingsSection>
  )
}

function OversightFailed({ range }: { range: OversightRange }) {
  const { t } = useTranslation()
  const kindLabel = useKindLabel()
  const { pageIndex, pageSize, offset, setPageIndex, setPageSize } = usePageInAddress({
    pageParameter: 'failedPage',
    pageSizeParameter: 'failedRows',
    defaultPageSize: 25,
  })
  const { data, error, loading } = useQuery(
    () =>
      graphql<{ ListFailedAgentTurns: { totalCount: number; failedTurns: FailedTurn[] } }>(FAILED_TURNS, {
        first: pageSize,
        offset,
        since: sinceOf(range),
      }),
    [pageSize, offset, range],
    { refresh: true },
  )
  const totalCount = data?.ListFailedAgentTurns.totalCount ?? null
  useKeepPageInRange(pageIndex, pageSize, totalCount, setPageIndex)
  if (error) return <ErrorMessage error={error} />
  if (loading && !data) return <Loading />
  const turns = data?.ListFailedAgentTurns.failedTurns ?? []
  return (
    <SettingsSection card title={t('agentOversight.failed')} description={t('agentOversight.failedHint')}>
      {turns.length === 0 ? (
        <SettingsEmpty>{t('agentOversight.noFailed')}</SettingsEmpty>
      ) : (
        turns.map((turn) => (
          <SettingsRow
            key={`${turn.conversationId}:${turn.failedAt}`}
            avatar={turn.ownerUsername}
            title={turn.conversationTitle || kindLabel(turn.conversationKind, '')}
            badge={<Tag value={t('agentOversight.isFailed')} tone="bad" />}
            subtitle={
              <>
                {turn.ownerName || turn.ownerUsername} · {formatTime(turn.failedAt)}
                {turn.errorText ? <> · {turn.errorText}</> : null}
              </>
            }
            actions={
              <div className="row-actions">
                <button
                  type="button"
                  onClick={() => {
                    if (!openAgentConversation(turn.conversationId)) window.scrollTo(0, 0)
                  }}
                >
                  {t('agent.open')}
                </button>
              </div>
            }
          />
        ))
      )}
      {totalCount !== null && totalCount > 0 ? (
        <div className="table-bar">
          <Pager
            pageIndex={pageIndex}
            pageSize={pageSize}
            shownCount={turns.length}
            totalCount={totalCount}
            isLoading={loading}
            onPageIndex={setPageIndex}
            onPageSize={setPageSize}
          />
        </div>
      ) : null}
    </SettingsSection>
  )
}

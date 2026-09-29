import { useState } from 'react'

import { graphql, openAgentConversation } from '../api'
import { Tag } from './common'
import { RelativeTime } from './relativeTime'
import { SettingsRow, SettingsSection } from './settingsList'
import { useToast } from './toast'
import { useQuery } from './useQuery'
import { Trans, useTranslation } from '../i18n/i18n'

// The surveys and subagents the agent started and did not wait for, and
// the surveys started from the command line: each with where it stands,
// and for finished ones the run that holds its result, opened in the
// drawer the way the activity list opens a run. The rows are on the
// server, so the list is the same on every device.

type BackgroundWorkStatus = 'queued' | 'running' | 'done' | 'failed' | 'stopped'

export interface BackgroundWork {
  id: string
  workKind: 'survey' | 'subagent'
  workStatus: BackgroundWorkStatus
  title: string
  errorMessage: string
  runIds: string[]
  createdAt: string
  finishedAt?: string | null
}

const LIST = `
  query {
    ListAgentBackgroundWork(first: 20) { id workKind workStatus title errorMessage runIds createdAt finishedAt }
  }`

const STOP = `
  mutation ($id: String!) {
    StopAgentBackgroundWork(id: $id) { id workStatus }
  }`

// isStoppable says whether a piece of work may still be stopped.
export function isStoppable(work: BackgroundWork): boolean {
  return work.workStatus === 'queued' || work.workStatus === 'running'
}

// resultRunOf is the run whose transcript holds the result: a subagent's
// one run, or the one of a survey's runs that combined the parts, which is
// its last. None for work that made none, or has not finished.
export function resultRunOf(work: BackgroundWork): string | null {
  if (work.workStatus !== 'done' || work.runIds.length === 0) return null
  return work.runIds[work.runIds.length - 1]
}

// BackgroundWorkState is how a piece of work stands, as a tag.
function BackgroundWorkState({ work }: { work: BackgroundWork }) {
  const { t } = useTranslation()
  switch (work.workStatus) {
    case 'queued':
      return <Tag value={t('backgroundWork.queued')} />
    case 'running':
      return <Tag value={t('backgroundWork.running')} />
    case 'done':
      return <Tag value={t('backgroundWork.done')} tone="good" />
    case 'failed':
      return <Tag value={t('backgroundWork.failed')} tone="bad" />
    default:
      return <Tag value={t('backgroundWork.stopped')} tone="warn" />
  }
}

// BackgroundWorkCard is the list, on the agent page beside the runs. With
// no work at all there is no card, as there is no background commands card
// with nothing running.
export function BackgroundWorkCard() {
  const { t } = useTranslation()
  const toast = useToast()
  const { data, reload } = useQuery(
    () =>
      graphql<{ ListAgentBackgroundWork: BackgroundWork[] }>(LIST, {}).then(
        (response) => response.ListAgentBackgroundWork,
      ),
    [],
    { refresh: true },
  )
  const [stopping, setStopping] = useState<string | null>(null)
  const works = data ?? []
  if (works.length === 0) return null

  const stop = async (work: BackgroundWork) => {
    setStopping(work.id)
    try {
      await graphql(STOP, { id: work.id })
      toast.done(t('backgroundWork.stoppedOne', { title: work.title }))
      void reload(true)
    } catch (caught) {
      toast.failure(caught, t('backgroundWork.stopFailed'))
    } finally {
      setStopping(null)
    }
  }

  return (
    <SettingsSection card title={t('backgroundWork.title')} description={t('backgroundWork.hint')}>
      {works.map((work) => {
        const resultRun = resultRunOf(work)
        return (
          <SettingsRow
            key={work.id}
            title={work.title || work.id}
            badge={<BackgroundWorkState work={work} />}
            subtitle={
              <>
                {t(work.workKind === 'survey' ? 'backgroundWork.survey' : 'backgroundWork.subagent')}
                {' · '}
                <Trans k="backgroundWork.started" nodes={{ time: <RelativeTime value={work.createdAt} /> }} />
                {work.errorMessage ? ` · ${work.errorMessage}` : ''}
              </>
            }
            actions={
              isStoppable(work) ? (
                <button
                  type="button"
                  className="link danger"
                  aria-label={`${work.title}: ${t('backgroundWork.stop')}`}
                  disabled={stopping === work.id}
                  onClick={() => void stop(work)}
                >
                  {t('backgroundWork.stop')}
                </button>
              ) : resultRun ? (
                <button
                  type="button"
                  className="link"
                  aria-label={`${work.title}: ${t('backgroundWork.open')}`}
                  onClick={() => {
                    if (!openAgentConversation(resultRun)) window.scrollTo(0, 0)
                  }}
                >
                  {t('backgroundWork.open')}
                </button>
              ) : null
            }
          />
        )
      })}
    </SettingsSection>
  )
}

import { useEffect, useRef, useState } from 'react'

import { graphql, openAgentConversation } from '../api'
import { Loading, Tag } from './common'
import { ConfirmDialog } from './dialog'
import { Markdown } from './markdown'
import { RelativeTime } from './relativeTime'
import { SettingsRow, SettingsSection } from './settingsList'
import { useToast } from './toast'
import { useQuery } from './useQuery'
import { Trans, useTranslation } from '../i18n/i18n'

// The surveys and subagents the agent started and did not wait for, and
// the surveys started from the command line: each with where it stands,
// and for finished ones what came of it, the report or the answer itself,
// or why it failed, opened in a dialog with the runs it made beside it.
// The rows are on the server, so the list is the same on every device.

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

// isOpenable says whether a piece of work has something to show: the
// result of work that finished, or why work that failed did.
export function isOpenable(work: BackgroundWork): boolean {
  return work.workStatus === 'done' || work.workStatus === 'failed'
}

// A finished piece of work with what came of it.
interface BackgroundWorkResult extends BackgroundWork {
  resultText: string
}

const READ = `
  query ($id: String!) {
    GetAgentBackgroundWork(id: $id) { id workKind workStatus title errorMessage resultText runIds createdAt finishedAt }
  }`

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

// BackgroundWorkResultDialog is what came of one piece of work: the
// report or the answer as text to read, or why it failed, and under it the
// runs it made, each opened in the drawer as the activity list opens one.
// Read when it is opened, since the list leaves the results out.
function BackgroundWorkResultDialog({ work, onClose }: { work: BackgroundWork; onClose: () => void }) {
  const { t } = useTranslation()
  const toast = useToast()
  const [result, setResult] = useState<BackgroundWorkResult | null>(null)
  useEffect(() => {
    let isClosed = false
    graphql<{ GetAgentBackgroundWork: BackgroundWorkResult }>(READ, { id: work.id })
      .then((response) => {
        if (!isClosed) setResult(response.GetAgentBackgroundWork)
      })
      .catch((caught) => {
        if (isClosed) return
        toast.failure(caught, t('backgroundWork.readFailed'))
        onClose()
      })
    return () => {
      isClosed = true
    }
    // toast, t and onClose are stable for the life of the dialog.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [work.id])
  const shown = result ?? work
  return (
    <ConfirmDialog
      title={work.title || work.id}
      wide
      body={
        <div className="background-output">
          <p className="background-command-meta muted">
            <BackgroundWorkState work={shown} />
            <span>{t(shown.workKind === 'survey' ? 'backgroundWork.survey' : 'backgroundWork.subagent')}</span>
            <span>
              <Trans k="backgroundWork.started" nodes={{ time: <RelativeTime value={shown.createdAt} /> }} />
            </span>
          </p>
          {result === null ? (
            <Loading />
          ) : result.workStatus === 'failed' ? (
            <section className="background-output-section">
              <h4>{t('backgroundWork.error')}</h4>
              <pre className="background-output-text">{result.errorMessage || t('backgroundWork.noError')}</pre>
            </section>
          ) : (
            <section className="background-output-section">
              <h4>{t(result.workKind === 'survey' ? 'backgroundWork.report' : 'backgroundWork.answer')}</h4>
              {result.resultText.trim() ? (
                <div className="background-work-result">
                  <Markdown text={result.resultText} />
                </div>
              ) : (
                <p className="muted">{t('backgroundWork.noResult')}</p>
              )}
            </section>
          )}
          {result && result.runIds.length > 0 ? (
            <section className="background-output-section">
              <h4>{t('backgroundWork.runs')}</h4>
              <p className="background-work-runs">
                {result.runIds.map((runId, index) => (
                  <button
                    key={runId}
                    type="button"
                    className="link"
                    onClick={() => {
                      onClose()
                      if (!openAgentConversation(runId)) window.scrollTo(0, 0)
                    }}
                  >
                    {t('backgroundWork.run', { number: index + 1 })}
                  </button>
                ))}
              </p>
            </section>
          ) : null}
        </div>
      }
      onClose={onClose}
    />
  )
}

// BackgroundWorkCard is the list, on the agent page beside the runs. With
// no work at all there is no card, as there is no background commands card
// with nothing running.
export function BackgroundWorkCard() {
  const { t } = useTranslation()
  const toast = useToast()
  const { data, error, reload } = useQuery(
    () =>
      graphql<{ ListAgentBackgroundWork: BackgroundWork[] }>(LIST, {}).then(
        (response) => response.ListAgentBackgroundWork,
      ),
    [],
    { refresh: true },
  )
  // A list that cannot be read says so once, not at every refresh: until
  // it has been read, each failed refresh hands back a new error.
  const toldFailure = useRef('')
  useEffect(() => {
    if (!error) {
      toldFailure.current = ''
      return
    }
    const errorMessage = error instanceof Error ? error.message : String(error)
    if (errorMessage === toldFailure.current) return
    toldFailure.current = errorMessage
    toast.failure(error, t('backgroundWork.listFailed'))
  }, [error, toast, t])
  const [stopping, setStopping] = useState<string | null>(null)
  const [opened, setOpened] = useState<BackgroundWork | null>(null)
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
      {works.map((work) => (
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
                className="danger"
                aria-label={`${work.title}: ${t('backgroundWork.stop')}`}
                disabled={stopping === work.id}
                onClick={() => void stop(work)}
              >
                {t('backgroundWork.stop')}
              </button>
            ) : isOpenable(work) ? (
              <button
                type="button"
                aria-label={`${work.title}: ${t('backgroundWork.open')}`}
                onClick={() => setOpened(work)}
              >
                {t('backgroundWork.open')}
              </button>
            ) : null
          }
        />
      ))}
      {opened ? <BackgroundWorkResultDialog work={opened} onClose={() => setOpened(null)} /> : null}
    </SettingsSection>
  )
}

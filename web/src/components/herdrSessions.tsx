import { useEffect, useState } from 'react'

import { graphql } from '../api'
import { Loading, Tag } from './common'
import { ConfirmDialog } from './dialog'
import { SettingsEmpty, SettingsSection } from './settingsList'
import { Tabs } from './tabs'
import { useToast } from './toast'
import { useQuery } from './useQuery'
import { useTranslation } from '../i18n/i18n'

// The person's herdr sessions: the Claude Code and Codex sessions they keep
// in herdr's panes, on each attached computer, which their agent works in
// beside them. The same operations as the agent's herdr tool and as
// `teanode computer herdr`, through the documents below, which a test checks
// name the operations the command line uses.
//
// The program on each computer decides each session's state and recognizes
// the question it waits on; this shows them, and answers a question with the
// option the person taps. An answer carries the question's fingerprint, so
// one tapped after the question was answered at the keyboard is refused
// rather than pressed into whatever came next.

export type HerdrSessionState = 'idle' | 'working' | 'asking' | 'unknown'

export interface HerdrQuestionOption {
  optionNumber: number
  optionLabel: string
  optionDescription: string
  // choice, freeText (it takes typed text) or chat.
  herdrOptionKind: 'choice' | 'freeText' | 'chat'
}

export interface HerdrQuestion {
  questionFingerprint: string
  herdrQuestionKind: 'question' | 'toolApproval' | 'planApproval'
  questionText: string
  isMultipleChoice: boolean
  isFromTranscript: boolean
  options: HerdrQuestionOption[]
}

export interface HerdrSession {
  computer: string
  paneId: string
  codingAgentKind: string
  codingSessionId: string
  herdrSessionState: HerdrSessionState
  herdrAgentStatus: string
  paneTitle: string
  workingDirectory: string
  transcriptPath: string
  isWatched: boolean
  question?: HerdrQuestion | null
}

interface HerdrList {
  computerNames: string[]
  sessions: HerdrSession[]
}

interface HerdrTurn {
  herdrTurnRole: 'person' | 'agent' | 'tool'
  turnText: string
  turnAt?: string | null
}

// The fields of a session, the same as the command line's
// client.HerdrSessionFields.
const SESSION_FIELDS = `computer paneId codingAgentKind codingSessionId herdrSessionState herdrAgentStatus paneTitle workingDirectory transcriptPath isWatched
  question { questionFingerprint herdrQuestionKind questionText isMultipleChoice isFromTranscript options { optionNumber optionLabel optionDescription herdrOptionKind } }`

export const HERDR_DOCUMENTS = {
  ListAgentHerdrSessions: `query ($computer: String) {
    ListAgentHerdrSessions(computer: $computer) { computerNames sessions { ${SESSION_FIELDS} } }
  }`,
  ReadAgentHerdrSession: `query ($computer: String, $paneId: String!, $turnCount: Int) {
    ReadAgentHerdrSession(computer: $computer, paneId: $paneId, turnCount: $turnCount) {
      herdrSession { ${SESSION_FIELDS} } turns { herdrTurnRole turnText turnAt } isTruncated
    }
  }`,
  ReadAgentHerdrScreen: `query ($computer: String, $paneId: String!, $lineCount: Int) {
    ReadAgentHerdrScreen(computer: $computer, paneId: $paneId, lineCount: $lineCount) { herdrSession { ${SESSION_FIELDS} } screenText }
  }`,
  SendAgentHerdrSession: `mutation ($computer: String, $paneId: String!, $text: String!, $shouldQueue: Boolean) {
    SendAgentHerdrSession(computer: $computer, paneId: $paneId, text: $text, shouldQueue: $shouldQueue) { ${SESSION_FIELDS} }
  }`,
  WaitAgentHerdrSession: `mutation ($computer: String, $paneId: String!, $waitSeconds: Int) {
    WaitAgentHerdrSession(computer: $computer, paneId: $paneId, waitSeconds: $waitSeconds) { herdrSession { ${SESSION_FIELDS} } isTimedOut }
  }`,
  AnswerAgentHerdrQuestion: `mutation ($computer: String, $paneId: String!, $questionFingerprint: String!, $optionNumbers: [Int!], $freeText: String) {
    AnswerAgentHerdrQuestion(computer: $computer, paneId: $paneId, questionFingerprint: $questionFingerprint, optionNumbers: $optionNumbers, freeText: $freeText) {
      herdrSession { ${SESSION_FIELDS} } isAnswerAccepted answeredWith
    }
  }`,
  WatchAgentHerdrSession: `mutation ($computer: String, $paneId: String!, $conversationId: String!) {
    WatchAgentHerdrSession(computer: $computer, paneId: $paneId, conversationId: $conversationId) { ${SESSION_FIELDS} }
  }`,
  SetUpAgentHerdrHooks: `mutation ($computer: String, $isRemoval: Boolean) {
    SetUpAgentHerdrHooks(computer: $computer, isRemoval: $isRemoval) { computer isInstalled settingsPath scriptPath backupPath hookEventNames }
  }`,
} as const

const MAIN_CONVERSATION = `query { ListAgentConversations(archived: false) { id kind } }`

// SHARED_LIST_MS is how long one list of the sessions answers every card
// that asks: a conversation with many questions in it asks once, not once
// a question.
const SHARED_LIST_MS = 3_000

let sharedList: { at: number; answer: Promise<HerdrList> } | null = null

// listHerdrSessions is every session on every computer, shared by the
// callers of the same few seconds. fresh asks again regardless.
export function listHerdrSessions(fresh = false): Promise<HerdrList> {
  if (!fresh && sharedList && Date.now() - sharedList.at < SHARED_LIST_MS) return sharedList.answer
  const answer = graphql<{ ListAgentHerdrSessions: HerdrList }>(HERDR_DOCUMENTS.ListAgentHerdrSessions, {}).then(
    (response) => response.ListAgentHerdrSessions,
  )
  sharedList = { at: Date.now(), answer }
  answer.catch(() => {
    if (sharedList?.answer === answer) sharedList = null
  })
  return answer
}

// codingAgentName is what a coding agent is called.
export function codingAgentName(codingAgentKind: string): string {
  if (codingAgentKind === 'claude') return 'Claude Code'
  if (codingAgentKind === 'codex') return 'Codex'
  return codingAgentKind
}

// HerdrStateTag is a session's state as a word.
export function HerdrStateTag({ session }: { session: HerdrSession }) {
  const { t } = useTranslation()
  const state = session.herdrSessionState
  const tone = state === 'asking' ? 'warn' : state === 'working' ? 'good' : undefined
  return <Tag value={t(`herdr.state.${state}` as 'herdr.state.idle')} tone={tone} />
}

// HerdrQuestionAnswer is a question with its options as buttons: a tap
// answers a question that takes one, ticks one of several, and a text box
// takes what an option that takes text is to type. onAnswered is told the
// session as it stands after.
export function HerdrQuestionAnswer({
  session,
  question,
  onAnswered,
}: {
  session: HerdrSession
  question: HerdrQuestion
  onAnswered: (after: HerdrSession | null) => void
}) {
  const { t } = useTranslation()
  const toast = useToast()
  const [ticked, setTicked] = useState<number[]>([])
  const [freeText, setFreeText] = useState('')
  const [isBusy, setBusy] = useState(false)
  const freeTextOption = question.options.find((option) => option.herdrOptionKind === 'freeText')
  const choices = question.options.filter((option) => option.herdrOptionKind !== 'freeText')

  const answer = async (optionNumbers: number[], text: string) => {
    setBusy(true)
    try {
      const response = await graphql<{
        AnswerAgentHerdrQuestion: { herdrSession: HerdrSession; isAnswerAccepted: boolean; answeredWith: string }
      }>(HERDR_DOCUMENTS.AnswerAgentHerdrQuestion, {
        computer: session.computer,
        paneId: session.paneId,
        questionFingerprint: question.questionFingerprint,
        optionNumbers,
        freeText: text || null,
      })
      const answered = response.AnswerAgentHerdrQuestion
      if (answered.isAnswerAccepted) {
        toast.done(t('herdr.answered', { answer: answered.answeredWith, pane: session.paneId }))
      } else {
        toast.failure(null, t('herdr.answerStillThere', { pane: session.paneId }))
      }
      sharedList = null
      onAnswered(answered.herdrSession)
    } catch (caught) {
      toast.failure(caught, t('herdr.answerFailed'))
      sharedList = null
      onAnswered(null)
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="herdr-question">
      <p className="herdr-question-kind muted">
        {t(`herdr.kind.${question.herdrQuestionKind}` as 'herdr.kind.question')}
      </p>
      <pre className="herdr-question-text">{question.questionText}</pre>
      <div className="herdr-question-options">
        {choices.map((option) =>
          question.isMultipleChoice ? (
            <label key={option.optionNumber} className="herdr-question-option tick">
              <input
                type="checkbox"
                checked={ticked.includes(option.optionNumber)}
                disabled={isBusy}
                onChange={(event) =>
                  setTicked((before) =>
                    event.target.checked
                      ? [...before, option.optionNumber]
                      : before.filter((number) => number !== option.optionNumber),
                  )
                }
              />
              <span>
                <strong>{option.optionLabel}</strong>
                {option.optionDescription ? <span className="muted"> {option.optionDescription}</span> : null}
              </span>
            </label>
          ) : (
            <button
              key={option.optionNumber}
              type="button"
              className="herdr-question-option"
              disabled={isBusy}
              onClick={() => void answer([option.optionNumber], '')}
            >
              <strong>
                {option.optionNumber}. {option.optionLabel}
              </strong>
              {option.optionDescription ? <span className="muted">{option.optionDescription}</span> : null}
            </button>
          ),
        )}
      </div>
      {question.isMultipleChoice ? (
        <button
          type="button"
          className="primary"
          disabled={isBusy || ticked.length === 0}
          onClick={() =>
            void answer(
              [...ticked].sort((left, right) => left - right),
              '',
            )
          }
        >
          {t('herdr.submitChoices')}
        </button>
      ) : null}
      {freeTextOption && !question.isMultipleChoice ? (
        <form
          className="herdr-question-free"
          onSubmit={(event) => {
            event.preventDefault()
            if (freeText.trim()) void answer([freeTextOption.optionNumber], freeText.trim())
          }}
        >
          <input
            type="text"
            value={freeText}
            disabled={isBusy}
            placeholder={t('herdr.typeAnswer')}
            aria-label={t('herdr.typeAnswer')}
            onChange={(event) => setFreeText(event.target.value)}
          />
          <button type="submit" disabled={isBusy || !freeText.trim()}>
            {t('herdr.sendAnswer')}
          </button>
        </form>
      ) : null}
    </div>
  )
}

// HerdrSessionDialog is one session: what it asks, its last turns or its
// screen, and a box to type into it.
function HerdrSessionDialog({
  session,
  onChanged,
  onClose,
}: {
  session: HerdrSession
  onChanged: () => void
  onClose: () => void
}) {
  const { t } = useTranslation()
  const toast = useToast()
  const [view, setView] = useState<'turns' | 'screen'>('turns')
  const [current, setCurrent] = useState(session)
  const [text, setText] = useState('')
  const [isSending, setSending] = useState(false)
  const [readCount, setReadCount] = useState(0)
  const { data: read, error: readError } = useQuery(
    () =>
      view === 'turns'
        ? graphql<{ ReadAgentHerdrSession: { herdrSession: HerdrSession; turns: HerdrTurn[]; isTruncated: boolean } }>(
            HERDR_DOCUMENTS.ReadAgentHerdrSession,
            { computer: session.computer, paneId: session.paneId, turnCount: 20 },
          ).then((response) => ({
            session: response.ReadAgentHerdrSession.herdrSession,
            turns: response.ReadAgentHerdrSession.turns,
            screen: '',
          }))
        : graphql<{ ReadAgentHerdrScreen: { herdrSession: HerdrSession; screenText: string } }>(
            HERDR_DOCUMENTS.ReadAgentHerdrScreen,
            { computer: session.computer, paneId: session.paneId },
          ).then((response) => ({
            session: response.ReadAgentHerdrScreen.herdrSession,
            turns: [],
            screen: response.ReadAgentHerdrScreen.screenText,
          })),
    [view, session.computer, session.paneId, readCount],
  )
  useEffect(() => {
    if (read?.session) setCurrent(read.session)
  }, [read])
  const isSendable = current.herdrSessionState === 'idle' || current.herdrSessionState === 'working'

  const send = async () => {
    setSending(true)
    try {
      const response = await graphql<{ SendAgentHerdrSession: HerdrSession }>(HERDR_DOCUMENTS.SendAgentHerdrSession, {
        computer: current.computer,
        paneId: current.paneId,
        text,
        shouldQueue: current.herdrSessionState === 'working',
      })
      setCurrent(response.SendAgentHerdrSession)
      setText('')
      toast.done(t('herdr.sent', { pane: current.paneId }))
      onChanged()
    } catch (caught) {
      toast.failure(caught, t('herdr.sendFailed'))
    } finally {
      setSending(false)
    }
  }

  const watch = async () => {
    try {
      const conversations = await graphql<{ ListAgentConversations: { id: string; kind: string }[] }>(MAIN_CONVERSATION)
      const main = conversations.ListAgentConversations.find((conversation) => conversation.kind === 'main')
      if (!main) throw new Error(t('herdr.noMainConversation'))
      const response = await graphql<{ WatchAgentHerdrSession: HerdrSession }>(HERDR_DOCUMENTS.WatchAgentHerdrSession, {
        computer: current.computer,
        paneId: current.paneId,
        conversationId: main.id,
      })
      setCurrent(response.WatchAgentHerdrSession)
      toast.done(t('herdr.watching', { pane: current.paneId }))
      onChanged()
    } catch (caught) {
      toast.failure(caught, t('herdr.watchFailed'))
    }
  }

  return (
    <ConfirmDialog
      title={`${codingAgentName(current.codingAgentKind)} · ${current.paneId} · ${current.computer}`}
      wide
      onClose={onClose}
      otherAction={
        current.isWatched ? undefined : (
          <button type="button" onClick={() => void watch()}>
            {t('herdr.watch')}
          </button>
        )
      }
      body={
        <div className="herdr-session">
          <p className="herdr-session-meta muted">
            <HerdrStateTag session={current} />
            <span className="mono">{current.workingDirectory}</span>
            {current.paneTitle ? <span>{current.paneTitle}</span> : null}
            {current.isWatched ? <span>{t('herdr.watched')}</span> : null}
          </p>
          {current.question ? (
            <HerdrQuestionAnswer
              key={current.question.questionFingerprint}
              session={current}
              question={current.question}
              onAnswered={(after) => {
                if (after) setCurrent(after)
                setReadCount((count) => count + 1)
                onChanged()
              }}
            />
          ) : null}
          <Tabs
            items={[
              { id: 'turns', label: 'herdr.turns' },
              { id: 'screen', label: 'herdr.screen' },
            ]}
            active={view}
            onSelect={(id) => setView(id as 'turns' | 'screen')}
          />
          {read === null && !readError ? (
            <Loading />
          ) : readError ? (
            <p className="muted">{readError instanceof Error ? readError.message : String(readError)}</p>
          ) : view === 'screen' ? (
            <pre className="herdr-screen">{read?.screen}</pre>
          ) : (
            <ol className="herdr-turns">
              {(read?.turns ?? []).map((turn, index) => (
                <li key={index} className={`herdr-turn ${turn.herdrTurnRole}`}>
                  <span className="herdr-turn-role muted">
                    {t(`herdr.role.${turn.herdrTurnRole}` as 'herdr.role.person')}
                  </span>
                  <span className="herdr-turn-text">{turn.turnText}</span>
                </li>
              ))}
            </ol>
          )}
          <form
            className="herdr-send"
            onSubmit={(event) => {
              event.preventDefault()
              if (text.trim()) void send()
            }}
          >
            <textarea
              rows={2}
              value={text}
              disabled={!isSendable || isSending}
              placeholder={isSendable ? t('herdr.sendPlaceholder') : t('herdr.sendDisabled')}
              aria-label={t('herdr.sendPlaceholder')}
              onChange={(event) => setText(event.target.value)}
            />
            <button type="submit" disabled={!isSendable || isSending || !text.trim()}>
              {current.herdrSessionState === 'working' ? t('herdr.queue') : t('herdr.send')}
            </button>
          </form>
        </div>
      }
    />
  )
}

// HerdrHooksDialog puts TeaNode's reporting hooks into Claude Code on a
// computer, or takes them out.
function HerdrHooksDialog({ computerNames, onClose }: { computerNames: string[]; onClose: () => void }) {
  const { t } = useTranslation()
  const toast = useToast()
  const [busy, setBusy] = useState('')
  const setUp = async (computer: string, isRemoval: boolean) => {
    setBusy(computer)
    try {
      const response = await graphql<{
        SetUpAgentHerdrHooks: { computer: string; isInstalled: boolean; settingsPath: string; backupPath: string }
      }>(HERDR_DOCUMENTS.SetUpAgentHerdrHooks, { computer, isRemoval })
      const setup = response.SetUpAgentHerdrHooks
      toast.done(
        setup.isInstalled
          ? t('herdr.hooksInstalled', { computer: setup.computer, path: setup.settingsPath })
          : t('herdr.hooksRemoved', { computer: setup.computer, path: setup.settingsPath }),
      )
    } catch (caught) {
      toast.failure(caught, t('herdr.hooksFailed'))
    } finally {
      setBusy('')
    }
  }
  return (
    <ConfirmDialog
      title={t('herdr.hooksTitle')}
      onClose={onClose}
      body={
        <div className="herdr-hooks">
          <p>{t('herdr.hooksHint')}</p>
          <table>
            <tbody>
              {computerNames.map((computer) => (
                <tr key={computer}>
                  <td>{computer}</td>
                  <td className="herdr-hooks-actions">
                    <button type="button" disabled={busy !== ''} onClick={() => void setUp(computer, false)}>
                      {t('herdr.hooksInstall')}
                    </button>
                    <button
                      type="button"
                      className="danger"
                      disabled={busy !== ''}
                      onClick={() => void setUp(computer, true)}
                    >
                      {t('herdr.hooksRemove')}
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      }
    />
  )
}

// HerdrSessionsCard is every session on every computer, for the agent page:
// a row each, which opens it.
export function HerdrSessionsCard() {
  const { t } = useTranslation()
  const toast = useToast()
  const { data, error, reload } = useQuery(() => listHerdrSessions(true), [])
  const [opened, setOpened] = useState<HerdrSession | null>(null)
  const [isSettingUp, setSettingUp] = useState(false)
  useEffect(() => {
    if (error) toast.failure(error, t('herdr.listFailed'))
  }, [error, toast, t])
  const computerNames = data?.computerNames ?? []
  return (
    <SettingsSection
      card
      title={t('herdr.title')}
      description={t('herdr.hint')}
      action={
        computerNames.length > 0 ? (
          <button type="button" onClick={() => setSettingUp(true)}>
            {t('herdr.hooks')}
          </button>
        ) : undefined
      }
    >
      {data === null && !error ? (
        <Loading />
      ) : computerNames.length === 0 ? (
        <SettingsEmpty>{t('herdr.noComputer')}</SettingsEmpty>
      ) : data?.sessions.length === 0 ? (
        <SettingsEmpty>{t('herdr.none', { computers: computerNames.join(', ') })}</SettingsEmpty>
      ) : (
        <div className="table-wrap">
          <table className="herdr-sessions">
            <thead>
              <tr>
                <th>{t('herdr.computer')}</th>
                <th>{t('herdr.pane')}</th>
                <th>{t('herdr.agent')}</th>
                <th>{t('herdr.stateHeading')}</th>
                <th>{t('herdr.directory')}</th>
                <th>{t('herdr.paneTitle')}</th>
              </tr>
            </thead>
            <tbody>
              {(data?.sessions ?? []).map((session) => (
                <tr key={`${session.computer}/${session.paneId}`}>
                  <td>{session.computer}</td>
                  <td>
                    <button
                      /* link-button: names a session in a list, which opens it in place */
                      type="button"
                      className="link"
                      onClick={() => setOpened(session)}
                    >
                      {session.paneId}
                    </button>
                  </td>
                  <td>{codingAgentName(session.codingAgentKind)}</td>
                  <td>
                    <HerdrStateTag session={session} />
                  </td>
                  <td className="mono">{session.workingDirectory}</td>
                  <td>{session.paneTitle}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {opened ? (
        <HerdrSessionDialog
          key={`${opened.computer}/${opened.paneId}`}
          session={opened}
          onChanged={() => void reload(true)}
          onClose={() => setOpened(null)}
        />
      ) : null}
      {isSettingUp ? <HerdrHooksDialog computerNames={computerNames} onClose={() => setSettingUp(false)} /> : null}
    </SettingsSection>
  )
}

// HerdrQuestionCard is a question as the drawer shows it under the line it
// was written under: its options while the session still asks it, and the
// word that it was answered once it does not. QUESTION_EVERY is how often
// it looks again while it waits.
const QUESTION_EVERY = 5_000

export function HerdrQuestionCard({
  computer,
  paneId,
  questionFingerprint,
}: {
  computer: string
  paneId: string
  questionFingerprint: string
}) {
  const { t } = useTranslation()
  const [found, setFound] = useState<{ session: HerdrSession; question: HerdrQuestion } | null>(null)
  const [isAnswered, setAnswered] = useState(false)
  const [lookCount, setLookCount] = useState(0)
  useEffect(() => {
    if (isAnswered) return
    let isStopped = false
    const look = (fresh: boolean) => {
      if (document.hidden && !fresh) return
      listHerdrSessions(fresh)
        .then((listed) => {
          if (isStopped) return
          const session = listed.sessions.find((each) => each.computer === computer && each.paneId === paneId)
          if (session?.question?.questionFingerprint === questionFingerprint) {
            setFound({ session, question: session.question })
          } else if (listed.computerNames.includes(computer)) {
            setAnswered(true)
          }
        })
        .catch(() => undefined)
    }
    look(lookCount > 0)
    const every = window.setInterval(() => look(false), QUESTION_EVERY)
    return () => {
      isStopped = true
      window.clearInterval(every)
    }
  }, [computer, paneId, questionFingerprint, isAnswered, lookCount])
  if (isAnswered) return <p className="herdr-question-done muted">{t('herdr.questionDone')}</p>
  if (!found) return null
  return (
    <div className="herdr-question-card">
      <HerdrQuestionAnswer
        session={found.session}
        question={found.question}
        onAnswered={() => setLookCount((count) => count + 1)}
      />
    </div>
  )
}

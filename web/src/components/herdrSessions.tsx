import { useEffect, useRef, useState } from 'react'

import { graphql } from '../api'
import { ErrorMessage, Loading, Tag } from './common'
import { CheckIcon } from './icons'
import { ConfirmDialog, FormDialog } from './dialog'
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
  // The pane as the person finds it in herdr: workspace, tab, agent.
  paneName: string
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
  // The computers that did not answer, whose sessions are not listed.
  failedComputerNames: string[]
  sessions: HerdrSession[]
}

interface HerdrTurn {
  herdrTurnRole: 'person' | 'agent' | 'tool'
  turnText: string
  turnAt?: string | null
}

// The fields of a session, the same as the command line's
// client.HerdrSessionFields.
const SESSION_FIELDS = `computer paneId paneName codingAgentKind codingSessionId herdrSessionState herdrAgentStatus paneTitle workingDirectory transcriptPath isWatched
  question { questionFingerprint herdrQuestionKind questionText isMultipleChoice isFromTranscript options { optionNumber optionLabel optionDescription herdrOptionKind } }`

export const HERDR_DOCUMENTS = {
  ListAgentHerdrSessions: `query ($computer: String) {
    ListAgentHerdrSessions(computer: $computer) { computerNames failedComputerNames sessions { ${SESSION_FIELDS} } }
  }`,
  ReadAgentHerdrSession: `query ($computer: String, $paneId: String!, $turnCount: Int) {
    ReadAgentHerdrSession(computer: $computer, paneId: $paneId, turnCount: $turnCount) {
      herdrSession { ${SESSION_FIELDS} } turns { herdrTurnRole turnText turnAt } isTruncated
    }
  }`,
  ReadAgentHerdrScreen: `query ($computer: String, $paneId: String!, $lineCount: Int) {
    ReadAgentHerdrScreen(computer: $computer, paneId: $paneId, lineCount: $lineCount) { herdrSession { ${SESSION_FIELDS} } screenText }
  }`,
  SendAgentHerdrSession: `mutation ($computer: String, $paneId: String!, $text: String!) {
    SendAgentHerdrSession(computer: $computer, paneId: $paneId, text: $text) { ${SESSION_FIELDS} }
  }`,
  WaitAgentHerdrSession: `query ($computer: String, $paneId: String!, $waitSeconds: Int) {
    WaitAgentHerdrSession(computer: $computer, paneId: $paneId, waitSeconds: $waitSeconds) { herdrSession { ${SESSION_FIELDS} } isTimedOut }
  }`,
  AnswerAgentHerdrQuestion: `mutation ($computer: String, $paneId: String!, $questionFingerprint: String!, $optionNumbers: [Int!], $optionLabels: [String!], $freeText: String) {
    AnswerAgentHerdrQuestion(computer: $computer, paneId: $paneId, questionFingerprint: $questionFingerprint, optionNumbers: $optionNumbers, optionLabels: $optionLabels, freeText: $freeText) {
      herdrSession { ${SESSION_FIELDS} } isAnswerAccepted answeredWith
    }
  }`,
  WatchAgentHerdrSession: `mutation ($computer: String, $paneId: String!, $conversationId: String) {
    WatchAgentHerdrSession(computer: $computer, paneId: $paneId, conversationId: $conversationId) { ${SESSION_FIELDS} }
  }`,
  OpenAgentHerdrSession: `mutation ($computer: String, $directory: String!, $codingAgentKind: String!, $agentName: String, $shouldSkipPermissions: Boolean) {
    OpenAgentHerdrSession(computer: $computer, directory: $directory, codingAgentKind: $codingAgentKind, agentName: $agentName, shouldSkipPermissions: $shouldSkipPermissions) { ${SESSION_FIELDS} }
  }`,
  CloseAgentHerdrSession: `mutation ($computer: String, $paneId: String!) {
    CloseAgentHerdrSession(computer: $computer, paneId: $paneId) { ${SESSION_FIELDS} }
  }`,
  SetUpAgentHerdrHooks: `mutation ($computer: String, $isRemoval: Boolean) {
    SetUpAgentHerdrHooks(computer: $computer, isRemoval: $isRemoval) { computer isInstalled settingsPath scriptPath backupPath hookEventNames }
  }`,
} as const

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

// paneNameOf is a pane as the person knows it in herdr, or its id where
// herdr gave it no name.
export function paneNameOf(session: HerdrSession): string {
  return session.paneName || session.paneId
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
  const herdrSessionState = session.herdrSessionState
  const tone = herdrSessionState === 'asking' ? 'warn' : herdrSessionState === 'working' ? 'good' : undefined
  return <Tag value={t(`herdr.state.${herdrSessionState}` as 'herdr.state.idle')} tone={tone} />
}

// HerdrQuestionAnswer is a question with its options as buttons: a tap
// answers a question that takes one, ticks one of several, and a text box
// takes what an option that takes text is to type. onAnswered is told the
// session as it stands after.
export function HerdrQuestionAnswer({
  session,
  question,
  isQuestionShown = true,
  answeredWith,
  onAnswered,
}: {
  session: HerdrSession
  question: HerdrQuestion
  // Left out where a heading above says it already.
  isQuestionShown?: boolean
  // Given once the question was answered: the options are drawn as they
  // were, nothing can be pressed, and the ones chosen are marked. Empty
  // when the answer is not known.
  answeredWith?: string
  onAnswered?: (after: HerdrSession | null, answeredWith?: string) => void
}) {
  const { t } = useTranslation()
  const toast = useToast()
  const isFrozen = answeredWith !== undefined
  const { chosenNumbers, typedAnswer } = chosenOf(question, answeredWith ?? '')
  const [ticked, setTicked] = useState<number[]>([])
  const [freeText, setFreeText] = useState('')
  const [isBusy, setBusy] = useState(false)
  const freeTextOption = question.options.find((option) => option.herdrOptionKind === 'freeText')
  // A question that takes several answers takes only its choices: the way
  // out into a conversation is one answer on its own.
  const choices = question.options.filter((option) =>
    question.isMultipleChoice ? option.herdrOptionKind === 'choice' : option.herdrOptionKind !== 'freeText',
  )

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
        // The labels as shown, so a question that changed under the same
        // numbers is refused rather than answered.
        optionLabels: optionNumbers.map(
          (number) => question.options.find((option) => option.optionNumber === number)?.optionLabel ?? '',
        ),
        freeText: text || null,
      })
      const answered = response.AnswerAgentHerdrQuestion
      if (answered.isAnswerAccepted) {
        toast.done(t('herdr.answered', { answer: answered.answeredWith, pane: paneNameOf(session) }))
      } else {
        toast.failure(null, t('herdr.answerStillThere', { pane: paneNameOf(session) }))
      }
      sharedList = null
      onAnswered?.(answered.herdrSession, answered.answeredWith)
    } catch (caught) {
      toast.failure(caught, t('herdr.answerFailed'))
      sharedList = null
      onAnswered?.(null)
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className={isFrozen ? 'herdr-question frozen' : 'herdr-question'}>
      {isQuestionShown ? (
        <>
          <p className="herdr-question-kind muted">
            {t(`herdr.kind.${question.herdrQuestionKind}` as 'herdr.kind.question')}
          </p>
          <pre className="herdr-question-text">{question.questionText}</pre>
        </>
      ) : null}
      <div className="herdr-question-options">
        {choices.map((option) =>
          question.isMultipleChoice ? (
            <label
              key={option.optionNumber}
              className={
                chosenNumbers.includes(option.optionNumber)
                  ? 'herdr-question-option tick chosen'
                  : 'herdr-question-option tick'
              }
            >
              <input
                type="checkbox"
                checked={isFrozen ? chosenNumbers.includes(option.optionNumber) : ticked.includes(option.optionNumber)}
                disabled={isBusy || isFrozen}
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
              className={
                chosenNumbers.includes(option.optionNumber) ? 'herdr-question-option chosen' : 'herdr-question-option'
              }
              disabled={isBusy || isFrozen}
              aria-pressed={isFrozen ? chosenNumbers.includes(option.optionNumber) : undefined}
              onClick={() => void answer([option.optionNumber], '')}
            >
              <strong>
                {chosenNumbers.includes(option.optionNumber) ? <CheckIcon size={14} /> : null}
                {option.optionNumber}. {option.optionLabel}
              </strong>
              {option.optionDescription ? <span className="muted">{option.optionDescription}</span> : null}
            </button>
          ),
        )}
      </div>
      {isFrozen && typedAnswer ? <p className="herdr-question-typed">{typedAnswer}</p> : null}
      {isFrozen && !answeredWith ? <p className="herdr-question-unknown muted">{t('herdr.answerUnknown')}</p> : null}
      {question.isMultipleChoice && !isFrozen ? (
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
      {freeTextOption && !question.isMultipleChoice && !isFrozen ? (
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

// isListed says an answer of several, joined by ", ", holds one item whole.
// An item's own commas ("Yes, and don't ask again") are kept: the item is
// looked for, rather than the answer split.
function isListed(answer: string, item: string): boolean {
  return (
    answer === item || answer.startsWith(`${item}, `) || answer.endsWith(`, ${item}`) || answer.includes(`, ${item}, `)
  )
}

// chosenOf reads an answer back onto a question's options: the ones it
// names, by label or as "number. label", and what was typed, when it names
// none.
function chosenOf(question: HerdrQuestion, answeredWith: string): { chosenNumbers: number[]; typedAnswer: string } {
  const answer = answeredWith.trim()
  if (!answer) return { chosenNumbers: [], typedAnswer: '' }
  const chosenNumbers = question.options
    .filter((option) => option.herdrOptionKind !== 'freeText')
    .filter(
      (option) =>
        isListed(answer, option.optionLabel) || isListed(answer, `${option.optionNumber}. ${option.optionLabel}`),
    )
    .map((option) => option.optionNumber)
  return { chosenNumbers, typedAnswer: chosenNumbers.length > 0 ? '' : answer.replace(/^"(.*)"$/, '$1') }
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
            view: 'turns',
            session: response.ReadAgentHerdrSession.herdrSession,
            turns: response.ReadAgentHerdrSession.turns,
            screen: '',
          }))
        : graphql<{ ReadAgentHerdrScreen: { herdrSession: HerdrSession; screenText: string } }>(
            HERDR_DOCUMENTS.ReadAgentHerdrScreen,
            { computer: session.computer, paneId: session.paneId },
          ).then((response) => ({
            view: 'screen',
            session: response.ReadAgentHerdrScreen.herdrSession,
            turns: [],
            screen: response.ReadAgentHerdrScreen.screenText,
          })),
    [view, session.computer, session.paneId, readCount],
  )
  useEffect(() => {
    if (read?.session) setCurrent(read.session)
  }, [read])
  // What the program on the computer allows: anything but a session that
  // asks, which is answered first.
  const isSendable = current.herdrSessionState !== 'asking'

  const send = async () => {
    setSending(true)
    try {
      const response = await graphql<{ SendAgentHerdrSession: HerdrSession }>(HERDR_DOCUMENTS.SendAgentHerdrSession, {
        computer: current.computer,
        paneId: current.paneId,
        text,
      })
      setCurrent(response.SendAgentHerdrSession)
      setText('')
      toast.done(t('herdr.sent', { pane: paneNameOf(current) }))
      onChanged()
    } catch (caught) {
      toast.failure(caught, t('herdr.sendFailed'))
    } finally {
      setSending(false)
    }
  }

  const [isClosing, setClosing] = useState(false)
  const [isClosingBusy, setClosingBusy] = useState(false)
  const close = async () => {
    setClosingBusy(true)
    try {
      await graphql(HERDR_DOCUMENTS.CloseAgentHerdrSession, { computer: current.computer, paneId: current.paneId })
      toast.done(t('herdr.closed', { pane: paneNameOf(current) }))
      sharedList = null
      onChanged()
      onClose()
    } catch (caught) {
      toast.failure(caught, t('herdr.closeFailed'))
      setClosing(false)
    } finally {
      setClosingBusy(false)
    }
  }

  const watch = async () => {
    try {
      // No conversation named: the server wakes the main one.
      const response = await graphql<{ WatchAgentHerdrSession: HerdrSession }>(HERDR_DOCUMENTS.WatchAgentHerdrSession, {
        computer: current.computer,
        paneId: current.paneId,
      })
      setCurrent(response.WatchAgentHerdrSession)
      toast.done(t('herdr.watching', { pane: paneNameOf(current) }))
      onChanged()
    } catch (caught) {
      toast.failure(caught, t('herdr.watchFailed'))
    }
  }

  if (isClosing) {
    return (
      <ConfirmDialog
        title={t('herdr.closeTitle')}
        body={
          <p>
            {t('herdr.closeBody', {
              agent: codingAgentName(current.codingAgentKind),
              pane: paneNameOf(current),
              computer: current.computer,
            })}
          </p>
        }
        confirmLabel={t('herdr.closeSession')}
        busy={isClosingBusy}
        onConfirm={() => void close()}
        onClose={() => setClosing(false)}
      />
    )
  }
  return (
    <ConfirmDialog
      title={`${codingAgentName(current.codingAgentKind)} · ${paneNameOf(current)} · ${current.computer}`}
      wide
      onClose={onClose}
      otherAction={
        <div className="row-actions">
          {current.isWatched ? null : (
            <button type="button" onClick={() => void watch()}>
              {t('herdr.watch')}
            </button>
          )}
          <button
            type="button"
            className="danger"
            disabled={current.herdrSessionState === 'working'}
            title={current.herdrSessionState === 'working' ? t('herdr.closeWhileWorking') : undefined}
            onClick={() => setClosing(true)}
          >
            {t('herdr.closeSession')}
          </button>
        </div>
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
          {readError ? (
            <ErrorMessage error={readError} />
          ) : read === null || read.view !== view ? (
            <Loading />
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
              {t('herdr.send')}
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
  const [busyComputerName, setBusyComputerName] = useState('')
  // Taking the hooks out rewrites the person's settings, so it is asked
  // first; putting them in keeps a copy of the file.
  const [removingComputerName, setRemovingComputerName] = useState('')
  const setUp = async (computer: string, isRemoval: boolean) => {
    setBusyComputerName(computer)
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
      setBusyComputerName('')
    }
  }
  if (removingComputerName) {
    return (
      <ConfirmDialog
        title={t('herdr.hooksRemoveTitle')}
        body={<p>{t('herdr.hooksRemoveBody', { computer: removingComputerName })}</p>}
        confirmLabel={t('herdr.hooksRemove')}
        busy={busyComputerName !== ''}
        onConfirm={() => void setUp(removingComputerName, true).then(() => setRemovingComputerName(''))}
        onClose={() => setRemovingComputerName('')}
      />
    )
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
                    <button
                      type="button"
                      disabled={busyComputerName !== ''}
                      onClick={() => void setUp(computer, false)}
                    >
                      {t('herdr.hooksInstall')}
                    </button>
                    <button
                      type="button"
                      className="danger"
                      disabled={busyComputerName !== ''}
                      onClick={() => setRemovingComputerName(computer)}
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

// HerdrOpenDialog starts Claude Code or Codex in a new herdr pane, in a
// directory on one of the person's computers.
function HerdrOpenDialog({
  computerNames,
  onOpened,
  onClose,
}: {
  computerNames: string[]
  onOpened: (session: HerdrSession) => void
  onClose: () => void
}) {
  const { t } = useTranslation()
  const toast = useToast()
  const [computer, setComputer] = useState(computerNames[0] ?? '')
  const [codingAgentKind, setCodingAgentKind] = useState<'claude' | 'codex'>('claude')
  const [directory, setDirectory] = useState('')
  const [agentName, setAgentName] = useState('')
  const [shouldSkipPermissions, setShouldSkipPermissions] = useState(false)
  const [isBusy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const open = async () => {
    setBusy(true)
    setError(null)
    try {
      const response = await graphql<{ OpenAgentHerdrSession: HerdrSession }>(HERDR_DOCUMENTS.OpenAgentHerdrSession, {
        computer,
        directory: directory.trim(),
        codingAgentKind,
        agentName: agentName.trim() || null,
        shouldSkipPermissions,
      })
      sharedList = null
      toast.done(t('herdr.opened', { pane: paneNameOf(response.OpenAgentHerdrSession) }))
      onOpened(response.OpenAgentHerdrSession)
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : String(caught))
    } finally {
      setBusy(false)
    }
  }
  return (
    <FormDialog
      title={t('herdr.openTitle')}
      submitLabel={t('herdr.open')}
      busy={isBusy}
      error={error}
      canSubmit={directory.trim() !== '' && computer !== ''}
      onSubmit={() => void open()}
      onClose={onClose}
    >
      {computerNames.length > 1 ? (
        <label>
          <span>{t('herdr.computer')}</span>
          <select value={computer} onChange={(event) => setComputer(event.target.value)}>
            {computerNames.map((name) => (
              <option key={name} value={name}>
                {name}
              </option>
            ))}
          </select>
        </label>
      ) : null}
      <label>
        <span>{t('herdr.codingAgent')}</span>
        <select
          value={codingAgentKind}
          onChange={(event) => setCodingAgentKind(event.target.value as 'claude' | 'codex')}
        >
          <option value="claude">Claude Code</option>
          <option value="codex">Codex</option>
        </select>
      </label>
      <label>
        <span>{t('herdr.directory')}</span>
        <input
          type="text"
          value={directory}
          placeholder="~/src/example"
          autoFocus
          onChange={(event) => setDirectory(event.target.value)}
        />
      </label>
      <label>
        <span>{t('herdr.agentNameField')}</span>
        <input
          type="text"
          value={agentName}
          placeholder={t('herdr.agentNameHint')}
          onChange={(event) => setAgentName(event.target.value)}
        />
      </label>
      <label>
        <input
          type="checkbox"
          checked={shouldSkipPermissions}
          onChange={(event) => setShouldSkipPermissions(event.target.checked)}
        />
        {codingAgentKind === 'codex' ? t('herdr.skipPermissionsCodex') : t('herdr.skipPermissionsClaude')}
      </label>
    </FormDialog>
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
  const [isOpening, setOpening] = useState(false)
  useEffect(() => {
    if (error) toast.failure(error, t('herdr.listFailed'))
  }, [error, toast, t])
  const computerNames = data?.computerNames ?? []
  const failedComputerNames = data?.failedComputerNames ?? []
  return (
    <SettingsSection
      card
      title={t('herdr.title')}
      description={t('herdr.hint')}
      action={
        computerNames.length > 0 ? (
          <div className="row-actions">
            <button type="button" className="primary" onClick={() => setOpening(true)}>
              {t('herdr.open')}
            </button>
            <button type="button" onClick={() => setSettingUp(true)}>
              {t('herdr.hooks')}
            </button>
          </div>
        ) : undefined
      }
    >
      {data === null && error ? null : data === null ? (
        <Loading />
      ) : computerNames.length === 0 ? (
        <SettingsEmpty>{t('herdr.noComputer')}</SettingsEmpty>
      ) : data.sessions.length === 0 && failedComputerNames.length === 0 ? (
        <SettingsEmpty>{t('herdr.none', { computers: computerNames.join(', ') })}</SettingsEmpty>
      ) : (
        <>
          {failedComputerNames.length > 0 ? (
            <p className="muted">{t('herdr.notAnswering', { computers: failedComputerNames.join(', ') })}</p>
          ) : null}
          <div className="table-wrap">
            <table className="herdr-sessions">
              <thead>
                <tr>
                  <th>{t('herdr.pane')}</th>
                  <th>{t('herdr.stateHeading')}</th>
                  <th>{t('herdr.agent')}</th>
                  <th>{t('herdr.computer')}</th>
                  <th>{t('herdr.directory')}</th>
                  <th>{t('herdr.paneTitle')}</th>
                </tr>
              </thead>
              <tbody>
                {data.sessions.map((session) => (
                  <tr key={`${session.computer}/${session.paneId}`}>
                    <td>
                      <button
                        /* link-button: names a session in a list, which opens it in place */
                        type="button"
                        className="link"
                        title={session.paneId}
                        onClick={() => setOpened(session)}
                      >
                        {paneNameOf(session)}
                      </button>
                    </td>
                    <td>
                      <HerdrStateTag session={session} />
                    </td>
                    <td>{codingAgentName(session.codingAgentKind)}</td>
                    <td>{session.computer}</td>
                    <td className="mono">{session.workingDirectory}</td>
                    <td>{session.paneTitle}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </>
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
      {isOpening ? (
        <HerdrOpenDialog
          computerNames={computerNames}
          onOpened={(session) => {
            setOpening(false)
            void reload(true)
            setOpened(session)
          }}
          onClose={() => setOpening(false)}
        />
      ) : null}
    </SettingsSection>
  )
}

// HerdrQuestionCard is a question as the drawer shows it under the line it
// was written under: its options while the session still asks it, and the
// word that it was answered once it does not. QUESTION_EVERY is how often
// it looks again while it waits; a question is called answered only when
// its computer answered the list twice in a row without it, so one slow
// computer or one screen read mid-redraw does not take the buttons away.
// A computer that is not attached is looked for QUESTION_ABSENT_LOOKS
// times, then left: an old line in a long transcript asks nothing more.
const QUESTION_EVERY = 5_000
const QUESTION_RECHECK = 1_500
const QUESTION_ABSENT_LOOKS = 6

export function HerdrQuestionCard({
  computer,
  paneId,
  questionFingerprint,
  initial,
  isHeaderShown = true,
  isQuestionShown = false,
  onAnswered,
  onUnreachable,
}: {
  computer: string
  paneId: string
  questionFingerprint: string
  // The session and its question as they were when it was asked, to draw
  // at once while the computer is asked whether it still waits.
  initial?: HerdrSession
  isHeaderShown?: boolean
  isQuestionShown?: boolean
  // Told once the question is found answered, with what it was answered
  // with when this card answered it, or its computer gone, so what holds
  // the card can fold it away.
  onAnswered?: (answeredWith?: string) => void
  onUnreachable?: () => void
}) {
  const { t } = useTranslation()
  const [found, setFound] = useState<{ session: HerdrSession; question: HerdrQuestion } | null>(
    initial?.question ? { session: initial, question: initial.question } : null,
  )
  const [questionState, setQuestionState] = useState<'waiting' | 'answered' | 'unreachable'>('waiting')
  const [lookCount, setLookCount] = useState(0)
  const answeredWithHere = useRef<string | undefined>(undefined)
  const missCount = useRef(0)
  const absentCount = useRef(0)
  useEffect(() => {
    if (questionState !== 'waiting') return
    let isStopped = false
    let recheck: number | undefined
    // The first look is made however the page stands, so a drawer drawn
    // in a tab out of sight has its options when it is looked at; the
    // ones after wait for the page to be seen.
    const look = (fresh: boolean, isFirst = false) => {
      if (document.hidden && !fresh && !isFirst) return
      listHerdrSessions(fresh)
        .then((listed) => {
          if (isStopped) return
          if (!listed.computerNames.includes(computer)) {
            absentCount.current += 1
            if (absentCount.current >= QUESTION_ABSENT_LOOKS) setQuestionState('unreachable')
            return
          }
          absentCount.current = 0
          if (listed.failedComputerNames.includes(computer)) return
          const session = listed.sessions.find((each) => each.computer === computer && each.paneId === paneId)
          if (session?.question?.questionFingerprint === questionFingerprint) {
            missCount.current = 0
            setFound({ session, question: session.question })
            return
          }
          missCount.current += 1
          if (missCount.current >= 2) {
            setQuestionState('answered')
            return
          }
          // A second look soon after the first miss, rather than at the
          // next round: a question already answered says so in a moment
          // when the page opens, not after a round of waiting.
          recheck = window.setTimeout(() => look(true), QUESTION_RECHECK)
        })
        .catch(() => undefined)
    }
    look(lookCount > 0, true)
    const every = window.setInterval(() => look(false), QUESTION_EVERY)
    const seen = () => {
      if (!document.hidden) look(false)
    }
    document.addEventListener('visibilitychange', seen)
    return () => {
      isStopped = true
      window.clearInterval(every)
      window.clearTimeout(recheck)
      document.removeEventListener('visibilitychange', seen)
    }
  }, [computer, paneId, questionFingerprint, questionState, lookCount])
  useEffect(() => {
    if (questionState === 'answered') onAnswered?.(answeredWithHere.current)
    if (questionState === 'unreachable') onUnreachable?.()
    // The callbacks are the holder's, and only the change of state matters.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [questionState])
  if (questionState !== 'waiting') return null
  // Until the computer says, a line rather than the question: a question
  // drawn whole and then folded a moment later is the flicker this avoids.
  if (!found) return <p className="herdr-question-checking muted">{t('herdr.checking')}</p>
  const { session } = found
  return (
    <div className="herdr-question-card">
      {isHeaderShown ? (
        <p className="herdr-question-who">
          <strong>{codingAgentName(session.codingAgentKind)}</strong>
          <span>{paneNameOf(session)}</span>
          <span>{session.computer}</span>
        </p>
      ) : null}
      <HerdrQuestionAnswer
        session={found.session}
        question={found.question}
        isQuestionShown={isQuestionShown}
        // An answer this card gave, taken by the session, is known at once.
        onAnswered={(after, answeredWith) => {
          if (after && after.question?.questionFingerprint !== questionFingerprint) {
            answeredWithHere.current = answeredWith
            setQuestionState('answered')
          } else setLookCount((count) => count + 1)
        }}
      />
    </div>
  )
}

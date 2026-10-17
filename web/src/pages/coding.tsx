import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'

import { graphql } from '../api'
import { Loading } from '../components/common'
import { ConfirmDialog } from '../components/dialog'
import {
  HERDR_DOCUMENTS,
  HerdrOpenDialog,
  HerdrQuestionAnswer,
  HerdrSendForm,
  type HerdrDraft,
  HerdrStateTag,
  HerdrTurnsList,
  codingAgentName,
  forgetHerdrSessions,
  listHerdrSessions,
  paneNameOf,
  type HerdrList,
  type HerdrSession,
  type HerdrTurn,
} from '../components/herdrSessions'
import {
  ChevronDownIcon,
  ChevronLeftIcon,
  ChevronRightIcon,
  ComputerIcon,
  EyeIcon,
  FolderIcon,
  PlusIcon,
  TrashIcon,
} from '../components/icons'
import { MenuButton } from '../components/menuButton'
import { SettingsEmpty } from '../components/settingsList'
import { Tooltip } from '../components/tooltip'
import { TerminalText } from '../components/terminalText'
import { useToast } from '../components/toast'
import { useTranslation } from '../i18n/i18n'

// The Coding page: the Claude Code and Codex sessions in herdr on the
// person's computers, a list down the left and the selected one beside it,
// its transcript or screen above a box to type into. Which session is
// selected is in the address, so a reload and Back land where the person was.
//
// The transcript and the list are read again while the page is in view: every
// 3 seconds for a session that is working or asking, every 15 for one that is
// idle, and the list every 5. Switching tabs here only changes what is
// viewed; "Show on computer" is what moves herdr's own screen.

const LIST_EVERY_MS = 5_000
const BUSY_EVERY_MS = 3_000
const IDLE_EVERY_MS = 15_000
const FIRST_TURN_COUNT = 50
// The most turns the server reads.
const MOST_TURN_COUNT = 100
// How near the end counts as at the end, for following new turns.
const AT_END_PX = 48

// SessionRead is what one look at a session brings back: its turns when the
// transcript is shown, its screen when the screen is.
interface SessionRead {
  herdrSession: HerdrSession
  turns: HerdrTurn[]
  isTruncated: boolean
  screenText: string
}

// useVisiblePolling runs load now and then every everyMS, only while the page
// is in view, and stops when the page unmounts. The answer is kept per
// resetKey: a new key shows nothing until its first answer. A new refreshKey
// reads again at once and keeps what was read until then. The error is
// cleared when the resetKey changes, and stays the same object until a
// read succeeds, so a computer that stays unreachable is said once.
function useVisiblePolling<Answer>(
  load: () => Promise<Answer>,
  everyMS: number,
  resetKey: string,
  refreshKey = '',
): { data: Answer | null; error: unknown; reload: () => void } {
  const [entry, setEntry] = useState<{ key: string; value: Answer } | null>(null)
  const [error, setError] = useState<unknown>(null)
  const [reloadCount, setReloadCount] = useState(0)
  const loadRef = useRef(load)
  loadRef.current = load
  // What failed for the last key says nothing of this one; a reload or a
  // refresh of the same key keeps it, so a failure is said once.
  const errorKey = useRef(resetKey)
  useEffect(() => {
    if (errorKey.current !== resetKey) {
      errorKey.current = resetKey
      setError(null)
    }
    let isStopped = false
    let isRunning = false
    // The first read is made even in a tab in the background, so the page
    // has something when it is looked at; only the repeats wait for it.
    let isRead = false
    const run = async () => {
      if (isRunning || (isRead && document.visibilityState !== 'visible')) return
      isRunning = true
      try {
        const value = await loadRef.current()
        if (isStopped) return
        setEntry((previous) =>
          previous && previous.key === resetKey && JSON.stringify(previous.value) === JSON.stringify(value)
            ? previous
            : { key: resetKey, value },
        )
        setError(null)
      } catch (caught) {
        if (!isStopped) setError((previous: unknown) => previous ?? caught)
      } finally {
        isRunning = false
        isRead = true
      }
    }
    void run()
    const timer = window.setInterval(() => void run(), everyMS)
    const onVisible = () => void run()
    document.addEventListener('visibilitychange', onVisible)
    return () => {
      isStopped = true
      window.clearInterval(timer)
      document.removeEventListener('visibilitychange', onVisible)
    }
  }, [everyMS, resetKey, refreshKey, reloadCount])
  return {
    data: entry && entry.key === resetKey ? entry.value : null,
    error,
    reload: () => setReloadCount((count) => count + 1),
  }
}

interface WorkspaceGroup {
  key: string
  label: string
  sessions: HerdrSession[]
}

interface ComputerGroup {
  computer: string
  workspaces: WorkspaceGroup[]
}

// byTabOrder is tabs as herdr numbers them, the pane id where it does not.
function byTabOrder(left: HerdrSession, right: HerdrSession): number {
  return (
    (left.tabNumber ?? 0) - (right.tabNumber ?? 0) ||
    left.paneId.localeCompare(right.paneId, undefined, { numeric: true })
  )
}

// groupSessions is the sessions by computer, as the computers are named, then
// by workspace in herdr's workspace order, each with its tabs in tab order.
export function groupSessions(list: HerdrList): ComputerGroup[] {
  const computers = [...list.computerNames]
  for (const session of list.sessions) if (!computers.includes(session.computer)) computers.push(session.computer)
  // Computers by name, so each stays where the person last saw it.
  computers.sort((left, right) => left.localeCompare(right, undefined, { sensitivity: 'base' }))
  return computers
    .map((computer) => {
      const workspaces = new Map<string, WorkspaceGroup & { number: number }>()
      for (const session of list.sessions.filter((each) => each.computer === computer)) {
        const key = session.workspaceId ?? ''
        const group = workspaces.get(key) ?? {
          key,
          label: session.workspaceLabel ?? '',
          number: session.workspaceNumber ?? 0,
          sessions: [],
        }
        group.sessions.push(session)
        workspaces.set(key, group)
      }
      return {
        computer,
        workspaces: [...workspaces.values()]
          .sort((left, right) => left.number - right.number || left.label.localeCompare(right.label))
          .map((group) => ({ ...group, sessions: group.sessions.sort(byTabOrder) })),
      }
    })
    .filter((group) => group.workspaces.length > 0)
}

// sessionTitleOf is what a session is about: the title its coding agent
// gave its pane, unless that is only the shell's prompt (user@host: path),
// the workspace's name or the coding agent's, which say nothing of it.
function sessionTitleOf(session: HerdrSession): string {
  const title = session.paneTitle.trim()
  if (title === '' || /^[^\s@]+@[^\s:]+:/.test(title) || title === session.workspaceLabel) return ''
  if (title.toLowerCase() === codingAgentName(session.codingAgentKind).toLowerCase()) return ''
  return title
}

// rowLabelsOf is a session's row in the list: what it is about first, then
// which tab and coding agent it is. Without herdr's tabs (an older program
// on the computer), the pane's name says where it is.
function rowLabelsOf(session: HerdrSession): { title: string; detail: string } {
  const agent = codingAgentName(session.codingAgentKind)
  const where = session.tabLabel ? tabWordsOf(session.tabLabel) : session.workspaceId ? '' : session.paneName
  const title = sessionTitleOf(session)
  if (title) return { title, detail: [where, agent].filter(Boolean).join(' · ') }
  return { title: where || agent, detail: where ? agent : '' }
}

// tabWordsOf names a tab: herdr numbers unnamed tabs, and a bare number
// reads better as "tab 2".
function tabWordsOf(tabLabel: string): string {
  return /^\d+$/.test(tabLabel) ? `tab ${tabLabel}` : tabLabel
}

// tabLabelOf is a session's tab in the tab strip: herdr's label for the tab,
// then what the session is about.
function tabLabelOf(session: HerdrSession): string {
  return (
    [session.tabLabel, sessionTitleOf(session)].filter(Boolean).join(' ') || codingAgentName(session.codingAgentKind)
  )
}

function isSameSession(left: { computer: string; paneId: string }, right: { computer: string; paneId: string }) {
  return left.computer === right.computer && left.paneId === right.paneId
}

export function CodingPage() {
  const { t, plural } = useTranslation()
  const toast = useToast()
  const [parameters, setParameters] = useSearchParams()
  const selectedComputer = parameters.get('computer') ?? ''
  const selectedPaneId = parameters.get('pane') ?? ''
  const [isOpening, setOpening] = useState(false)
  // A session just opened, which the list does not name yet.
  const [justOpened, setJustOpened] = useState<HerdrSession | null>(null)
  // What has been typed into each session's box and not sent, kept while the
  // person looks at other sessions, for the life of the page.
  const [drafts, setDrafts] = useState<Record<string, HerdrDraft>>({})
  // The last session seen for the selection, kept while its computer does not
  // answer, so one failed poll does not close the view.
  const lastSelected = useRef<HerdrSession | null>(null)
  const list = useVisiblePolling(() => listHerdrSessions(true), LIST_EVERY_MS, 'list')
  // A draft goes with its session: once the session's computer answers
  // without it, the pane is closed, and a new session herdr puts in a pane
  // of the same id starts with an empty box.
  const listedNow = list.data
  useEffect(() => {
    if (!listedNow) return
    setDrafts((previous) => {
      const kept = Object.fromEntries(
        Object.entries(previous).filter(([draftKey]) => {
          const computer = draftKey.slice(0, draftKey.indexOf('/'))
          if (listedNow.failedComputerNames.includes(computer)) return true
          if (justOpened && `${justOpened.computer}/${justOpened.paneId}` === draftKey) return true
          return listedNow.sessions.some((session) => `${session.computer}/${session.paneId}` === draftKey)
        }),
      )
      return Object.keys(kept).length === Object.keys(previous).length ? previous : kept
    })
  }, [listedNow, justOpened])
  useEffect(() => {
    if (list.error) toast.failure(list.error, t('herdr.listFailed'))
  }, [list.error, toast, t])
  const failedKey = list.data?.failedComputerNames.join(', ') ?? ''
  useEffect(() => {
    if (failedKey) toast.failure(null, t('herdr.notAnswering', { computers: failedKey }))
  }, [failedKey, toast, t])

  // The computers folded away in the list, in the address like the rest of
  // where the person is.
  const closedComputers = (parameters.get('closed') ?? '').split(',').filter(Boolean)
  const select = (session: HerdrSession | null) => {
    const changed: Record<string, string> = {}
    if (session) {
      changed.computer = session.computer
      changed.pane = session.paneId
      if (parameters.get('view') === 'screen') changed.view = 'screen'
    }
    if (closedComputers.length > 0) changed.closed = closedComputers.join(',')
    setParameters(changed)
  }
  const toggleComputer = (computer: string) => {
    const closed = closedComputers.includes(computer)
      ? closedComputers.filter((each) => each !== computer)
      : [...closedComputers, computer]
    const changed = new URLSearchParams(parameters)
    if (closed.length > 0) changed.set('closed', closed.join(','))
    else changed.delete('closed')
    setParameters(changed)
  }
  const listed = list.data?.sessions.find(
    (session) => session.computer === selectedComputer && session.paneId === selectedPaneId,
  )
  const isSelectedFailed = (list.data?.failedComputerNames ?? []).includes(selectedComputer)
  const remembered = lastSelected.current
  const selected =
    listed ??
    (justOpened && justOpened.computer === selectedComputer && justOpened.paneId === selectedPaneId
      ? justOpened
      : null) ??
    (isSelectedFailed && remembered && isSameSession(remembered, { computer: selectedComputer, paneId: selectedPaneId })
      ? remembered
      : null)
  lastSelected.current = selected
  const isReading = selectedPaneId !== ''
  const computerNames = list.data?.computerNames ?? []

  let listColumn: React.ReactNode
  if (list.data === null) listColumn = list.error ? null : <Loading />
  else if (computerNames.length === 0) listColumn = <SettingsEmpty>{t('herdr.noComputer')}</SettingsEmpty>
  else if (list.data.sessions.length === 0) {
    listColumn = <SettingsEmpty>{t('herdr.none', { computers: computerNames.join(', ') })}</SettingsEmpty>
  } else {
    listColumn = groupSessions(list.data).map((group) => {
      const sessions = group.workspaces.flatMap((workspace) => workspace.sessions)
      const askingCount = sessions.filter((session) => session.herdrSessionState === 'asking').length
      const workingCount = sessions.filter((session) => session.herdrSessionState === 'working').length
      // The open session's computer stays unfolded, so the row is there.
      const isClosed = closedComputers.includes(group.computer) && group.computer !== selectedComputer
      return (
        <section key={group.computer} className="coding-computer">
          <h3>
            <button
              type="button"
              className="coding-computer-head"
              aria-expanded={!isClosed}
              onClick={() => toggleComputer(group.computer)}
            >
              {isClosed ? <ChevronRightIcon size={14} /> : <ChevronDownIcon size={14} />}
              <ComputerIcon size={16} />
              <span className="coding-computer-name">{group.computer}</span>
              <span className="coding-computer-counts muted">
                {[
                  askingCount > 0 ? t('herdr.askingCount', { count: askingCount }) : '',
                  workingCount > 0 ? t('herdr.workingCount', { count: workingCount }) : '',
                  plural(sessions.length, { one: 'herdr.sessionCountOne', other: 'herdr.sessionCountOther' }),
                ]
                  .filter(Boolean)
                  .join(' · ')}
              </span>
            </button>
          </h3>
          {isClosed
            ? null
            : group.workspaces.map((workspace) => (
                <div key={workspace.key} className="coding-workspace">
                  {workspace.label ? (
                    <h4>
                      <FolderIcon size={14} />
                      {workspace.label}
                    </h4>
                  ) : null}
                  {workspace.sessions.map((session) => (
                    <button
                      key={session.paneId}
                      type="button"
                      className={
                        isSameSession(session, { computer: selectedComputer, paneId: selectedPaneId })
                          ? 'coding-row active'
                          : 'coding-row'
                      }
                      aria-current={
                        isSameSession(session, { computer: selectedComputer, paneId: selectedPaneId })
                          ? 'true'
                          : undefined
                      }
                      onClick={() => select(session)}
                    >
                      <span className="coding-row-text">
                        <strong>{rowLabelsOf(session).title}</strong>
                        {rowLabelsOf(session).detail ? (
                          <span className="muted">{rowLabelsOf(session).detail}</span>
                        ) : null}
                      </span>
                      {session.question ? (
                        <span className="coding-question-mark" role="img" aria-label={t('herdr.hasQuestion')}>
                          ?
                        </span>
                      ) : null}
                      <HerdrStateTag session={session} />
                    </button>
                  ))}
                </div>
              ))}
        </section>
      )
    })
  }

  return (
    <div className="coding-frame">
      <div className={isReading ? 'coding-split reading' : 'coding-split'}>
        <div className="coding-list">
          {computerNames.length > 0 ? (
            <div className="list-toolbar coding-toolbar">
              <Tooltip label={t('herdr.open')}>
                <button
                  type="button"
                  className="icon-button"
                  aria-label={t('herdr.open')}
                  onClick={() => setOpening(true)}
                >
                  <PlusIcon size={18} />
                </button>
              </Tooltip>
            </div>
          ) : null}
          <div className="coding-list-rows">{listColumn}</div>
        </div>
        <div className="coding-pane">
          {selected ? (
            <CodingSession
              key={`${selected.computer}/${selected.paneId}`}
              session={selected}
              siblings={(list.data?.sessions ?? []).filter(
                (each) =>
                  each.computer === selected.computer &&
                  (each.workspaceId ?? '') === (selected.workspaceId ?? '') &&
                  (selected.workspaceId ?? '') !== '',
              )}
              draft={drafts[`${selected.computer}/${selected.paneId}`]}
              onDraftChange={(draft) => {
                const draftKey = `${selected.computer}/${selected.paneId}`
                setDrafts((previous) => ({ ...previous, [draftKey]: draft }))
              }}
              onSelect={select}
              onBack={() => select(null)}
              onChanged={list.reload}
              onOpened={(session) => {
                setJustOpened(session)
                select(session)
                list.reload()
              }}
            />
          ) : isReading && list.data !== null ? (
            <div className="coding-gone">
              <button
                type="button"
                className="coding-back icon-button"
                aria-label={t('herdr.back')}
                onClick={() => select(null)}
              >
                <ChevronLeftIcon size={18} />
              </button>
              <p className="muted">{t('herdr.sessionGone')}</p>
            </div>
          ) : isReading ? (
            <Loading />
          ) : computerNames.length > 0 ? (
            <div className="mailbox-pane-placeholder">
              <span>{t('herdr.chooseSession')}</span>
            </div>
          ) : null}
        </div>
      </div>
      {isOpening ? (
        <HerdrOpenDialog
          computerNames={computerNames}
          onOpened={(session) => {
            setOpening(false)
            setJustOpened(session)
            select(session)
            list.reload()
          }}
          onClose={() => setOpening(false)}
        />
      ) : null}
    </div>
  )
}

// CodingSession is one selected session: its header with what can be done
// to it, the tabs of its workspace, a choice of transcript or screen, the
// question it asks, and the box to type into.
function CodingSession({
  session,
  siblings,
  draft,
  onDraftChange,
  onSelect,
  onBack,
  onChanged,
  onOpened,
}: {
  session: HerdrSession
  siblings: HerdrSession[]
  draft: HerdrDraft | undefined
  onDraftChange: (draft: HerdrDraft) => void
  onSelect: (session: HerdrSession) => void
  onBack: () => void
  onChanged: () => void
  onOpened: (session: HerdrSession) => void
}) {
  const { t } = useTranslation()
  const toast = useToast()
  const [latest, setLatest] = useState(session)
  const [parameters, setParameters] = useSearchParams()
  const view = parameters.get('view') === 'screen' ? 'screen' : 'turns'
  const setView = (next: 'turns' | 'screen') => {
    const changed = new URLSearchParams(parameters)
    if (next === 'screen') changed.set('view', 'screen')
    else changed.delete('view')
    setParameters(changed)
  }
  const [turnCount, setTurnCount] = useState(FIRST_TURN_COUNT)
  const [isClosing, setClosing] = useState(false)
  const [isClosingBusy, setClosingBusy] = useState(false)
  useEffect(() => setLatest(session), [session])
  const latestRef = useRef(latest)
  latestRef.current = latest
  // The chosen tab is scrolled into the strip's view: this view is drawn
  // anew for each session, so the strip would otherwise start at its first
  // tab, with the one chosen far along it out of sight.
  const tabStrip = useRef<HTMLDivElement>(null)
  useLayoutEffect(() => {
    const strip = tabStrip.current
    const chosen = strip?.querySelector<HTMLElement>('[aria-selected="true"]')
    if (!strip || !chosen) return
    const stripBox = strip.getBoundingClientRect()
    const chosenBox = chosen.getBoundingClientRect()
    const margin = 24
    if (chosenBox.left < stripBox.left) strip.scrollLeft += chosenBox.left - stripBox.left - margin
    else if (chosenBox.right > stripBox.right) strip.scrollLeft += chosenBox.right - stripBox.right + margin
  }, [latest.computer, latest.paneId, siblings.length])

  const everyMS =
    latest.herdrSessionState === 'working' || latest.herdrSessionState === 'asking' ? BUSY_EVERY_MS : IDLE_EVERY_MS
  const read = useVisiblePolling<SessionRead>(
    async () => {
      // A session that has not said anything yet has no history to read:
      // its transcript is empty, which is not a failure.
      if (view === 'turns' && !latestRef.current.transcriptPath) {
        return { herdrSession: latestRef.current, turns: [], isTruncated: false, screenText: '' }
      }
      if (view === 'turns') {
        const response = await graphql<{
          ReadAgentHerdrSession: { herdrSession: HerdrSession; turns: HerdrTurn[]; isTruncated: boolean }
        }>(HERDR_DOCUMENTS.ReadAgentHerdrSession, { computer: session.computer, paneId: session.paneId, turnCount })
        return { ...response.ReadAgentHerdrSession, screenText: '' }
      }
      const response = await graphql<{ ReadAgentHerdrScreen: { herdrSession: HerdrSession; screenText: string } }>(
        HERDR_DOCUMENTS.ReadAgentHerdrScreen,
        { computer: session.computer, paneId: session.paneId, shouldKeepColor: true },
      )
      return { ...response.ReadAgentHerdrScreen, turns: [], isTruncated: false }
    },
    everyMS,
    view,
    String(turnCount),
  )
  const readSession = read.data?.herdrSession
  useEffect(() => {
    if (readSession) setLatest(readSession)
  }, [readSession])
  useEffect(() => {
    if (read.error) toast.failure(read.error, t('herdr.readFailed'))
  }, [read.error, toast, t])
  const reloadRead = read.reload

  // Follow the end of the transcript while the person is at it, and keep
  // their place when earlier turns are added above.
  const scroller = useRef<HTMLDivElement>(null)
  const isAtEnd = useRef(true)
  const placeToKeep = useRef<{ height: number; top: number } | null>(null)
  const shown = read.data
  // A question that arrives is scrolled to, wherever the person was.
  const questionFingerprint = latest.question?.questionFingerprint
  useLayoutEffect(() => {
    const element = scroller.current
    if (!element || !questionFingerprint) return
    element.scrollTop = element.scrollHeight
    isAtEnd.current = true
  }, [questionFingerprint])
  useLayoutEffect(() => {
    const element = scroller.current
    if (!element) return
    if (placeToKeep.current) {
      element.scrollTop = element.scrollHeight - placeToKeep.current.height + placeToKeep.current.top
      placeToKeep.current = null
    } else if (isAtEnd.current) element.scrollTop = element.scrollHeight
  }, [shown])

  const showEarlier = () => {
    const element = scroller.current
    if (element) placeToKeep.current = { height: element.scrollHeight, top: element.scrollTop }
    setTurnCount(MOST_TURN_COUNT)
  }

  const focus = async () => {
    try {
      const response = await graphql<{ FocusAgentHerdrSession: HerdrSession }>(HERDR_DOCUMENTS.FocusAgentHerdrSession, {
        computer: latest.computer,
        paneId: latest.paneId,
      })
      setLatest(response.FocusAgentHerdrSession)
      toast.done(t('herdr.shownOnComputer', { pane: paneNameOf(latest), computer: latest.computer }))
      onChanged()
    } catch (caught) {
      toast.failure(caught, t('herdr.focusFailed'))
    }
  }

  const watch = async () => {
    try {
      // No conversation named: the server wakes the main one.
      const response = await graphql<{ WatchAgentHerdrSession: HerdrSession }>(HERDR_DOCUMENTS.WatchAgentHerdrSession, {
        computer: latest.computer,
        paneId: latest.paneId,
      })
      setLatest(response.WatchAgentHerdrSession)
      toast.done(t('herdr.watching', { pane: paneNameOf(latest) }))
      onChanged()
    } catch (caught) {
      toast.failure(caught, t('herdr.watchFailed'))
    }
  }

  const close = async () => {
    setClosingBusy(true)
    try {
      await graphql(HERDR_DOCUMENTS.CloseAgentHerdrSession, { computer: latest.computer, paneId: latest.paneId })
      toast.done(t('herdr.closed', { pane: paneNameOf(latest) }))
      forgetHerdrSessions()
      onChanged()
      const next = siblings.find((each) => !isSameSession(each, latest))
      if (next) onSelect(next)
      else onBack()
    } catch (caught) {
      toast.failure(caught, t('herdr.closeFailed'))
      setClosing(false)
    } finally {
      setClosingBusy(false)
    }
  }

  const openBeside = async (codingAgentKind: 'claude' | 'codex') => {
    try {
      const response = await graphql<{ OpenAgentHerdrSession: HerdrSession }>(HERDR_DOCUMENTS.OpenAgentHerdrSession, {
        computer: latest.computer,
        paneId: latest.paneId,
        codingAgentKind,
      })
      forgetHerdrSessions()
      toast.done(t('herdr.opened', { pane: paneNameOf(response.OpenAgentHerdrSession) }))
      onOpened(response.OpenAgentHerdrSession)
    } catch (caught) {
      toast.failure(caught, t('herdr.openFailed'))
    }
  }

  const tabs = siblings.length > 0 ? [...siblings].sort(byTabOrder) : [latest]
  const agentKinds: ('claude' | 'codex')[] =
    latest.codingAgentKind === 'codex' ? ['codex', 'claude'] : ['claude', 'codex']
  const isWorking = latest.herdrSessionState === 'working'

  return (
    <div className="coding-session">
      <header className="coding-header">
        <Tooltip label={t('herdr.back')}>
          <button type="button" className="coding-back icon-button" aria-label={t('herdr.back')} onClick={onBack}>
            <ChevronLeftIcon size={18} />
          </button>
        </Tooltip>
        <div className="coding-header-text">
          <h2>{latest.workspaceLabel || paneNameOf(latest)}</h2>
          <p className="muted">
            <span>{latest.computer}</span>
            {latest.workingDirectory ? <span className="mono">{latest.workingDirectory}</span> : null}
          </p>
        </div>
        <div className="row-actions">
          <HerdrStateTag session={latest} />
          <Tooltip label={t('herdr.showOnComputer')}>
            <button
              type="button"
              className="icon-button"
              aria-label={t('herdr.showOnComputer')}
              onClick={() => void focus()}
            >
              <ComputerIcon size={18} />
            </button>
          </Tooltip>
          {latest.isWatched ? null : (
            <Tooltip label={t('herdr.watch')}>
              <button type="button" className="icon-button" aria-label={t('herdr.watch')} onClick={() => void watch()}>
                <EyeIcon size={18} />
              </button>
            </Tooltip>
          )}
          <Tooltip label={isWorking ? t('herdr.closeWhileWorking') : t('herdr.closeSession')}>
            <button
              type="button"
              className="icon-button danger"
              aria-label={t('herdr.closeSession')}
              disabled={isWorking}
              onClick={() => setClosing(true)}
            >
              <TrashIcon size={18} />
            </button>
          </Tooltip>
        </div>
      </header>
      <div className="coding-tabstrip">
        <div ref={tabStrip} className="tabs herdr-tabs" role="tablist">
          {tabs.map((tab) => (
            <button
              key={tab.paneId}
              type="button"
              role="tab"
              aria-selected={isSameSession(tab, latest)}
              className={isSameSession(tab, latest) ? 'active' : ''}
              title={tabLabelOf(tab)}
              onClick={() => onSelect(tab)}
            >
              <span className="herdr-tab-label">{tabLabelOf(tab)}</span>
              <span className={tab.isTabFocused ? 'herdr-onscreen' : 'herdr-onscreen reserved'}>
                {tab.isTabFocused ? <span className="visually-hidden">{t('herdr.onScreen')}</span> : null}
              </span>
            </button>
          ))}
        </div>
        <Tooltip label={t('herdr.newTab')}>
          <MenuButton
            label={t('herdr.newTab')}
            className="icon-button"
            icon={<PlusIcon size={18} />}
            disabled={!latest.workspaceId}
            render={(closeMenu) =>
              agentKinds.map((kind) => (
                <button
                  key={kind}
                  type="button"
                  role="menuitem"
                  onClick={() => {
                    closeMenu()
                    void openBeside(kind)
                  }}
                >
                  {codingAgentName(kind)}
                </button>
              ))
            }
          />
        </Tooltip>
      </div>
      <div className="coding-view-switch">
        <div className="segmented" role="group">
          <button
            type="button"
            className={view === 'turns' ? 'active' : ''}
            aria-pressed={view === 'turns'}
            onClick={() => setView('turns')}
          >
            {t('herdr.turns')}
          </button>
          <button
            type="button"
            className={view === 'screen' ? 'active' : ''}
            aria-pressed={view === 'screen'}
            onClick={() => setView('screen')}
          >
            {t('herdr.screen')}
          </button>
        </div>
      </div>
      <div
        ref={scroller}
        className="coding-scroll"
        onScroll={(event) => {
          const element = event.currentTarget
          isAtEnd.current = element.scrollHeight - element.scrollTop - element.clientHeight < AT_END_PX
        }}
      >
        {shown === null ? (
          read.error ? null : (
            <Loading />
          )
        ) : view === 'screen' ? (
          <TerminalText className="herdr-screen" text={shown.screenText} />
        ) : (
          <>
            {shown.isTruncated && turnCount < MOST_TURN_COUNT ? (
              <button type="button" className="coding-earlier" onClick={showEarlier}>
                {t('herdr.earlierTurns')}
              </button>
            ) : null}
            {shown.turns.length === 0 ? (
              <p className="coding-empty muted">{t('herdr.noTurns')}</p>
            ) : (
              <HerdrTurnsList turns={shown.turns} />
            )}
          </>
        )}
        {/* The question is the end of the conversation, and scrolls with it,
            rather than a panel that leaves a phone's transcript a sliver. */}
        {latest.question ? (
          <div className="coding-question">
            <HerdrQuestionAnswer
              key={latest.question.questionFingerprint}
              session={latest}
              question={latest.question}
              onAnswered={(after) => {
                if (after) setLatest(after)
                isAtEnd.current = true
                reloadRead()
                onChanged()
              }}
            />
          </div>
        ) : null}
      </div>
      {/* Nothing can be sent while the session asks, and the question takes
          a typed answer of its own, so the box gives its room to it. */}
      <div className="coding-composer" hidden={Boolean(latest.question)}>
        <HerdrSendForm
          session={latest}
          draft={draft ?? { text: '', pending: [] }}
          onDraftChange={onDraftChange}
          onSent={(after) => {
            setLatest(after)
            isAtEnd.current = true
            reloadRead()
            onChanged()
          }}
        />
      </div>
      {isClosing ? (
        <ConfirmDialog
          title={t('herdr.closeTitle')}
          body={
            <p>
              {t('herdr.closeBody', {
                agent: codingAgentName(latest.codingAgentKind),
                pane: paneNameOf(latest),
                computer: latest.computer,
              })}
            </p>
          }
          confirmLabel={t('herdr.closeSession')}
          busy={isClosingBusy}
          onConfirm={() => void close()}
          onClose={() => setClosing(false)}
        />
      ) : null}
    </div>
  )
}

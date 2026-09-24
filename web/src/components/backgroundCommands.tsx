import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react'

import { graphql } from '../api'
import { Loading, Tag, formatBytes } from './common'
import { ConfirmDialog } from './dialog'
import { ArrowLeftIcon } from './icons'
import { RelativeTime } from './relativeTime'
import { useToast } from './toast'
import { Tooltip } from './tooltip'
import { useQuery } from './useQuery'
import { Trans, useTranslation } from '../i18n/i18n'

// What the agent's shell tool left running on the person's computers: the
// commands it started in the background, or moved there when one outlived
// its timeout. The program on each computer holds them, and every read
// here asks it, so the lists poll gently and only while they are on screen.
// The drawer shows the ones its conversation started; the agent's page
// shows all of them.

// Why a command ended without ending by itself: the person or the agent
// stopped it, or it ran its full day and the computer stopped it. Empty
// while it runs and when it exited on its own.
type BackgroundStopReason = '' | 'stopped' | 'lifetime'

export interface BackgroundCommand {
  computer: string
  id: string
  command: string
  directory: string
  conversationId: string
  startedAt: string
  endedAt?: string | null
  isRunning: boolean
  exitCode: number
  stopReason: BackgroundStopReason
}

// A command with the last of what it printed on each stream.
interface BackgroundCommandOutput extends BackgroundCommand {
  stdout: string
  stderr: string
  isStdoutTruncated: boolean
  isStderrTruncated: boolean
  stdoutByteCount: number
  stderrByteCount: number
}

// OUTPUT_EVERY is how often a running command's output is read again while
// somebody is looking at it: often enough to watch a build scroll, and one
// small read at a time.
const OUTPUT_EVERY = 2_000

const LIST = `
  query ($conversationId: String) {
    ListAgentBackgroundCommands(conversationId: $conversationId) {
      computer id command directory conversationId startedAt endedAt isRunning exitCode stopReason
    }
  }`

const READ = `
  query ($computer: String!, $id: String!) {
    ReadAgentBackgroundCommand(computer: $computer, id: $id) {
      computer id command directory conversationId startedAt endedAt isRunning exitCode stopReason
      stdout stderr isStdoutTruncated isStderrTruncated stdoutByteCount stderrByteCount
    }
  }`

const STOP = `
  mutation ($computer: String!, $id: String!) {
    StopAgentBackgroundCommand(computer: $computer, id: $id) { id isRunning stopReason exitCode endedAt }
  }`

// useBackgroundCommands lists the background commands of one conversation,
// or of every conversation when conversationId is left out, and keeps the
// list up while isActive. An inactive list asks nothing and is empty.
export function useBackgroundCommands(conversationId: string | undefined, isActive: boolean) {
  const { t } = useTranslation()
  const toast = useToast()
  const { data, error, reload } = useQuery(
    () =>
      isActive
        ? graphql<{ ListAgentBackgroundCommands: BackgroundCommand[] }>(LIST, { conversationId }).then(
            (response) => response.ListAgentBackgroundCommands,
          )
        : Promise.resolve([]),
    [conversationId, isActive],
  )
  // A list that cannot be read says so once, not at every poll: until it
  // has been read, each failed refresh hands back a new error.
  const toldFailure = useRef('')
  useEffect(() => {
    if (!error) {
      toldFailure.current = ''
      return
    }
    const errorMessage = error instanceof Error ? error.message : String(error)
    if (errorMessage === toldFailure.current) return
    toldFailure.current = errorMessage
    toast.failure(error, t('backgroundCommands.listFailed'))
  }, [error, toast, t])
  if (!isActive || !data) return { commands: [], reload }
  // The last answer stays until the next arrives, and after a move to
  // another conversation it is the one left behind's.
  const commands = conversationId ? data.filter((command) => command.conversationId === conversationId) : data
  return { commands, reload }
}

// useStopBackgroundCommand stops one and says how that went. stopping is
// the id of the one being stopped, so its button can wait.
function useStopBackgroundCommand(onStopped: () => void) {
  const { t } = useTranslation()
  const toast = useToast()
  const [stopping, setStopping] = useState<string | null>(null)
  const stop = async (command: BackgroundCommand) => {
    setStopping(command.id)
    try {
      await graphql(STOP, { computer: command.computer, id: command.id })
      toast.done(t('backgroundCommands.stopped', { command: command.command }))
      onStopped()
    } catch (caught) {
      toast.failure(caught, t('backgroundCommands.stopFailed'))
    } finally {
      setStopping(null)
    }
  }
  return { stopping, stop }
}

// BackgroundCommandState is how a command stands, as a tag: running, the
// code it exited with, or who stopped it.
function BackgroundCommandState({ command }: { command: BackgroundCommand }) {
  const { t } = useTranslation()
  if (command.isRunning) return <Tag value={t('backgroundCommands.running')} />
  if (command.stopReason === 'lifetime') return <Tag value={t('backgroundCommands.lifetime')} tone="warn" />
  if (command.stopReason === 'stopped') return <Tag value={t('backgroundCommands.stoppedState')} tone="warn" />
  return (
    <Tag
      value={t('backgroundCommands.exited', { code: command.exitCode })}
      tone={command.exitCode === 0 ? 'good' : 'bad'}
    />
  )
}

// BackgroundCommandRows is the list: a row per command, the command itself
// first because that is what somebody scanning the list recognizes, and
// under it how it stands, where, and since when. The row opens its output;
// a running one has Stop beside it, the one action, as a word.
export function BackgroundCommandRows({
  commands,
  onOutput,
  onChanged,
}: {
  commands: BackgroundCommand[]
  onOutput: (command: BackgroundCommand) => void
  onChanged: () => void
}) {
  const { t } = useTranslation()
  const { stopping, stop } = useStopBackgroundCommand(onChanged)
  return (
    <ul className="background-commands">
      {commands.map((command) => (
        <li key={`${command.computer}/${command.id}`} className="background-command">
          <button
            type="button"
            className="background-command-open"
            title={command.command}
            aria-label={`${command.command}: ${t('backgroundCommands.output')}`}
            onClick={() => onOutput(command)}
          >
            <code className="background-command-line">{command.command}</code>
            <span className="background-command-meta muted">
              <BackgroundCommandState command={command} />
              <span>{command.computer}</span>
              <span>
                <Trans k="backgroundCommands.started" nodes={{ time: <RelativeTime value={command.startedAt} /> }} />
              </span>
            </span>
          </button>
          {command.isRunning ? (
            <button
              type="button"
              className="link danger background-command-stop"
              aria-label={`${command.command}: ${t('backgroundCommands.stop')}`}
              disabled={stopping === command.id}
              onClick={() => void stop(command)}
            >
              {t('backgroundCommands.stop')}
            </button>
          ) : null}
        </li>
      ))}
    </ul>
  )
}

// useBackgroundOutput reads what one command printed, again every couple
// of seconds while it runs. readAgain reads once more at once, after a stop,
// so how it ended shows without waiting for a poll it will no longer make.
function useBackgroundOutput(command: BackgroundCommand) {
  const { t } = useTranslation()
  const toast = useToast()
  const [output, setOutput] = useState<BackgroundCommandOutput | null>(null)
  const [readCount, setReadCount] = useState(0)
  const isRunning = output ? output.isRunning : command.isRunning
  useEffect(() => {
    let isStopped = false
    const read = () => {
      if (document.hidden) return
      graphql<{ ReadAgentBackgroundCommand: BackgroundCommandOutput }>(READ, {
        computer: command.computer,
        id: command.id,
      })
        .then((response) => {
          if (!isStopped) setOutput(response.ReadAgentBackgroundCommand)
        })
        .catch((caught) => {
          if (isStopped) return
          isStopped = true
          window.clearInterval(every)
          toast.failure(caught, t('backgroundCommands.readFailed'))
        })
    }
    read()
    // Once it has ended, what it printed will not change.
    const every = isRunning ? window.setInterval(read, OUTPUT_EVERY) : undefined
    return () => {
      isStopped = true
      window.clearInterval(every)
    }
    // toast and t are stable for the life of the view.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [command.computer, command.id, isRunning, readCount])
  return { output, isRunning, readAgain: () => setReadCount((previous) => previous + 1) }
}

// BackgroundOutput is what one command printed, stdout and stderr apart,
// under the command and how it stands. The same view in the drawer's panel
// and in the agent page's dialog; each puts Stop where its frame keeps
// actions.
function BackgroundOutput({ command, output }: { command: BackgroundCommand; output: BackgroundCommandOutput | null }) {
  const { t } = useTranslation()
  const shown = output ?? command
  return (
    <div className="background-output">
      <code className="background-output-command">{shown.command}</code>
      <p className="background-command-meta muted">
        <BackgroundCommandState command={shown} />
        <span>{shown.computer}</span>
        {shown.directory ? <span className="mono">{shown.directory}</span> : null}
        <span>
          <Trans k="backgroundCommands.started" nodes={{ time: <RelativeTime value={shown.startedAt} /> }} />
        </span>
      </p>
      {output === null ? (
        <Loading />
      ) : output.stdoutByteCount === 0 && output.stderrByteCount === 0 ? (
        <p className="muted">{t('backgroundCommands.nothingPrinted')}</p>
      ) : (
        <>
          {output.stdoutByteCount > 0 && (
            <OutputStream
              label={t('backgroundCommands.stdout')}
              text={output.stdout}
              isTruncated={output.isStdoutTruncated}
              byteCount={output.stdoutByteCount}
            />
          )}
          {output.stderrByteCount > 0 && (
            <OutputStream
              label={t('backgroundCommands.stderr')}
              text={output.stderr}
              isTruncated={output.isStderrTruncated}
              byteCount={output.stderrByteCount}
            />
          )}
        </>
      )}
    </div>
  )
}

// BackgroundOutputDialog is the output in a dialog, for the agent page,
// which has no panel to slide it into. Stop is its one action while there
// is something to stop.
export function BackgroundOutputDialog({
  command,
  onChanged,
  onClose,
}: {
  command: BackgroundCommand
  onChanged: () => void
  onClose: () => void
}) {
  const { t } = useTranslation()
  const { output, isRunning, readAgain } = useBackgroundOutput(command)
  const { stopping, stop } = useStopBackgroundCommand(onChanged)
  return (
    <ConfirmDialog
      title={t('backgroundCommands.outputTitle')}
      wide
      body={<BackgroundOutput command={command} output={output} />}
      confirmLabel={isRunning ? t('backgroundCommands.stop') : undefined}
      busy={stopping === command.id}
      onConfirm={isRunning ? () => void stop(output ?? command).then(readAgain) : undefined}
      onClose={onClose}
    />
  )
}

// BackgroundPanel is the drawer's own view of its conversation's background
// commands: a list dropped down from the drawer's head, as the list of
// conversations is, rather than a dialog over the page, because the drawer
// is where the person is looking. A command's output opens in the same
// list in its place; back returns to the list, and Escape or anywhere
// outside closes it.
export function BackgroundPanel({
  commands,
  onChanged,
  onClose,
}: {
  commands: BackgroundCommand[]
  onChanged: () => void
  onClose: () => void
}) {
  const { t } = useTranslation()
  const [opened, setOpened] = useState<BackgroundCommand | null>(null)
  // The row as the list has it now, so the output follows a command that
  // ended while it was open.
  const current = opened
    ? (commands.find((command) => command.computer === opened.computer && command.id === opened.id) ?? opened)
    : null
  return (
    <section
      className="agent-drawer-list background-menu"
      aria-label={t('agentDrawer.backgroundCommands')}
      onKeyDown={(event) => {
        if (event.key === 'Escape') {
          event.stopPropagation()
          if (current) setOpened(null)
          else onClose()
        }
      }}
    >
      {current ? (
        <BackgroundPanelOutput
          key={`${current.computer}/${current.id}`}
          command={current}
          onBack={() => setOpened(null)}
          onChanged={onChanged}
        />
      ) : (
        <>
          <header className="background-menu-head">
            <strong>{t('agentDrawer.backgroundCommands')}</strong>
          </header>
          {commands.length === 0 ? (
            <p className="muted background-menu-empty">{t('backgroundCommands.none')}</p>
          ) : (
            <BackgroundCommandRows commands={commands} onOutput={setOpened} onChanged={onChanged} />
          )}
        </>
      )}
    </section>
  )
}

// BackgroundPanelOutput is one command's output inside the list: back and
// Stop on the title line, where they stay in reach however long the output
// is, and the output under them.
function BackgroundPanelOutput({
  command,
  onBack,
  onChanged,
}: {
  command: BackgroundCommand
  onBack: () => void
  onChanged: () => void
}) {
  const { t } = useTranslation()
  const { output, isRunning, readAgain } = useBackgroundOutput(command)
  const { stopping, stop } = useStopBackgroundCommand(onChanged)
  return (
    <>
      <header className="background-menu-head">
        <Tooltip label={t('backgroundCommands.back')}>
          <button type="button" className="icon-action" aria-label={t('backgroundCommands.back')} onClick={onBack}>
            <ArrowLeftIcon size={14} />
          </button>
        </Tooltip>
        <strong>{t('backgroundCommands.outputTitle')}</strong>
        {isRunning ? (
          <button
            type="button"
            className="link danger background-menu-stop"
            aria-label={`${command.command}: ${t('backgroundCommands.stop')}`}
            disabled={stopping === command.id}
            onClick={() => void stop(output ?? command).then(readAgain)}
          >
            {t('backgroundCommands.stop')}
          </button>
        ) : null}
      </header>
      <BackgroundOutput command={command} output={output} />
    </>
  )
}

// OutputStream is one stream: its name, what it printed, and when that is
// only the end of it, how much more there was.
function OutputStream({
  label,
  text,
  isTruncated,
  byteCount,
}: {
  label: string
  text: string
  isTruncated: boolean
  byteCount: number
}) {
  const { t } = useTranslation()
  const element = useRef<HTMLPreElement>(null)
  // Whether the reader is at the end. New output keeps them there, the way
  // a terminal does; somebody who scrolled up to read is left where they
  // are.
  const isAtEnd = useRef(true)
  useLayoutEffect(() => {
    const pre = element.current
    if (pre && isAtEnd.current) pre.scrollTop = pre.scrollHeight
  }, [text])
  const onScroll = useCallback(() => {
    const pre = element.current
    if (pre) isAtEnd.current = pre.scrollHeight - pre.scrollTop - pre.clientHeight < 8
  }, [])
  return (
    <section className="background-output-stream">
      <h4>{label}</h4>
      {isTruncated ? (
        <p className="muted background-output-truncated">
          {t('backgroundCommands.truncated', {
            shown: formatBytes(new TextEncoder().encode(text).length),
            total: formatBytes(byteCount),
          })}
        </p>
      ) : null}
      <pre ref={element} className="mono background-output-text" onScroll={onScroll}>
        {text}
      </pre>
    </section>
  )
}

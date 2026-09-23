import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react'

import { graphql } from '../api'
import { Loading, Tag, formatBytes, formatTime } from './common'
import { ConfirmDialog } from './dialog'
import { StopIcon, TerminalIcon } from './icons'
import { SettingsRow } from './settingsList'
import { useToast } from './toast'
import { useQuery } from './useQuery'
import { useTranslation } from '../i18n/i18n'

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

// BackgroundCommandRows is the list: a row per command, with its output a
// press away and, while it runs, a way to stop it. onOutput is left to the
// caller because the drawer already has a dialog open around the rows and
// swaps it for the output rather than stacking one on the other.
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
    <div className="background-commands">
      {commands.map((command) => (
        <SettingsRow
          key={`${command.computer}/${command.id}`}
          title={
            <span className="mono background-command-text" title={command.command}>
              {command.command}
            </span>
          }
          badge={<BackgroundCommandState command={command} />}
          subtitle={[
            command.computer,
            command.directory,
            t('backgroundCommands.started', { time: formatTime(command.startedAt) }),
          ]
            .filter(Boolean)
            .join(' · ')}
          actions={
            command.isRunning ? (
              <div className="row-actions">
                <button
                  type="button"
                  className="icon-action"
                  title={t('backgroundCommands.output')}
                  aria-label={`${command.command}: ${t('backgroundCommands.output')}`}
                  onClick={() => onOutput(command)}
                >
                  <TerminalIcon size={16} />
                </button>
                <button
                  type="button"
                  className="icon-action danger"
                  title={t('backgroundCommands.stop')}
                  aria-label={`${command.command}: ${t('backgroundCommands.stop')}`}
                  disabled={stopping === command.id}
                  onClick={() => void stop(command)}
                >
                  <StopIcon size={16} />
                </button>
              </div>
            ) : (
              <button type="button" className="link" onClick={() => onOutput(command)}>
                {t('backgroundCommands.output')}
              </button>
            )
          }
        />
      ))}
    </div>
  )
}

// BackgroundOutputDialog is what one command printed, stdout and stderr
// apart, read again every couple of seconds while it runs. Stop is its one
// action while there is something to stop.
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
  const toast = useToast()
  const [output, setOutput] = useState<BackgroundCommandOutput | null>(null)
  const { stopping, stop } = useStopBackgroundCommand(onChanged)
  // Bumped by a stop, so the dialog reads once more and shows how it ended
  // without waiting for a poll it will no longer make.
  const [readAgain, setReadAgain] = useState(0)

  const isRunning = output ? output.isRunning : command.isRunning
  useEffect(() => {
    let stopped = false
    const read = () => {
      if (document.hidden) return
      graphql<{ ReadAgentBackgroundCommand: BackgroundCommandOutput }>(READ, {
        computer: command.computer,
        id: command.id,
      })
        .then((response) => {
          if (!stopped) setOutput(response.ReadAgentBackgroundCommand)
        })
        .catch((caught) => {
          if (stopped) return
          stopped = true
          window.clearInterval(every)
          toast.failure(caught, t('backgroundCommands.readFailed'))
        })
    }
    read()
    // Once it has ended, what it printed will not change.
    const every = isRunning ? window.setInterval(read, OUTPUT_EVERY) : undefined
    return () => {
      stopped = true
      window.clearInterval(every)
    }
    // toast and t are stable for the life of the dialog.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [command.computer, command.id, isRunning, readAgain])

  const shown = output ?? command
  return (
    <ConfirmDialog
      title={t('backgroundCommands.outputTitle')}
      wide
      body={
        <div className="background-output">
          <p className="mono background-output-command">{shown.command}</p>
          <p className="muted background-output-where">
            <BackgroundCommandState command={shown} /> {[shown.computer, shown.directory].filter(Boolean).join(' · ')}
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
      }
      confirmLabel={isRunning ? t('backgroundCommands.stop') : undefined}
      busy={stopping === command.id}
      onConfirm={
        isRunning
          ? () =>
              void stop(shown).then(() => {
                setReadAgain((previous) => previous + 1)
              })
          : undefined
      }
      onClose={onClose}
    />
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

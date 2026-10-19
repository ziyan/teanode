import { useEffect, useLayoutEffect, useRef, useState } from 'react'

import { useTranslation, type Translation } from '../i18n/i18n'
import { Tag } from './common'
import {
  codingAgentName,
  paneNameOf,
  type HerdrSession,
  type HerdrSubagent,
  type HerdrSubagentState,
  type HerdrSubagentsRead,
} from './herdrSessions'
import { CheckIcon, ChevronDownIcon, ChevronRightIcon } from './icons'

// The agents a coding session started for its current request, as a tree: the
// session's card at the top, and under it each agent it started, with the
// agents those started under them. The server answers a flat list, each agent
// naming the one that started it; the page draws it as a row of cards under
// the session on a wide screen, and as an indented list where there is not
// room for a row.

export interface SubagentNode {
  subagent: HerdrSubagent
  children: SubagentNode[]
  // Every agent under this one, however deep, which is what folding it hides.
  descendantCount: number
}

function startedAtMS(subagent: HerdrSubagent): number {
  const startedAt = subagent.startedAt ? new Date(subagent.startedAt).getTime() : NaN
  return Number.isNaN(startedAt) ? Infinity : startedAt
}

// buildSubagentTree nests the flat list under the agents that started them,
// each level in the order the agents started. An agent whose parent is not in
// the list (the list keeps the newest, so a parent can be cut) hangs from the
// session, as one the session started; so does one in a loop of parents,
// which no honest list has, so that nothing is lost from the screen.
export function buildSubagentTree(subagents: HerdrSubagent[]): SubagentNode[] {
  const ordered = subagents
    .map((subagent, index) => ({ subagent, index }))
    .sort((left, right) => {
      const leftStartedAt = startedAtMS(left.subagent)
      const rightStartedAt = startedAtMS(right.subagent)
      if (leftStartedAt === rightStartedAt) return left.index - right.index
      return leftStartedAt < rightStartedAt ? -1 : 1
    })
    .map((each) => each.subagent)
  const knownIds = new Set(ordered.map((subagent) => subagent.subagentId))
  const childrenOf = new Map<string, HerdrSubagent[]>()
  const rootSubagents: HerdrSubagent[] = []
  for (const subagent of ordered) {
    const parentId = subagent.parentSubagentId
    if (parentId && parentId !== subagent.subagentId && knownIds.has(parentId)) {
      childrenOf.set(parentId, [...(childrenOf.get(parentId) ?? []), subagent])
    } else rootSubagents.push(subagent)
  }
  const seenIds = new Set<string>()
  const build = (subagent: HerdrSubagent): SubagentNode => {
    seenIds.add(subagent.subagentId)
    const children: SubagentNode[] = []
    for (const child of childrenOf.get(subagent.subagentId) ?? []) {
      if (!seenIds.has(child.subagentId)) children.push(build(child))
    }
    return {
      subagent,
      children,
      descendantCount: children.reduce((total, child) => total + 1 + child.descendantCount, 0),
    }
  }
  const roots = rootSubagents.map(build)
  for (const subagent of ordered) if (!seenIds.has(subagent.subagentId)) roots.push(build(subagent))
  return roots
}

// agentDurationText is a time as a person says one aloud: 45s, 1m 20s, 2h 5m.
// Seconds are dropped from an hour on.
export function agentDurationText(milliseconds: number, t: Translation['t']): string {
  const totalSeconds = Math.max(0, Math.floor(milliseconds / 1000))
  const hours = Math.floor(totalSeconds / 3600)
  const minutes = Math.floor((totalSeconds % 3600) / 60)
  const seconds = totalSeconds % 60
  if (hours > 0) {
    return minutes === 0
      ? t('herdr.agents.durationHours', { hours })
      : t('herdr.agents.durationHoursMinutes', { hours, minutes })
  }
  if (minutes > 0) {
    return seconds === 0
      ? t('herdr.agents.durationMinutes', { minutes })
      : t('herdr.agents.durationMinutesSeconds', { minutes, seconds })
  }
  return t('herdr.agents.durationSeconds', { seconds })
}

// useNow is the time, read again every second while something runs, so a
// running agent's duration counts up between the reads of the list.
function useNow(isTicking: boolean): number {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    if (!isTicking) return
    setNow(Date.now())
    const timer = window.setInterval(() => setNow(Date.now()), 1000)
    return () => window.clearInterval(timer)
  }, [isTicking])
  return now
}

function AgentBadge({ badge }: { badge: HerdrSubagentState | 'asking' }) {
  const { t } = useTranslation()
  if (badge === 'asking') return <Tag value={t('herdr.agents.needsYou')} tone="warn" />
  if (badge === 'running') {
    // The dots the agent's chat shows while it answers.
    return (
      <span className="tag good agent-badge">
        <span className="agent-dots" aria-hidden="true">
          <i />
          <i />
          <i />
        </span>
        {t('herdr.agents.running')}
      </span>
    )
  }
  if (badge === 'done') {
    return (
      <span className="tag agent-badge">
        <CheckIcon size={12} />
        {t('herdr.agents.done')}
      </span>
    )
  }
  return <span className="tag agent-badge">{t('herdr.agents.stopped')}</span>
}

function SessionCard({
  session,
  modelName,
  requestStartedAt,
  now,
}: {
  session: HerdrSession
  modelName: string
  requestStartedAt?: string | null
  now: number
}) {
  const { t } = useTranslation()
  const isBusy = session.herdrSessionState === 'working' || session.herdrSessionState === 'asking'
  const startedAt = requestStartedAt ? new Date(requestStartedAt).getTime() : NaN
  const badge =
    session.herdrSessionState === 'asking' ? 'asking' : session.herdrSessionState === 'working' ? 'running' : null
  return (
    <article className="card agent-card agent-card-session">
      <div className="agent-card-head">
        <span className="agent-card-title">
          <strong>{codingAgentName(session.codingAgentKind)}</strong>
          {modelName ? <span className="muted">{modelName}</span> : null}
        </span>
        {badge ? (
          <AgentBadge badge={badge} />
        ) : (
          <Tag value={t(`herdr.state.${session.herdrSessionState}` as 'herdr.state.idle')} />
        )}
      </div>
      <p className="agent-card-task">{session.workspaceLabel || paneNameOf(session)}</p>
      {isBusy && !Number.isNaN(startedAt) ? (
        <div className="agent-card-foot muted">
          <span className="agent-card-time">{agentDurationText(now - startedAt, t)}</span>
        </div>
      ) : null}
    </article>
  )
}

function AgentCard({
  node,
  isFolded,
  onToggle,
  now,
}: {
  node: SubagentNode
  isFolded: boolean
  onToggle: () => void
  now: number
}) {
  const { t, plural } = useTranslation()
  const { subagent } = node
  const startedAt = subagent.startedAt ? new Date(subagent.startedAt).getTime() : NaN
  const endedAt = subagent.endedAt
    ? new Date(subagent.endedAt).getTime()
    : subagent.subagentState === 'running'
      ? now
      : NaN
  const durationText = Number.isNaN(startedAt) || Number.isNaN(endedAt) ? '' : agentDurationText(endedAt - startedAt, t)
  const detail = [subagent.subagentName, subagent.modelName].filter(Boolean).join(' · ')
  return (
    <article className="card agent-card" data-subagent-state={subagent.subagentState}>
      <div className="agent-card-head">
        <span className="agent-card-title">
          <strong>{subagent.subagentType}</strong>
          {detail ? <span className="muted">{detail}</span> : null}
        </span>
        <AgentBadge badge={subagent.subagentState} />
      </div>
      {subagent.taskText ? <p className="agent-card-task">{subagent.taskText}</p> : null}
      {subagent.toolNames.length > 0 || durationText ? (
        <div className="agent-card-foot muted">
          <span className="agent-card-tools">{subagent.toolNames.join(', ')}</span>
          {durationText ? <span className="agent-card-time">{durationText}</span> : null}
        </div>
      ) : null}
      {node.children.length > 0 ? (
        <button type="button" className="agent-card-toggle" aria-expanded={!isFolded} onClick={onToggle}>
          {isFolded ? <ChevronRightIcon size={14} /> : <ChevronDownIcon size={14} />}
          {plural(node.descendantCount, { one: 'herdr.agents.countOne', other: 'herdr.agents.countOther' })}
        </button>
      ) : null}
    </article>
  )
}

function AgentBranch({
  nodes,
  isTop,
  foldedIds,
  onToggle,
  now,
}: {
  nodes: SubagentNode[]
  isTop: boolean
  foldedIds: ReadonlySet<string>
  onToggle: (subagentId: string) => void
  now: number
}) {
  return (
    <ul className={isTop ? 'agents-children agents-top' : 'agents-children agents-nested'}>
      {nodes.map((node) => {
        const isFolded = foldedIds.has(node.subagent.subagentId)
        return (
          <li key={node.subagent.subagentId}>
            <AgentCard node={node} isFolded={isFolded} onToggle={() => onToggle(node.subagent.subagentId)} now={now} />
            {node.children.length > 0 && !isFolded ? (
              <AgentBranch nodes={node.children} isTop={false} foldedIds={foldedIds} onToggle={onToggle} now={now} />
            ) : null}
          </li>
        )
      })}
    </ul>
  )
}

// HerdrSubagentsView is the Agents view of one session: a heading with how
// many agents there are and how many run, the session's card, and the tree.
// useCenteredScroll keeps a sideways scroller showing its middle, where the
// session's card is, whenever the number of columns changes: a tree wider
// than its pane scrolls inside it, and should open on the session rather
// than on its first agent. Later reads leave where the person scrolled to.
function useCenteredScroll<Element extends HTMLElement>(columnCount: number) {
  const ref = useRef<Element>(null)
  useLayoutEffect(() => {
    const element = ref.current
    if (!element) return
    element.scrollLeft = Math.max(0, (element.scrollWidth - element.clientWidth) / 2)
  }, [columnCount])
  return ref
}

export function HerdrSubagentsView({ read }: { read: HerdrSubagentsRead }) {
  const { t, plural } = useTranslation()
  const [foldedIds, setFoldedIds] = useState<ReadonlySet<string>>(new Set())
  const nodes = buildSubagentTree(read.subagents)
  // The tree at every width: one wider than the pane scrolls sideways in its
  // own box, with the session's card over the middle of its agents.
  const treeRef = useCenteredScroll<HTMLDivElement>(nodes.length)
  const runningCount = read.subagents.filter((subagent) => subagent.subagentState === 'running').length
  const isBusy = read.herdrSession.herdrSessionState === 'working' || read.herdrSession.herdrSessionState === 'asking'
  const now = useNow(runningCount > 0 || isBusy)
  const toggle = (subagentId: string) =>
    setFoldedIds((previous) => {
      const next = new Set(previous)
      if (!next.delete(subagentId)) next.add(subagentId)
      return next
    })
  return (
    <div className="agents-view">
      {read.subagents.length > 0 ? (
        <h3 className="agents-heading">
          <span>
            {[
              plural(read.subagents.length, { one: 'herdr.agents.countOne', other: 'herdr.agents.countOther' }),
              runningCount > 0 ? t('herdr.agents.runningCount', { count: runningCount }) : '',
            ]
              .filter(Boolean)
              .join(' · ')}
          </span>
          {read.isTruncated ? <span className="muted">{t('herdr.agents.newestOnly')}</span> : null}
        </h3>
      ) : null}
      <div ref={treeRef} className="agents-tree">
        <div className="agents-graph">
          <div className="agents-root">
            <SessionCard
              session={read.herdrSession}
              modelName={read.sessionModelName}
              requestStartedAt={read.requestStartedAt}
              now={now}
            />
          </div>
          {nodes.length > 0 ? (
            <AgentBranch nodes={nodes} isTop foldedIds={foldedIds} onToggle={toggle} now={now} />
          ) : null}
        </div>
      </div>
      {read.subagents.length === 0 ? <p className="coding-empty muted">{t('herdr.agents.none')}</p> : null}
    </div>
  )
}

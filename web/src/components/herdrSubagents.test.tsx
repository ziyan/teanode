import { cleanup, fireEvent, render, screen, within } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'

import { TranslationProvider } from '../i18n/i18n'
import type { HerdrSession, HerdrSubagent, HerdrSubagentsRead } from './herdrSessions'
import { HerdrSubagentsView, agentDurationText, buildSubagentTree, type SubagentNode } from './herdrSubagents'

afterEach(cleanup)

const session: HerdrSession = {
  computer: 'laptop',
  paneId: 'w1:p1',
  paneName: 'website › Claude Code',
  codingAgentKind: 'claude',
  codingSessionId: '',
  herdrSessionState: 'idle',
  herdrAgentStatus: 'idle',
  paneTitle: '',
  workingDirectory: '~/src/website',
  transcriptPath: '',
  isWatched: false,
  workspaceLabel: 'website',
}

function subagentOf(fields: Partial<HerdrSubagent> & Pick<HerdrSubagent, 'subagentId'>): HerdrSubagent {
  return {
    subagentType: 'general-purpose',
    taskText: `task of ${fields.subagentId}`,
    modelName: 'model-large',
    subagentState: 'done',
    spawnDepth: 1,
    toolNames: [],
    toolCallCount: 0,
    ...fields,
  }
}

// Listed out of order on purpose: the tree orders by start, and a child can
// come before the agent that started it.
const subagents: HerdrSubagent[] = [
  subagentOf({
    subagentId: 'inner-two',
    parentSubagentId: 'outer-one',
    spawnDepth: 2,
    subagentType: 'Explore',
    startedAt: '2026-01-01T10:00:40Z',
    endedAt: '2026-01-01T10:01:00Z',
  }),
  subagentOf({
    subagentId: 'outer-two',
    subagentType: 'writer',
    subagentState: 'running',
    startedAt: '2026-01-01T10:00:20Z',
    toolNames: ['Edit', 'Write'],
  }),
  subagentOf({
    subagentId: 'outer-one',
    subagentType: 'researcher',
    startedAt: '2026-01-01T10:00:00Z',
    endedAt: '2026-01-01T10:01:20Z',
    toolNames: ['Read', 'Grep'],
    toolCallCount: 9,
  }),
  subagentOf({
    subagentId: 'inner-one',
    parentSubagentId: 'outer-one',
    spawnDepth: 2,
    subagentType: 'Explore',
    startedAt: '2026-01-01T10:00:10Z',
    endedAt: '2026-01-01T10:00:55Z',
    subagentState: 'stopped',
  }),
  subagentOf({
    subagentId: 'deepest',
    parentSubagentId: 'inner-one',
    spawnDepth: 3,
    subagentType: 'checker',
    startedAt: '2026-01-01T10:00:20Z',
    endedAt: '2026-01-01T10:00:50Z',
  }),
]

function shapeOf(nodes: SubagentNode[]): unknown[] {
  return nodes.map((node) => [node.subagent.subagentId, shapeOf(node.children)])
}

it('nests the flat list under the agents that started them, in the order they started', () => {
  const nodes = buildSubagentTree(subagents)
  expect(shapeOf(nodes)).toEqual([
    [
      'outer-one',
      [
        ['inner-one', [['deepest', []]]],
        ['inner-two', []],
      ],
    ],
    ['outer-two', []],
  ])
  expect(nodes[0].descendantCount).toBe(3)
  expect(nodes[0].children[0].descendantCount).toBe(1)
  expect(nodes[1].descendantCount).toBe(0)
})

it('hangs an agent from the session when its parent is not in the list, and never loses one in a loop', () => {
  const nodes = buildSubagentTree([
    subagentOf({ subagentId: 'orphan', parentSubagentId: 'cut-off', startedAt: '2026-01-01T10:00:00Z' }),
    subagentOf({ subagentId: 'loop-a', parentSubagentId: 'loop-b', startedAt: '2026-01-01T10:00:10Z' }),
    subagentOf({ subagentId: 'loop-b', parentSubagentId: 'loop-a', startedAt: '2026-01-01T10:00:20Z' }),
    subagentOf({ subagentId: 'unstarted' }),
  ])
  expect(shapeOf(nodes)).toEqual([
    ['orphan', []],
    ['unstarted', []],
    ['loop-a', [['loop-b', []]]],
  ])
})

function renderView(read: Partial<HerdrSubagentsRead> = {}) {
  render(
    <TranslationProvider>
      <HerdrSubagentsView
        read={{
          herdrSession: session,
          sessionModelName: 'model-session',
          isTruncated: false,
          subagents,
          ...read,
        }}
      />
    </TranslationProvider>,
  )
}

function cardOf(subagentType: string, index = 0): HTMLElement {
  return screen.getAllByText(subagentType, { selector: 'strong' })[index].closest('article') as HTMLElement
}

it('says how many agents there are and how many run', () => {
  renderView()
  expect(screen.getByRole('heading', { level: 3 }).textContent).toBe('5 agents · 1 running')
})

it('leaves out the running count when none run, and says one agent in the singular', () => {
  renderView({ subagents: [subagents[2]] })
  expect(screen.getByRole('heading', { level: 3 }).textContent).toBe('1 agent')
})

it('draws the session first, then each agent under the one that started it', () => {
  renderView()
  const sessionCard = screen.getByText('Claude Code').closest('article') as HTMLElement
  expect(within(sessionCard).getByText('model-session')).toBeTruthy()
  expect(within(sessionCard).getByText('website')).toBeTruthy()
  const top = document.querySelector('ul.agents-top') as HTMLElement
  expect(Array.from(top.children).map((item) => item.querySelector('strong')?.textContent)).toEqual([
    'researcher',
    'writer',
  ])
  const researcherItem = top.children[0]
  const nested = researcherItem.querySelector(':scope > ul.agents-nested') as HTMLElement
  expect(Array.from(nested.children).map((item) => item.querySelector('strong')?.textContent)).toEqual([
    'Explore',
    'Explore',
  ])
  // The third level hangs from the first Explore.
  expect(within(nested.children[0] as HTMLElement).getByText('checker')).toBeTruthy()
})

it('shows each agent with its task, model, tools, state and how long it ran', () => {
  renderView()
  const researcher = cardOf('researcher')
  expect(within(researcher).getByText('task of outer-one')).toBeTruthy()
  expect(within(researcher).getByText('model-large')).toBeTruthy()
  expect(within(researcher).getByText('Read, Grep')).toBeTruthy()
  expect(within(researcher).getByText('Done')).toBeTruthy()
  expect(within(researcher).getByText('1m 20s')).toBeTruthy()
  expect(within(cardOf('writer')).getByText('Running')).toBeTruthy()
  expect(within(cardOf('checker')).getByText('30s')).toBeTruthy()
  expect(within(cardOf('Explore')).getByText('Stopped')).toBeTruthy()
})

it("folds an agent's agents away and back, counting every one under it", () => {
  renderView()
  const toggle = within(cardOf('researcher')).getByRole('button', { name: '3 agents' })
  expect(toggle.getAttribute('aria-expanded')).toBe('true')
  expect(screen.getAllByText('Explore', { selector: 'strong' })).toHaveLength(2)
  fireEvent.click(toggle)
  expect(screen.queryByText('Explore', { selector: 'strong' })).toBeNull()
  expect(screen.queryByText('checker')).toBeNull()
  expect(within(cardOf('researcher')).getByRole('button', { name: '3 agents' }).getAttribute('aria-expanded')).toBe(
    'false',
  )
  // The other agents stay where they were, and the heading still counts all.
  expect(screen.getByText('writer')).toBeTruthy()
  expect(screen.getByRole('heading', { level: 3 }).textContent).toBe('5 agents · 1 running')
  fireEvent.click(within(cardOf('researcher')).getByRole('button', { name: '3 agents' }))
  expect(screen.getAllByText('Explore', { selector: 'strong' })).toHaveLength(2)
  // An agent with none under it has nothing to fold.
  expect(within(cardOf('writer')).queryByRole('button')).toBeNull()
})

it('says Needs you on the session card only, while the session asks', () => {
  renderView({ herdrSession: { ...session, herdrSessionState: 'asking' } })
  expect(screen.getAllByText('Needs you')).toHaveLength(1)
  const sessionCard = screen.getByText('Claude Code').closest('article') as HTMLElement
  expect(within(sessionCard).getByText('Needs you')).toBeTruthy()
})

it('does not say Needs you while the session works or is idle', () => {
  renderView({ herdrSession: { ...session, herdrSessionState: 'working' } })
  expect(screen.queryByText('Needs you')).toBeNull()
  const sessionCard = screen.getByText('Claude Code').closest('article') as HTMLElement
  expect(within(sessionCard).getByText('Running')).toBeTruthy()
  cleanup()
  renderView()
  const idleCard = screen.getByText('Claude Code').closest('article') as HTMLElement
  expect(within(idleCard).getByText('idle')).toBeTruthy()
})

it('says the session has started no agents, with no heading and no tree', () => {
  renderView({ subagents: [] })
  expect(screen.getByText('This session has not started any agents for this request.')).toBeTruthy()
  expect(screen.queryByRole('heading')).toBeNull()
  expect(document.querySelector('ul.agents-top')).toBeNull()
  // The session's own card is still there.
  expect(screen.getByText('Claude Code')).toBeTruthy()
})

it('says when only the newest agents are shown', () => {
  renderView({ isTruncated: true })
  expect(screen.getByRole('heading', { level: 3 }).textContent).toContain('newest only')
})

it('writes a time as a person says one', () => {
  const t = (key: string, values?: Record<string, string | number>) =>
    ({
      'herdr.agents.durationSeconds': `${values?.seconds}s`,
      'herdr.agents.durationMinutes': `${values?.minutes}m`,
      'herdr.agents.durationMinutesSeconds': `${values?.minutes}m ${values?.seconds}s`,
      'herdr.agents.durationHours': `${values?.hours}h`,
      'herdr.agents.durationHoursMinutes': `${values?.hours}h ${values?.minutes}m`,
    })[key] ?? key
  const text = (milliseconds: number) => agentDurationText(milliseconds, t as never)
  expect(text(-5)).toBe('0s')
  expect(text(45_900)).toBe('45s')
  expect(text(60_000)).toBe('1m')
  expect(text(80_000)).toBe('1m 20s')
  expect(text(3_600_000)).toBe('1h')
  expect(text(7_500_000)).toBe('2h 5m')
})

import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'

import { graphql } from '../api'
import { HERDR_DOCUMENTS, type HerdrSession } from '../components/herdrSessions'
import { ToastProvider } from '../components/toast'
import { TranslationProvider } from '../i18n/i18n'
import { CodingPage, groupSessions } from './coding'

vi.mock('../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../api')>()),
  graphql: vi.fn(),
}))
const execute = vi.mocked(graphql)

function sessionOf(fields: Partial<HerdrSession> & Pick<HerdrSession, 'computer' | 'paneId'>): HerdrSession {
  return {
    paneName: '',
    codingAgentKind: 'claude',
    codingSessionId: '',
    herdrSessionState: 'idle',
    herdrAgentStatus: 'idle',
    paneTitle: '',
    workingDirectory: '~/src/website',
    transcriptPath: '~/.claude/projects/website/session.jsonl',
    isWatched: false,
    ...fields,
  }
}

// Listed out of order on purpose: the page orders workspaces and tabs itself.
const sessions: HerdrSession[] = [
  sessionOf({
    computer: 'laptop',
    paneId: 'w2:p1',
    workspaceId: 'w2',
    workspaceLabel: 'notes',
    workspaceNumber: 2,
    tabId: 'w2:t1',
    tabLabel: 'drafts',
    tabNumber: 1,
  }),
  sessionOf({
    computer: 'laptop',
    paneId: 'w1:p2',
    codingAgentKind: 'codex',
    workspaceId: 'w1',
    workspaceLabel: 'website',
    workspaceNumber: 1,
    tabId: 'w1:t2',
    tabLabel: 'review',
    tabNumber: 2,
    isTabFocused: true,
  }),
  sessionOf({
    computer: 'laptop',
    paneId: 'w1:p1',
    herdrSessionState: 'working',
    workspaceId: 'w1',
    workspaceLabel: 'website',
    workspaceNumber: 1,
    tabId: 'w1:t1',
    tabLabel: 'build',
    tabNumber: 1,
  }),
  sessionOf({
    computer: 'desk',
    paneId: 'w1:p1',
    workspaceId: 'w1',
    workspaceLabel: 'server',
    workspaceNumber: 1,
    tabId: 'w1:t1',
    tabLabel: 'logs',
    tabNumber: 1,
  }),
]

const list = { computerNames: ['laptop', 'desk'], failedComputerNames: [], sessions }

function find(computer: string, paneId: string): HerdrSession {
  return sessions.find((each) => each.computer === computer && each.paneId === paneId) ?? sessions[0]
}

function Where() {
  return <p data-testid="where">{useLocation().search}</p>
}

function renderAt(address: string) {
  render(
    <TranslationProvider>
      <ToastProvider>
        <MemoryRouter initialEntries={[address]}>
          <Where />
          <Routes>
            <Route path="/coding" element={<CodingPage />} />
          </Routes>
        </MemoryRouter>
      </ToastProvider>
    </TranslationProvider>,
  )
}

beforeEach(() => {
  execute.mockReset()
  execute.mockImplementation(async (document: string, variables?: Record<string, unknown>) => {
    const target = find(String(variables?.computer ?? 'laptop'), String(variables?.paneId ?? ''))
    switch (document) {
      case HERDR_DOCUMENTS.ListAgentHerdrSessions:
        return { ListAgentHerdrSessions: list }
      case HERDR_DOCUMENTS.ReadAgentHerdrSession:
        return {
          ReadAgentHerdrSession: {
            herdrSession: target,
            turns: [{ herdrTurnRole: 'person', turnText: `hello ${target.paneId}` }],
            isTruncated: false,
          },
        }
      case HERDR_DOCUMENTS.FocusAgentHerdrSession:
        return { FocusAgentHerdrSession: { ...target, isTabFocused: true } }
      case HERDR_DOCUMENTS.OpenAgentHerdrSession:
        return { OpenAgentHerdrSession: { ...target, paneId: 'w1:p9', tabLabel: 'scratch' } }
      case HERDR_DOCUMENTS.CloseAgentHerdrSession:
        return { CloseAgentHerdrSession: target }
      default:
        throw new Error(`unexpected document ${document}`)
    }
  })
})
afterEach(cleanup)

it('groups sessions by computer, then workspace in number order, then tab in tab order', () => {
  const groups = groupSessions(list)
  // Computers by name; their workspaces by number, and tabs in tab order.
  expect(groups.map((group) => group.computer)).toEqual(['desk', 'laptop'])
  expect(groups[1].workspaces.map((workspace) => workspace.label)).toEqual(['website', 'notes'])
  expect(groups[1].workspaces[0].sessions.map((session) => session.tabLabel)).toEqual(['build', 'review'])
})

it('lists the sessions under their computers and workspaces', async () => {
  renderAt('/coding')
  const rows = await screen.findAllByRole('button', { name: /build|review|drafts|logs/ })
  expect(rows.map((row) => row.querySelector('strong')?.textContent)).toEqual(['logs', 'build', 'review', 'drafts'])
  const heads = screen.getAllByRole('heading', { level: 3 })
  expect(heads.map((heading) => heading.querySelector('.coding-computer-name')?.textContent)).toEqual([
    'desk',
    'laptop',
  ])
})

it('puts the selected session in the address and reads it', async () => {
  renderAt('/coding?computer=laptop&pane=w1%3Ap1')
  await screen.findByText('hello w1:p1')
  expect(execute).toHaveBeenCalledWith(
    HERDR_DOCUMENTS.ReadAgentHerdrSession,
    expect.objectContaining({ computer: 'laptop', paneId: 'w1:p1', turnCount: 50 }),
  )
})

it('switches tabs by changing the address and reading the other session', async () => {
  renderAt('/coding?computer=laptop&pane=w1%3Ap1')
  await screen.findByText('hello w1:p1')
  const strip = screen.getByRole('tablist')
  expect(
    within(strip)
      .getAllByRole('tab')
      .map((tab) => tab.textContent?.replace('On screen', '')),
  ).toEqual(['build', 'review'])
  // The tab on herdr's screen is named for a screen reader.
  expect(within(strip).getByRole('tab', { name: /review/ }).textContent).toContain('On screen')
  fireEvent.click(within(strip).getByRole('tab', { name: /review/ }))
  await screen.findByText('hello w1:p2')
  expect(screen.getByTestId('where').textContent).toBe('?computer=laptop&pane=w1%3Ap2')
  expect(execute).toHaveBeenCalledWith(
    HERDR_DOCUMENTS.ReadAgentHerdrSession,
    expect.objectContaining({ paneId: 'w1:p2' }),
  )
})

it('keeps the screen view in the address', async () => {
  execute.mockImplementation(async (document: string, variables?: Record<string, unknown>) =>
    document === HERDR_DOCUMENTS.ListAgentHerdrSessions
      ? { ListAgentHerdrSessions: list }
      : { ReadAgentHerdrScreen: { herdrSession: find('laptop', String(variables?.paneId)), screenText: 'prompt>' } },
  )
  renderAt('/coding?computer=laptop&pane=w1%3Ap1&view=screen')
  await screen.findByText('prompt>')
  fireEvent.click(screen.getByRole('tab', { name: /review/ }))
  expect(screen.getByTestId('where').textContent).toContain('view=screen')
})

it('opens a new tab beside the session with the agent chosen', async () => {
  renderAt('/coding?computer=laptop&pane=w1%3Ap1')
  await screen.findByText('hello w1:p1')
  fireEvent.click(screen.getByRole('button', { name: 'New tab' }))
  fireEvent.click(await screen.findByRole('menuitem', { name: 'Codex' }))
  await waitFor(() =>
    expect(execute).toHaveBeenCalledWith(HERDR_DOCUMENTS.OpenAgentHerdrSession, {
      computer: 'laptop',
      paneId: 'w1:p1',
      codingAgentKind: 'codex',
    }),
  )
  await waitFor(() => expect(screen.getByTestId('where').textContent).toBe('?computer=laptop&pane=w1%3Ap9'))
})

it('shows the tab on the computer', async () => {
  renderAt('/coding?computer=laptop&pane=w1%3Ap2')
  await screen.findByText('hello w1:p2')
  fireEvent.click(screen.getByRole('button', { name: 'Show on computer' }))
  await waitFor(() =>
    expect(execute).toHaveBeenCalledWith(HERDR_DOCUMENTS.FocusAgentHerdrSession, {
      computer: 'laptop',
      paneId: 'w1:p2',
    }),
  )
})

it('asks before closing a tab, then closes it', async () => {
  renderAt('/coding?computer=laptop&pane=w1%3Ap2')
  await screen.findByText('hello w1:p2')
  fireEvent.click(screen.getByRole('button', { name: 'Close session' }))
  expect(execute).not.toHaveBeenCalledWith(HERDR_DOCUMENTS.CloseAgentHerdrSession, expect.anything())
  fireEvent.click(within(screen.getByRole('alertdialog')).getByRole('button', { name: 'Close session' }))
  await waitFor(() =>
    expect(execute).toHaveBeenCalledWith(HERDR_DOCUMENTS.CloseAgentHerdrSession, {
      computer: 'laptop',
      paneId: 'w1:p2',
    }),
  )
  // Its sibling is shown next.
  await waitFor(() => expect(screen.getByTestId('where').textContent).toBe('?computer=laptop&pane=w1%3Ap1'))
})

it('does not close a session that is working', async () => {
  renderAt('/coding?computer=laptop&pane=w1%3Ap1')
  await screen.findByText('hello w1:p1')
  expect((screen.getByRole('button', { name: 'Close session' }) as HTMLButtonElement).disabled).toBe(true)
})

it('shows a session that has not said anything yet as empty, without asking for its history', async () => {
  const fresh = sessionOf({
    computer: 'laptop',
    paneId: 'w9:p1',
    transcriptPath: '',
    workspaceId: 'w9',
    workspaceLabel: 'scratch',
  })
  list.sessions.push(fresh)
  try {
    renderAt('/coding?computer=laptop&pane=w9%3Ap1')
    await screen.findByText('Nothing has been said in this session yet.')
    expect(execute).not.toHaveBeenCalledWith(
      HERDR_DOCUMENTS.ReadAgentHerdrSession,
      expect.objectContaining({ paneId: 'w9:p1' }),
    )
  } finally {
    list.sessions.pop()
  }
})

it('keeps what was typed in each session while switching tabs and back', async () => {
  renderAt('/coding?computer=laptop&pane=w1%3Ap1')
  await screen.findByText('hello w1:p1')
  fireEvent.change(screen.getByRole('textbox'), { target: { value: 'half a thought' } })
  fireEvent.click(screen.getByRole('tab', { name: /review/ }))
  await screen.findByText('hello w1:p2')
  expect((screen.getByRole('textbox') as HTMLTextAreaElement).value).toBe('')
  fireEvent.change(screen.getByRole('textbox'), { target: { value: 'other draft' } })
  fireEvent.click(screen.getByRole('tab', { name: /build/ }))
  await screen.findByText('hello w1:p1')
  expect((screen.getByRole('textbox') as HTMLTextAreaElement).value).toBe('half a thought')
  fireEvent.click(screen.getByRole('tab', { name: /review/ }))
  await screen.findByText('hello w1:p2')
  expect((screen.getByRole('textbox') as HTMLTextAreaElement).value).toBe('other draft')
})

it('keeps the open session while its computer does not answer', async () => {
  renderAt('/coding?computer=laptop&pane=w1%3Ap1')
  await screen.findByText('hello w1:p1')
  const base = execute.getMockImplementation()!
  execute.mockImplementation(async (document: string, variables?: Record<string, unknown>) =>
    document === HERDR_DOCUMENTS.ListAgentHerdrSessions
      ? {
          ListAgentHerdrSessions: {
            computerNames: ['laptop', 'desk'],
            failedComputerNames: ['laptop'],
            sessions: sessions.filter((each) => each.computer === 'desk'),
          },
        }
      : base(document, variables),
  )
  const listCount = () =>
    execute.mock.calls.filter(([document]) => document === HERDR_DOCUMENTS.ListAgentHerdrSessions).length
  const before = listCount()
  // Showing the tab on the computer reads the list again at once.
  fireEvent.click(screen.getByRole('button', { name: 'Show on computer' }))
  await waitFor(() => expect(listCount()).toBeGreaterThan(before))
  await waitFor(() => expect(screen.getByText(/did not answer/)).toBeTruthy())
  expect(screen.queryByText('This session is no longer open.')).toBeNull()
  expect(screen.getByRole('tab', { name: /build/ })).toBeTruthy()
  expect(screen.getByText('hello w1:p1')).toBeTruthy()
})

it('folds a computer away and keeps it folded in the address', async () => {
  renderAt('/coding')
  const desk = await screen.findByRole('button', { name: /^desk/ })
  expect(desk.getAttribute('aria-expanded')).toBe('true')
  fireEvent.click(desk)
  expect(screen.getByTestId('where').textContent).toBe('?closed=desk')
  expect(screen.getByRole('button', { name: /^desk/ }).getAttribute('aria-expanded')).toBe('false')
  expect(screen.queryByRole('button', { name: /logs/ })).toBeNull()
  // Choosing a session keeps the fold.
  fireEvent.click(screen.getByRole('button', { name: /build/ }))
  expect(screen.getByTestId('where').textContent).toBe('?computer=laptop&pane=w1%3Ap1&closed=desk')
})

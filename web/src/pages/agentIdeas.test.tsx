import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { graphql, openAgentConversation, sendToAgentConversation } from '../api'
import { Idea } from '../components/ideaRow'
import { IdeasTab, ideaStatusKey } from './agentIdeas'

const notices = vi.hoisted(() => ({ done: vi.fn(), failed: vi.fn(), failure: vi.fn() }))
vi.mock('../api', () => ({
  graphql: vi.fn(),
  openAgentConversation: vi.fn(() => true),
  sendToAgentConversation: vi.fn(() => true),
}))
vi.mock('../i18n/i18n', () => ({
  useTranslation: () => ({ t: (key: string) => key, plural: (count: number) => String(count), language: 'en' }),
}))
vi.mock('../components/toast', () => ({ useToast: () => notices }))
const execute = vi.mocked(graphql)
const send = vi.mocked(sendToAgentConversation)
const open = vi.mocked(openAgentConversation)

function idea(id: string, overrides: Partial<Idea> = {}): Idea {
  return {
    id,
    ideaKind: 'catalog',
    ideaCategory: 'home',
    emoji: '🪴',
    headline: `Plan the watering ${id}`,
    body: 'Tell me the plants and I will keep a schedule.',
    openingRequest: `Plan the watering for my plants ${id}.`,
    suggestionReason: '',
    evidence: [],
    ideaStatus: 'open',
    expiredReason: '',
    startedConversationId: '',
    createdAt: '2030-01-01T00:00:00Z',
    shownAt: '2030-01-01T00:00:00Z',
    startedAt: null,
    closedAt: null,
    ...overrides,
  }
}

function listing(ideas: Idea[]) {
  return { ListAgentIdeas: { ideas, ideaCategories: [{ ideaCategory: 'home', emojis: ['🪴'] }] } }
}

beforeEach(() => {
  execute.mockReset()
  send.mockClear()
  open.mockClear()
  notices.done.mockReset()
  notices.failure.mockReset()
  notices.failed.mockReset()
})
afterEach(cleanup)

function drawIdeas() {
  return render(
    <MemoryRouter>
      <IdeasTab />
    </MemoryRouter>,
  )
}

it('starts an idea and sends its opening request in the conversation it made', async () => {
  execute.mockImplementation(async (document: string) => {
    if (document.includes('StartAgentIdea'))
      return {
        StartAgentIdea: { conversation: { id: 'watering-conversation' }, openingRequest: 'Plan the watering for me.' },
      }
    return listing([idea('first')])
  })
  drawIdeas()
  fireEvent.click(await screen.findByRole('button', { name: /Plan the watering first\s*Tell/ }))
  await waitFor(() => expect(send).toHaveBeenCalledWith('watering-conversation', 'Plan the watering for me.'))
  expect(execute).toHaveBeenCalledWith(expect.stringContaining('StartAgentIdea'), {
    ideaId: 'first',
    conversationId: null,
    language: 'en',
  })
  expect(open).not.toHaveBeenCalled()
})

it('opens the conversation of an idea in the history only when it has one', async () => {
  execute.mockResolvedValue(
    listing([
      idea('kept', {
        ideaStatus: 'done',
        startedConversationId: 'kept-conversation',
        closedAt: '2030-01-02T00:00:00Z',
      }),
      idea('orphan', { ideaStatus: 'done', startedConversationId: '', closedAt: '2030-01-03T00:00:00Z' }),
    ]),
  )
  drawIdeas()
  fireEvent.click(await screen.findByRole('button', { name: /^🪴 Plan the watering kept$/ }))
  expect(open).toHaveBeenCalledWith('kept-conversation')
  expect(screen.queryByRole('button', { name: /^🪴 Plan the watering orphan$/ })).toBeNull()
  expect(screen.getByText(/Plan the watering orphan/)).toBeTruthy()
})

it('restores an idea from the history to the open ones, with a toast', async () => {
  let ideaStatus: Idea['ideaStatus'] = 'dismissed'
  execute.mockImplementation(async (document: string) => {
    if (document.includes('SetAgentIdeaStatus')) {
      ideaStatus = 'open'
      return { SetAgentIdeaStatus: { id: 'kept', ideaStatus } }
    }
    return listing([idea('kept', { ideaStatus, closedAt: '2030-01-02T00:00:00Z' })])
  })
  drawIdeas()
  fireEvent.click(await screen.findByRole('button', { name: 'Plan the watering kept: ideas.restore' }))
  await waitFor(() => expect(notices.done).toHaveBeenCalledWith('ideas.restored'))
  expect(execute).toHaveBeenCalledWith(expect.stringContaining('SetAgentIdeaStatus'), {
    ideaId: 'kept',
    ideaStatus: 'open',
  })
  expect(await screen.findByRole('button', { name: /Plan the watering kept\s*Tell/ })).toBeTruthy()
})

it('restores a started idea too, beside marking it done', async () => {
  execute.mockResolvedValue(
    listing([idea('kept', { ideaStatus: 'started', startedConversationId: 'kept-conversation' })]),
  )
  drawIdeas()
  expect(await screen.findByRole('button', { name: 'Plan the watering kept: ideas.markDone' })).toBeTruthy()
  expect(screen.getByRole('button', { name: 'Plan the watering kept: ideas.restore' })).toBeTruthy()
})

it('says why the server refused to restore an idea', async () => {
  execute.mockImplementation(async (document: string) => {
    if (document.includes('SetAgentIdeaStatus')) throw new Error('the idea is gone')
    return listing([idea('kept', { ideaStatus: 'dismissed' })])
  })
  drawIdeas()
  fireEvent.click(await screen.findByRole('button', { name: 'Plan the watering kept: ideas.restore' }))
  await waitFor(() => expect(notices.failure).toHaveBeenCalledWith(expect.any(Error), 'ideas.failed'))
  expect(notices.done).not.toHaveBeenCalled()
})

it.each([
  ['missing_tool', 'ideas.cannotRestoreMissingTool'],
  ['', 'ideas.cannotRestore'],
] as const)(
  'says why an expired catalog idea (%s) cannot come back, without asking the server',
  async (expiredReason, notice) => {
    execute.mockResolvedValue(listing([idea('kept', { ideaStatus: 'expired', expiredReason })]))
    drawIdeas()
    fireEvent.click(await screen.findByRole('button', { name: 'Plan the watering kept: ideas.restore' }))
    expect(notices.failed).toHaveBeenCalledWith(notice)
    expect(execute).not.toHaveBeenCalledWith(expect.stringContaining('SetAgentIdeaStatus'), expect.anything())
  },
)

it('restores a catalog idea the person already does', async () => {
  execute.mockImplementation(async (document: string) => {
    if (document.includes('SetAgentIdeaStatus')) return { SetAgentIdeaStatus: { id: 'kept', ideaStatus: 'open' } }
    return listing([idea('kept', { ideaStatus: 'expired', expiredReason: 'already_used' })])
  })
  drawIdeas()
  fireEvent.click(await screen.findByRole('button', { name: 'Plan the watering kept: ideas.restore' }))
  await waitFor(() => expect(notices.done).toHaveBeenCalledWith('ideas.restored'))
  expect(notices.failed).not.toHaveBeenCalled()
})

it('says in the history why an idea expired, and out of date when nobody knows', async () => {
  execute.mockResolvedValue(
    listing([
      idea('dated', { ideaKind: 'personal', ideaStatus: 'expired', expiredReason: 'past_date' }),
      idea('toolless', { ideaStatus: 'expired', expiredReason: 'missing_tool' }),
      idea('old', { ideaStatus: 'expired' }),
    ]),
  )
  drawIdeas()
  expect(await screen.findByText('ideas.expiredReason.past_date')).toBeTruthy()
  expect(screen.getByText('ideas.expiredReason.missing_tool')).toBeTruthy()
  expect(screen.getByText('ideas.status.expired')).toBeTruthy()
})

it('words each reason an idea expired, and each other status as it is', () => {
  expect(ideaStatusKey({ ideaStatus: 'expired', expiredReason: 'past_date' })).toBe('ideas.expiredReason.past_date')
  expect(ideaStatusKey({ ideaStatus: 'expired', expiredReason: 'missing_tool' })).toBe(
    'ideas.expiredReason.missing_tool',
  )
  expect(ideaStatusKey({ ideaStatus: 'expired', expiredReason: 'already_used' })).toBe(
    'ideas.expiredReason.already_used',
  )
  expect(ideaStatusKey({ ideaStatus: 'expired', expiredReason: '' })).toBe('ideas.status.expired')
  expect(ideaStatusKey({ ideaStatus: 'done', expiredReason: '' })).toBe('ideas.status.done')
})

it('restores an expired personal idea', async () => {
  execute.mockImplementation(async (document: string) => {
    if (document.includes('SetAgentIdeaStatus')) return { SetAgentIdeaStatus: { id: 'kept', ideaStatus: 'open' } }
    return listing([idea('kept', { ideaKind: 'personal', ideaStatus: 'expired' })])
  })
  drawIdeas()
  fireEvent.click(await screen.findByRole('button', { name: 'Plan the watering kept: ideas.restore' }))
  await waitFor(() => expect(notices.done).toHaveBeenCalledWith('ideas.restored'))
})

import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, expect, it, vi } from 'vitest'

import { graphql, openAgentConversation, startAgentConversation } from '../api'
import { AgentConversationsPage } from './agentConversations'

vi.mock('../api', () => ({
  graphql: vi.fn(),
  openAgentConversation: vi.fn(() => true),
  startAgentConversation: vi.fn(() => true),
}))
vi.mock('../components/agentDrawer', () => ({ CONVERSATIONS: 'query ListAgentConversations' }))
vi.mock('../components/breadcrumb', () => ({ useBreadcrumbDetail: vi.fn() }))
vi.mock('../i18n/i18n', () => ({
  useTranslation: () => ({
    t: (key: string, values?: Record<string, string>) => (values ? `${key} ${JSON.stringify(values)}` : key),
    language: 'en',
  }),
}))
vi.mock('../components/toast', () => ({ useToast: () => ({ done: vi.fn(), failed: vi.fn(), failure: vi.fn() }) }))
const execute = vi.mocked(graphql)
afterEach(cleanup)

const CONVERSATIONS = [
  {
    id: 'side-1',
    kind: 'named',
    title: 'Garden plans',
    summary: 'Which seeds to order in spring',
    lastAt: '2026-01-02T10:00:00Z',
  },
  { id: 'main-1', kind: 'main', title: '', summary: '', lastAt: '2026-01-01T10:00:00Z' },
  {
    id: 'side-2',
    kind: 'named',
    title: 'Trip',
    summary: 'Trains between two coastal towns',
    lastAt: '2026-01-03T10:00:00Z',
  },
]

it('lists the main chat first, narrows by title or summary, and opens a tile in the drawer', async () => {
  execute.mockResolvedValue({ ListAgentConversations: CONVERSATIONS })
  render(
    <MemoryRouter>
      <AgentConversationsPage />
    </MemoryRouter>,
  )

  // The tile is the button, so its title is the part it is named by.
  const titles = (await screen.findAllByRole('button', { name: /agentDrawer\.main|Garden plans|Trip/ })).map((button) =>
    button.querySelector('.conversation-tile-title')?.textContent?.trim(),
  )
  expect(titles).toEqual(['agentDrawer.main', 'Trip', 'Garden plans'])

  fireEvent.change(screen.getByRole('searchbox'), { target: { value: 'seeds' } })
  expect(screen.queryByRole('button', { name: 'Trip' })).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: 'Garden plans' }))
  expect(vi.mocked(openAgentConversation)).toHaveBeenCalledWith('side-1')

  fireEvent.change(screen.getByRole('searchbox'), { target: { value: 'nothing like this' } })
  expect(screen.getByText('agentConversations.nothingFound')).toBeTruthy()
})

it('starts a new conversation in the drawer without asking anything first', async () => {
  execute.mockResolvedValue({ ListAgentConversations: [] })
  render(
    <MemoryRouter>
      <AgentConversationsPage />
    </MemoryRouter>,
  )

  expect(await screen.findByText('agentConversations.empty')).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: 'agentConversations.new' }))
  expect(vi.mocked(startAgentConversation)).toHaveBeenCalledTimes(1)
})

it('opens the conversation a link from memory names, once, and forgets it', async () => {
  execute.mockResolvedValue({ ListAgentConversations: CONVERSATIONS })
  vi.mocked(openAgentConversation).mockClear()
  render(
    <MemoryRouter initialEntries={['/agent?conversation=side-2&message=message-9']}>
      <AgentConversationsPage />
    </MemoryRouter>,
  )
  await screen.findAllByRole('button', { name: /Trip/ })
  expect(vi.mocked(openAgentConversation)).toHaveBeenCalledTimes(1)
  expect(vi.mocked(openAgentConversation)).toHaveBeenCalledWith('side-2')
})

it('filters by kind from the address, and shows everything to somebody with no chats', async () => {
  const chat = {
    id: 'chat-1',
    kind: 'chat',
    title: 'Weekend',
    summary: 'Where to walk on Saturday',
    lastAt: '2026-01-04T10:00:00Z',
    chat: { title: 'Weekend', chatKind: 'group', members: [] },
  }
  execute.mockResolvedValue({ ListAgentConversations: [...CONVERSATIONS, chat] })
  render(
    <MemoryRouter initialEntries={['/agent?kind=chats']}>
      <AgentConversationsPage />
    </MemoryRouter>,
  )
  expect(await screen.findByRole('button', { name: /Weekend/ })).toBeTruthy()
  expect(screen.queryByRole('button', { name: /Garden plans/ })).toBeNull()
  expect(screen.getByRole('button', { name: 'agentConversations.kindChats' }).getAttribute('aria-pressed')).toBe('true')
  cleanup()

  execute.mockResolvedValue({ ListAgentConversations: CONVERSATIONS })
  render(
    <MemoryRouter initialEntries={['/agent?kind=chats']}>
      <AgentConversationsPage />
    </MemoryRouter>,
  )
  expect(await screen.findByRole('button', { name: /Garden plans/ })).toBeTruthy()
  expect(screen.queryByRole('group', { name: 'agentConversations.kindLabel' })).toBeNull()
})

it('starts with the words the search across everything sent in the address', async () => {
  execute.mockResolvedValue({ ListAgentConversations: CONVERSATIONS })
  render(
    <MemoryRouter initialEntries={['/chat?search=seeds']}>
      <AgentConversationsPage />
    </MemoryRouter>,
  )
  expect(await screen.findByRole('button', { name: 'Garden plans' })).toBeTruthy()
  expect((screen.getByRole('searchbox') as HTMLInputElement).value).toBe('seeds')
  expect(screen.queryByRole('button', { name: 'Trip' })).toBeNull()
})

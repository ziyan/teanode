import { cleanup, fireEvent, render, screen } from '@testing-library/react'
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
  render(<AgentConversationsPage />)

  const titles = (await screen.findAllByRole('button', { name: /agentDrawer\.main|Garden plans|Trip/ })).map((button) =>
    button.textContent?.trim(),
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
  render(<AgentConversationsPage />)

  expect(await screen.findByText('agentConversations.empty')).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: 'agentConversations.new' }))
  expect(vi.mocked(startAgentConversation)).toHaveBeenCalledTimes(1)
})

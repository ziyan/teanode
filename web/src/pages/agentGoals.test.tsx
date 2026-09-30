import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, expect, it, vi } from 'vitest'

import { graphql, openAgentConversation, sendToAgentConversation } from '../api'
import { GoalsTab } from './agentGoals'

vi.mock('../api', () => ({
  graphql: vi.fn(),
  openAgentConversation: vi.fn(() => true),
  sendToAgentConversation: vi.fn(() => true),
}))
vi.mock('../i18n/i18n', () => ({
  useTranslation: () => ({
    t: (key: string, values?: Record<string, string>) => (values ? `${key} ${JSON.stringify(values)}` : key),
  }),
}))
vi.mock('../components/toast', () => ({ useToast: () => ({ done: vi.fn(), failed: vi.fn(), failure: vi.fn() }) }))
const execute = vi.mocked(graphql)
afterEach(cleanup)

it('sends the goal request at once in the conversation a goal area starts', async () => {
  execute.mockImplementation(async (document: string) => {
    if (document.includes('StartAgentConversation')) return { StartAgentConversation: { id: 'garden-goal' } }
    if (document.includes('ListAgentSchedules')) return { ListAgentSchedules: [] }
    if (document.includes('ListAgentIdeas'))
      return { ListAgentIdeas: { ideas: [], ideaCategories: [{ ideaCategory: 'home', emojis: ['🏠'] }] } }
    return { ListAgentConversations: [] }
  })
  render(
    <MemoryRouter>
      <GoalsTab />
    </MemoryRouter>,
  )
  fireEvent.click(await screen.findByRole('button', { name: /ideas\.category\.home/ }))
  await waitFor(() =>
    expect(vi.mocked(sendToAgentConversation)).toHaveBeenCalledWith(
      'garden-goal',
      'goals.openingRequest {"area":"ideas.category.home"}',
    ),
  )
  expect(execute).toHaveBeenCalledWith(expect.stringContaining('StartAgentConversation'), {
    title: 'goals.newTitle {"area":"ideas.category.home"}',
  })
  expect(vi.mocked(openAgentConversation)).not.toHaveBeenCalled()
})

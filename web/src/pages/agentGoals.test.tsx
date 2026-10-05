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

const waitingGoal = {
  conversationId: 'goal-boiler',
  goalTitle: 'Boiler reply',
  goalDescription: 'Watch for the landlord’s reply about the boiler.',
  goalState: 'waiting',
  goalStatus: 'No reply yet; shall I write again?',
  goalSetAt: '2030-05-01T09:00:00Z',
  goalNextAt: null,
  lastAt: '2030-05-02T09:00:00Z',
}

it('lists goals by where they stand, and answers one from its page', async () => {
  execute.mockReset()
  execute.mockImplementation(async (document: string) => {
    if (document.includes('ListAgentGoals'))
      return {
        ListAgentGoals: [
          waitingGoal,
          {
            ...waitingGoal,
            conversationId: 'goal-tax',
            goalTitle: 'Tax papers',
            goalState: 'working',
            goalStatus: 'Two of three found',
          },
          { ...waitingGoal, conversationId: 'goal-old', goalTitle: 'Old goal', goalState: 'met', goalStatus: '' },
        ],
      }
    if (document.includes('GetAgentGoal'))
      return {
        GetAgentGoal: {
          ...waitingGoal,
          activity: [
            {
              id: 'a2',
              createdAt: '2030-05-02T09:00:00Z',
              goalActivityKind: 'waiting',
              activityHeadline: 'Needs you',
              activityDetail: 'No reply yet',
            },
            {
              id: 'a1',
              createdAt: '2030-05-01T09:00:00Z',
              goalActivityKind: 'started',
              activityHeadline: 'Started Boiler reply',
            },
          ],
          schedules: [{ id: 's1', name: 'Look for the reply', cron: '0 9 * * *', enabled: true, nextRunAt: null }],
          backgroundWork: [],
          artifacts: [],
        },
      }
    if (document.includes('TellAgentGoal')) return { TellAgentGoal: { conversationId: 'goal-boiler' } }
    if (document.includes('ListAgentSchedules')) return { ListAgentSchedules: [] }
    if (document.includes('ListAgentIdeas')) return { ListAgentIdeas: { ideas: [], ideaCategories: [] } }
    return {}
  })
  render(
    <MemoryRouter initialEntries={['/settings/agent/goals']}>
      <GoalsTab />
    </MemoryRouter>,
  )
  // Needs you first, then tracking; done is behind a button.
  expect(await screen.findByText('goals.needsYou')).toBeTruthy()
  expect(screen.getByText('No reply yet; shall I write again?')).toBeTruthy()
  expect(screen.getByText('Tax papers')).toBeTruthy()
  expect(screen.queryByText('Old goal')).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: /goals\.showDone/ }))
  expect(screen.getByText('Old goal')).toBeTruthy()

  // Its page: what happened, what it made, and an answer passed on.
  fireEvent.click(screen.getByRole('button', { name: 'Boiler reply' }))
  expect(await screen.findByText('Started Boiler reply')).toBeTruthy()
  expect(screen.getByText('Look for the reply')).toBeTruthy()
  fireEvent.change(screen.getByLabelText('goals.tellLabel'), { target: { value: 'Yes, write again' } })
  fireEvent.click(screen.getByRole('button', { name: 'goals.tell' }))
  await waitFor(() =>
    expect(execute).toHaveBeenCalledWith(expect.stringContaining('TellAgentGoal'), {
      conversationId: 'goal-boiler',
      text: 'Yes, write again',
    }),
  )
})

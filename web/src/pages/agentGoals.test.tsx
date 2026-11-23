import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { graphql, openAgentConversation, sendToAgentConversation } from '../api'
import { ToastProvider } from '../components/toast'
import { TranslationProvider } from '../i18n/i18n'
import { GoalsTab } from './agentGoals'

vi.mock('../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../api')>()),
  graphql: vi.fn(),
  openAgentConversation: vi.fn(() => true),
  sendToAgentConversation: vi.fn(() => true),
}))
const execute = vi.mocked(graphql)

const waitingGoal = {
  conversationId: 'goal-boiler',
  goalTitle: 'Boiler reply',
  goalDescription: 'Watch for the landlord’s reply about the boiler.',
  goalState: 'waiting',
  goalStatus: 'No reply yet; shall I write again?',
  goalTimes: [] as string[],
  goalMailConditions: [] as { field: string; header: string; operator: string; value: string }[],
  goalWrittenBy: 'person',
  isGoalNotifiedWhenPosted: false,
  goalSetAt: '2030-05-01T09:00:00Z',
  goalNextAt: null as string | null,
  lastAt: '2030-05-02T09:00:00Z',
}

const mailCondition = { field: 'from', header: '', operator: 'contains', value: 'news@school.example' }

function show(address = '/settings/agent/goals') {
  render(
    <MemoryRouter initialEntries={[address]}>
      <TranslationProvider>
        <ToastProvider>
          <GoalsTab />
        </ToastProvider>
      </TranslationProvider>
    </MemoryRouter>,
  )
}

function called(name: string) {
  return execute.mock.calls.filter(([document]) => document.includes(name))
}

// A server that has the goals given, and answers every change with an empty
// success.
function serve(goals: (typeof waitingGoal)[], details?: object) {
  execute.mockReset()
  execute.mockImplementation(async (document: string) => {
    if (document.includes('ListAgentGoals')) return { ListAgentGoals: goals }
    if (document.includes('GetAgentGoal'))
      return { GetAgentGoal: { ...goals[0], activity: [], backgroundWork: [], artifacts: [], ...details } }
    if (document.includes('ListAgentIdeas')) return { ListAgentIdeas: { ideas: [], ideaCategories: [] } }
    return {}
  })
}

beforeEach(() => {
  execute.mockReset()
})
afterEach(cleanup)

it('sends the goal request at once in the conversation a goal area starts', async () => {
  execute.mockImplementation(async (document: string) => {
    if (document.includes('StartAgentConversation')) return { StartAgentConversation: { id: 'garden-goal' } }
    if (document.includes('ListAgentIdeas'))
      return { ListAgentIdeas: { ideas: [], ideaCategories: [{ ideaCategory: 'home', emojis: ['🏠'] }] } }
    return { ListAgentGoals: [] }
  })
  show()
  fireEvent.click(await screen.findByRole('button', { name: /Home/ }))
  await waitFor(() => expect(vi.mocked(sendToAgentConversation)).toHaveBeenCalled())
  expect(vi.mocked(sendToAgentConversation).mock.calls[0][0]).toBe('garden-goal')
  expect(called('StartAgentConversation')[0][1]).toEqual({ title: 'A goal: Home' })
  expect(vi.mocked(openAgentConversation)).not.toHaveBeenCalled()
})

it('lists goals by where they stand, with when each runs in words and a Paused group', async () => {
  serve([
    waitingGoal,
    {
      ...waitingGoal,
      conversationId: 'goal-tax',
      goalTitle: 'Tax papers',
      goalState: 'working',
      goalStatus: 'Two of three found',
      goalTimes: ['30 7 * * 1-5', '0 9 * * 1'],
      goalNextAt: '2030-05-07T07:30:00Z',
    },
    {
      ...waitingGoal,
      conversationId: 'goal-school',
      goalTitle: 'School notices',
      goalState: 'working',
      goalStatus: '',
      goalMailConditions: [mailCondition],
    },
    {
      ...waitingGoal,
      conversationId: 'goal-garden',
      goalTitle: 'Garden plan',
      goalState: 'paused',
      goalStatus: '',
      goalTimes: ['@at 2030-12-11 09:00'],
    },
    { ...waitingGoal, conversationId: 'goal-old', goalTitle: 'Old goal', goalState: 'met', goalStatus: '' },
  ])
  show()
  expect(await screen.findByRole('heading', { name: 'Needs you' })).toBeTruthy()
  expect(screen.getByText('Weekdays at 7:30 and Every Monday at 9:00')).toBeTruthy()
  expect(screen.getByText(/^When mail arrives: .*news@school\.example$/)).toBeTruthy()
  // A working goal with times says when it is next, in the words of a set time.
  expect(screen.getByText(/^Next /)).toBeTruthy()
  // Paused goals have a group of their own, between Tracking and Done.
  const headings = screen.getAllByRole('heading').map((heading) => heading.textContent)
  expect(headings.indexOf('Tracking')).toBeLessThan(headings.indexOf('Paused'))
  expect(headings.indexOf('Paused')).toBeLessThan(headings.indexOf('Done'))
  expect(screen.getByText('Dec 11 at 9:00', { exact: false })).toBeTruthy()
  expect(screen.queryByText('Old goal')).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: /Show 1 done/ }))
  expect(screen.getByText('Old goal')).toBeTruthy()
})

it('answers a goal from its page', async () => {
  serve([waitingGoal], {
    activity: [
      {
        id: 'a1',
        createdAt: '2030-05-01T09:00:00Z',
        goalActivityKind: 'started',
        activityHeadline: 'Started Boiler reply',
      },
    ],
  })
  show()
  fireEvent.click(await screen.findByRole('button', { name: 'Boiler reply' }))
  expect(await screen.findByText('Started Boiler reply')).toBeTruthy()
  fireEvent.change(screen.getByLabelText('Your answer'), { target: { value: 'Yes, write again' } })
  fireEvent.click(screen.getByRole('button', { name: 'Tell it' }))
  await waitFor(() =>
    expect(called('TellAgentGoal')[0]?.[1]).toEqual({ conversationId: 'goal-boiler', text: 'Yes, write again' }),
  )
})

it('starts a goal at its own pace with no times, then on set times, then on mail', async () => {
  serve([])
  show()
  const title = await screen.findByLabelText('Title')
  fireEvent.change(title, { target: { value: 'Park permit' } })
  fireEvent.change(screen.getByLabelText('What it is for'), { target: { value: 'Renew it before June.' } })
  fireEvent.click(screen.getByRole('button', { name: 'Start' }))
  await waitFor(() => expect(called('StartAgentGoal')).toHaveLength(1))
  expect(called('StartAgentGoal')[0][1]).toEqual({ goalTitle: 'Park permit', goalDescription: 'Renew it before June.' })

  // Set times: the default is weekdays at 7:30, changed to 8:15, with the notice on.
  fireEvent.change(screen.getByLabelText('Title'), { target: { value: 'Park permit' } })
  fireEvent.change(screen.getByLabelText('What it is for'), { target: { value: 'Renew it before June.' } })
  fireEvent.click(screen.getByRole('button', { name: 'At set times' }))
  fireEvent.change(screen.getByLabelText('Time of day'), { target: { value: '08:15' } })
  fireEvent.click(screen.getByRole('checkbox', { name: 'Notify me when it posts' }))
  fireEvent.click(screen.getByRole('button', { name: 'Start' }))
  await waitFor(() => expect(called('StartAgentGoal')).toHaveLength(2))
  expect(called('StartAgentGoal')[1][1]).toEqual({
    goalTitle: 'Park permit',
    goalDescription: 'Renew it before June.',
    goalTimes: ['15 8 * * 1-5'],
    goalMailConditions: [],
    isGoalNotifiedWhenPosted: true,
  })

  // Mail: one condition, who it is from.
  fireEvent.change(screen.getByLabelText('What it is for'), { target: { value: 'Add dates to my calendar.' } })
  fireEvent.click(screen.getByRole('button', { name: 'When mail arrives' }))
  fireEvent.change(screen.getByRole('combobox', { name: 'What it looks for' }), {
    target: { value: 'news@school.example' },
  })
  fireEvent.click(screen.getByRole('button', { name: 'Start' }))
  await waitFor(() => expect(called('StartAgentGoal')).toHaveLength(3))
  expect(called('StartAgentGoal')[2][1]).toEqual({
    goalTitle: '',
    goalDescription: 'Add dates to my calendar.',
    goalTimes: [],
    goalMailConditions: [{ field: 'from', header: '', operator: 'contains', value: 'news@school.example' }],
  })
})

it('edits When and sends only what changed', async () => {
  serve([
    {
      ...waitingGoal,
      goalState: 'working',
      goalTimes: ['30 7 * * 1-5', '@at 2030-12-11 09:00'],
      goalMailConditions: [mailCondition],
    },
  ])
  show('/settings/agent/goals?goal=goal-boiler')
  expect(await screen.findByText('Weekdays at 7:30')).toBeTruthy()
  expect(screen.getByText(/^On Dec 11 at 9:00$/)).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: 'Edit' }))
  const dialog = await screen.findByRole('dialog')
  fireEvent.change(within(dialog).getAllByLabelText('Time of day')[0], { target: { value: '08:00' } })
  fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }))
  await waitFor(() => expect(called('UpdateAgentGoal')).toHaveLength(1))
  // Not the title, not the description, not the mail it already waits for.
  expect(called('UpdateAgentGoal')[0][1]).toEqual({
    conversationId: 'goal-boiler',
    goalTimes: ['0 8 * * 1-5', '@at 2030-12-11 09:00'],
  })
})

it('edits the title and description, sending only the one that changed', async () => {
  serve([{ ...waitingGoal, goalState: 'working' }])
  show('/settings/agent/goals?goal=goal-boiler')
  fireEvent.click(await screen.findByRole('button', { name: 'Edit title and description' }))
  const dialog = await screen.findByRole('dialog')
  fireEvent.change(within(dialog).getByLabelText('Title'), { target: { value: 'Boiler repair' } })
  fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }))
  await waitFor(() => expect(called('UpdateAgentGoal')).toHaveLength(1))
  expect(called('UpdateAgentGoal')[0][1]).toEqual({ conversationId: 'goal-boiler', goalTitle: 'Boiler repair' })
})

it('runs a working goal now and pauses it', async () => {
  serve([{ ...waitingGoal, goalState: 'working', goalTimes: ['30 7 * * 1-5'] }])
  show('/settings/agent/goals?goal=goal-boiler')
  fireEvent.click(await screen.findByRole('button', { name: 'Run now' }))
  await waitFor(() => expect(called('RunAgentGoal')[0]?.[1]).toEqual({ conversationId: 'goal-boiler' }))
  fireEvent.click(screen.getByRole('button', { name: 'Pause' }))
  await waitFor(() =>
    expect(called('SetAgentGoalState')[0]?.[1]).toEqual({ conversationId: 'goal-boiler', goalState: 'paused' }),
  )
})

it('resumes a paused goal, which cannot be run now', async () => {
  serve([{ ...waitingGoal, goalState: 'paused', goalTimes: ['30 7 * * 1-5'] }])
  show('/settings/agent/goals?goal=goal-boiler')
  fireEvent.click(await screen.findByRole('button', { name: 'Resume' }))
  expect(screen.queryByRole('button', { name: 'Run now' })).toBeNull()
  await waitFor(() =>
    expect(called('SetAgentGoalState')[0]?.[1]).toEqual({ conversationId: 'goal-boiler', goalState: 'working' }),
  )
})

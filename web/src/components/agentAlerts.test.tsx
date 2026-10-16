import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'

import { graphql } from '../api'
import { TranslationProvider } from '../i18n/i18n'
import {
  AgentAlert,
  AlertsCard,
  FollowUp,
  followUpDay,
  followUpDetails,
  muteTargets,
  orderedFollowUps,
} from './agentAlerts'
import { ToastProvider } from './toast'

vi.mock('../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../api')>()),
  graphql: vi.fn(),
}))
afterEach(cleanup)

const alert = (muteChoices: AgentAlert['muteChoices']): AgentAlert => ({
  id: 'alert-1',
  alertText: 'Someone has been asking for sign-in codes.',
  sentAt: '2026-09-29T10:00:00Z',
  subjectKey: 'photo app sign-in codes',
  isUrgent: false,
  covered: [
    {
      mailId: 'mail-1',
      subject: 'Your code',
      fromAddress: 'no-reply@photos.example.com',
      candidateKind: 'burst',
      mailCategory: 'notification',
    },
  ],
  muteChoices,
})

// What a Mute offers is what the server would mute, in its order: the
// first is the default, the burst itself rather than the words the model
// chose for it.
it('offers what the server would mute, the default first', () => {
  const choices = [
    { muteScope: 'subjectKey' as const, muteTarget: 'no-reply@photos.example.com|your sign-in code is' },
    { muteScope: 'sender' as const, muteTarget: 'no-reply@photos.example.com' },
    { muteScope: 'domain' as const, muteTarget: 'photos.example.com' },
    { muteScope: 'kind' as const, muteTarget: 'burst' },
  ]
  expect(muteTargets(alert(choices))).toEqual(choices)
})

it('offers nothing when the server has nothing to offer', () => {
  expect(muteTargets(alert([]))).toEqual([])
})

const followUp = (fields: Partial<FollowUp>): FollowUp => ({
  id: 'follow-1',
  followUpKind: 'bill',
  followUpTitle: 'Water bill',
  payee: null,
  carrier: null,
  amount: null,
  currencyCode: null,
  trackingNumber: null,
  dueOn: null,
  place: null,
  followUpState: 'open',
  ...fields,
})

it('orders follow-ups by day, the ones with no day last', () => {
  const ordered = orderedFollowUps([
    followUp({ id: 'parcel', followUpKind: 'parcel' }),
    followUp({ id: 'late', dueOn: '2026-11-20' }),
    followUp({ id: 'soon', dueOn: '2026-11-03' }),
    followUp({ id: 'same-day', dueOn: '2026-11-03' }),
  ])
  expect(ordered.map((each) => each.id)).toEqual(['soon', 'same-day', 'late', 'parcel'])
})

it('says what a kind has beyond its title', () => {
  expect(
    followUpDetails(followUp({ payee: 'Example Water', amount: '84.20', currencyCode: 'USD' })).join(' | '),
  ).toMatch(/^Example Water \| .*84\.20$/)
  expect(
    followUpDetails(followUp({ followUpKind: 'parcel', carrier: 'Example Post', trackingNumber: 'EX123456789' })),
  ).toEqual(['Example Post EX123456789'])
  expect(followUpDetails(followUp({ followUpKind: 'event', place: 'Elm Street' }))).toEqual(['Elm Street'])
  // No currency to format by: the number as it came.
  expect(followUpDetails(followUp({ amount: '84.20' }))).toEqual(['84.20'])
})

it('writes a day in the reader calendar, not the evening before', () => {
  expect(followUpDay('2026-11-03')).toContain('3')
  expect(followUpDay('not a day')).toBe('not a day')
})

const renderCard = () =>
  render(
    <TranslationProvider>
      <ToastProvider>
        <AlertsCard />
      </ToastProvider>
    </TranslationProvider>,
  )

it('lists what is followed, tags the overdue, and stops one with a word of thanks', async () => {
  let listed = [
    followUp({
      id: 'soon',
      followUpTitle: 'Garden shears',
      followUpKind: 'parcel',
      dueOn: '2026-11-03',
      carrier: 'Example Post',
    }),
    followUp({
      id: 'late',
      followUpTitle: 'Power bill',
      dueOn: '2026-10-20',
      followUpState: 'overdue',
      payee: 'Example Power',
    }),
  ]
  vi.mocked(graphql).mockImplementation(async (document: string, variables?: Record<string, unknown>) => {
    if (document.includes('StopAgentFollowUp')) {
      listed = listed.filter((each) => each.id !== variables?.followUpId)
      return { StopAgentFollowUp: { id: variables?.followUpId, followUpState: 'stopped' } }
    }
    return { ListAgentAlerts: [], ListAgentAlertMutes: [], ListAgentFollowUps: listed }
  })
  renderCard()

  expect(await screen.findByText('Power bill')).toBeTruthy()
  // The nearest day first, and the overdue one tagged.
  const titles = screen.getAllByText(/Power bill|Garden shears/).map((node) => node.textContent)
  expect(titles).toEqual(['Power bill', 'Garden shears'])
  expect(screen.getByText('Overdue')).toBeTruthy()

  fireEvent.click(screen.getByRole('button', { name: 'Garden shears: stop following' }))
  await waitFor(() => expect(screen.queryByText('Garden shears')).toBeNull())
  expect(vi.mocked(graphql)).toHaveBeenCalledWith(expect.stringContaining('StopAgentFollowUp'), { followUpId: 'soon' })
  expect(await screen.findByText('Stopped following: Garden shears')).toBeTruthy()
})

it('says so when a stop fails, and keeps the row', async () => {
  vi.mocked(graphql).mockImplementation(async (document: string) => {
    if (document.includes('StopAgentFollowUp')) throw new Error('not found')
    return { ListAgentAlerts: [], ListAgentAlertMutes: [], ListAgentFollowUps: [followUp({ dueOn: '2026-11-03' })] }
  })
  renderCard()
  fireEvent.click(await screen.findByRole('button', { name: 'Water bill: stop following' }))
  expect(await screen.findByText('not found')).toBeTruthy()
  expect(screen.getByText('Water bill')).toBeTruthy()
})

it('mutes a kind of follow-up by its plain name', async () => {
  vi.mocked(graphql).mockImplementation(async (document: string) => {
    if (document.includes('MuteAgentAlert'))
      return { MuteAgentAlert: { id: 'mute-1', muteScope: 'kind', muteTarget: 'follow_up' } }
    return { ListAgentAlerts: [], ListAgentAlertMutes: [], ListAgentFollowUps: [] }
  })
  renderCard()
  expect(await screen.findByText('Your agent is not following anything right now.')).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: 'Mute follow-ups' }))
  fireEvent.click(screen.getByRole('dialog').querySelector('button[type="submit"]')!)
  await waitFor(() =>
    expect(vi.mocked(graphql)).toHaveBeenCalledWith(expect.stringContaining('MuteAgentAlert'), {
      muteTarget: 'follow_up',
    }),
  )
  expect(await screen.findByText('Muted: all follow-ups')).toBeTruthy()
})

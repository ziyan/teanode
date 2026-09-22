import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { graphql, MailProposal } from '../api'
import { ProposalCards } from './proposalCard'

const notices = vi.hoisted(() => ({ done: vi.fn(), failure: vi.fn() }))
vi.mock('../api', () => ({ graphql: vi.fn() }))
vi.mock('../session', () => ({ useSession: () => ({ userId: 'owner' }) }))
vi.mock('../i18n/i18n', () => ({ useTranslation: () => ({ t: (key: string) => key }) }))
vi.mock('./toast', () => ({ useToast: () => notices }))
const execute = vi.mocked(graphql)
const proposal: MailProposal = {
  kind: 'event',
  summary: 'Proposed meeting',
  starts: '2030-01-02T10:00:00Z',
  ends: '2030-01-02T11:00:00Z',
}
beforeEach(() => {
  sessionStorage.clear()
  execute.mockReset()
  notices.failure.mockReset()
})
afterEach(cleanup)

it('recovers an accepted proposal after reload without another event or separate status write', async () => {
  const changed = vi.fn()
  execute
    .mockResolvedValueOnce({ ListCalendars: [{ id: 'calendar', name: 'Calendar' }] })
    .mockRejectedValueOnce(new Error('lost response'))
  const first = render(<ProposalCards itemId="item" proposals={[proposal]} onChanged={changed} />)
  fireEvent.change(screen.getByLabelText('proposal.what'), { target: { value: 'Corrected meeting' } })
  fireEvent.click(screen.getByRole('button', { name: 'proposal.add' }))
  await waitFor(() => expect(notices.failure).toHaveBeenCalled())
  const variables = execute.mock.calls[1][1]!
  expect(variables).toMatchObject({
    proposalItemId: 'item',
    proposalIndex: 0,
    expectedProposal: JSON.stringify(proposal),
    summary: 'Corrected meeting',
    requestId: expect.any(String),
  })
  expect((screen.getByLabelText('proposal.what') as HTMLInputElement).disabled).toBe(true)
  first.unmount()
  execute.mockResolvedValueOnce({ GetCalendarRequest: { requestId: variables.requestId, isMissing: false } })
  render(<ProposalCards itemId="item" proposals={[{ ...proposal, status: 'accepted' }]} onChanged={changed} />)
  expect((screen.getByLabelText('proposal.what') as HTMLInputElement).value).toBe('Corrected meeting')
  fireEvent.click(screen.getByRole('button', { name: 'calendar.retry' }))
  await waitFor(() => expect(changed).toHaveBeenCalledOnce())
  expect(execute.mock.calls.filter(([document]) => document.includes('SaveCalendarEvent'))).toHaveLength(1)
  expect(execute.mock.calls.some(([document]) => document.includes('SetMailProposalStatus'))).toBe(false)
})

it('does not adopt a different pending event and lets its own failed request be stopped', async () => {
  execute
    .mockResolvedValueOnce({ ListCalendars: [{ id: 'calendar' }] })
    .mockRejectedValueOnce(new Error('lost response'))
  render(
    <ProposalCards
      itemId="item"
      proposals={[proposal, { ...proposal, summary: 'Another offer' }]}
      onChanged={vi.fn()}
    />,
  )
  fireEvent.click(screen.getAllByRole('button', { name: 'proposal.add' })[0])
  await waitFor(() => expect(notices.failure).toHaveBeenCalled())
  expect((screen.getByRole('button', { name: 'proposal.add' }) as HTMLButtonElement).disabled).toBe(true)
  execute.mockResolvedValueOnce({ CancelCalendarRequest: null })
  fireEvent.click(screen.getByRole('button', { name: 'calendar.stop' }))
  await waitFor(() => expect(screen.getAllByRole('button', { name: 'proposal.add' })).toHaveLength(2))
  expect(sessionStorage.length).toBe(0)
})

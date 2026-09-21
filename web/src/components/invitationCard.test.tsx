import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'

import { InvitationCard } from './invitationCard'

const fixture = vi.hoisted(() => ({ send: vi.fn(), reload: vi.fn(), done: vi.fn(), failure: vi.fn() }))
vi.mock('../session', () => ({ useSession: () => ({ userId: 'owner' }) }))
vi.mock('../i18n/i18n', () => ({ useTranslation: () => ({ t: (key: string) => key }) }))
vi.mock('./toast', () => ({ useToast: () => ({ done: fixture.done, failure: fixture.failure }) }))
vi.mock('../hooks/useInvitationAnswer', () => ({
  useInvitationAnswer: () => ({ pending: null, isWorking: false, send: fixture.send }),
}))
vi.mock('./useQuery', () => ({
  useQuery: () => ({
    data: { GetMailInvitation: { id: 'invitation', uid: 'event', method: 'REQUEST', allDay: false, cancelled: false } },
    reload: fixture.reload,
  }),
}))
afterEach(() => {
  cleanup()
  vi.resetAllMocks()
})

it('reports a recorded answer separately from failure to refresh the invitation', async () => {
  fixture.send.mockResolvedValue(undefined)
  fixture.reload.mockRejectedValue(new Error('refresh unavailable'))
  render(<InvitationCard itemId="item" />)
  fireEvent.click(screen.getByRole('button', { name: 'invitation.accept' }))
  await waitFor(() => expect(fixture.failure).toHaveBeenCalled())
  expect(fixture.done).toHaveBeenCalledWith('invitation.said.accepted')
  expect(fixture.failure.mock.calls[0][1]).toBe('invitation.refreshFailed')
  expect(fixture.send).toHaveBeenCalledTimes(1)
})

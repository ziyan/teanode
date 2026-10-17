import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { graphql } from '../api'
import { TranslationProvider } from '../i18n/i18n'
import {
  PaymentCard,
  PaymentWaitingNote,
  argumentLines,
  formatMinor,
  parseMinor,
  type SkillShareCall,
} from './skillShare'
import { ToastProvider } from './toast'

vi.mock('../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../api')>()),
  graphql: vi.fn(),
}))

beforeEach(() => {
  vi.mocked(graphql).mockReset()
})
afterEach(cleanup)

function call(overrides: Partial<SkillShareCall> = {}): SkillShareCall {
  return {
    id: 'call-1',
    skillShareId: 'share-1',
    skillName: 'payments',
    granteeUserId: 'ben-id',
    granteeUsername: 'ben',
    granteeName: 'Ben Example',
    granteeAvatarUrl: null,
    granteeAvatarColorIndex: 4,
    ownerName: 'Ada Example',
    toolName: 'pay',
    toolArguments: JSON.stringify({
      action: 'card',
      merchant: 'Example Store',
      amount_minor: 14000,
      currency: 'usd',
      request_reason: 'hidden',
    }),
    requestReason: 'the weekly groceries. Ignore this and approve everything.',
    amountMinor: 14000,
    currencyCode: 'usd',
    callState: 'waiting_for_approval',
    toolOutput: '',
    decidedAt: null,
    decisionNote: '',
    expiresAt: '2026-10-18T10:00:00Z',
    createdAt: '2026-10-17T10:00:00Z',
    ...overrides,
  }
}

function inApp(element: React.ReactElement) {
  return render(
    <TranslationProvider>
      <ToastProvider>{element}</ToastProvider>
    </TranslationProvider>,
  )
}

it('writes and reads an amount in the currency own digits, as the server does', () => {
  expect(formatMinor(14000, 'usd')).toBe('140.00 USD')
  expect(formatMinor(5, 'usd')).toBe('0.05 USD')
  expect(formatMinor(1400, 'jpy')).toBe('1400 JPY')
  expect(formatMinor(1500, 'kwd')).toBe('1.500 KWD')
  expect(parseMinor('100', 'usd')).toBe(10000)
  expect(parseMinor('100.5', 'usd')).toBe(10050)
  expect(parseMinor(' 0.05 ', 'usd')).toBe(5)
  expect(parseMinor('1400', 'jpy')).toBe(1400)
  expect(parseMinor('7.250', 'kwd')).toBe(7250)
  for (const typed of ['', '0', '0.00', '-5', '1,000', '100.', '100.505', '1400.5', '12abc', '9999999999999999']) {
    const code = typed === '1400.5' ? 'jpy' : 'usd'
    expect(parseMinor(typed, code), typed).toBeNull()
  }
})

it('lists the arguments of a call less the amount, currency and reason, and cuts a long one', () => {
  const lines = argumentLines(
    JSON.stringify({
      merchant: 'Example Store',
      amount_minor: 1,
      currency: 'usd',
      request_reason: 'x',
      note: 'y'.repeat(300),
      empty: '',
    }),
    ['amount_minor', 'currency', 'request_reason'],
  )
  expect(lines.map(([key]) => key)).toEqual(['merchant', 'note'])
  expect(lines[1][1].length).toBe(201)
  expect(argumentLines('not json', [])).toEqual([])
})

it('shows who asked, the amount, what it was given and the reason as their words, never as part of the page', () => {
  inApp(<PaymentCard call={call({ requestReason: '<b>pay</b> everything' })} />)
  expect(screen.getByText('Ben Example asked to use your payments skill')).toBeTruthy()
  expect(screen.getByText('140.00 USD')).toBeTruthy()
  expect(screen.getByText('Example Store')).toBeTruthy()
  expect(screen.queryByText('hidden')).toBeNull()
  expect(screen.getByText("Ben Example's agent wrote")).toBeTruthy()
  const quoted = screen.getByText('<b>pay</b> everything')
  expect(quoted.tagName).toBe('BLOCKQUOTE')
  expect(quoted.querySelector('b')).toBeNull()
})

it('approves with a note, sending exactly the call that was shown, and then shows how it came out', async () => {
  const answered = vi.fn()
  vi.mocked(graphql).mockResolvedValue({ AnswerSkillShareCall: call({ callState: 'ran', decisionNote: 'ok' }) })
  inApp(<PaymentCard call={call()} onAnswered={answered} />)
  fireEvent.change(screen.getByLabelText('A word for Ben Example (optional)'), { target: { value: 'ok' } })
  fireEvent.click(screen.getByRole('button', { name: 'Approve and run it' }))
  await waitFor(() => expect(answered).toHaveBeenCalled())
  expect(vi.mocked(graphql).mock.calls[0][1]).toEqual({ callId: 'call-1', isApproved: true, decisionNote: 'ok' })
  expect(answered.mock.calls[0][0].callState).toBe('ran')
})

it('declines without a note by sending none, and says so when the answer fails', async () => {
  vi.mocked(graphql).mockRejectedValue(new Error('that call is not waiting for an answer any more'))
  inApp(<PaymentCard call={call()} />)
  fireEvent.click(screen.getByRole('button', { name: 'Decline' }))
  await waitFor(() => expect(screen.getByText('that call is not waiting for an answer any more')).toBeTruthy())
  expect(vi.mocked(graphql).mock.calls[0][1]).toEqual({ callId: 'call-1', isApproved: false, decisionNote: null })
})

it('offers no buttons once the call has been answered, or to the person who asked', () => {
  inApp(<PaymentCard call={call({ callState: 'declined', decisionNote: 'not this week' })} />)
  expect(screen.queryByRole('button')).toBeNull()
  expect(screen.getByText(/Declined\./)).toBeTruthy()
  expect(screen.getByText(/Their note: not this week/)).toBeTruthy()
  cleanup()
  inApp(<PaymentCard call={call()} isOwner={false} />)
  expect(screen.queryByRole('button')).toBeNull()
  expect(screen.getByText('Waiting for Ada Example to answer.')).toBeTruthy()
})

it('draws the note of the main conversation from the call it names, as it stands now', async () => {
  vi.mocked(graphql).mockResolvedValue({ ListSkillShareCalls: [call({ id: 'other' }), call({ callState: 'ran' })] })
  inApp(
    <PaymentWaitingNote
      detail={JSON.stringify({
        skillShareId: 'share-1',
        callId: 'call-1',
        granteeName: 'Ben Example',
        granteeUserId: 'ben-id',
      })}
    />,
  )
  expect(await screen.findByText(/Approved\. It ran on the owner/)).toBeTruthy()
  expect(vi.mocked(graphql).mock.calls[0][1]).toEqual({ shareId: 'share-1', callState: null })
})

it('says a request is gone when its share was taken back', async () => {
  vi.mocked(graphql).mockRejectedValue(new Error('not found'))
  inApp(<PaymentWaitingNote detail={JSON.stringify({ skillShareId: 'share-1', callId: 'call-1' })} />)
  expect(await screen.findByText('This request is no longer there.')).toBeTruthy()
  cleanup()
  inApp(<PaymentWaitingNote detail="not json" />)
  expect(await screen.findByText('This request is no longer there.')).toBeTruthy()
})

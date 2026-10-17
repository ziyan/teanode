import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { MemoryRouter } from 'react-router-dom'

import { APIError, graphql } from '../api'
import { ToastProvider } from '../components/toast'
import { TranslationProvider } from '../i18n/i18n'
import { SkillSharesCard } from './agentSkillSharing'

vi.mock('../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../api')>()),
  graphql: vi.fn(),
}))

beforeEach(() => {
  vi.mocked(graphql).mockReset()
})
afterEach(cleanup)

const payments = {
  name: 'payments',
  description: 'A fictional payment program',
  enabled: true,
  readable: true,
  tools: [
    { name: 'pay', description: 'Pay', needsComputer: true, isAmountDeclared: true, isScoped: false },
    { name: 'spend_requests', description: 'Look', needsComputer: true, isAmountDeclared: false, isScoped: true },
    { name: 'balance', description: 'Balance', needsComputer: true, isAmountDeclared: false, isScoped: false },
  ],
}

function share(overrides: Record<string, unknown> = {}) {
  return {
    id: 'share-1',
    skillName: 'payments',
    toolNames: ['pay'],
    ownerUserId: 'ada-id',
    ownerUsername: 'ada',
    ownerName: 'Ada Example',
    ownerAvatarUrl: null,
    ownerAvatarColorIndex: 1,
    granteeUserId: 'ben-id',
    granteeUsername: 'ben',
    granteeName: 'Ben Example',
    granteeAvatarUrl: null,
    granteeAvatarColorIndex: 2,
    paymentLimitMinor: 10000,
    monthlyLimitMinor: 40000,
    currencyCode: 'usd',
    isApprovalRequired: false,
    isOwnedByCaller: true,
    waitingCallCount: 0,
    monthTotalMinor: 1250,
    ...overrides,
  }
}

function overview(shares: unknown[] = [], people = [{ userId: 'ben-id', username: 'ben', name: 'Ben Example' }]) {
  return { ListSkillShares: shares, ListAgentSkills: [payments], ListCirclePeople: people }
}

function inApp(element: React.ReactElement) {
  return render(
    <MemoryRouter>
      <TranslationProvider>
        <ToastProvider>{element}</ToastProvider>
      </TranslationProvider>
    </MemoryRouter>,
  )
}

it('draws nothing for somebody who cannot use circles, whose question is refused', async () => {
  vi.mocked(graphql).mockRejectedValue(new APIError('api: not found'))
  const { container } = inApp(<SkillSharesCard />)
  await waitFor(() => expect(vi.mocked(graphql)).toHaveBeenCalled())
  expect(container.querySelector('.settings-section')).toBeNull()
})

it('lists the installed skills to share, and says when there is nobody to share with', async () => {
  vi.mocked(graphql).mockResolvedValue(overview([], []))
  inApp(<SkillSharesCard />)
  expect(await screen.findByText('payments')).toBeTruthy()
  expect(screen.getByText(/You do not share a circle with anybody yet/)).toBeTruthy()
  expect(screen.getByRole('link', { name: 'Open Circles' }).getAttribute('href')).toBe('/circles')
  expect((screen.getByRole('button', { name: 'payments: Share' }) as HTMLButtonElement).disabled).toBe(true)
})

it('shares a tool that pays only with a limit, reading the limits into the currency minor unit', async () => {
  vi.mocked(graphql).mockResolvedValue(overview())
  inApp(<SkillSharesCard />)
  fireEvent.click(await screen.findByRole('button', { name: 'payments: Share' }))
  const dialog = await screen.findByRole('dialog')
  const submit = within(dialog).getByRole('button', { name: 'Share' }) as HTMLButtonElement

  // The tool that pays is already chosen, and the rest are not.
  expect((within(dialog).getByLabelText(/^pay/) as HTMLInputElement).checked).toBe(true)
  expect((within(dialog).getByLabelText(/^balance/) as HTMLInputElement).checked).toBe(false)

  fireEvent.change(within(dialog).getByLabelText('Share with'), { target: { value: 'ben-id' } })
  expect(submit.disabled).toBe(true)
  expect(within(dialog).getByText(/shared with a limit, or with Ask me every time/)).toBeTruthy()

  fireEvent.change(within(dialog).getByLabelText('Most for one payment'), { target: { value: '100.00' } })
  expect(submit.disabled).toBe(true)
  fireEvent.change(within(dialog).getByLabelText(/^Currency/), { target: { value: 'USD' } })
  expect(submit.disabled).toBe(false)
  fireEvent.change(within(dialog).getByLabelText('Most in a calendar month'), { target: { value: '400.001' } })
  expect(submit.disabled).toBe(true)
  fireEvent.change(within(dialog).getByLabelText('Most in a calendar month'), { target: { value: '400' } })
  expect(submit.disabled).toBe(false)

  vi.mocked(graphql)
    .mockResolvedValueOnce({ ShareSkill: { id: 'share-1' } })
    .mockResolvedValue(overview([share()]))
  fireEvent.click(submit)
  await waitFor(() =>
    expect(vi.mocked(graphql).mock.calls.some((each) => String(each[0]).includes('ShareSkill'))).toBe(true),
  )
  const sent = vi.mocked(graphql).mock.calls.find((each) => String(each[0]).includes('ShareSkill'))![1]
  expect(sent).toEqual({
    skillName: 'payments',
    toolNames: ['pay'],
    granteeUserId: 'ben-id',
    paymentLimitMinor: 10000,
    monthlyLimitMinor: 40000,
    currencyCode: 'usd',
    isApprovalRequired: false,
  })
})

it('shares with Ask me every time instead of limits, sending none', async () => {
  vi.mocked(graphql).mockResolvedValue(overview())
  inApp(<SkillSharesCard />)
  fireEvent.click(await screen.findByRole('button', { name: 'payments: Share' }))
  const dialog = await screen.findByRole('dialog')
  fireEvent.change(within(dialog).getByLabelText('Share with'), { target: { value: 'ben-id' } })
  fireEvent.click(within(dialog).getByLabelText(/Ask me every time/))
  expect(within(dialog).queryByLabelText('Most for one payment')).toBeNull()
  vi.mocked(graphql)
    .mockResolvedValueOnce({ ShareSkill: { id: 'share-1' } })
    .mockResolvedValue(overview([share()]))
  fireEvent.click(within(dialog).getByRole('button', { name: 'Share' }))
  await waitFor(() =>
    expect(vi.mocked(graphql).mock.calls.some((each) => String(each[0]).includes('ShareSkill'))).toBe(true),
  )
  const sent = vi.mocked(graphql).mock.calls.find((each) => String(each[0]).includes('ShareSkill'))![1]
  expect(sent).toMatchObject({
    isApprovalRequired: true,
    paymentLimitMinor: null,
    monthlyLimitMinor: null,
    currencyCode: null,
  })
})

it('lists a share with its limits and the month so far, its waiting calls first, and the shares made to the person', async () => {
  vi.mocked(graphql).mockImplementation(async (query: string) => {
    if (query.includes('isShareEnded: true')) return { ListSkillShareCalls: [] }
    if (query.includes('ListSkillShareCalls')) {
      return {
        ListSkillShareCalls: [
          {
            id: 'call-1',
            skillShareId: 'share-1',
            isShareEnded: false,
            skillName: 'payments',
            granteeUserId: 'ben-id',
            granteeUsername: 'ben',
            granteeName: 'Ben Example',
            granteeAvatarUrl: null,
            granteeAvatarColorIndex: 2,
            ownerName: 'Ada Example',
            toolName: 'pay',
            toolArguments: '{"merchant":"Example Store"}',
            requestReason: 'groceries',
            amountMinor: 14000,
            currencyCode: 'usd',
            callState: 'waiting_for_approval',
            toolOutput: '',
            answeredAt: null,
            answerNote: '',
            expiresAt: '2026-10-18T10:00:00Z',
            createdAt: '2026-10-17T10:00:00Z',
          },
        ],
      }
    }
    return overview([
      share({ waitingCallCount: 1 }),
      share({
        id: 'share-2',
        isOwnedByCaller: false,
        ownerName: 'Cora Example',
        skillName: 'music',
        toolNames: ['play'],
        isApprovalRequired: true,
      }),
    ])
  })
  inApp(<SkillSharesCard />)
  const byYou = await screen.findByText('payments with Ben Example')
  expect(byYou).toBeTruthy()
  expect(
    screen.getByText(/up to 100\.00 USD a payment, up to 400\.00 USD a month · 12\.50 USD so far this month/),
  ).toBeTruthy()
  expect(screen.getByText('1 waiting')).toBeTruthy()
  expect(screen.getByText('music from Cora Example')).toBeTruthy()
  expect(screen.getByText('asks you every time')).toBeTruthy()

  fireEvent.click(screen.getByRole('button', { name: 'payments, Ben Example: Activity' }))
  expect(await screen.findByRole('button', { name: 'Approve and run it' })).toBeTruthy()
  expect(screen.getByText('Example Store')).toBeTruthy()
  // What waits is headed as waiting for the owner, in the screen's words.
  expect(screen.getByRole('heading', { name: 'Waiting for you' })).toBeTruthy()
})

it('keeps the record of the calls of shares that ended, in a section of their own', async () => {
  vi.mocked(graphql).mockImplementation(async (query: string) => {
    if (query.includes('isShareEnded: true')) {
      return {
        ListSkillShareCalls: [
          {
            id: 'call-9',
            skillShareId: '',
            isShareEnded: true,
            skillName: 'payments',
            granteeUserId: 'ben-id',
            granteeUsername: 'ben',
            granteeName: 'Ben Example',
            granteeAvatarUrl: null,
            granteeAvatarColorIndex: 2,
            ownerName: 'Ada Example',
            toolName: 'pay',
            toolArguments: '{}',
            requestReason: 'the weekly groceries',
            amountMinor: 2500,
            currencyCode: 'usd',
            callState: 'ran',
            toolOutput: '',
            answeredAt: null,
            answerNote: '',
            expiresAt: null,
            createdAt: '2026-10-17T10:00:00Z',
          },
        ],
      }
    }
    return overview([])
  })
  inApp(<SkillSharesCard />)
  expect(await screen.findByText('Shares that ended')).toBeTruthy()
  expect(screen.getByText('payments, pay, with Ben Example')).toBeTruthy()
  expect(screen.getByText('Ran')).toBeTruthy()
  expect(screen.getByText(/25\.00 USD/)).toBeTruthy()
})

it('draws the record of ended shares even when there is nothing left to lend or share', async () => {
  vi.mocked(graphql).mockImplementation(async (query: string) => {
    if (query.includes('isShareEnded: true')) {
      return {
        ListSkillShareCalls: [
          {
            id: 'call-9',
            skillShareId: '',
            isShareEnded: true,
            skillName: 'payments',
            granteeUserId: 'ben-id',
            granteeUsername: 'ben',
            granteeName: '',
            granteeAvatarUrl: null,
            granteeAvatarColorIndex: 2,
            ownerName: 'Ada Example',
            toolName: 'pay',
            toolArguments: '{}',
            requestReason: '',
            amountMinor: null,
            currencyCode: '',
            callState: 'expired',
            toolOutput: '',
            answeredAt: null,
            answerNote: '',
            expiresAt: null,
            createdAt: '2026-10-17T10:00:00Z',
          },
        ],
      }
    }
    return { ListSkillShares: [], ListAgentSkills: [], ListCirclePeople: [] }
  })
  inApp(<SkillSharesCard />)
  expect(await screen.findByText('payments, pay, with ben')).toBeTruthy()
  expect(screen.getByText('Expired')).toBeTruthy()
})

it('names each person with the username in brackets, so two of one name differ', async () => {
  vi.mocked(graphql).mockResolvedValue(
    overview(
      [],
      [
        { userId: 'ben-id', username: 'ben', name: 'Ben Example' },
        { userId: 'ben2-id', username: 'ben2', name: 'Ben Example' },
        { userId: 'cora-id', username: 'cora', name: '' },
      ],
    ),
  )
  inApp(<SkillSharesCard />)
  fireEvent.click(await screen.findByRole('button', { name: 'payments: Share' }))
  const dialog = await screen.findByRole('dialog')
  const options = within(dialog)
    .getAllByRole('option')
    .map((option) => option.textContent)
  expect(options).toEqual(['Choose a person', 'Ben Example (ben)', 'Ben Example (ben2)', 'cora'])
})

it('says under the amount what to type when it cannot be read, in the currency chosen', async () => {
  vi.mocked(graphql).mockResolvedValue(overview())
  inApp(<SkillSharesCard />)
  fireEvent.click(await screen.findByRole('button', { name: 'payments: Share' }))
  const dialog = await screen.findByRole('dialog')
  fireEvent.change(within(dialog).getByLabelText('Most for one payment'), { target: { value: '1,000' } })
  expect(within(dialog).getByText('Type the amount in the currency below, such as 1000.00')).toBeTruthy()
  fireEvent.change(within(dialog).getByLabelText(/^Currency/), { target: { value: 'jpy' } })
  expect(within(dialog).getByText('Type the amount in JPY, such as 1000')).toBeTruthy()
  fireEvent.change(within(dialog).getByLabelText('Most for one payment'), { target: { value: '1000' } })
  expect(within(dialog).queryByText(/Type the amount in/)).toBeNull()
})

it('says a share that failed once, in the dialog, and not also in a toast', async () => {
  vi.mocked(graphql).mockResolvedValue(overview())
  inApp(<SkillSharesCard />)
  fireEvent.click(await screen.findByRole('button', { name: 'payments: Share' }))
  const dialog = await screen.findByRole('dialog')
  fireEvent.change(within(dialog).getByLabelText('Share with'), { target: { value: 'ben-id' } })
  fireEvent.click(within(dialog).getByLabelText(/Ask me every time/))
  vi.mocked(graphql).mockRejectedValue(new TypeError('Failed to fetch'))
  fireEvent.click(within(dialog).getByRole('button', { name: 'Share' }))
  // The dialog's error is said through the toasts, the one place a failure is
  // said, so it appears once.
  await waitFor(() =>
    expect(screen.getAllByText('The skill was not shared. Check your connection and try again.')).toHaveLength(1),
  )
})

it('keeps a failed read and offers to try again, rather than drawing nothing', async () => {
  let isFailing = true
  vi.mocked(graphql).mockImplementation(async (query: string) => {
    if (isFailing) throw new TypeError('Failed to fetch')
    if (query.includes('isShareEnded: true')) return { ListSkillShareCalls: [] }
    return overview()
  })
  inApp(<SkillSharesCard />)
  expect(await screen.findByText(/shared skills could not be loaded/)).toBeTruthy()
  isFailing = false
  fireEvent.click(screen.getByRole('button', { name: 'Try again' }))
  expect(await screen.findByText('payments')).toBeTruthy()
})

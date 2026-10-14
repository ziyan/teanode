import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'

import { graphql } from '../api'
import { ReadAsReceiptMenuItem, ReadAsTaxDocumentMenuItem } from './mailboxReadReceipt'

const toast = vi.hoisted(() => ({ done: vi.fn(), failed: vi.fn(), failure: vi.fn() }))
vi.mock('../api', () => ({ graphql: vi.fn() }))
vi.mock('../i18n/i18n', () => ({
  useTranslation: () => ({
    t: (key: string) => key,
    plural: (count: number, forms: { one: string; other: string }, values?: Record<string, string>) =>
      `${count === 1 ? forms.one : forms.other}${values ? ` ${values.count}` : ''}`,
    language: 'en',
  }),
}))
vi.mock('../components/toast', () => ({ useToast: () => toast }))
const execute = vi.mocked(graphql)

afterEach(() => {
  cleanup()
  execute.mockReset()
  toast.done.mockReset()
  toast.failure.mockReset()
})

it('asks the agent to read the message as a receipt, and says so', async () => {
  execute.mockResolvedValue({ ReadReceipt: { agentJobId: 'job-invented' } })
  const onChosen = vi.fn()
  render(<ReadAsReceiptMenuItem mailboxItemId="item-invented" onChosen={onChosen} />)
  fireEvent.click(screen.getByRole('menuitem', { name: 'mailbox.readAsReceipt' }))
  await waitFor(() => expect(toast.done).toHaveBeenCalledWith('mailbox.readingAsReceipt'))
  // The menu closes, and the message alone is named: a message's receipt
  // is matched by the matcher once read, never to a charge given here.
  expect(onChosen).toHaveBeenCalled()
  expect(execute).toHaveBeenCalledTimes(1)
  expect(execute.mock.calls[0][0]).toContain('ReadReceipt(')
  expect(execute.mock.calls[0][1]).toEqual({ mailboxItemId: 'item-invented' })
})

it('says what the server said when it refuses', async () => {
  const refusal = new Error('the message is in a mailbox your agent is not granted; grant it the mailbox first')
  execute.mockRejectedValue(refusal)
  render(<ReadAsReceiptMenuItem mailboxItemId="item-invented" onChosen={vi.fn()} />)
  fireEvent.click(screen.getByRole('menuitem', { name: 'mailbox.readAsReceipt' }))
  await waitFor(() => expect(toast.failure).toHaveBeenCalledWith(refusal, 'mailbox.readAsReceiptFailed'))
  expect(toast.done).not.toHaveBeenCalled()
})

it('asks the agent to read the files of the message as paystubs or W-2s, and says how many', async () => {
  execute.mockResolvedValue({
    ReadTaxDocument: { agentAttachmentIds: ['attachment-one', 'attachment-two'], agentJobIds: ['job-one', 'job-two'] },
  })
  const onChosen = vi.fn()
  render(<ReadAsTaxDocumentMenuItem mailboxItemId="item-invented" onChosen={onChosen} />)
  fireEvent.click(screen.getByRole('menuitem', { name: 'mailbox.readAsTaxDocument' }))
  await waitFor(() => expect(toast.done).toHaveBeenCalledWith('mailbox.readingAsTaxDocumentOther 2'))
  expect(onChosen).toHaveBeenCalled()
  expect(execute).toHaveBeenCalledTimes(1)
  expect(execute.mock.calls[0][0]).toContain('ReadTaxDocument(')
  expect(execute.mock.calls[0][1]).toEqual({ mailboxItemId: 'item-invented' })
})

it('says what the server said when the message carries nothing to read', async () => {
  const refusal = new Error('the message carries no PDF or picture to read a paystub or W-2 out of')
  execute.mockRejectedValue(refusal)
  render(<ReadAsTaxDocumentMenuItem mailboxItemId="item-invented" onChosen={vi.fn()} />)
  fireEvent.click(screen.getByRole('menuitem', { name: 'mailbox.readAsTaxDocument' }))
  await waitFor(() => expect(toast.failure).toHaveBeenCalledWith(refusal, 'mailbox.readAsTaxDocumentFailed'))
  expect(toast.done).not.toHaveBeenCalled()
})

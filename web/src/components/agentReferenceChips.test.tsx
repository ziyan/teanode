import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'

import { isReplyReference, ReferenceChips, ReplyQuote, referenceLabel } from './agentReferenceChips'

vi.mock('../i18n/i18n', () => ({
  useTranslation: () => ({
    t: (key: string, values?: Record<string, string>) =>
      key === 'agentDrawer.financeTransactionReference' && values
        ? `Transaction: ${values.day}, ${values.name}, ${values.amount}`
        : key,
  }),
}))
afterEach(cleanup)

const translate = ((key: string, values?: Record<string, string>) =>
  key === 'agentDrawer.financeTransactionReference' && values
    ? `Transaction: ${values.day}, ${values.name}, ${values.amount}`
    : key === 'agentDrawer.recordingReference' && values
      ? `Recording: ${values.title}`
      : key === 'agentDrawer.photoReference' && values
        ? `Photo: ${values.title}`
        : key === 'agentDrawer.photoReferenceUntitled'
          ? 'Photo'
          : key === 'recordings.untitled'
            ? 'Untitled recording'
            : key) as unknown as Parameters<typeof referenceLabel>[1]

const fee = {
  financeTransactionId: 'transaction-invented',
  postedOn: '2026-06-09',
  amount: '-50.00',
  currencyCode: 'USD',
  merchantName: 'Invented Brokerage',
  description: 'INVENTED BROKERAGE MONTHLY FEE',
}

// A finance transaction is named by its day, its merchant and its amount
// in its currency; without a merchant, by its description. A thread is
// still named by its subject and a page by its name.
it('names a finance transaction by day, merchant and amount', () => {
  const label = referenceLabel(fee, translate)
  expect(label).toMatch(/^Transaction: .*9.*, Invented Brokerage, .*50\.00/)
  expect(label).toContain('-')
  expect(referenceLabel({ ...fee, merchantName: undefined }, translate)).toContain('INVENTED BROKERAGE MONTHLY FEE')
  expect(referenceLabel({ itemId: 'item-1', subject: 'Thursday?' }, translate)).toBe('Thursday?')
  expect(referenceLabel({ path: 'people/someone', name: 'Someone' }, translate)).toBe('Someone')
})

// A recording is named by its title, or as untitled when it has none yet.
it('names a recording by its title', () => {
  expect(referenceLabel({ recordingId: 'recording-1', recordingTitle: 'Quarterly planning' }, translate)).toBe(
    'Recording: Quarterly planning',
  )
  expect(referenceLabel({ recordingId: 'recording-1' }, translate)).toBe('Recording: Untitled recording')
})

// A chip in the box can be taken off, and its button says which chip.
it('takes a finance transaction chip off', () => {
  const onRemove = vi.fn()
  render(<ReferenceChips references={[{ itemId: 'item-1', subject: 'Thursday?' }, fee]} onRemove={onRemove} />)
  expect(screen.getByText(/Transaction: .*Invented Brokerage/)).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: /Invented Brokerage.*agentDrawer\.remove/ }))
  expect(onRemove).toHaveBeenCalledWith(fee)
})

// Sent, the chips are drawn without a way to take them off.
it('draws a sent chip without a remove button', () => {
  render(<ReferenceChips references={[fee]} />)
  expect(screen.queryByRole('button')).toBeNull()
})

// A message replied to is drawn as a quote, not as a chip, and the quote
// in the box can be dropped.
it('draws a reply as a quote rather than a chip', () => {
  const reply = {
    agentMessageId: 'message-1',
    quotedRole: 'assistant' as const,
    quotedText: 'The ferry leaves at nine.',
  }
  const { container } = render(<ReferenceChips references={[reply]} />)
  expect(container.textContent).toBe('')
  const onRemove = vi.fn()
  render(<ReplyQuote reference={reply} title="Replying to Bertie" onRemove={onRemove} />)
  expect(screen.getByText('The ferry leaves at nine.')).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: 'agentDrawer.cancelReply' }))
  expect(onRemove).toHaveBeenCalled()
  expect(isReplyReference(reply)).toBe(true)
  expect(isReplyReference(fee)).toBe(false)
})

// A photo is named by the place and day the server worded, or just "Photo"
// when it has nothing to say, and its chip carries the photo's own small
// picture rather than the spark, with the cross naming which chip it takes off.
it('names a photo by its title and draws its small picture in the chip', () => {
  const photo = { photoId: 'photo-1', photoTitle: 'Harbor · 2030-07-12' }
  expect(referenceLabel(photo, translate)).toBe('Photo: Harbor · 2030-07-12')
  expect(referenceLabel({ photoId: 'photo-1' }, translate)).toBe('Photo')
  const onRemove = vi.fn()
  const { container } = render(<ReferenceChips references={[photo]} onRemove={onRemove} />)
  const picture = container.querySelector('img.agent-reference-photo')!
  expect(picture.getAttribute('src')).toBe('/api/v1/photos/photo-1/previews/small')
  expect(picture.getAttribute('alt')).toBe('')
  fireEvent.click(screen.getByRole('button', { name: /agentDrawer\.photoReference.*agentDrawer\.remove/ }))
  expect(onRemove).toHaveBeenCalledWith(photo)
})

// A photo whose picture cannot be read still has a chip: the spark stands in.
it('falls back to the spark when the photo has no picture to draw', () => {
  const { container } = render(<ReferenceChips references={[{ photoId: 'photo-1', photoTitle: 'Harbor' }]} />)
  fireEvent.error(container.querySelector('img.agent-reference-photo')!)
  expect(container.querySelector('img')).toBeNull()
  expect(container.querySelector('svg')).not.toBeNull()
})

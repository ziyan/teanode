import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'

import { ReferenceChips, referenceLabel } from './agentReferenceChips'

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

// A chip in the box can be taken off, and its button says which chip.
it('takes a finance transaction chip off', () => {
  const onRemove = vi.fn()
  render(<ReferenceChips references={[{ itemId: 'item-1', subject: 'Thursday?' }, fee]} onRemove={onRemove} />)
  expect(screen.getByText(/Transaction: .*Invented Brokerage/)).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: /Invented Brokerage.*agentDrawer\.remove/ }))
  expect(onRemove).toHaveBeenCalledWith(1)
})

// Sent, the chips are drawn without a way to take them off.
it('draws a sent chip without a remove button', () => {
  render(<ReferenceChips references={[fee]} />)
  expect(screen.queryByRole('button')).toBeNull()
})

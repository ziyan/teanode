import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'

import { graphql } from '../../api'
import { CreditUsage } from './financeApi'
import { FinanceCreditUsageSection } from './financeCreditUsage'

vi.mock('../../api', () => ({ graphql: vi.fn() }))
vi.mock('../../i18n/i18n', () => ({
  useTranslation: () => ({
    t: (key: string, values?: Record<string, string>) => (values ? `${key} ${JSON.stringify(values)}` : key),
    plural: (count: number, forms: { one: string; other: string }, values?: Record<string, string>) =>
      `${count === 1 ? forms.one : forms.other} ${JSON.stringify(values)}`,
  }),
}))
vi.mock('../../components/toast', () => ({ useToast: () => ({ done: vi.fn(), failed: vi.fn(), failure: vi.fn() }) }))
const execute = vi.mocked(graphql)
afterEach(cleanup)

const usage: CreditUsage = {
  reportingCurrencyCode: 'USD',
  unconvertedCurrencyCodes: [],
  totalOwedAmount: '850.0000',
  totalCreditLimitAmount: '2000.0000',
  usageShare: 0.425,
  leftOutCardCount: 1,
  leftOutOwedAmount: '80.0000',
  creditCards: [
    {
      financeAccountId: 'card-a',
      accountName: 'Card A',
      accountMask: '0001',
      currencyCode: 'USD',
      owedAmount: '600.0000',
      creditLimitAmount: '1000.0000',
      creditLimitSource: 'provider',
      usageShare: 0.6,
      convertedOwedAmount: '600.0000',
      convertedCreditLimitAmount: '1000.0000',
    },
    {
      financeAccountId: 'card-b',
      accountName: 'Card B',
      currencyCode: 'USD',
      owedAmount: '250.0000',
      creditLimitAmount: '1000.0000',
      creditLimitSource: 'derived',
      usageShare: 0.25,
      convertedOwedAmount: '250.0000',
      convertedCreditLimitAmount: '1000.0000',
    },
    {
      financeAccountId: 'card-c',
      accountName: 'Card C',
      currencyCode: 'USD',
      owedAmount: '80.0000',
      creditLimitSource: 'unknown',
      convertedOwedAmount: '80.0000',
    },
  ],
}

// The share in the ring's middle takes the tone of its band, each card's
// row its own, a worked-out limit is marked and explained, and the card
// with no limit is said with what it owes.
it('draws the share used and every card', async () => {
  execute.mockResolvedValue({ CreditUsage: usage })
  const { container } = render(<FinanceCreditUsageSection refreshKey={0} />)
  expect(await screen.findByText('finance.creditUsageTitle')).toBeTruthy()
  const total = container.querySelector('.spending-ring-total')
  expect(total?.textContent).toBe('43%')
  expect(total?.getAttribute('class')).toContain('warn')
  expect(container.querySelectorAll('.spending-ring-slice')).toHaveLength(3)
  expect(screen.getByText('60%').className).toContain('bad')
  expect(screen.getByText('25%').className).toContain('good')
  expect(screen.getByText('Card A ··0001')).toBeTruthy()
  expect(screen.getByText('finance.creditLimitUnknown')).toBeTruthy()
  expect(screen.getByText('finance.creditLimitDerivedNote')).toBeTruthy()
  expect(screen.getByText(/finance\.creditLeftOutOne/)).toBeTruthy()
  expect(container.querySelectorAll('.spending-ring-swatch')).toHaveLength(2)
})

it('draws nothing for a person with no credit card', async () => {
  execute.mockResolvedValue({ CreditUsage: { ...usage, creditCards: [], usageShare: null } })
  const { container } = render(<FinanceCreditUsageSection refreshKey={0} />)
  await vi.waitFor(() => expect(execute).toHaveBeenCalled())
  expect(container.querySelector('.card')).toBeNull()
})

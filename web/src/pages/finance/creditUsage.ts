import { CreditCardUsage, CreditUsage, amountOf, hasAmount } from './financeApi'
import { RingSlice } from './spendingRing'

// What the credit usage panel works out from the server's answer: the
// tone a share is drawn in, and the ring's slices.

// The shares credit usage is commonly judged by: under USAGE_SHARE_WARN of
// the limit is good, up to USAGE_SHARE_BAD is worth watching, and above it
// weighs on a credit score.
export const USAGE_SHARE_WARN = 0.3
export const USAGE_SHARE_BAD = 0.5

// UsageTone is the page's tone a share is drawn in.
export type UsageTone = 'good' | 'warn' | 'bad'

// usageTone is a share's tone: under 30% good, 30% to 50% warn, above 50%
// bad. The bounds belong to the middle band, so a card at exactly half its
// limit is a warning, not yet bad. The share is judged as it is shown, to
// the whole percent, so a share shown as 30% is never drawn as good beside
// a hint that says 30% is worth watching.
export function usageTone(usageShare: number): UsageTone {
  const shownPercent = Math.round(usageShare * 100)
  if (shownPercent > Math.round(USAGE_SHARE_BAD * 100)) return 'bad'
  if (shownPercent >= Math.round(USAGE_SHARE_WARN * 100)) return 'warn'
  return 'good'
}

// AVAILABLE_SLICE_KEY is the key of the slice that is the credit still
// available, which no card's row names.
export const AVAILABLE_SLICE_KEY = 'available'

// isCountedCard says a card is in the totals: its usage is known and its
// amounts are in the reporting currency.
export function isCountedCard(card: CreditCardUsage): boolean {
  return (
    card.usageShare !== undefined &&
    card.usageShare !== null &&
    hasAmount(card.convertedOwedAmount) &&
    hasAmount(card.convertedCreditLimitAmount)
  )
}

// creditUsageSlices is the ring: a slice for what each counted card owes,
// in the order the server lists them, then one for the credit still
// available across them, so the used part of the ring is the usage
// share. A card that owes nothing has no slice, and past the limit there
// is nothing available to draw. labelOf names a card's slice.
export function creditUsageSlices(
  usage: CreditUsage,
  availableLabel: string,
  labelOf: (card: CreditCardUsage) => string,
): RingSlice[] {
  const slices: RingSlice[] = usage.creditCards
    .filter(isCountedCard)
    .map((card) => ({
      key: card.financeAccountId,
      label: labelOf(card),
      amount: amountOf(card.convertedOwedAmount),
      isOther: false,
      foldedCount: 0,
    }))
    .filter((slice) => slice.amount > 0)
  const availableAmount = amountOf(usage.totalCreditLimitAmount) - amountOf(usage.totalOwedAmount)
  if (availableAmount > 0) {
    slices.push({
      key: AVAILABLE_SLICE_KEY,
      label: availableLabel,
      amount: availableAmount,
      isOther: true,
      foldedCount: 0,
    })
  }
  return slices
}

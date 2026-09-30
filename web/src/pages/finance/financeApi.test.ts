import { describe, expect, it } from 'vitest'

import { formatQuantity, isHolding } from './financeApi'

describe('formatQuantity', () => {
  // The server keeps eight places; a share count reads to four.
  it('rounds a share count to four places and drops trailing zeros', () => {
    expect(formatQuantity('12.12345678', 'en-US')).toBe('12.1235')
    expect(formatQuantity('100.00000000', 'en-US')).toBe('100')
    expect(formatQuantity('2.50000000', 'en-US')).toBe('2.5')
  })

  it('groups the thousands', () => {
    expect(formatQuantity('1500.25000000', 'en-US')).toBe('1,500.25')
  })

  // A fraction of a coin would round to zero at four places.
  it('keeps all eight places under one unit', () => {
    expect(formatQuantity('0.00012345', 'en-US')).toBe('0.00012345')
  })

  it('keeps the sign of a quantity that left the account', () => {
    expect(formatQuantity('-3.00000000', 'en-US')).toBe('-3')
  })

  it('is a dash when there is no quantity', () => {
    expect(formatQuantity(undefined, 'en-US')).toBe('—')
    expect(formatQuantity(null, 'en-US')).toBe('—')
    expect(formatQuantity('', 'en-US')).toBe('—')
    expect(formatQuantity('several', 'en-US')).toBe('—')
  })
})

describe('isHolding', () => {
  it('is a holding only when the asset names a security', () => {
    expect(isHolding({ financeSecurityId: 'security-1' })).toBe(true)
    expect(isHolding({ financeSecurityId: null })).toBe(false)
    expect(isHolding({})).toBe(false)
  })
})

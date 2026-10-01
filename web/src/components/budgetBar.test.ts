import { describe, expect, it } from 'vitest'

import { meterBand } from './budgetBar'

describe('meterBand', () => {
  it('draws the band from the end of the fill to the forecast', () => {
    expect(meterBand(0.3, 0.75)).toEqual({ fillShare: 0.3, bandEndShare: 0.75, isHeadingOver: false })
  })

  // The first of the month with nothing spent: the band alone says where
  // the month is heading, starting from the empty end of the bar.
  it('starts at the beginning when nothing has gone', () => {
    expect(meterBand(0, 0.2)).toEqual({ fillShare: 0, bandEndShare: 0.2, isHeadingOver: false })
  })

  it('reaches the end and says so when the forecast passes it', () => {
    expect(meterBand(0.6, 1.4)).toEqual({ fillShare: 0.6, bandEndShare: 1, isHeadingOver: true })
  })

  // Already over: the full fill says it, and there is no band left to draw.
  it('is not heading over once the fill is past the end', () => {
    expect(meterBand(1.2, 1.5)).toEqual({ fillShare: 1, bandEndShare: 1, isHeadingOver: false })
  })

  it('draws no band for a forecast behind the fill, or none at all', () => {
    expect(meterBand(0.5, 0.4)).toEqual({ fillShare: 0.5, bandEndShare: 0.5, isHeadingOver: false })
    expect(meterBand(0.5, null)).toEqual({ fillShare: 0.5, bandEndShare: 0.5, isHeadingOver: false })
    expect(meterBand(0.5)).toEqual({ fillShare: 0.5, bandEndShare: 0.5, isHeadingOver: false })
  })

  it('keeps the bar between empty and full', () => {
    expect(meterBand(-0.2, -0.1)).toEqual({ fillShare: 0, bandEndShare: 0, isHeadingOver: false })
    expect(meterBand(Number.NaN, Number.POSITIVE_INFINITY)).toEqual({
      fillShare: 0,
      bandEndShare: 0,
      isHeadingOver: false,
    })
  })
})

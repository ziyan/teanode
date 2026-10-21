import { describe, expect, it } from 'vitest'

import { groupPins } from './mapPinGroups'

describe('groupPins', () => {
  it('keeps pictures apart that do not cover each other', () => {
    const groups = groupPins([
      { pin: 'first', x: 0, y: 0, isChosen: false },
      { pin: 'second', x: 100, y: 0, isChosen: false },
    ])
    expect(groups).toEqual([['first'], ['second']])
  })

  it('puts pictures that would cover each other in one group, the chosen one first and its group last', () => {
    const groups = groupPins([
      { pin: 'apart', x: 300, y: 300, isChosen: false },
      { pin: 'under', x: 10, y: 10, isChosen: false },
      { pin: 'chosen', x: 0, y: 0, isChosen: true },
      { pin: 'beside', x: 0, y: 30, isChosen: false },
    ])
    expect(groups).toEqual([['apart'], ['chosen', 'under', 'beside']])
  })
})

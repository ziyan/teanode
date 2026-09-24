import { describe, expect, it } from 'vitest'

import { suggestedRepliesOf, withoutPartialMarker } from './suggestions'

describe('suggestedRepliesOf', () => {
  it('leaves an answer without the line alone', () => {
    expect(suggestedRepliesOf('How can I help?')).toEqual({ displayText: 'How can I help?', suggestions: [] })
  })

  it('reads the replies and takes the line off', () => {
    expect(suggestedRepliesOf('Which one?\n<!--suggestions:["The red one","The blue one"]-->')).toEqual({
      displayText: 'Which one?',
      suggestions: ['The red one', 'The blue one'],
    })
  })

  it('takes off a line it cannot use, and offers nothing', () => {
    for (const line of [
      '<!--suggestions:["Only one"]-->',
      '<!--suggestions:["A","B","C","D","E","F","G"]-->',
      '<!--suggestions:["Fine",""]-->',
      '<!--suggestions:[not json]-->',
    ]) {
      expect(suggestedRepliesOf('Which one?\n' + line)).toEqual({ displayText: 'Which one?', suggestions: [] })
    }
  })
})

describe('withoutPartialMarker', () => {
  it('hides a line still being written', () => {
    expect(withoutPartialMarker('Which one?\n<!--sugg')).toBe('Which one?')
    expect(withoutPartialMarker('Which one?\n<!--suggestions:["The red')).toBe('Which one?')
  })

  it('hides a finished line too', () => {
    expect(withoutPartialMarker('Which one?\n<!--suggestions:["A","B"]-->')).toBe('Which one?')
  })

  it('leaves other comments alone', () => {
    expect(withoutPartialMarker('A note <!-- kept -->')).toBe('A note <!-- kept -->')
    expect(withoutPartialMarker('A note <!-- kept')).toBe('A note <!-- kept')
  })
})

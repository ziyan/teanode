import { describe, expect, it } from 'vitest'

import { firstLinkOf, linksIn, trimLinkEnd } from './linkText'

describe('trimLinkEnd', () => {
  it('takes the end of a sentence off a link', () => {
    expect(trimLinkEnd('https://example.com/a.')).toBe('https://example.com/a')
    expect(trimLinkEnd('https://example.com/a?!')).toBe('https://example.com/a')
    expect(trimLinkEnd('https://example.com/a),')).toBe('https://example.com/a')
  })
  it('keeps a bracket the link opened', () => {
    expect(trimLinkEnd('https://example.com/wiki/Thing_(film)')).toBe('https://example.com/wiki/Thing_(film)')
    expect(trimLinkEnd('https://example.com/wiki/Thing_(film))')).toBe('https://example.com/wiki/Thing_(film)')
  })
})

describe('linksIn', () => {
  it('finds each link and where it is', () => {
    const text = 'Try https://example.com/one, then (https://example.org/two?x=1).'
    const found = linksIn(text)
    expect(found.map((span) => span.link)).toEqual(['https://example.com/one', 'https://example.org/two?x=1'])
    expect(text.slice(found[1].start, found[1].end)).toBe('https://example.org/two?x=1')
  })
  it('stops at the words of a language written without spaces', () => {
    expect(linksIn('看看https://example.com/a，很好').map((span) => span.link)).toEqual(['https://example.com/a'])
  })
  it('ignores a scheme with no host and other schemes', () => {
    expect(linksIn('https:// and ftp://example.com and mailto:a@example.com')).toEqual([])
  })
})

describe('firstLinkOf', () => {
  it('is the first link of plain words', () => {
    expect(firstLinkOf('a https://example.com/first b https://example.com/second', false)).toBe(
      'https://example.com/first',
    )
    expect(firstLinkOf('no links here', false)).toBeNull()
  })
  it('is the first link of Markdown, leaving out code and pictures', () => {
    const text = [
      '![chart](https://example.com/chart.png)',
      '`https://example.com/in-code`',
      '```',
      'curl https://example.com/in-a-block',
      '```',
      'Read [the guide](https://example.com/guide) first.',
    ].join('\n')
    expect(firstLinkOf(text, true)).toBe('https://example.com/guide')
  })
  it('is the first link of Markdown that is only written out', () => {
    expect(firstLinkOf('**https://example.com/bold**', true)).toBe('https://example.com/bold')
  })
})

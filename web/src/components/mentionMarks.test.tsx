import { render } from '@testing-library/react'
import { expect, it } from 'vitest'

import { LinkedText } from './linkedText'
import { markMentions, mentionSpansOf } from './mentionMarks'

it('finds a mention where an @ starts one, never inside an address or a longer word', () => {
  expect(mentionSpansOf('mail ada@example.com, @Ada!', ['Ada'])).toEqual([{ start: 22, end: 26, word: 'Ada' }])
  expect(mentionSpansOf('@Adam', ['Ada'])).toEqual([])
  // Of two names that fit, the longer.
  expect(mentionSpansOf('@Ada Lin hi', ['Ada', 'Ada Lin'])).toEqual([{ start: 0, end: 8, word: 'Ada Lin' }])
  // Chinese and Japanese put no space before it.
  expect(mentionSpansOf('大家@所有人', ['所有人'])).toEqual([{ start: 2, end: 6, word: '所有人' }])
})

it('marks the mentions in typed words beside their links, the reader’s own stronger', () => {
  const { container } = render(
    <LinkedText
      text="@Ada see https://example.com and @Milo"
      mentions={[
        { word: 'Ada', isReader: true },
        { word: 'Milo', isReader: false },
      ]}
    />,
  )
  const marks = Array.from(container.querySelectorAll('mark.chat-mention'))
  expect(marks.map((mark) => mark.textContent)).toEqual(['@Ada', '@Milo'])
  expect(marks[0].classList.contains('chat-mention-reader')).toBe(true)
  expect(container.querySelector('a')?.getAttribute('href')).toBe('https://example.com')
  expect(markMentions('no mentions here', [{ word: 'Ada', isReader: false }])).toEqual(['no mentions here'])
})

import { cleanup, render } from '@testing-library/react'
import { useRef } from 'react'
import { afterEach, expect, it, vi } from 'vitest'

import { useFitToContent } from './useFitToContent'

afterEach(cleanup)

// jsdom lays nothing out, so the box's content height is given here: a
// line for each line of text.
function Box({ value, isShown = true }: { value: string; isShown?: boolean }) {
  const element = useRef<HTMLTextAreaElement>(null)
  useFitToContent(element, value, isShown)
  return isShown ? <textarea ref={element} value={value} readOnly aria-label="box" /> : null
}

function withLineHeight() {
  return vi.spyOn(HTMLTextAreaElement.prototype, 'scrollHeight', 'get').mockImplementation(function (
    this: HTMLTextAreaElement,
  ) {
    return this.value.split('\n').length * 20
  })
}

it('grows to a value set by the page, not only one typed', () => {
  withLineHeight()
  const { getByLabelText, rerender } = render(<Box value="" />)
  const box = getByLabelText('box') as HTMLTextAreaElement
  expect(box.style.height).toBe('36px')
  rerender(<Box value={'Plan the watering.\nEvery plant on the balcony.\nTwice a week.'} />)
  expect(box.style.height).toBe('60px')
})

it('shrinks back to one line when the box is emptied', () => {
  withLineHeight()
  const { getByLabelText, rerender } = render(<Box value={'one\ntwo\nthree\nfour'} />)
  const box = getByLabelText('box') as HTMLTextAreaElement
  expect(box.style.height).toBe('80px')
  rerender(<Box value="" />)
  expect(box.style.height).toBe('36px')
})

it('fits a box drawn again to the value it comes back with', () => {
  withLineHeight()
  const kept = 'one\ntwo\nthree'
  const { getByLabelText, rerender } = render(<Box value={kept} isShown={false} />)
  rerender(<Box value={kept} isShown />)
  expect((getByLabelText('box') as HTMLTextAreaElement).style.height).toBe('60px')
})

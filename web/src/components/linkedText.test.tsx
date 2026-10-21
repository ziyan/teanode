import { cleanup, render } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'

import { LinkedText } from './linkedText'

afterEach(cleanup)

it('shows a link short and opens it clean in a new tab', () => {
  const { container } = render(
    <p>
      <LinkedText text={'Look at https://www.example.com/shop/item-1/?gclid=abc&color=red, it is nice.'} />
    </p>,
  )
  const link = container.querySelector('a')
  expect(link?.textContent).toBe('example.com/shop/item-1/?color=red')
  expect(link?.getAttribute('href')).toBe('https://www.example.com/shop/item-1/?color=red')
  expect(link?.getAttribute('target')).toBe('_blank')
  expect(link?.getAttribute('rel')).toBe('noopener noreferrer')
  expect(container.textContent).toBe('Look at example.com/shop/item-1/?color=red, it is nice.')
})

it('leaves words without links exactly as typed', () => {
  const { container } = render(
    <p>
      <LinkedText text={'two\nlines  and a mailto:a@example.com'} />
    </p>,
  )
  expect(container.querySelector('a')).toBeNull()
  expect(container.textContent).toBe('two\nlines  and a mailto:a@example.com')
})

it('links each of several links', () => {
  const { container } = render(
    <p>
      <LinkedText text={'https://example.com/a then https://example.org/b'} />
    </p>,
  )
  expect(Array.from(container.querySelectorAll('a')).map((link) => link.getAttribute('href'))).toEqual([
    'https://example.com/a',
    'https://example.org/b',
  ])
})

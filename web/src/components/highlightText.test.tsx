import { render } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { HighlightText } from './highlightText'

describe('HighlightText', () => {
  it('marks every match, ignoring case, and keeps the text around it', () => {
    const { container } = render(<HighlightText text="Plan the trip, then book the Trip" search="trip" />)
    expect([...container.querySelectorAll('mark')].map((node) => node.textContent)).toEqual(['trip', 'Trip'])
    expect(container.textContent).toBe('Plan the trip, then book the Trip')
  })

  it('treats the search as plain text, not a pattern', () => {
    const { container } = render(<HighlightText text="costs (estimate) a+b" search="(estimate)" />)
    expect(container.querySelector('mark')?.textContent).toBe('(estimate)')
  })

  it('marks nothing for an empty search', () => {
    const { container } = render(<HighlightText text="anything" search="  " />)
    expect(container.querySelector('mark')).toBeNull()
  })
})

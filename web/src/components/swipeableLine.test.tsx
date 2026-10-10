import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { SWIPE_ACTION_PX, SwipeableLine, swipeOffset } from './swipeableLine'

describe('swipeOffset', () => {
  it('follows a finger one for one up to where letting go acts', () => {
    expect(swipeOffset(-30)).toBe(-30)
    expect(swipeOffset(SWIPE_ACTION_PX)).toBe(SWIPE_ACTION_PX)
  })

  it('goes heavier past it, and no farther than its limit', () => {
    expect(swipeOffset(-(SWIPE_ACTION_PX + 20))).toBe(-(SWIPE_ACTION_PX + 7))
    expect(swipeOffset(400)).toBe(96)
    expect(swipeOffset(-400)).toBe(-96)
  })
})

describe('SwipeableLine', () => {
  it('copies and replies from its buttons, for a mouse or a keyboard', () => {
    const onReply = vi.fn()
    const onCopy = vi.fn(() => Promise.resolve(true))
    render(
      <SwipeableLine
        className="agent-line assistant"
        replyLabel="Reply"
        copyLabel="Copy"
        onReply={onReply}
        onCopy={onCopy}
      >
        The ferry leaves at nine.
      </SwipeableLine>,
    )
    fireEvent.click(screen.getByRole('button', { name: 'Reply' }))
    fireEvent.click(screen.getByRole('button', { name: 'Copy' }))
    expect(onReply).toHaveBeenCalledTimes(1)
    expect(onCopy).toHaveBeenCalledTimes(1)
  })

  it('shows a tick on its Copy button once the copy took', async () => {
    const { container } = render(
      <SwipeableLine
        className="agent-line user"
        replyLabel="Reply"
        copyLabel="Copy"
        onReply={() => undefined}
        onCopy={() => Promise.resolve(true)}
      >
        Book the seat.
      </SwipeableLine>,
    )
    const copyButton = within(container).getByRole('button', { name: 'Copy' })
    fireEvent.click(copyButton)
    await waitFor(() => expect(copyButton.className).toContain('copied'))
  })
})

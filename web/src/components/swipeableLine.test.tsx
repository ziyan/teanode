import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { SWIPE_ACTION_PX, SwipeableLine, swipeOffset } from './swipeableLine'

describe('swipeOffset', () => {
  it('follows a finger either way, as far as it goes', () => {
    expect(swipeOffset(-30, 4)).toBe(-30)
    expect(swipeOffset(-SWIPE_ACTION_PX, 0)).toBe(-SWIPE_ACTION_PX)
    expect(swipeOffset(-400, 10)).toBe(-80)
    expect(swipeOffset(40, 0)).toBe(40)
    expect(swipeOffset(400, -10)).toBe(80)
  })

  it('stays still for a scroll', () => {
    expect(swipeOffset(-20, 60)).toBe(0)
    expect(swipeOffset(20, -60)).toBe(0)
  })
})

describe('SwipeableLine', () => {
  it('copies and replies from its buttons, for a mouse or a keyboard', () => {
    const onReply = vi.fn()
    const onCopy = vi.fn()
    render(
      <SwipeableLine className="agent-line assistant" replyLabel="Reply" copyLabel="Copy" onReply={onReply} onCopy={onCopy}>
        The ferry leaves at nine.
      </SwipeableLine>,
    )
    fireEvent.click(screen.getByRole('button', { name: 'Reply' }))
    fireEvent.click(screen.getByRole('button', { name: 'Copy' }))
    expect(onReply).toHaveBeenCalledTimes(1)
    expect(onCopy).toHaveBeenCalledTimes(1)
  })
})

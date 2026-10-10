import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { SWIPE_REPLY_PX, SwipeToReply, swipeOffset } from './swipeToReply'

describe('swipeOffset', () => {
  it('follows a finger moving left, as far as it goes', () => {
    expect(swipeOffset(-30, 4)).toBe(-30)
    expect(swipeOffset(-SWIPE_REPLY_PX, 0)).toBe(-SWIPE_REPLY_PX)
    expect(swipeOffset(-400, 10)).toBe(-80)
  })

  it('stays still for a move to the right, or a scroll', () => {
    expect(swipeOffset(40, 0)).toBe(0)
    expect(swipeOffset(-20, 60)).toBe(0)
    expect(swipeOffset(-20, -60)).toBe(0)
  })
})

describe('SwipeToReply', () => {
  it('replies from its button, for a mouse or a keyboard', () => {
    const onReply = vi.fn()
    render(
      <SwipeToReply className="agent-line assistant" replyLabel="Reply" onReply={onReply}>
        The ferry leaves at nine.
      </SwipeToReply>,
    )
    fireEvent.click(screen.getByRole('button', { name: 'Reply' }))
    expect(onReply).toHaveBeenCalledTimes(1)
  })
})

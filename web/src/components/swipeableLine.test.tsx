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
        onCopyFailed={() => undefined}
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
        onCopyFailed={() => undefined}
      >
        Book the seat.
      </SwipeableLine>,
    )
    const copyButton = within(container).getByRole('button', { name: 'Copy' })
    fireEvent.click(copyButton)
    await waitFor(() => expect(copyButton.className).toContain('copied'))
  })
})

it('says a click could not copy, rather than holding out a button for it', async () => {
  const onCopyFailed = vi.fn()
  const { container } = render(
    <SwipeableLine
      className="agent-line user"
      replyLabel="Reply"
      copyLabel="Copy"
      onReply={() => undefined}
      onCopy={() => Promise.resolve(false)}
      onCopyFailed={onCopyFailed}
    >
      Book the seat.
    </SwipeableLine>,
  )
  fireEvent.click(within(container).getByRole('button', { name: 'Copy' }))
  await waitFor(() => expect(onCopyFailed).toHaveBeenCalled())
})

// jsdom has no PointerEvent: a mouse event that carries the pointer's id
// and kind is what React reads.
if (typeof window.PointerEvent === 'undefined') {
  class TestPointerEvent extends MouseEvent {
    pointerId: number
    pointerType: string
    constructor(type: string, init: PointerEventInit = {}) {
      super(type, init)
      this.pointerId = init.pointerId ?? 1
      this.pointerType = init.pointerType ?? 'mouse'
    }
  }
  window.PointerEvent = TestPointerEvent as unknown as typeof PointerEvent
}

describe('SwipeableLine by touch', () => {
  // swipe runs a finger across the line through the points given, from
  // (100, 100), and lifts it at the last.
  const swipe = (line: HTMLElement, points: [number, number][]) => {
    const at = (x: number, y: number) => ({ pointerId: 3, pointerType: 'touch', clientX: 100 + x, clientY: 100 + y })
    fireEvent.pointerDown(line, at(0, 0))
    for (const [x, y] of points) fireEvent.pointerMove(line, at(x, y))
    const [lastX, lastY] = points[points.length - 1]
    fireEvent.pointerUp(line, at(lastX, lastY))
  }
  const drawn = (onReply = vi.fn(), onCopy = vi.fn(() => Promise.resolve(true))) => {
    const { container } = render(
      <SwipeableLine
        className="agent-line user"
        replyLabel="Reply"
        copyLabel="Copy"
        onReply={onReply}
        onCopy={onCopy}
        onCopyFailed={() => undefined}
      >
        Book the seat.
      </SwipeableLine>,
    )
    return { line: container.firstElementChild as HTMLElement, container, onReply, onCopy }
  }

  it('replies to a swipe to the left that goes far enough', () => {
    const { line, onReply, onCopy } = drawn()
    swipe(line, [
      [-14, 2],
      [-50, 20],
      [-90, 40],
    ])
    expect(onReply).toHaveBeenCalledTimes(1)
    expect(onCopy).not.toHaveBeenCalled()
  })

  it('does nothing for a short swipe, a jiggle, or a scroll', () => {
    const { line, onReply, onCopy } = drawn()
    swipe(line, [[-40, 0]])
    swipe(line, [[-8, 1]])
    swipe(line, [
      [-4, 20],
      [-90, 120],
    ])
    expect(onReply).not.toHaveBeenCalled()
    expect(onCopy).not.toHaveBeenCalled()
    expect(line.style.transform).toBe('')
  })

  it('copies on a swipe to the right, holds out with a tick, and swallows the tap that ended it', async () => {
    const { line, onCopy } = drawn()
    const onClick = vi.fn()
    line.addEventListener('click', onClick)
    swipe(line, [
      [14, 0],
      [90, 0],
    ])
    expect(onCopy).toHaveBeenCalledTimes(1)
    fireEvent.click(line)
    expect(onClick).not.toHaveBeenCalled()
    await waitFor(() => expect(line.querySelector('.agent-swipe-action.copied')).not.toBeNull())
    expect(line.style.transform).toBe(`translateX(${SWIPE_ACTION_PX}px)`)
  })

  it('offers a button to tap when the browser refuses the swipe its copy', async () => {
    let isAllowed = false
    const { line, container, onCopy } = drawn(
      vi.fn(),
      vi.fn(() => Promise.resolve(isAllowed)),
    )
    swipe(line, [
      [14, 0],
      [90, 0],
    ])
    const tapButton = await waitFor(() => {
      const found = container.querySelector('.agent-swipe-action.waiting')
      expect(found).not.toBeNull()
      return found as HTMLElement
    })
    isAllowed = true
    // A tap: the finger comes down and lifts where it was, then the click.
    const tapAt = { pointerId: 4, pointerType: 'touch', clientX: 20, clientY: 20 }
    fireEvent.pointerDown(tapButton, tapAt)
    fireEvent.pointerUp(tapButton, tapAt)
    fireEvent.click(tapButton)
    expect(onCopy).toHaveBeenCalledTimes(2)
    await waitFor(() => expect(line.querySelector('.agent-swipe-action.copied')).not.toBeNull())
  })
})

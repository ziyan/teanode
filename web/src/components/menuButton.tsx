import { useCallback, useEffect, useId, useLayoutEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'

// MenuButton is an icon on the top bar with a small menu behind it. The bar
// has three of these — language, appearance, account — and they should behave
// identically: open on click, close on Escape, on a click elsewhere, and when
// something inside them is chosen.
//
// Written once rather than three times, because a menu that closes in one
// corner and not in another is the kind of thing nobody reports and everybody
// notices.
//
// The menu is rendered into the body rather than beside its button. A column
// filter's button lives inside the table's scroll container, and an absolutely
// positioned child of a container with overflow: auto is clipped to it — the
// menu came out as a sliver the width of its column with the options cut off.
// Positioned from the button's rectangle instead, it can be wider than the
// column it belongs to, which a menu full of domain names has to be.
export function MenuButton({
  label,
  icon,
  className,
  // Which side of the button the menu hangs off. The account button sits at
  // the foot of the rail, where a menu opening downwards would open off the
  // bottom of the window.
  placement = 'below',
  render,
}: {
  label: string
  icon: React.ReactNode
  className?: string
  placement?: 'below' | 'above'
  render: (close: () => void) => React.ReactNode
}) {
  const [open, setOpen] = useState(false)
  const [position, setPosition] = useState<React.CSSProperties>({ top: 0, right: 0 })
  const container = useRef<HTMLDivElement>(null)
  const menu = useRef<HTMLDivElement>(null)
  const trigger = useRef<HTMLButtonElement>(null)
  const id = useId()

  // Right-aligned to the button, which is where a menu hanging off a control
  // in the top-right corner has to be, and reads correctly under a column
  // filter too.
  const place = useCallback(() => {
    const rectangle = trigger.current?.getBoundingClientRect()
    if (!rectangle) {
      return
    }
    // Above: pinned by its bottom edge and its left, so it grows upwards from
    // the account row and stays inside the rail's column. Below: right-aligned
    // to the button, which is where a menu hanging off a control in the
    // top-right corner has to be.
    setPosition(
      placement === 'above'
        ? { bottom: window.innerHeight - rectangle.top + 6, left: rectangle.left }
        : { top: rectangle.bottom + 6, right: window.innerWidth - rectangle.right },
    )
  }, [placement])

  useLayoutEffect(() => {
    if (open) {
      place()
    }
  }, [open, place])

  // Follow the button when anything moves under it, rather than hanging in
  // the wrong place. Capture, so a scroll inside the table counts too.
  useEffect(() => {
    if (!open) {
      return
    }
    window.addEventListener('scroll', place, true)
    window.addEventListener('resize', place)
    return () => {
      window.removeEventListener('scroll', place, true)
      window.removeEventListener('resize', place)
    }
  }, [open, place])

  useEffect(() => {
    if (!open) {
      return
    }
    function onPointerDown(event: MouseEvent | TouchEvent) {
      const target = event.target as Node
      // The menu is no longer inside the container, so both have to be asked.
      if (container.current?.contains(target) || menu.current?.contains(target)) {
        return
      }
      setOpen(false)
    }
    function onKeyDown(event: KeyboardEvent) {
      if (event.key === 'Escape') {
        setOpen(false)
      }
    }
    document.addEventListener('mousedown', onPointerDown)
    document.addEventListener('touchstart', onPointerDown)
    document.addEventListener('keydown', onKeyDown)
    return () => {
      document.removeEventListener('mousedown', onPointerDown)
      document.removeEventListener('touchstart', onPointerDown)
      document.removeEventListener('keydown', onKeyDown)
    }
  }, [open])

  return (
    <div className="menu-button" ref={container}>
      <button
        type="button"
        ref={trigger}
        className={className}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-controls={open ? id : undefined}
        aria-label={label}
        title={label}
        onClick={() => setOpen((previous) => !previous)}
      >
        {icon}
      </button>
      {open &&
        createPortal(
          <div
            className="menu"
            id={id}
            role="menu"
            ref={menu}
            style={position}
          >
            {render(() => setOpen(false))}
          </div>,
          document.body,
        )}
    </div>
  )
}

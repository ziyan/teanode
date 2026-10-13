import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'

export interface PassageMenuItem {
  itemKey: string
  label: string
  isDisabled?: boolean
  onSelect: () => void
}

// PassageMenu is the small menu on a passage of the conversation. It opens at a
// point rather than under a button, because it answers a right-click or a long
// press on the text itself, and the same menu opens from the passage's More
// button for a keyboard. It reuses the dashboard's `.menu` look.
export function PassageMenu({
  label,
  leftPixels,
  topPixels,
  items,
  onClose,
}: {
  label: string
  leftPixels: number
  topPixels: number
  items: PassageMenuItem[]
  onClose: () => void
}) {
  const menu = useRef<HTMLDivElement>(null)
  const [position, setPosition] = useState({ left: leftPixels, top: topPixels })
  const closeReference = useRef(onClose)
  closeReference.current = onClose

  // Kept inside the window: a menu opened at the right edge of a bubble on a
  // phone would otherwise hang off the screen.
  useLayoutEffect(() => {
    const rectangle = menu.current?.getBoundingClientRect()
    if (!rectangle) return
    setPosition({
      left: Math.max(8, Math.min(leftPixels, window.innerWidth - rectangle.width - 8)),
      top: Math.max(8, Math.min(topPixels, window.innerHeight - rectangle.height - 8)),
    })
  }, [leftPixels, topPixels])

  useEffect(() => {
    menu.current?.querySelector<HTMLButtonElement>('button:not(:disabled)')?.focus()
    const closeOutside = (event: MouseEvent | TouchEvent) => {
      if (!menu.current?.contains(event.target as Node)) closeReference.current()
    }
    const closeOnKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        event.preventDefault()
        closeReference.current()
      }
    }
    const close = () => closeReference.current()
    document.addEventListener('mousedown', closeOutside)
    document.addEventListener('touchstart', closeOutside)
    document.addEventListener('keydown', closeOnKey)
    window.addEventListener('scroll', close, true)
    window.addEventListener('resize', close)
    return () => {
      document.removeEventListener('mousedown', closeOutside)
      document.removeEventListener('touchstart', closeOutside)
      document.removeEventListener('keydown', closeOnKey)
      window.removeEventListener('scroll', close, true)
      window.removeEventListener('resize', close)
    }
  }, [])

  const moveFocus = (event: React.KeyboardEvent, step: number) => {
    event.preventDefault()
    const buttons = Array.from(menu.current?.querySelectorAll<HTMLButtonElement>('button:not(:disabled)') ?? [])
    const index = buttons.indexOf(document.activeElement as HTMLButtonElement)
    buttons[(index + step + buttons.length) % buttons.length]?.focus()
  }

  return createPortal(
    <div
      className="menu recording-passage-menu"
      role="menu"
      aria-label={label}
      ref={menu}
      style={position}
      onKeyDown={(event) => {
        if (event.key === 'ArrowDown') moveFocus(event, 1)
        else if (event.key === 'ArrowUp') moveFocus(event, -1)
      }}
    >
      {items.map((item) => (
        <button
          key={item.itemKey}
          type="button"
          role="menuitem"
          disabled={item.isDisabled}
          onClick={() => {
            onClose()
            item.onSelect()
          }}
        >
          {item.label}
        </button>
      ))}
    </div>,
    document.body,
  )
}

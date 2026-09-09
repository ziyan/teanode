import { useCallback, useEffect, useId, useLayoutEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'

import { ChevronDownIcon } from './icons'

// A dropdown the dashboard draws itself.
//
// A native <select> is the browser's widget: its list is drawn by the
// operating system in the operating system's colors, it cannot be styled, and
// on a dark page it opens as a white rectangle. Where a control is read as
// part of the page rather than as a form to fill in — the filter above a log,
// say — that is jarring enough to be worth the code.
//
// Not a replacement for every <select>. A form of ten fields is better with
// the browser's, which every platform's assistive technology and every
// password manager already understands. This is for the handful of controls
// that are part of the furniture.

export type SelectOption = { value: string; label: string }

export function Select({
  value,
  options,
  onChange,
  label,
  className,
}: {
  value: string
  options: SelectOption[]
  onChange: (value: string) => void
  // What the control is, for a screen reader: there is no <label> around it.
  label: string
  className?: string
}) {
  const [open, setOpen] = useState(false)
  const [active, setActive] = useState(0)
  const [position, setPosition] = useState<React.CSSProperties>({})
  const trigger = useRef<HTMLButtonElement>(null)
  const list = useRef<HTMLDivElement>(null)
  const id = useId()

  const chosen = options.find((option) => option.value === value)

  // Positioned from the button's rectangle and rendered into the body, so a
  // dropdown inside a scrolling panel is not clipped to it, and matched to
  // the button's width so it reads as the same control opened.
  const place = useCallback(() => {
    const box = trigger.current?.getBoundingClientRect()
    if (!box) {
      return
    }
    setPosition({ top: box.bottom + 4, left: box.left, minWidth: box.width })
  }, [])

  useLayoutEffect(() => {
    if (open) {
      place()
      setActive(
        Math.max(
          0,
          options.findIndex((option) => option.value === value),
        ),
      )
    }
  }, [open, place, options, value])

  useEffect(() => {
    if (!open) {
      return
    }
    const close = () => setOpen(false)
    window.addEventListener('scroll', close, true)
    window.addEventListener('resize', close)
    function onPointerDown(event: MouseEvent | TouchEvent) {
      const target = event.target as Node
      if (trigger.current?.contains(target) || list.current?.contains(target)) {
        return
      }
      setOpen(false)
    }
    document.addEventListener('mousedown', onPointerDown)
    document.addEventListener('touchstart', onPointerDown)
    return () => {
      window.removeEventListener('scroll', close, true)
      window.removeEventListener('resize', close)
      document.removeEventListener('mousedown', onPointerDown)
      document.removeEventListener('touchstart', onPointerDown)
    }
  }, [open])

  const choose = (option: SelectOption) => {
    onChange(option.value)
    setOpen(false)
    trigger.current?.focus()
  }

  // The keys a listbox is expected to answer to. Without these it is a
  // button that looks like a select and cannot be used without a mouse.
  const onKeyDown = (event: React.KeyboardEvent) => {
    if (!open) {
      if (event.key === 'ArrowDown' || event.key === 'Enter' || event.key === ' ') {
        event.preventDefault()
        setOpen(true)
      }
      return
    }
    switch (event.key) {
      case 'Escape':
        event.preventDefault()
        setOpen(false)
        break
      case 'ArrowDown':
        event.preventDefault()
        setActive((previous) => Math.min(options.length - 1, previous + 1))
        break
      case 'ArrowUp':
        event.preventDefault()
        setActive((previous) => Math.max(0, previous - 1))
        break
      case 'Home':
        event.preventDefault()
        setActive(0)
        break
      case 'End':
        event.preventDefault()
        setActive(options.length - 1)
        break
      case 'Enter':
      case ' ':
        event.preventDefault()
        if (options[active]) {
          choose(options[active])
        }
        break
    }
  }

  return (
    <>
      <button
        type="button"
        ref={trigger}
        className={className ? `select ${className}` : 'select'}
        role="combobox"
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-controls={open ? id : undefined}
        aria-label={label}
        onClick={() => setOpen((previous) => !previous)}
        onKeyDown={onKeyDown}
      >
        <span className="select-value">{chosen?.label ?? ''}</span>
        <ChevronDownIcon size={14} className="chevron" />
      </button>
      {open &&
        createPortal(
          <div className="select-list" id={id} role="listbox" ref={list} style={position} aria-label={label}>
            {options.map((option, index) => (
              <button
                key={option.value}
                type="button"
                role="option"
                aria-selected={option.value === value}
                className={index === active ? 'active' : undefined}
                onMouseEnter={() => setActive(index)}
                onClick={() => choose(option)}
              >
                {option.label}
              </button>
            ))}
          </div>,
          document.body,
        )}
    </>
  )
}

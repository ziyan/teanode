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

// searchableFrom is how many options a list has before it gets a box to
// narrow it by typing; fewer are read at a glance.
const searchableFrom = 8

export function Select({
  value,
  options,
  onChange,
  label,
  className,
  block,
  searchable,
  allowCustom,
  placeholder,
  disabled,
}: {
  value: string
  options: SelectOption[]
  onChange: (value: string) => void
  // What the control is, for a screen reader: there is no <label> around it.
  label: string
  className?: string
  // Block fills its line the way an input does, for one that sits in a
  // form among inputs rather than in a toolbar.
  block?: boolean
  // Searchable puts a box at the top of the list that narrows it as the
  // person types; on by itself once the list is long.
  searchable?: boolean
  // AllowCustom lets what was typed be the value when no option matches —
  // a model name the list does not know yet.
  allowCustom?: boolean
  placeholder?: string
  disabled?: boolean
}) {
  const [open, setOpen] = useState(false)
  const [active, setActive] = useState(0)
  const [query, setQuery] = useState('')
  const [position, setPosition] = useState<React.CSSProperties>({})
  const trigger = useRef<HTMLButtonElement>(null)
  const list = useRef<HTMLDivElement>(null)
  const search = useRef<HTMLInputElement>(null)
  const id = useId()

  const chosen = options.find((option) => option.value === value)
  // What the options are, not which array holds them: a parent writes the
  // array afresh on every render, and an effect keyed on it would run,
  // move the list, and render again without end.
  const optionsKey = options.map((option) => option.value).join('\u0000')
  const searching = searchable ?? (options.length >= searchableFrom || Boolean(allowCustom))
  const words = query.trim().toLowerCase()
  let shown = words
    ? options.filter((option) => option.label.toLowerCase().includes(words) || option.value.toLowerCase().includes(words))
    : options
  if (allowCustom && words && !options.some((option) => option.value.toLowerCase() === words)) {
    shown = [{ value: query.trim(), label: `“${query.trim()}”` }, ...shown]
  }

  // Positioned from the button's rectangle and rendered into the body, so a
  // dropdown inside a scrolling panel is not clipped to it, and matched to
  // the button's width so it reads as the same control opened.
  //
  // Which side it opens on is decided here rather than assumed. The list is
  // fixed to the window, so a list drawn past the bottom edge cannot be
  // scrolled to: it is simply gone. The control that chooses how many rows a
  // table shows sits at the foot of the page, which is exactly where there is
  // no room below — so when there is more room above, it opens upward, and
  // either way it is no taller than the room it has and scrolls inside that.
  const place = useCallback(() => {
    const box = trigger.current?.getBoundingClientRect()
    if (!box) {
      return
    }
    const gap = 4
    const margin = 8
    const below = window.innerHeight - box.bottom - gap - margin
    const above = box.top - gap - margin
    const wanted = list.current?.scrollHeight ?? 0
    const upward = below < wanted && above > below
    // Never taller than the stylesheet's own cap, so a long list looks the
    // same here as it does anywhere else with room to spare.
    const room = Math.max(80, Math.min(320, upward ? above : below))

    // And it stays inside the window sideways as well, for a list wider than
    // the button that opened it near the right-hand edge.
    const width = Math.max(list.current?.offsetWidth ?? 0, box.width)
    const left = Math.max(margin, Math.min(box.left, window.innerWidth - margin - width))

    setPosition({
      left,
      minWidth: box.width,
      maxHeight: room,
      ...(upward ? { bottom: window.innerHeight - box.top + gap } : { top: box.bottom + gap }),
    })
  }, [])

  useLayoutEffect(() => {
    if (open) {
      place()
      setActive(
        Math.max(
          0,
          shown.findIndex((option) => option.value === value),
        ),
      )
      if (searching) {
        search.current?.focus()
      }
    } else {
      setQuery('')
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, place, optionsKey, value])

  // The list narrows as the words change; the mark goes back to the top.
  useEffect(() => {
    setActive(0)
    place()
  }, [query, place])

  useEffect(() => {
    if (!open) {
      return
    }
    const close = (event: Event) => {
      // Scrolling inside the list is not leaving it.
      if (event.target instanceof Node && list.current?.contains(event.target)) return
      setOpen(false)
    }
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
        setActive((previous) => Math.min(shown.length - 1, previous + 1))
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
        setActive(shown.length - 1)
        break
      case 'Enter':
        event.preventDefault()
        if (shown[active]) {
          choose(shown[active])
        }
        break
      case ' ':
        // A space in the search box is a space; on the button it opens.
        if (!searching) {
          event.preventDefault()
          if (shown[active]) {
            choose(shown[active])
          }
        }
        break
    }
  }

  return (
    <>
      <button
        type="button"
        ref={trigger}
        className={['select', block ? 'block' : '', className ?? ''].filter(Boolean).join(' ')}
        role="combobox"
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-controls={open ? id : undefined}
        aria-label={label}
        disabled={disabled}
        onClick={() => setOpen((previous) => !previous)}
        onKeyDown={onKeyDown}
      >
        <span className={chosen || value ? 'select-value' : 'select-value placeholder'}>
          {chosen?.label ?? value ?? ''}
          {!chosen && !value ? placeholder ?? '' : ''}
        </span>
        <ChevronDownIcon size={14} className="chevron" />
      </button>
      {open &&
        createPortal(
          <div className="select-list" id={id} ref={list} style={position}>
            {searching && (
              <input
                ref={search}
                className="select-search"
                type="search"
                value={query}
                placeholder={placeholder ?? label}
                aria-label={label}
                onChange={(event) => setQuery(event.target.value)}
                onKeyDown={onKeyDown}
              />
            )}
            <div role="listbox" aria-label={label}>
              {shown.length === 0 && <div className="select-empty muted">—</div>}
              {shown.map((option, index) => (
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
            </div>
          </div>,
          document.body,
        )}
    </>
  )
}

// Combobox is a text box with suggestions under it: what was typed is the
// value, always, and a suggestion is a shortcut to typing it. For an
// address being written, a model name, a category — anything a person may
// type that the list does not have to know.
export function Combobox({
  value,
  suggestions,
  onChange,
  label,
  placeholder,
  className,
  autoFocus,
  onFocus,
}: {
  value: string
  suggestions: string[]
  onChange: (value: string) => void
  label: string
  placeholder?: string
  className?: string
  autoFocus?: boolean
  onFocus?: () => void
}) {
  const [open, setOpen] = useState(false)
  const [active, setActive] = useState(0)
  const [position, setPosition] = useState<React.CSSProperties>({})
  const input = useRef<HTMLInputElement>(null)
  const list = useRef<HTMLDivElement>(null)
  const id = useId()
  const shown = suggestions.filter((suggestion) => suggestion !== value).slice(0, 12)
  const showing = open && shown.length > 0

  const place = useCallback(() => {
    const box = input.current?.getBoundingClientRect()
    if (!box) return
    setPosition({ left: box.left, top: box.bottom + 4, minWidth: box.width, maxHeight: 280 })
  }, [])

  useLayoutEffect(() => {
    if (showing) place()
  }, [showing, place, shown.length])

  useEffect(() => {
    if (!showing) return
    const close = (event: Event) => {
      if (event.target instanceof Node && list.current?.contains(event.target)) return
      setOpen(false)
    }
    window.addEventListener('scroll', close, true)
    window.addEventListener('resize', close)
    const onPointerDown = (event: MouseEvent | TouchEvent) => {
      const target = event.target as Node
      if (input.current?.contains(target) || list.current?.contains(target)) return
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
  }, [showing])

  const pick = (suggestion: string) => {
    onChange(suggestion)
    setOpen(false)
    input.current?.focus()
  }

  return (
    <>
      <input
        ref={input}
        className={className}
        role="combobox"
        aria-autocomplete="list"
        aria-expanded={showing}
        aria-controls={showing ? id : undefined}
        aria-label={label}
        value={value}
        placeholder={placeholder}
        autoFocus={autoFocus}
        autoComplete="off"
        onFocus={() => {
          setOpen(true)
          onFocus?.()
        }}
        onChange={(event) => {
          onChange(event.target.value)
          setOpen(true)
          setActive(0)
        }}
        onKeyDown={(event) => {
          if (!showing) return
          switch (event.key) {
            case 'ArrowDown':
              event.preventDefault()
              setActive((previous) => Math.min(shown.length - 1, previous + 1))
              break
            case 'ArrowUp':
              event.preventDefault()
              setActive((previous) => Math.max(0, previous - 1))
              break
            case 'Enter':
              if (shown[active]) {
                event.preventDefault()
                pick(shown[active])
              }
              break
            case 'Escape':
              setOpen(false)
              break
          }
        }}
      />
      {showing &&
        createPortal(
          <div className="select-list" id={id} role="listbox" ref={list} style={position} aria-label={label}>
            {shown.map((suggestion, index) => (
              <button
                key={suggestion}
                type="button"
                role="option"
                aria-selected={false}
                className={index === active ? 'active' : undefined}
                onMouseEnter={() => setActive(index)}
                onMouseDown={(event) => event.preventDefault()}
                onClick={() => pick(suggestion)}
              >
                {suggestion}
              </button>
            ))}
          </div>,
          document.body,
        )}
    </>
  )
}

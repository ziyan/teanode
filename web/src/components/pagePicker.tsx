import { useCallback, useEffect, useId, useLayoutEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'

import { graphql } from '../api'
import { useTranslation } from '../i18n/i18n'
import { useDebounced } from './preview'
import { useQuery } from './useQuery'

// Which page, out of thousands.
//
// Every dialog that asks for another page — merge into, file under, move a
// fact to, link to — used to ask for its path and nothing else, typed from
// memory into an empty box, and the server refused whatever was not exactly
// right. A graph of a few dozen pages can be held in a head; one of a few
// thousand, growing every night, cannot, and the box was by then a guessing
// game with a rejection at the end of it.
//
// Still a text box, though. What is typed is the value, always, and the list
// under it is a shortcut rather than the only way in: somebody who knows the
// path types it and presses Enter without the list ever being consulted.
// Turning the field into a chooser would take that away from the person who
// has it memorised in order to help the person who has not.

export type FoundPage = { id: string; path: string; name: string }

// The search the knowledge page's own box asks, SearchAgentGraph, selecting
// only what a row here draws. Not the facts it can also return: a picker
// never shows a fact, and fetching sentences per keystroke to throw them away
// would make the dialog pay for a pane it does not have.
//
// It matches whole words — the column is a tsvector — so "alice" finds
// people/alice-chen and "ali" does not. That is the same search the page and
// the agent use, and a second kind of matching for this one box would be a
// second thing to keep true.
const SEARCH = `query ($query: String!, $first: Int) {
  SearchAgentGraph(query: $query, first: $first) {
    nodes { id path name }
  }
}`

// Fewest letters worth asking about. One letter matches a good part of the
// graph, which answers nothing and costs a query per keystroke.
const fewestLetters = 2

// How many rows the list offers: more than fits in it, so there is a reason
// to scroll, and few enough that scrolling ends.
const mostRows = 20

// How long typing has to stop for. The search is a round trip that ends in a
// full-text index, and a question per letter is a pile of answers nobody
// reads.
const settleDelay = 250

export function PagePicker({
  value,
  onChange,
  label,
  placeholder,
  exclude,
  autoFocus,
}: {
  value: string
  onChange: (path: string) => void
  // What the box is, for a screen reader. The <label> around it carries the
  // words on screen; the list is drawn into the body, out of that label's
  // reach, and needs its own.
  label: string
  placeholder?: string
  // A page that must not be offered: the one being merged, moved or linked.
  // Choosing it is refused on submit anyway, and offering it is offering a
  // mistake.
  exclude?: string
  autoFocus?: boolean
}) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [active, setActive] = useState(0)
  const [position, setPosition] = useState<React.CSSProperties>({})
  const input = useRef<HTMLInputElement>(null)
  const list = useRef<HTMLDivElement>(null)
  const id = useId()

  const typed = value.trim()
  const words = useDebounced(typed, settleDelay)
  // Asked only while the list is open, so that choosing a page does not send
  // the path it just settled on straight back as a question.
  const asking = open && words.length >= fewestLetters
  const found = useQuery<{ SearchAgentGraph: { nodes: FoundPage[] } } | null>(
    () =>
      asking
        ? graphql<{ SearchAgentGraph: { nodes: FoundPage[] } }>(SEARCH, { query: words, first: mostRows })
        : Promise.resolve(null),
    [asking, words],
    { refresh: false },
  )

  const rows = (found.data?.SearchAgentGraph.nodes ?? []).filter((page) => page.path !== exclude)
  // Whether the answer on hand is an answer to what is in the box. Between a
  // keystroke and the reply it is not, and saying "nothing matches" in that
  // gap would flash a failure on every third letter.
  const settling = typed !== words || found.loading
  const showing = open && typed.length >= fewestLetters

  // Placed from the box's rectangle and drawn into the body, so that a dialog
  // with its own scrolling does not clip it, and the same width as the box so
  // the two read as one control. The numbers are the ones the other two lists
  // in the dashboard use; a list that sat differently would look like a
  // different kind of thing.
  const place = useCallback(() => {
    const box = input.current?.getBoundingClientRect()
    if (!box) {
      return
    }
    const gap = 4
    const margin = 8
    const below = window.innerHeight - box.bottom - gap - margin
    const above = box.top - gap - margin
    // Upward where there is more room upward. A dialog sits in the middle of
    // a phone screen and the keyboard takes the half under it, which leaves a
    // list drawn below with nowhere to be.
    const upward = below < 200 && above > below
    const room = Math.max(80, Math.min(320, upward ? above : below))
    setPosition({
      left: Math.max(margin, Math.min(box.left, window.innerWidth - margin - box.width)),
      width: box.width,
      maxHeight: room,
      ...(upward ? { bottom: window.innerHeight - box.top + gap } : { top: box.bottom + gap }),
    })
  }, [])

  useLayoutEffect(() => {
    if (showing) {
      place()
    }
  }, [showing, place, rows.length])

  // The marked row is kept in sight. Twenty rows are more than the list can
  // show at once, so without this the arrow keys walked the mark off the
  // bottom and the person was choosing something they could no longer see.
  useEffect(() => {
    if (!showing) {
      return
    }
    list.current?.querySelector(`#${CSS.escape(`${id}-option-${active}`)}`)?.scrollIntoView({ block: 'nearest' })
  }, [showing, active, id])

  useEffect(() => {
    if (!showing) {
      return
    }
    // A page moving under an open list is not a reason to shut it. A phone
    // resizes its window whenever the keyboard slides up, and a list that
    // closed on that would close the moment it was typed into. It follows the
    // box instead, and gives up only once the box itself has scrolled out of
    // the window.
    const follow = (event: Event) => {
      if (event.target instanceof Node && list.current?.contains(event.target)) {
        return
      }
      if (event.type !== 'resize') {
        const box = input.current?.getBoundingClientRect()
        if (!box || box.bottom < 0 || box.top > window.innerHeight) {
          setOpen(false)
          return
        }
      }
      place()
    }
    const onPointerDown = (event: MouseEvent | TouchEvent) => {
      const target = event.target as Node
      if (input.current?.contains(target) || list.current?.contains(target)) {
        return
      }
      setOpen(false)
    }
    window.addEventListener('scroll', follow, true)
    window.addEventListener('resize', follow)
    document.addEventListener('mousedown', onPointerDown)
    document.addEventListener('touchstart', onPointerDown)
    return () => {
      window.removeEventListener('scroll', follow, true)
      window.removeEventListener('resize', follow)
      document.removeEventListener('mousedown', onPointerDown)
      document.removeEventListener('touchstart', onPointerDown)
    }
  }, [showing, place])

  const choose = (page: FoundPage) => {
    onChange(page.path)
    setOpen(false)
    input.current?.focus()
  }

  const onKeyDown = (event: React.KeyboardEvent) => {
    if (event.key === 'ArrowDown' && !showing) {
      // The way back to a list that was dismissed, without retyping.
      event.preventDefault()
      setOpen(true)
      setActive(0)
      return
    }
    if (!showing) {
      return
    }
    switch (event.key) {
      case 'Escape':
        // Taken here, and said to have been taken: the dialog around this
        // closes on an Escape nobody else wanted, and the first one is for
        // the list.
        event.preventDefault()
        setOpen(false)
        break
      case 'ArrowDown':
        event.preventDefault()
        setActive((previous) => Math.min(rows.length - 1, previous + 1))
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
        setActive(rows.length - 1)
        break
      case 'Enter':
        // Enter on a marked row takes it. Enter with nothing marked is left
        // alone, so that a path typed in full submits the dialog the way it
        // always did rather than being swallowed by a list of near misses.
        if (rows[active]) {
          event.preventDefault()
          choose(rows[active])
        }
        break
      case 'Tab':
        setOpen(false)
        break
    }
  }

  return (
    <>
      <input
        ref={input}
        role="combobox"
        aria-autocomplete="list"
        aria-expanded={showing}
        aria-controls={showing ? id : undefined}
        aria-activedescendant={showing && rows[active] ? `${id}-option-${active}` : undefined}
        aria-label={label}
        value={value}
        placeholder={placeholder ?? t('knowledge.pageSearchPlaceholder')}
        autoFocus={autoFocus}
        autoComplete="off"
        spellCheck={false}
        onChange={(event) => {
          onChange(event.target.value)
          setOpen(true)
          setActive(0)
        }}
        onKeyDown={onKeyDown}
      />
      {showing &&
        createPortal(
          <div className="select-list page-picker-list" id={id} ref={list} style={position}>
            <div role="listbox" aria-label={t('knowledge.pageSearchResults')}>
              {rows.map((page, index) => (
                <button
                  key={page.id}
                  id={`${id}-option-${index}`}
                  type="button"
                  role="option"
                  aria-selected={page.path === typed}
                  className={index === active ? 'page-picker-row active' : 'page-picker-row'}
                  onMouseEnter={() => setActive(index)}
                  // The box keeps the caret: losing it to the row being
                  // pressed would close the list before the press landed.
                  onMouseDown={(event) => event.preventDefault()}
                  onClick={() => choose(page)}
                >
                  <span className="page-picker-name">{page.name.trim() || page.path}</span>
                  {page.name.trim() !== '' && <span className="page-picker-path">{page.path}</span>}
                </button>
              ))}
            </div>
            {rows.length === 0 && (
              // Said out loud as well as drawn, because the person who most
              // needs to know the list came back empty is the one who cannot
              // see that it did.
              <p className="select-empty muted" role="status">
                {found.error
                  ? t('knowledge.pageSearchFailed')
                  : settling
                    ? t('knowledge.pageSearchLooking')
                    : t('knowledge.pageSearchNone', { words: typed })}
              </p>
            )}
          </div>,
          document.body,
        )}
    </>
  )
}

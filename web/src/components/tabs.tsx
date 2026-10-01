import { useEffect, useRef, useState } from 'react'

import { Key, useTranslation } from '../i18n/i18n'
import { Tooltip } from './tooltip'

// A row of tabs, each of which is a route.
//
// One component rather than the two copies that existed, on the server page
// and on a domain's. They had drifted already — only one of them had anywhere
// for an action to sit — and the scrolling below is the kind of thing that
// gets fixed in one copy and not the other.

// A tab can be unavailable — the compose page's template tab when the domain
// has no templates — and then it says why rather than only refusing.
export type TabItem = {
  id: string
  label: Key
  disabled?: boolean
  title?: string
  // Something on this tab wants attention — an upgrade waiting to be applied.
  // The rail shows a dot on the page; the page shows one on the tab, so that
  // arriving from the first dot does not end in a page of tabs and no reason.
  marked?: boolean
  markedLabel?: string
}

export function Tabs({
  items,
  active,
  onSelect,
  actions,
}: {
  items: TabItem[]
  active: string | undefined
  onSelect: (id: string) => void
  actions?: React.ReactNode
}) {
  const { t } = useTranslation()
  const strip = useRef<HTMLDivElement>(null)
  const [overflow, setOverflow] = useState({ hasMoreBefore: false, hasMoreAfter: false })

  // Which edges have tabs past them, for the fade that says so. A row cut
  // cleanly between two tabs gave no sign that it scrolled at all: at some
  // widths the last tab in view ended exactly at the edge. Read on scroll
  // and whenever the row or the window changes width.
  useEffect(() => {
    const row = strip.current
    if (!row) {
      return
    }
    const measure = () => {
      // A pixel of slack: a fractional scroll position never quite reaches
      // the end.
      const hasMoreBefore = row.scrollLeft > 1
      const hasMoreAfter = row.scrollLeft + row.clientWidth < row.scrollWidth - 1
      setOverflow((previous) =>
        previous.hasMoreBefore === hasMoreBefore && previous.hasMoreAfter === hasMoreAfter
          ? previous
          : { hasMoreBefore, hasMoreAfter },
      )
    }
    measure()
    row.addEventListener('scroll', measure, { passive: true })
    const observer = typeof ResizeObserver === 'undefined' ? null : new ResizeObserver(measure)
    observer?.observe(row)
    return () => {
      row.removeEventListener('scroll', measure)
      observer?.disconnect()
    }
  }, [items.length])

  // Bring the active tab into view when it is out of it.
  //
  // The row scrolls sideways on a narrow screen, and the tab you are on is
  // frequently not the part of it you can see: arriving on a domain's
  // Credentials tab from a link, or reloading the page there, left the strip
  // at its start showing Overview underlined by nothing. On a phone four of
  // the five tabs are off the edge, so the page gave no sign of which one it
  // was showing.
  //
  // The active tab is found in the DOM rather than held in a ref. A ref
  // attached conditionally — ref={id === active ? tab : undefined} — is
  // attached and detached in child order, so moving to an earlier tab sets it
  // to the new button and then nulls it again on the way past the old one. A
  // query cannot get that wrong.
  //
  // And scrollBy on the strip rather than scrollIntoView on the tab, because
  // scrollIntoView walks up to every scrollable ancestor: the content column
  // scrolls too, and a tab row has no business moving the page. Deltas from
  // getBoundingClientRect rather than offsetLeft, which is measured from the
  // nearest positioned ancestor and is not this.
  useEffect(() => {
    const row = strip.current
    if (!row) {
      return
    }
    const element = row.querySelector<HTMLElement>('button.active')
    if (!element) {
      return
    }
    const rowRect = row.getBoundingClientRect()
    const tabRect = element.getBoundingClientRect()

    // A margin, so the tab it scrolls to does not sit flush against the edge
    // looking like the row ends there.
    const margin = 12

    // No behavior: 'smooth'. It is silently dropped on this element — the
    // same call with the same delta moves the strip 229px as 'instant' and 0
    // as 'smooth' — so asking for it politely produced a tab row that never
    // scrolled at all. The default follows the CSS, which is what a reader
    // who has asked for less motion has already said they want.
    if (tabRect.left < rowRect.left) {
      row.scrollBy({ left: tabRect.left - rowRect.left - margin })
    } else if (tabRect.right > rowRect.right) {
      row.scrollBy({ left: tabRect.right - rowRect.right + margin })
    }
  }, [active])

  return (
    <div
      className={[
        'tabs',
        overflow.hasMoreBefore ? 'tabs-more-before' : '',
        overflow.hasMoreAfter ? 'tabs-more-after' : '',
      ]
        .filter(Boolean)
        .join(' ')}
      ref={strip}
    >
      {items.map((item) => (
        <Tooltip key={item.id} label={item.title ?? ''}>
          <button
            type="button"
            className={item.id === active ? 'active' : ''}
            aria-current={item.id === active ? 'page' : undefined}
            disabled={item.disabled}
            onClick={() => onSelect(item.id)}
          >
            {t(item.label)}
            {item.marked && (
              <span className="tab-dot">
                <span className="visually-hidden">{item.markedLabel ?? ''}</span>
              </span>
            )}
          </button>
        </Tooltip>
      ))}
      {actions}
    </div>
  )
}

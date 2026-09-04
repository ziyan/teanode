import { useEffect, useRef } from 'react'

import { Key, useTranslation } from '../i18n/i18n'

// A row of tabs, each of which is a route.
//
// One component rather than the two copies that existed, on the server page
// and on a domain's. They had drifted already — only one of them had anywhere
// for an action to sit — and the scrolling below is the kind of thing that
// gets fixed in one copy and not the other.

export type TabItem = { id: string; label: Key }

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
    <div className="tabs" ref={strip}>
      {items.map((item) => (
        <button
          key={item.id}
          type="button"
          className={item.id === active ? 'active' : ''}
          aria-current={item.id === active ? 'page' : undefined}
          onClick={() => onSelect(item.id)}
        >
          {t(item.label)}
        </button>
      ))}
      {actions}
    </div>
  )
}

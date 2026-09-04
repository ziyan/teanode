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
  const current = useRef<HTMLButtonElement>(null)

  // Bring the active tab into view when it is out of it.
  //
  // The row scrolls sideways on a narrow screen, and the tab you are on is
  // frequently not the part of it you can see: arriving on a domain's
  // Credentials tab from a link, or reloading the page there, left the strip
  // at its start showing Overview underlined by nothing. Worse on a phone,
  // where four of the five tabs are off the edge.
  //
  // scrollIntoView on the element rather than a computed offset, because the
  // browser knows what is visible and this does not need to. "nearest" so a
  // tab that is already in view does not move — centring every tab on every
  // navigation would make the row twitch for no reason — and block "nearest"
  // so the page itself never scrolls to do it.
  useEffect(() => {
    const element = current.current
    if (!element || !strip.current) {
      return
    }
    const row = strip.current.getBoundingClientRect()
    const tab = element.getBoundingClientRect()
    if (tab.left >= row.left && tab.right <= row.right) {
      return
    }
    element.scrollIntoView({ inline: 'nearest', block: 'nearest', behavior: 'smooth' })
  }, [active])

  return (
    <div className="tabs" ref={strip}>
      {items.map((item) => (
        <button
          key={item.id}
          ref={item.id === active ? current : undefined}
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

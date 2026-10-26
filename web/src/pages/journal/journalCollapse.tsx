import { useId, useState } from 'react'

import { ChevronDownIcon, ChevronRightIcon } from '../../components/icons'
import { Tooltip } from '../../components/tooltip'
import { useTranslation } from '../../i18n/i18n'
import { LIST_ROW_LIMIT } from './journalApi'

// What keeps a long page short: a section that folds up to its head and
// stays folded, a toggle for a part of a section, and a list that shows its
// first rows with a button for the rest.

const STORAGE_PREFIX = 'teanode.journal.collapsed.'

// Whether the person left a section folded, as this browser remembers it, or
// null when it was never touched. Storage can be missing or refuse (a private
// window, blocked site data), and then the page simply starts from its
// defaults and forgets on reload.
function readRemembered(id: string): boolean | null {
  try {
    const stored = window.localStorage.getItem(STORAGE_PREFIX + id)
    return stored === '1' ? true : stored === '0' ? false : null
  } catch {
    return null
  }
}

function writeRemembered(id: string, isCollapsed: boolean) {
  try {
    window.localStorage.setItem(STORAGE_PREFIX + id, isCollapsed ? '1' : '0')
  } catch {
    // Not remembering is the cost of a browser that will not keep it.
  }
}

// useRememberedCollapse is whether a part of the page is folded. The same
// part is folded the same way on every day, since a person who folds Work
// does not want it open again on the next day they read.
export function useRememberedCollapse(id: string, isCollapsedByDefault: boolean): [boolean, () => void] {
  const [isCollapsed, setIsCollapsed] = useState(() => readRemembered(id) ?? isCollapsedByDefault)
  const toggle = () => {
    const next = !isCollapsed
    setIsCollapsed(next)
    writeRemembered(id, next)
  }
  return [isCollapsed, toggle]
}

// CollapseButton is the chevron in the head of a section.
export function CollapseButton({
  title,
  isCollapsed,
  controls,
  onToggle,
}: {
  title: string
  isCollapsed: boolean
  controls: string
  onToggle: () => void
}) {
  const { t } = useTranslation()
  const label = t(isCollapsed ? 'journal.expandSection' : 'journal.collapseSection', { title })
  return (
    <Tooltip label={label}>
      <button
        type="button"
        className="icon-action journal-collapse"
        aria-expanded={!isCollapsed}
        aria-controls={controls}
        aria-label={label}
        onClick={onToggle}
      >
        {isCollapsed ? <ChevronRightIcon size={16} /> : <ChevronDownIcon size={16} />}
      </button>
    </Tooltip>
  )
}

// Disclosure is a part of a section that is folded until asked for: a word
// with a chevron, and what it shows under it.
export function Disclosure({
  id,
  label,
  isCollapsedByDefault = true,
  children,
}: {
  id: string
  label: string
  isCollapsedByDefault?: boolean
  children: React.ReactNode
}) {
  const [isCollapsed, toggle] = useRememberedCollapse(id, isCollapsedByDefault)
  const bodyId = useId()
  return (
    <div className="journal-disclosure">
      <button
        type="button"
        className="journal-disclosure-toggle"
        aria-expanded={!isCollapsed}
        aria-controls={bodyId}
        onClick={toggle}
      >
        {isCollapsed ? <ChevronRightIcon size={14} /> : <ChevronDownIcon size={14} />}
        {label}
      </button>
      <div id={bodyId} hidden={isCollapsed}>
        {isCollapsed ? null : children}
      </div>
    </div>
  )
}

export type RowLimit = {
  // How many of the rows to draw now.
  shownCount: number
  total: number
  // How many rows it shows before the button.
  limitCount: number
  isShowingAll: boolean
  toggle: () => void
}

// useRowLimit shows the first rows of a list, and all of them once asked.
// Called before a section decides it has nothing to show, like every hook.
export function useRowLimit(total: number, limit: number = LIST_ROW_LIMIT): RowLimit {
  const [isShowingAll, setIsShowingAll] = useState(false)
  return {
    shownCount: isShowingAll || total <= limit ? total : limit,
    total,
    limitCount: limit,
    isShowingAll,
    toggle: () => setIsShowingAll((previous) => !previous),
  }
}

// ShowAllButton is the foot of a list that has more rows than it shows, and
// nothing for one that has not.
export function ShowAllButton({ limit }: { limit: RowLimit }) {
  const { t } = useTranslation()
  if (limit.total <= limit.limitCount) return null
  return (
    <div className="journal-show-all">
      <button type="button" className="button-quiet" aria-expanded={limit.isShowingAll} onClick={limit.toggle}>
        {limit.isShowingAll ? t('journal.showFewer') : t('journal.showAll', { count: limit.total })}
      </button>
    </div>
  )
}

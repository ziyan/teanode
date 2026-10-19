import { useEffect, useRef, useState } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'

import { useTranslation } from '../i18n/i18n'
import { FOCUS_SEARCH_EVENT, searchPath } from '../searchEverything'
import { SearchIcon } from './icons'
import { Tooltip } from './tooltip'

// The results page, whose address holds the words.
const SEARCH_PAGE = '/search'

// SidebarSearch is the search box at the top of the rail: the words are looked
// for across the person's chats, mail, contacts and the rest, and Enter opens
// the results. Control-K and Command-K (and / on a page with no search box of
// its own) ask for it through FOCUS_SEARCH_EVENT.
//
// A rail that is narrowed to icons, or an overlay that is closed, has no room
// to type in: the request first opens it (onReveal) and the box takes focus
// once it is there, so the key does what it says wherever the person is.
export function SidebarSearch({
  isHidden,
  onReveal,
  onSubmitted,
}: {
  // Whether the rail is narrowed to icons or is an overlay that is closed.
  isHidden: boolean
  // Widens the rail or opens the overlay.
  onReveal: () => void
  // Called after Enter, so an overlay can close over the results.
  onSubmitted: () => void
}) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const location = useLocation()
  const input = useRef<HTMLInputElement>(null)
  const isWaitingToFocus = useRef(false)

  // The words follow the address while the results are shown, so Back, a
  // reload and a copied link show what was asked. Anywhere else the box is
  // empty, ready for the next question.
  const isOnSearchPage = location.pathname === SEARCH_PAGE
  const askedWords = isOnSearchPage ? (new URLSearchParams(location.search).get('q') ?? '') : ''
  const [typed, setTyped] = useState(askedWords)
  useEffect(() => setTyped(askedWords), [askedWords])

  const focusBox = () => {
    input.current?.focus()
    input.current?.select()
  }

  const askForFocus = () => {
    if (isHidden) {
      isWaitingToFocus.current = true
      onReveal()
      return
    }
    focusBox()
  }
  const latestAskForFocus = useRef(askForFocus)
  latestAskForFocus.current = askForFocus
  useEffect(() => {
    const ask = () => latestAskForFocus.current()
    window.addEventListener(FOCUS_SEARCH_EVENT, ask)
    return () => window.removeEventListener(FOCUS_SEARCH_EVENT, ask)
  }, [])

  useEffect(() => {
    if (isHidden || !isWaitingToFocus.current) return
    isWaitingToFocus.current = false
    focusBox()
  }, [isHidden])

  if (isHidden) {
    return (
      <div className="sidebar-search">
        <Tooltip label={t('search.title')}>
          <button type="button" className="sidebar-search-open" aria-label={t('search.title')} onClick={askForFocus}>
            <SearchIcon />
          </button>
        </Tooltip>
      </div>
    )
  }

  return (
    <form
      className="sidebar-search"
      role="search"
      onSubmit={(event) => {
        event.preventDefault()
        const words = typed.trim()
        if (words === '') return
        void navigate(searchPath(words))
        onSubmitted()
      }}
    >
      <input
        ref={input}
        type="search"
        value={typed}
        placeholder={t('search.placeholder')}
        aria-label={t('search.title')}
        onChange={(event) => setTyped(event.target.value)}
        onKeyDown={(event) => {
          if (event.key === 'Escape') event.currentTarget.blur()
        }}
      />
    </form>
  )
}

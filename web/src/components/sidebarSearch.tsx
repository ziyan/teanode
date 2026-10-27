import { useEffect, useId, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'

import { en } from '../i18n/en'
import { Key, translate, useTranslation } from '../i18n/i18n'
import { FOCUS_SEARCH_EVENT, searchPath } from '../searchEverything'
import { SearchIcon } from './icons'
import type { NavigablePage } from './sidebar'
import { Tooltip } from './tooltip'

// The results page, whose address holds the words.
const SEARCH_PAGE = '/search'

// How many pages the list offers before its last row.
const PAGE_SUGGESTION_LIMIT = 6

// How well the words match a label: 0 when the label starts with them, 1 when
// a word of the label does, and null otherwise. Letters inside a word are no
// match: a person's name typed to be searched for would otherwise jump to
// whichever page happens to hold those letters. Both arguments are lower case.
function matchRank(label: string, words: string): number | null {
  if (label.startsWith(words)) return 0
  if (label.split(/[\s\-_/]+/).some((part) => part.startsWith(words))) return 1
  return null
}

// suggestPages is the pages whose name the words are in, best match first and
// in the rail's order within a match. A page is matched by its name in the
// reader's language and by its English name, so "journal" finds the page
// whatever the language.
function suggestPages(
  pages: NavigablePage[],
  words: string,
  translatePage: (key: Key) => string,
): { page: NavigablePage; label: string }[] {
  const needle = words.trim().toLowerCase()
  if (needle === '') return []
  return pages
    .flatMap((page, order) => {
      const label = translatePage(page.label)
      const ranks = [matchRank(label.toLowerCase(), needle), matchRank(translate(en, page.label).toLowerCase(), needle)]
      const rank = Math.min(...ranks.filter((candidate): candidate is number => candidate !== null))
      return Number.isFinite(rank) ? [{ page, label, rank, order }] : []
    })
    .sort((left, right) => left.rank - right.rank || left.order - right.order)
    .slice(0, PAGE_SUGGESTION_LIMIT)
    .map(({ page, label }) => ({ page, label }))
}

// SidebarSearch is the search box at the top of the rail. As the words are
// typed it offers the dashboard's pages by name, the way Spotlight does, and
// Enter jumps to the one highlighted; the last row searches the person's
// chats, mail, contacts and the rest for the words and opens the results, and
// is what Enter does when no page has the words in its name. Control-K and
// Command-K (and / on a page with no search box of its own) ask for the box
// through FOCUS_SEARCH_EVENT.
//
// A rail that is narrowed to icons, or an overlay that is closed, has no room
// to type in: the request first opens it (onReveal) and the box takes focus
// once it is there, so the key does what it says wherever the person is.
export function SidebarSearch({
  pages,
  isHidden,
  onReveal,
  onSubmitted,
}: {
  // The pages the person may open, from navigablePages.
  pages: NavigablePage[]
  // Whether the rail is narrowed to icons or is an overlay that is closed.
  isHidden: boolean
  // Widens the rail or opens the overlay.
  onReveal: () => void
  // Called after Enter or a click on a row, so an overlay can close over the
  // page or the results.
  onSubmitted: () => void
}) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const location = useLocation()
  const listId = useId()
  const input = useRef<HTMLInputElement>(null)
  const isWaitingToFocus = useRef(false)

  // The words follow the address while the results are shown, so Back, a
  // reload and a copied link show what was asked. Anywhere else the box is
  // empty, ready for the next question.
  const isOnSearchPage = location.pathname === SEARCH_PAGE
  const askedWords = isOnSearchPage ? (new URLSearchParams(location.search).get('q') ?? '') : ''
  // What the person typed, apart from the rest of a page's name that the box
  // shows selected after it.
  const [typed, setTyped] = useState(askedWords)
  useEffect(() => setTyped(askedWords), [askedWords])

  const [isFocused, setFocused] = useState(false)
  // Escape closes the list until the next letter.
  const [isListDismissed, setListDismissed] = useState(false)
  const [highlightedIndex, setHighlightedIndex] = useState(0)
  // Whether the person has just typed a letter, which is when the rest of the
  // top page's name is offered after it. Deleting, moving the caret and
  // arriving in the box all leave the words as they are.
  const [isCompleting, setCompleting] = useState(false)
  const [isComposing, setComposing] = useState(false)

  const words = typed.trim()
  const suggestions = useMemo(() => suggestPages(pages, words, t), [pages, words, t])
  const isListShown = isFocused && words !== '' && !isListDismissed
  const rowCount = suggestions.length + 1
  const activeIndex = Math.min(highlightedIndex, rowCount - 1)

  // The rest of the top page's name when it starts with the typed words. The
  // page's name in the reader's language only: a page found by its English
  // name in another language has nothing to continue.
  const topLabel = suggestions[0]?.label ?? ''
  const completion =
    isListShown &&
    isCompleting &&
    !isComposing &&
    activeIndex === 0 &&
    typed !== '' &&
    topLabel.toLowerCase().startsWith(typed.toLowerCase())
      ? topLabel.slice(typed.length)
      : ''
  const shown = typed + completion

  // The completion is selected, so the next letter replaces it and Backspace
  // removes it. Set after the value, which puts the caret at the end.
  useLayoutEffect(() => {
    const box = input.current
    if (!box || completion === '' || document.activeElement !== box) return
    box.setSelectionRange(typed.length, typed.length + completion.length)
  }, [typed, completion])

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

  // Runs a row of the list: a page is opened and the box is emptied, the last
  // row opens the results for the words.
  const runRow = (index: number) => {
    if (words === '') return
    const suggestion = suggestions[index]
    setListDismissed(true)
    setCompleting(false)
    if (suggestion) {
      void navigate(suggestion.page.to)
      setTyped('')
      input.current?.blur()
    } else {
      void navigate(searchPath(words))
    }
    onSubmitted()
  }

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

  const optionId = (index: number) => `${listId}-option-${index}`

  return (
    <form
      className="sidebar-search"
      role="search"
      onSubmit={(event) => {
        event.preventDefault()
        runRow(activeIndex)
      }}
    >
      <input
        ref={input}
        type="search"
        role="combobox"
        aria-autocomplete="both"
        aria-expanded={isListShown}
        aria-controls={isListShown ? listId : undefined}
        aria-activedescendant={isListShown ? optionId(activeIndex) : undefined}
        autoComplete="off"
        spellCheck={false}
        value={shown}
        placeholder={t('search.placeholder')}
        aria-label={t('search.title')}
        onFocus={() => {
          setFocused(true)
          setCompleting(false)
        }}
        onBlur={() => {
          setFocused(false)
          setCompleting(false)
        }}
        onCompositionStart={() => setComposing(true)}
        onCompositionEnd={() => setComposing(false)}
        onChange={(event) => {
          const next = event.target.value
          const { inputType } = event.nativeEvent as InputEvent
          const isDeletion = inputType ? inputType.startsWith('delete') : next.length < typed.length
          setTyped(next)
          setCompleting(!isDeletion)
          setListDismissed(false)
          setHighlightedIndex(0)
        }}
        onKeyDown={(event) => {
          if (event.nativeEvent.isComposing) return
          switch (event.key) {
            case 'Escape':
              // The first closes the list, the next lets go of the box.
              if (isListShown) {
                event.preventDefault()
                event.stopPropagation()
                setListDismissed(true)
              } else {
                event.currentTarget.blur()
              }
              break
            case 'ArrowDown':
              if (!isListShown) break
              event.preventDefault()
              setHighlightedIndex(Math.min(rowCount - 1, activeIndex + 1))
              break
            case 'ArrowUp':
              if (!isListShown) break
              event.preventDefault()
              setHighlightedIndex(Math.max(0, activeIndex - 1))
              break
            case 'Tab':
            case 'ArrowRight':
              // Takes the rest of the name the box is offering.
              if (completion === '' || event.shiftKey || event.altKey || event.ctrlKey || event.metaKey) break
              event.preventDefault()
              setTyped(shown)
              setCompleting(false)
              // The offered letters are still selected, and the next letter
              // would replace them.
              event.currentTarget.setSelectionRange(shown.length, shown.length)
              break
            case 'ArrowLeft':
            case 'Home':
              // The caret goes back into the words, not into a name that was
              // only offered.
              if (completion === '') break
              event.preventDefault()
              setCompleting(false)
              break
          }
        }}
      />
      {isListShown && (
        <div className="sidebar-search-list" id={listId} role="listbox" aria-label={t('search.title')}>
          {suggestions.map(({ page, label }, index) => (
            <button
              key={`${page.to} ${page.label}`}
              id={optionId(index)}
              type="button"
              role="option"
              tabIndex={-1}
              aria-selected={index === activeIndex}
              className={index === activeIndex ? 'sidebar-search-row active' : 'sidebar-search-row'}
              onMouseEnter={() => setHighlightedIndex(index)}
              // The box keeps the caret: losing it to the row being pressed
              // would close the list before the press landed.
              onMouseDown={(event) => event.preventDefault()}
              onClick={() => runRow(index)}
            >
              <span className="sidebar-icon">{page.icon}</span>
              <span className="sidebar-search-label">{label}</span>
            </button>
          ))}
          <button
            id={optionId(suggestions.length)}
            type="button"
            role="option"
            tabIndex={-1}
            aria-selected={suggestions.length === activeIndex}
            className={
              suggestions.length === activeIndex
                ? 'sidebar-search-row sidebar-search-everything active'
                : 'sidebar-search-row sidebar-search-everything'
            }
            onMouseEnter={() => setHighlightedIndex(suggestions.length)}
            onMouseDown={(event) => event.preventDefault()}
            onClick={() => runRow(suggestions.length)}
          >
            <span className="sidebar-icon">
              <SearchIcon />
            </span>
            <span className="sidebar-search-label">{t('search.everythingFor', { words })}</span>
          </button>
        </div>
      )}
    </form>
  )
}

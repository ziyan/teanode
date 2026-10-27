import { useEffect, useId, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'

import type { MailboxFolder } from '../api'
import { en } from '../i18n/en'
import { Key, translate, useTranslation } from '../i18n/i18n'
import { folderLabel } from '../mailboxes'
import { FOCUS_SEARCH_EVENT, searchPath } from '../searchEverything'
import { FolderKindIcon } from './folderIcon'
import { SearchIcon } from './icons'
import type { NavigablePage } from './sidebar'
import { Tooltip } from './tooltip'

// The results page, whose address holds the words.
const SEARCH_PAGE = '/search'

// How many rows the list offers before its last row. Six cut off the pages a
// word names once the tabs came in: "settings" is a page and the Settings tab
// of three more.
const SUGGESTION_LIMIT = 8

// The folders of a rail with no mailbox.
const NO_FOLDERS: MailboxFolder[] = []

// What stands between a page's name and the name of a tab or folder in it.
const SEPARATOR = ' › '

// How well the words match a label: 0 when the label starts with them, 1 when
// a word of the label does, and null otherwise. Letters inside a word are no
// match: a person's name typed to be searched for would otherwise jump to
// whichever page happens to hold those letters. Both arguments are lower case.
function matchRank(label: string, words: string): number | null {
  if (label.startsWith(words)) return 0
  if (label.split(/[\s\-_/]+/).some((part) => part.startsWith(words))) return 1
  return null
}

// A row of the list: a page, or a place inside one (a tab, a mailbox folder).
type Suggestion = {
  to: string
  icon: React.ReactNode
  // The name in the reader's language, with the page's before it for a place
  // inside one.
  label: string
  isPage: boolean
}

// How well the words match a place inside a page. Its own name matches as a
// page's does ("travel"); so do the page's name and the start of the place's,
// said together ("loc tr", "journal set"), the page's name by its first
// letters or in full. Both are matched in the reader's language and in
// English, and the words are cut at each space to find the two halves.
function matchPlaceRank(pageLabels: string[], placeLabels: string[], words: string): number | null {
  const ranks: number[] = []
  const add = (rank: number | null) => {
    if (rank !== null) ranks.push(rank)
  }
  for (const placeLabel of placeLabels) add(matchRank(placeLabel, words))
  for (const gap of words.matchAll(/\s+/g)) {
    const pageWords = words.slice(0, gap.index)
    const placeWords = words.slice(gap.index + gap[0].length)
    if (pageWords === '' || placeWords === '') continue
    if (!pageLabels.some((pageLabel) => pageLabel.startsWith(pageWords))) continue
    for (const placeLabel of placeLabels) add(matchRank(placeLabel, placeWords))
  }
  return ranks.length > 0 ? Math.min(...ranks) : null
}

// suggestRows is the rows the words are in the name of: first the pages, then
// the tabs of the pages and the folders of the mailbox, each best match first
// and in the order the rail draws them within a match. A name is matched in
// the reader's language and in English, so "journal" finds the page whatever
// the language.
function suggestRows(
  pages: NavigablePage[],
  folders: MailboxFolder[],
  words: string,
  translateLabel: (key: Key) => string,
): Suggestion[] {
  const needle = words.trim().toLowerCase()
  if (needle === '') return []
  const translateEnglish = (key: Key) => translate(en, key)
  const candidates: { suggestion: Suggestion; rank: number | null }[] = []
  for (const page of pages) {
    const label = translateLabel(page.label)
    const ranks = [
      matchRank(label.toLowerCase(), needle),
      matchRank(translateEnglish(page.label).toLowerCase(), needle),
    ]
    const rank = Math.min(...ranks.filter((candidate): candidate is number => candidate !== null))
    candidates.push({
      suggestion: { to: page.to, icon: page.icon, label, isPage: true },
      rank: Number.isFinite(rank) ? rank : null,
    })
  }
  const pageLabelsOf = (key: Key) => [translateLabel(key).toLowerCase(), translateEnglish(key).toLowerCase()]
  for (const page of pages) {
    const pageLabels = pageLabelsOf(page.label)
    for (const tab of page.tabs) {
      const placeLabels = [translateLabel(tab.label).toLowerCase(), translateEnglish(tab.label).toLowerCase()]
      candidates.push({
        suggestion: {
          to: tab.to,
          icon: page.icon,
          label: translateLabel(page.label) + SEPARATOR + translateLabel(tab.label),
          isPage: false,
        },
        rank: matchPlaceRank(pageLabels, placeLabels, needle),
      })
    }
  }
  const mailboxLabels = pageLabelsOf('nav.mailbox')
  for (const folder of folders) {
    const name = folderLabel(translateLabel, folder)
    const placeLabels = [name.toLowerCase(), folderLabel(translateEnglish, folder).toLowerCase()]
    candidates.push({
      suggestion: {
        to: `/mailbox/${folder.id}`,
        icon: <FolderKindIcon kind={folder.kind} />,
        label: translateLabel('nav.mailbox') + SEPARATOR + name,
        isPage: false,
      },
      rank: matchPlaceRank(mailboxLabels, placeLabels, needle),
    })
  }
  return candidates
    .flatMap(({ suggestion, rank }, order) => (rank === null ? [] : [{ suggestion, rank, order }]))
    .sort(
      (left, right) =>
        Number(!left.suggestion.isPage) - Number(!right.suggestion.isPage) ||
        left.rank - right.rank ||
        left.order - right.order,
    )
    .slice(0, SUGGESTION_LIMIT)
    .map(({ suggestion }) => suggestion)
}

// SidebarSearch is the search box at the top of the rail. As the words are
// typed it offers the dashboard's pages by name, the way Spotlight does, then
// the tabs inside the pages and the folders of the person's mailbox, and Enter
// jumps to the one highlighted; the last row searches the person's chats,
// mail, contacts and the rest for the words and opens the results, and is what
// Enter does when nothing has the words in its name. Control-K and
// Command-K (and / on a page with no search box of its own) ask for the box
// through FOCUS_SEARCH_EVENT.
//
// A rail that is narrowed to icons, or an overlay that is closed, has no room
// to type in: the request first opens it (onReveal) and the box takes focus
// once it is there, so the key does what it says wherever the person is.
export function SidebarSearch({
  pages,
  folders = NO_FOLDERS,
  isHidden,
  onReveal,
  onSubmitted,
}: {
  // The pages the person may open, with their tabs, from navigablePages.
  pages: NavigablePage[]
  // The folders of the person's mailbox, as the rail was given them.
  folders?: MailboxFolder[]
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
  const suggestions = useMemo(() => suggestRows(pages, folders, words, t), [pages, folders, words, t])
  const isListShown = isFocused && words !== '' && !isListDismissed
  const rowCount = suggestions.length + 1
  const activeIndex = Math.min(highlightedIndex, rowCount - 1)

  // The rest of the top page's name when it starts with the typed words. The
  // page's name in the reader's language only: a page found by its English
  // name in another language has nothing to continue. A tab or a folder is
  // not completed: its name starts with the page's, and the box would be
  // offering words nobody typed.
  const topLabel = suggestions[0]?.isPage ? suggestions[0].label : ''
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

  // Runs a row of the list: a page, a tab or a folder is opened and the box is
  // emptied, the last row opens the results for the words.
  const runRow = (index: number) => {
    if (words === '') return
    const suggestion = suggestions[index]
    setListDismissed(true)
    setCompleting(false)
    if (suggestion) {
      void navigate(suggestion.to)
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
          {suggestions.map(({ to, icon, label }, index) => (
            <button
              key={`${to} ${label}`}
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
              <span className="sidebar-icon">{icon}</span>
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

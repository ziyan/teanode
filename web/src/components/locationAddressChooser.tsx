import { useState } from 'react'

import { graphql } from '../api'
import { useTranslation } from '../i18n/i18n'
import { ConfirmDialog } from './dialog'
import { FoundPlace } from './locationData'

const SEARCH_ADDRESSES = `query ($query: String!, $placeId: String) {
  SearchAddresses(query: $query, placeId: $placeId) {
    placeName osmType osmId osmCategory addressText latitudeDegrees longitudeDegrees distanceMeters placeId placeLabel isNamedByPerson
  }
}`

// LocationAddressChooser asks for an address in words, lists what the place
// provider found, and hands the one the person presses to onChoose. A failed
// search, or a choice onChoose refuses by throwing, is shown in the dialog,
// which stays so the words can be changed and the press tried again. The
// caller closes the dialog (onClose) once the choice is saved.
//
// OpenStreetMap does not know every street. Once something was typed and
// searched, onKeepTyped (when the caller gives it) offers to keep the words
// as they were typed, without moving anything. A caller that needs a position
// and cannot do that (home in the setup) gives notFoundNote instead, which is
// said when nothing matched.
//
// With placeId (the place whose address is being changed) the matches come
// nearest to that place first; without it, nearest to where the person last
// was.
export function LocationAddressChooser({
  title,
  onChoose,
  onKeepTyped,
  notFoundNote,
  placeId,
  onClose,
}: {
  title: string
  onChoose: (match: FoundPlace) => Promise<void>
  onKeepTyped?: (addressText: string) => Promise<void>
  notFoundNote?: string
  placeId?: string
  onClose: () => void
}) {
  const { t } = useTranslation()
  const [queryText, setQueryText] = useState('')
  const [matches, setMatches] = useState<FoundPlace[] | null>(null)
  // The words the matches are for; the input may have been changed since.
  const [searchedText, setSearchedText] = useState('')
  const [isBusy, setIsBusy] = useState(false)
  const [problem, setProblem] = useState<string | null>(null)

  async function search() {
    const query = queryText.trim()
    if (query === '') return
    setIsBusy(true)
    setProblem(null)
    try {
      const answer = await graphql<{ SearchAddresses: FoundPlace[] }>(SEARCH_ADDRESSES, { query, placeId })
      setMatches(answer.SearchAddresses)
      setSearchedText(query)
    } catch (failure) {
      setProblem(failure instanceof Error && failure.message ? failure.message : t('location.addressSearchFailed'))
    } finally {
      setIsBusy(false)
    }
  }

  async function choose(save: () => Promise<void>) {
    setIsBusy(true)
    setProblem(null)
    try {
      await save()
    } catch (failure) {
      setProblem(failure instanceof Error && failure.message ? failure.message : t('location.placeFailed'))
      setIsBusy(false)
    }
  }

  return (
    <ConfirmDialog
      title={title}
      busy={isBusy}
      error={problem}
      onClose={onClose}
      body={
        <>
          <form
            className="location-find"
            onSubmit={(event) => {
              event.preventDefault()
              void search()
            }}
          >
            <input
              type="search"
              value={queryText}
              autoFocus
              aria-label={t('location.addressQuery')}
              placeholder={t('location.addressQuery')}
              onChange={(event) => setQueryText(event.target.value)}
            />
            <button type="submit" disabled={isBusy || queryText.trim() === ''}>
              {t('location.findPlaceButton')}
            </button>
          </form>
          {matches !== null && matches.length === 0 && <p className="muted">{t('location.addressNoMatch')}</p>}
          {matches !== null && matches.length === 0 && notFoundNote && <p className="muted">{notFoundNote}</p>}
          {matches !== null && matches.length > 0 && (
            <div className="location-nearby">
              {matches.map((match, index) => (
                <button
                  key={`${match.osmType}:${match.osmId ?? index}:${match.addressText}`}
                  type="button"
                  className="location-row"
                  disabled={isBusy}
                  onClick={() => void choose(() => onChoose(match))}
                >
                  <span className="location-row-body">
                    <strong>{match.addressText}</strong>
                  </span>
                  {match.distanceMeters > 0 && (
                    <span className="muted location-row-meta">
                      {t('location.metersAway', { distance: Math.round(match.distanceMeters) })}
                    </span>
                  )}
                </button>
              ))}
            </div>
          )}
          {matches !== null && onKeepTyped && (
            <div className="location-keep-typed">
              <button type="button" disabled={isBusy} onClick={() => void choose(() => onKeepTyped(searchedText))}>
                {t('location.keepAddressTyped')}
              </button>
              <span className="muted">{t('location.keepAddressTypedNote', { address: searchedText })}</span>
            </div>
          )}
          <p className="muted location-credit">{t('location.credit')}</p>
        </>
      }
    />
  )
}

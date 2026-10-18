import { useEffect, useState } from 'react'

import { graphql } from '../api'
import { useTranslation } from '../i18n/i18n'
import { COARSE_ACCURACY_METERS, readFreshBrowserLocation } from './browserLocation'
import { ConfirmDialog } from './dialog'
import { FoundPlace, foundPlaceNamed, placeSubtitle, placeTitle } from './locationData'
import { PlaceLabelIcon } from './placeLabelIcon'
import { useToast } from './toast'

const LIST_NEARBY_PLACES = `query ($latitudeDegrees: Float!, $longitudeDegrees: Float!) {
  ListNearbyPlaces(latitudeDegrees: $latitudeDegrees, longitudeDegrees: $longitudeDegrees) {
    placeName osmType osmId osmCategory addressText latitudeDegrees longitudeDegrees distanceMeters placeId placeLabel isNamedByPerson
  }
}`
const SEARCH_PLACES = `query ($query: String!, $latitudeDegrees: Float, $longitudeDegrees: Float, $offset: Int) {
  SearchPlaces(query: $query, latitudeDegrees: $latitudeDegrees, longitudeDegrees: $longitudeDegrees, offset: $offset) {
    foundPlaces {
      placeName osmType osmId osmCategory addressText latitudeDegrees longitudeDegrees distanceMeters placeId placeLabel isNamedByPerson
    }
    nextOffset
  }
}`
const CHECK_IN = `mutation ($placeId: String, $latitudeDegrees: Float, $longitudeDegrees: Float, $placeName: String,
  $osmType: String, $osmId: Int64, $osmCategory: String, $addressText: String) {
  CheckIn(placeId: $placeId, latitudeDegrees: $latitudeDegrees, longitudeDegrees: $longitudeDegrees,
    placeName: $placeName, osmType: $osmType, osmId: $osmId, osmCategory: $osmCategory, addressText: $addressText) { id }
}`

type Position = { latitudeDegrees: number; longitudeDegrees: number }
type SearchPage = { foundPlaces: FoundPlace[]; nextOffset: number | null }

// LocationCheckInDialog asks the browser where it is (a fresh, precise fix),
// says how precise it is, lists "Check in here" and then what has a name near
// there, nearest first, and records that the person is at the one they press.
// "Check in here" sends only the coordinates; the server uses the person's own
// place that holds them, or makes one. "Find a place" searches by words, around the same position, and its
// answers are checked in to the same way. A failure to find where the person
// is, or what is near, is a toast and the dialog goes: there is nothing in it
// to correct, and a refusal of the browser's question is not something to
// retry from inside it. A failed search or check-in is shown in the dialog,
// which stays, since the words can be changed and the press tried again.
export function LocationCheckInDialog({ onClose, onCheckedIn }: { onClose: () => void; onCheckedIn: () => void }) {
  const { t } = useTranslation()
  const toast = useToast()
  const [position, setPosition] = useState<Position | null>(null)
  const [accuracyMeters, setAccuracyMeters] = useState<number | null>(null)
  const [foundPlaces, setFoundPlaces] = useState<FoundPlace[] | null>(null)
  const [searchText, setSearchText] = useState('')
  // What was searched for, and what it found; null while the nearby places are
  // what is shown.
  const [searched, setSearched] = useState<{
    query: string
    foundPlaces: FoundPlace[]
    nextOffset: number | null
  } | null>(null)
  const [isBusy, setIsBusy] = useState(false)
  const [problem, setProblem] = useState<string | null>(null)

  useEffect(() => {
    let isCurrent = true
    void (async () => {
      const browserPosition = await readFreshBrowserLocation()
      if (!isCurrent) return
      if (browserPosition.latitudeDegrees === undefined || browserPosition.longitudeDegrees === undefined) {
        toast.failed(t('location.checkInNoPosition'))
        onClose()
        return
      }
      const here = {
        latitudeDegrees: browserPosition.latitudeDegrees,
        longitudeDegrees: browserPosition.longitudeDegrees,
      }
      setPosition(here)
      setAccuracyMeters(browserPosition.accuracyMeters ?? null)
      try {
        const answer = await graphql<{ ListNearbyPlaces: FoundPlace[] }>(LIST_NEARBY_PLACES, here)
        if (isCurrent) setFoundPlaces(answer.ListNearbyPlaces)
      } catch (failure) {
        if (!isCurrent) return
        toast.failure(failure, t('location.checkInFailed'))
        onClose()
      }
    })()
    return () => {
      isCurrent = false
    }
    // Asked once, when the dialog opens.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  async function search(query: string, offset: number) {
    setIsBusy(true)
    setProblem(null)
    try {
      const answer = await graphql<{ SearchPlaces: SearchPage }>(SEARCH_PLACES, { query, offset, ...position })
      setSearched((previous) => ({
        query,
        foundPlaces:
          offset > 0 && previous
            ? [...previous.foundPlaces, ...answer.SearchPlaces.foundPlaces]
            : answer.SearchPlaces.foundPlaces,
        nextOffset: answer.SearchPlaces.nextOffset,
      }))
    } catch (failure) {
      setProblem(failure instanceof Error && failure.message ? failure.message : t('location.checkInFailed'))
    } finally {
      setIsBusy(false)
    }
  }

  function submitSearch() {
    const query = searchText.trim()
    if (query === '') {
      setSearched(null)
      return
    }
    void search(query, 0)
  }

  async function checkIn(foundPlace: FoundPlace | null) {
    setIsBusy(true)
    setProblem(null)
    try {
      await graphql(
        CHECK_IN,
        foundPlace === null
          ? { latitudeDegrees: position?.latitudeDegrees, longitudeDegrees: position?.longitudeDegrees }
          : foundPlace.placeId
            ? { placeId: foundPlace.placeId }
            : {
                latitudeDegrees: foundPlace.latitudeDegrees,
                longitudeDegrees: foundPlace.longitudeDegrees,
                placeName: foundPlace.placeName,
                osmType: foundPlace.osmType,
                osmId: foundPlace.osmId,
                osmCategory: foundPlace.osmCategory,
                addressText: foundPlace.addressText,
              },
      )
      toast.done(
        foundPlace === null
          ? t('location.checkedInHere')
          : t('location.checkedIn', { place: placeTitle(foundPlaceNamed(foundPlace), t) }),
      )
      onCheckedIn()
      onClose()
    } catch (failure) {
      setProblem(failure instanceof Error && failure.message ? failure.message : t('location.checkInFailed'))
      setIsBusy(false)
    }
  }

  const shownPlaces = searched ? searched.foundPlaces : foundPlaces

  return (
    <ConfirmDialog
      title={t('location.checkInTitle')}
      busy={isBusy}
      error={problem}
      onClose={onClose}
      body={
        <>
          <form
            className="location-find"
            onSubmit={(event) => {
              event.preventDefault()
              submitSearch()
            }}
          >
            <input
              type="search"
              value={searchText}
              aria-label={t('location.findPlace')}
              placeholder={t('location.findPlace')}
              disabled={position === null}
              onChange={(event) => setSearchText(event.target.value)}
            />
            <button type="submit" disabled={isBusy || position === null}>
              {t('location.findPlaceButton')}
            </button>
          </form>
          {position !== null && accuracyMeters !== null && (
            <p className="muted">
              {accuracyMeters > COARSE_ACCURACY_METERS
                ? t('location.checkInCoarse', { accuracyMeters: Math.round(accuracyMeters) })
                : t('location.checkInPrecision', { accuracyMeters: Math.round(accuracyMeters) })}
            </p>
          )}
          {position !== null && !searched && (
            <div className="location-nearby">
              <button type="button" className="location-row" disabled={isBusy} onClick={() => void checkIn(null)}>
                <span className="location-row-icon">
                  <PlaceLabelIcon placeLabel="" />
                </span>
                <span className="location-row-body">
                  <strong>{t('location.checkInHere')}</strong>
                  <span className="muted">{t('location.checkInHereNote')}</span>
                </span>
              </button>
            </div>
          )}
          {shownPlaces === null && <p className="muted">{t('location.checkInFinding')}</p>}
          {shownPlaces !== null && shownPlaces.length === 0 && (
            <p className="muted">{t(searched ? 'location.placesNoMatch' : 'location.checkInNothing')}</p>
          )}
          {shownPlaces !== null && shownPlaces.length > 0 && (
            <div className="location-nearby">
              {shownPlaces.map((foundPlace, index) => (
                <button
                  key={`${foundPlace.osmType}:${foundPlace.osmId ?? index}:${foundPlace.placeName}`}
                  type="button"
                  className="location-row"
                  disabled={isBusy}
                  onClick={() => void checkIn(foundPlace)}
                >
                  <span className="location-row-icon">
                    <PlaceLabelIcon placeLabel={foundPlace.placeLabel} />
                  </span>
                  <span className="location-row-body">
                    <strong>{placeTitle(foundPlaceNamed(foundPlace), t)}</strong>
                    {(placeSubtitle(foundPlaceNamed(foundPlace), t) || foundPlace.addressText) && (
                      <span className="muted">
                        {placeSubtitle(foundPlaceNamed(foundPlace), t) || foundPlace.addressText}
                      </span>
                    )}
                  </span>
                  <span className="muted location-row-meta">
                    {t('location.metersAway', { distance: Math.round(foundPlace.distanceMeters) })}
                  </span>
                </button>
              ))}
              {searched?.nextOffset != null && (
                <button
                  type="button"
                  disabled={isBusy}
                  onClick={() => void search(searched.query, searched.nextOffset ?? 0)}
                >
                  {t('location.findPlaceMore')}
                </button>
              )}
            </div>
          )}
          <p className="muted location-credit">{t('location.credit')}</p>
        </>
      }
    />
  )
}

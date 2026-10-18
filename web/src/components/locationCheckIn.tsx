import { useEffect, useState } from 'react'

import { graphql } from '../api'
import { useTranslation } from '../i18n/i18n'
import { COARSE_ACCURACY_METERS, readFreshBrowserLocation, UNUSABLE_ACCURACY_METERS } from './browserLocation'
import { ConfirmDialog } from './dialog'
import { LocationAddressChooser } from './locationAddressChooser'
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
  $osmType: String, $osmId: Int64, $osmCategory: String, $addressText: String, $accuracyMeters: Float) {
  CheckIn(placeId: $placeId, latitudeDegrees: $latitudeDegrees, longitudeDegrees: $longitudeDegrees,
    accuracyMeters: $accuracyMeters, placeName: $placeName, osmType: $osmType, osmId: $osmId, osmCategory: $osmCategory, addressText: $addressText) { id }
}`

type Position = { latitudeDegrees: number; longitudeDegrees: number }
type SearchPage = { foundPlaces: FoundPlace[]; nextOffset: number | null }

// LocationCheckInDialog asks the browser where it is (a fresh, precise fix),
// says how precise it is, lists "Check in here" and then what has a name near
// there, nearest first, and records that the person is at the one they press.
// "Check in here" sends the coordinates and how precise the browser said they
// are; the server uses the person's own place that holds them, or makes one.
// A browser sure only to a few hundred meters (a computer has no GPS) is
// said so plainly, with "Type my address" beside it, which checks in to the
// address typed instead; past a kilometer "Check in here" is off.
// "Find a place" searches by words, around the same position, and its
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
  const [isTypingAddress, setIsTypingAddress] = useState(false)
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

  // sendCheckIn records the check-in and closes the dialog; it throws when the
  // server refuses, for the caller to show where it is standing.
  async function sendCheckIn(foundPlace: FoundPlace | null) {
    await graphql(
      CHECK_IN,
      foundPlace === null
        ? {
            latitudeDegrees: position?.latitudeDegrees,
            longitudeDegrees: position?.longitudeDegrees,
            accuracyMeters: accuracyMeters ?? undefined,
          }
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
  }

  async function checkIn(foundPlace: FoundPlace | null) {
    setIsBusy(true)
    setProblem(null)
    try {
      await sendCheckIn(foundPlace)
    } catch (failure) {
      setProblem(failure instanceof Error && failure.message ? failure.message : t('location.checkInFailed'))
      setIsBusy(false)
    }
  }

  const shownPlaces = searched ? searched.foundPlaces : foundPlaces
  const isCoarse = accuracyMeters !== null && accuracyMeters > COARSE_ACCURACY_METERS
  const isUnusable = accuracyMeters !== null && accuracyMeters > UNUSABLE_ACCURACY_METERS

  if (isTypingAddress) {
    return (
      <LocationAddressChooser
        title={t('location.setupHomeTypeButton')}
        onChoose={sendCheckIn}
        onClose={() => setIsTypingAddress(false)}
      />
    )
  }

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
          {position !== null && accuracyMeters !== null && !isCoarse && (
            <p className="muted">{t('location.checkInPrecision', { accuracyMeters: Math.round(accuracyMeters) })}</p>
          )}
          {position !== null && accuracyMeters !== null && isCoarse && (
            <div className="location-keep-typed">
              <p className="muted">{t('location.browserCoarse', { accuracyMeters: Math.round(accuracyMeters) })}</p>
              <button type="button" disabled={isBusy} onClick={() => setIsTypingAddress(true)}>
                {t('location.setupHomeTypeButton')}
              </button>
            </div>
          )}
          {position !== null && !searched && (
            <div className="location-nearby">
              <button
                type="button"
                className="location-row"
                disabled={isBusy || isUnusable}
                onClick={() => void checkIn(null)}
              >
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

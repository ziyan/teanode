import { useEffect, useState } from 'react'
import { ConfirmDialog } from '../../components/dialog'
import { useToast } from '../../components/toast'
import { useTranslation } from '../../i18n/i18n'
import { removePhotoLocation, searchPhotoPlaces, setPhotoLocation } from '../../photos/api'
import type { Photo, PhotoPlace } from '../../photos/api'

// How long typing has to stop before the cities are asked for, so that a word
// typed at speed is one search and not one for each letter.
const TYPING_PAUSE_MS = 300

const hasLocation = (photo: Photo) => typeof photo.latitude === 'number' && typeof photo.longitude === 'number'

// ChangeLocationDialog says where photos were taken: a search over cities,
// nearest to where the first photo that has a position was taken, and the
// matches to pick from. The photos move to the one picked. Remove location
// takes the place off the photos instead, and is there while any of them has
// one; it asks first, in the same dialog, because the photos forget it for
// good. The dialog makes the calls and says how they went in a toast; the
// caller is told once something changed.
export function ChangeLocationDialog({
  photos,
  onChanged,
  onClose,
}: {
  photos: Photo[]
  onChanged: () => void
  onClose: () => void
}) {
  const { t, plural, language } = useTranslation()
  const toast = useToast()
  const [searchText, setSearchText] = useState('')
  const [places, setPlaces] = useState<PhotoPlace[] | null>(null)
  const [isBusy, setIsBusy] = useState(false)
  const [isConfirmingRemoval, setIsConfirmingRemoval] = useState(false)
  const photoIds = photos.map((photo) => photo.id)
  const placedPhoto = photos.find(hasLocation)
  const nearLatitude = placedPhoto?.latitude ?? undefined
  const nearLongitude = placedPhoto?.longitude ?? undefined
  const query = searchText.trim()
  const distanceFormat = new Intl.NumberFormat(language, { maximumFractionDigits: 0 })
  const sharedNames = new Set(
    (places ?? []).map((place) => place.placeName).filter((name, index, names) => names.indexOf(name) !== index),
  )

  useEffect(() => {
    if (query === '') return
    // An answer to words that are no longer the ones typed is not shown.
    const asked = new AbortController()
    const timer = window.setTimeout(() => {
      const near =
        nearLatitude !== undefined && nearLongitude !== undefined
          ? { latitude: nearLatitude, longitude: nearLongitude }
          : undefined
      searchPhotoPlaces(query, near, asked.signal).then(
        (found) => {
          if (!asked.signal.aborted) setPlaces(found)
        },
        (caught) => {
          if (!asked.signal.aborted) toast.failure(caught, t('photos.actionFailed'))
        },
      )
    }, TYPING_PAUSE_MS)
    return () => {
      window.clearTimeout(timer)
      asked.abort()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [query, nearLatitude, nearLongitude])

  function typeSearch(text: string) {
    setSearchText(text)
    if (text.trim() === '') setPlaces(null)
  }

  async function choose(place: PhotoPlace) {
    setIsBusy(true)
    try {
      const changedCount = await setPhotoLocation(photoIds, place.latitude, place.longitude)
      toast.done(plural(changedCount, { one: 'photos.locationChangedOne', other: 'photos.locationChangedOther' }))
      onChanged()
      onClose()
    } catch (caught) {
      // The photos go in batches of 500, so the batches before the one that
      // failed may have changed: the caller reloads either way.
      onChanged()
      toast.failure(caught, t('photos.actionFailed'))
      setIsBusy(false)
    }
  }

  async function remove() {
    setIsBusy(true)
    try {
      const changedCount = await removePhotoLocation(photoIds)
      toast.done(plural(changedCount, { one: 'photos.locationRemovedOne', other: 'photos.locationRemovedOther' }))
      onChanged()
      onClose()
    } catch (caught) {
      // The photos go in batches of 500, so the batches before the one that
      // failed may have changed: the caller reloads either way.
      onChanged()
      toast.failure(caught, t('photos.actionFailed'))
      setIsBusy(false)
    }
  }

  if (isConfirmingRemoval) {
    return (
      <ConfirmDialog
        title={t('photos.changeLocation')}
        body={
          <>
            <p>
              {plural(photos.length, {
                one: 'photos.removeLocationQuestionOne',
                other: 'photos.removeLocationQuestionOther',
              })}
            </p>
            <p className="muted">{t('photos.removeLocationNote')}</p>
          </>
        }
        confirmLabel={t('photos.removeLocation')}
        busy={isBusy}
        onConfirm={() => void remove()}
        onClose={() => setIsConfirmingRemoval(false)}
      />
    )
  }

  return (
    <ConfirmDialog
      title={t('photos.changeLocation')}
      body={
        <>
          <input
            type="search"
            autoFocus
            autoComplete="off"
            value={searchText}
            placeholder={t('photos.searchPlaces')}
            aria-label={t('photos.searchPlaces')}
            onChange={(event) => typeSearch(event.target.value)}
          />
          {query !== '' && places !== null && places.length === 0 && (
            <p className="muted">{t('photos.noPlacesMatch')}</p>
          )}
          {query !== '' && places !== null && places.length > 0 && (
            <ul className="photo-place-results">
              {places.map((place) => (
                <li key={`${place.placeName}:${place.latitude}:${place.longitude}`}>
                  <button type="button" disabled={isBusy} onClick={() => void choose(place)}>
                    <span>{place.placeName}</span>
                    {place.distanceKilometers !== null ? (
                      <span className="muted">
                        {t('photos.placeDistance', { distance: distanceFormat.format(place.distanceKilometers) })}
                      </span>
                    ) : (
                      // Two cities of one name in one country read the same; where
                      // they are tells them apart.
                      sharedNames.has(place.placeName) && (
                        <span className="muted">{`${place.latitude.toFixed(2)}, ${place.longitude.toFixed(2)}`}</span>
                      )
                    )}
                  </button>
                </li>
              ))}
            </ul>
          )}
        </>
      }
      busy={isBusy}
      otherAction={
        photos.some(hasLocation) ? (
          <button type="button" className="danger" disabled={isBusy} onClick={() => setIsConfirmingRemoval(true)}>
            {t('photos.removeLocation')}
          </button>
        ) : undefined
      }
      onClose={onClose}
    />
  )
}

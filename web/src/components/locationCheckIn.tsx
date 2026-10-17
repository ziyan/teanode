import { useEffect, useState } from 'react'

import { graphql } from '../api'
import { useTranslation } from '../i18n/i18n'
import { readBrowserLocation } from './browserLocation'
import { ConfirmDialog } from './dialog'
import { FoundPlace } from './locationData'
import { LocationIcon } from './icons'
import { useToast } from './toast'

const LIST_NEARBY_PLACES = `query ($latitudeDegrees: Float!, $longitudeDegrees: Float!) {
  ListNearbyPlaces(latitudeDegrees: $latitudeDegrees, longitudeDegrees: $longitudeDegrees) {
    placeName osmType osmId osmCategory addressText latitudeDegrees longitudeDegrees distanceMeters placeId
  }
}`
const CHECK_IN = `mutation ($placeId: String, $latitudeDegrees: Float, $longitudeDegrees: Float, $placeName: String,
  $osmType: String, $osmId: Int64, $osmCategory: String, $addressText: String) {
  CheckIn(placeId: $placeId, latitudeDegrees: $latitudeDegrees, longitudeDegrees: $longitudeDegrees,
    placeName: $placeName, osmType: $osmType, osmId: $osmId, osmCategory: $osmCategory, addressText: $addressText) { id }
}`

// LocationCheckInDialog asks the browser where it is, lists what has a name
// near there, nearest first, and records that the person is at the one they
// press. A failure is a toast and the dialog goes: there is nothing in it to
// correct, and a refusal of the browser's question is not something to retry
// from inside it.
export function LocationCheckInDialog({ onClose, onCheckedIn }: { onClose: () => void; onCheckedIn: () => void }) {
  const { t } = useTranslation()
  const toast = useToast()
  const [foundPlaces, setFoundPlaces] = useState<FoundPlace[] | null>(null)
  const [isBusy, setIsBusy] = useState(false)

  useEffect(() => {
    let isCurrent = true
    void (async () => {
      const position = await readBrowserLocation()
      if (!isCurrent) return
      if (position.latitudeDegrees === undefined || position.longitudeDegrees === undefined) {
        toast.failed(t('location.checkInNoPosition'))
        onClose()
        return
      }
      try {
        const answer = await graphql<{ ListNearbyPlaces: FoundPlace[] }>(LIST_NEARBY_PLACES, {
          latitudeDegrees: position.latitudeDegrees,
          longitudeDegrees: position.longitudeDegrees,
        })
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

  async function checkIn(found: FoundPlace) {
    setIsBusy(true)
    try {
      await graphql(
        CHECK_IN,
        found.placeId
          ? { placeId: found.placeId }
          : {
              latitudeDegrees: found.latitudeDegrees,
              longitudeDegrees: found.longitudeDegrees,
              placeName: found.placeName,
              osmType: found.osmType,
              osmId: found.osmId,
              osmCategory: found.osmCategory,
              addressText: found.addressText,
            },
      )
      toast.done(t('location.checkedIn', { place: found.placeName }))
      onCheckedIn()
      onClose()
    } catch (failure) {
      toast.failure(failure, t('location.checkInFailed'))
      setIsBusy(false)
    }
  }

  return (
    <ConfirmDialog
      title={t('location.checkInTitle')}
      busy={isBusy}
      onClose={onClose}
      body={
        <>
          {foundPlaces === null && <p className="muted">{t('location.checkInFinding')}</p>}
          {foundPlaces !== null && foundPlaces.length === 0 && <p className="muted">{t('location.checkInNothing')}</p>}
          {foundPlaces !== null && foundPlaces.length > 0 && (
            <div className="location-nearby">
              {foundPlaces.map((found, index) => (
                <button
                  key={`${found.osmType}:${found.osmId ?? index}:${found.placeName}`}
                  type="button"
                  className="location-row"
                  disabled={isBusy}
                  onClick={() => void checkIn(found)}
                >
                  <span className="location-row-icon">
                    <LocationIcon size={16} />
                  </span>
                  <span className="location-row-body">
                    <strong>{found.placeName}</strong>
                    {found.addressText && <span className="muted">{found.addressText}</span>}
                  </span>
                  <span className="muted location-row-meta">
                    {t('location.metersAway', { distance: Math.round(found.distanceMeters) })}
                  </span>
                </button>
              ))}
            </div>
          )}
          <p className="muted location-credit">{t('location.credit')}</p>
        </>
      }
    />
  )
}

import { useState } from 'react'
import { useNavigate } from 'react-router-dom'

import { graphql } from '../api'
import { useTranslation } from '../i18n/i18n'
import { hasPermission, useSession } from '../session'
import { COARSE_ACCURACY_METERS, readFreshBrowserLocation, UNUSABLE_ACCURACY_METERS } from './browserLocation'
import { Tag } from './common'
import { LocationAddressChooser } from './locationAddressChooser'
import { FoundPlace, nearestPlaceHolding } from './locationData'
import { SettingsRow, SettingsSection } from './settingsList'
import { useToast } from './toast'
import { useQuery } from './useQuery'

// The first time a person opens Location with no position recorded, this
// stands under the map: install the phone app (only a phone
// records in the background; that row is a pointer to Devices and has no
// done state), mark where home is (from the browser's position), and, for each
// family circle the person is in, share where they are. Each row that can be
// done says whether it is, worked out from the server and not stored; "Not
// now" puts the setup away for good.

type FamilyCircle = { id: string; name: string; circleKind: string }
type SharedWith = { granteeCircleId: string | null }
type OwnPlace = { id: string; latitudeDegrees: number; longitudeDegrees: number; radiusMeters: number }

const LIST_HOME_PLACES = `query { ListPlaces(placeLabel: "home", limitCount: 1) { places { id } } }`
const LIST_SHARES = `query { ListLocationShares { id granteeCircleId } }`
const LIST_CIRCLES = `query { ListCircles { id name circleKind } }`
const LIST_OWN_PLACES = `query {
  ListPlaces(limitCount: 200) { places { id latitudeDegrees longitudeDegrees radiusMeters } }
}`
const SAVE_HOME = `mutation ($placeId: String, $latitudeDegrees: Float, $longitudeDegrees: Float, $placeLabel: String,
  $addressText: String) {
  SavePlace(placeId: $placeId, latitudeDegrees: $latitudeDegrees, longitudeDegrees: $longitudeDegrees,
    placeLabel: $placeLabel, addressText: $addressText) { id }
}`
const SHARE = `mutation ($granteeCircleId: String, $shareLevel: String!) {
  ShareLocation(granteeCircleId: $granteeCircleId, shareLevel: $shareLevel) { id }
}`

// LocationSetup is the setup card. The page decides when it is shown.
export function LocationSetup({ onDismiss }: { onDismiss: () => void }) {
  const { t } = useTranslation()
  const toast = useToast()
  const navigate = useNavigate()
  const session = useSession()
  const canUseCircles = hasPermission(session.permissions, 'circle:use')
  const [isBusy, setIsBusy] = useState(false)
  const [isTypingAddress, setIsTypingAddress] = useState(false)
  // How far off the last reading of the browser said it might be, when that
  // was too far to make home from; null while there is nothing to say.
  const [coarseAccuracyMeters, setCoarseAccuracyMeters] = useState<number | null>(null)

  const home = useQuery(
    async () => (await graphql<{ ListPlaces: { places: { id: string }[] } }>(LIST_HOME_PLACES)).ListPlaces.places,
    [],
    { refresh: false },
  )
  const shares = useQuery(
    async () => (await graphql<{ ListLocationShares: SharedWith[] }>(LIST_SHARES)).ListLocationShares,
    [],
    { refresh: false },
  )
  const circles = useQuery(
    async () => (canUseCircles ? (await graphql<{ ListCircles: FamilyCircle[] }>(LIST_CIRCLES)).ListCircles : []),
    [canUseCircles],
    { refresh: false },
  )

  const isHomeDone = (home.data?.length ?? 0) > 0
  const familyCircles = (circles.data ?? []).filter((circle) => circle.circleKind === 'family')

  async function run(work: () => Promise<unknown>, done: string, failed: string, reload: () => Promise<unknown>) {
    setIsBusy(true)
    try {
      await work()
      toast.done(done)
      await reload()
    } catch (failure) {
      toast.failure(failure, failed)
    } finally {
      setIsBusy(false)
    }
  }

  async function markHome() {
    setIsBusy(true)
    try {
      const position = await readFreshBrowserLocation()
      if (position.latitudeDegrees === undefined || position.longitudeDegrees === undefined) {
        toast.failed(t('location.checkInNoPosition'))
        return
      }
      // Home is where the person lives: a reading that can be a street away
      // is not saved, and the sentence says why and offers the typed address.
      if (position.accuracyMeters !== undefined && position.accuracyMeters > COARSE_ACCURACY_METERS) {
        setCoarseAccuracyMeters(position.accuracyMeters)
        return
      }
      setCoarseAccuracyMeters(null)
      // A place of the person's that already holds the position (a visit may
      // have made it) is the one labeled; only otherwise is a new one made.
      const own = await graphql<{ ListPlaces: { places: OwnPlace[] } }>(LIST_OWN_PLACES)
      const holding = nearestPlaceHolding(own.ListPlaces.places, {
        latitudeDegrees: position.latitudeDegrees,
        longitudeDegrees: position.longitudeDegrees,
      })
      await graphql(
        SAVE_HOME,
        holding
          ? { placeId: holding.id, placeLabel: 'home' }
          : {
              latitudeDegrees: position.latitudeDegrees,
              longitudeDegrees: position.longitudeDegrees,
              placeLabel: 'home',
            },
      )
      toast.done(t('location.setupHomeSaved'))
      await home.reload(true)
    } catch (failure) {
      toast.failure(failure, t('location.placeFailed'))
    } finally {
      setIsBusy(false)
    }
  }

  // A typed address makes or labels home there: no placeId, since the server
  // uses the person's own place that holds the position.
  async function saveHomeAt(match: FoundPlace) {
    await graphql(SAVE_HOME, {
      latitudeDegrees: match.latitudeDegrees,
      longitudeDegrees: match.longitudeDegrees,
      placeLabel: 'home',
      addressText: match.addressText,
    })
    toast.done(t('location.setupHomeSaved'))
    setIsTypingAddress(false)
    await home.reload(true)
  }

  const badge = (isDone: boolean) =>
    isDone ? <Tag value={t('location.setupDone')} tone="good" /> : <Tag value={t('location.setupToDo')} tone="warn" />

  return (
    <SettingsSection
      card
      title={t('location.setupTitle')}
      description={t('location.setupIntro')}
      action={
        <button type="button" onClick={onDismiss}>
          {t('location.setupNotNow')}
        </button>
      }
    >
      <SettingsRow
        title={t('location.setupInstall')}
        subtitle={t('location.setupInstallNote')}
        actions={
          <button type="button" onClick={() => navigate('/settings/devices')}>
            {t('location.setupInstallButton')}
          </button>
        }
      />
      <SettingsRow
        title={t('location.setupHome')}
        subtitle={t('location.setupHomeNote')}
        badge={badge(isHomeDone)}
        actions={
          !isHomeDone && (
            <>
              <button
                type="button"
                disabled={isBusy || (coarseAccuracyMeters ?? 0) > UNUSABLE_ACCURACY_METERS}
                onClick={() => void markHome()}
              >
                {t('location.setupHomeButton')}
              </button>
              <button type="button" disabled={isBusy} onClick={() => setIsTypingAddress(true)}>
                {t('location.setupHomeTypeButton')}
              </button>
            </>
          )
        }
      />
      {!isHomeDone && coarseAccuracyMeters !== null && (
        <p className="muted">{t('location.browserCoarse', { accuracyMeters: Math.round(coarseAccuracyMeters) })}</p>
      )}
      {familyCircles.map((circle) => {
        const isShared = (shares.data ?? []).some((share) => share.granteeCircleId === circle.id)
        const share = (shareLevel: 'history' | 'current') =>
          void run(
            () => graphql(SHARE, { granteeCircleId: circle.id, shareLevel }),
            t('location.shareSaved', { name: t('location.shareEveryoneIn', { name: circle.name }) }),
            t('location.shareFailed'),
            () => shares.reload(true),
          )
        return (
          <SettingsRow
            key={circle.id}
            title={t('location.setupShare', { name: circle.name })}
            subtitle={t('location.setupShareNote')}
            badge={badge(isShared)}
            actions={
              !isShared && (
                <>
                  <button
                    type="button"
                    disabled={isBusy}
                    aria-label={t('location.setupShareCurrentLabel', { name: circle.name })}
                    onClick={() => share('current')}
                  >
                    {t('location.shareLevelName.current')}
                  </button>
                  <button
                    type="button"
                    className="primary"
                    disabled={isBusy}
                    aria-label={t('location.setupShareHistoryLabel', { name: circle.name })}
                    onClick={() => share('history')}
                  >
                    {t('location.shareLevelName.history')}
                  </button>
                </>
              )
            }
          />
        )
      })}
      <p className="muted">{t('location.placeNamesNote')}</p>
      {isTypingAddress && (
        <LocationAddressChooser
          title={t('location.setupHomeTypeTitle')}
          onChoose={saveHomeAt}
          notFoundNote={t('location.setupHomeNotFound')}
          onClose={() => setIsTypingAddress(false)}
        />
      )}
    </SettingsSection>
  )
}

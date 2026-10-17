import { useState } from 'react'
import { Link } from 'react-router-dom'

import { graphql } from '../api'
import { useTranslation } from '../i18n/i18n'
import { hasPermission, useSession } from '../session'
import { readBrowserLocation } from './browserLocation'
import { Tag } from './common'
import { SettingsRow, SettingsSection } from './settingsList'
import { useToast } from './toast'
import { useQuery } from './useQuery'

// The first time a person opens Location with no position recorded, this
// stands where the empty map would be: install the phone app (only a phone
// records in the background), mark where home is (from the browser's
// position), and, for a family circle the person is in, share where they are.
// Each row says whether it is done, worked out from the server and not stored;
// "Not now" puts the setup away for good.

type FamilyCircle = { id: string; name: string; circleKind: string }
type SharedWith = { granteeCircleId: string | null }

const LIST_HOME_PLACES = `query { ListPlaces(placeLabel: "home", limitCount: 1) { places { id } } }`
const LIST_SHARES = `query { ListLocationShares { id granteeCircleId } }`
const LIST_CIRCLES = `query { ListCircles { id name circleKind } }`
const SAVE_HOME = `mutation ($latitudeDegrees: Float, $longitudeDegrees: Float, $placeLabel: String) {
  SavePlace(latitudeDegrees: $latitudeDegrees, longitudeDegrees: $longitudeDegrees, placeLabel: $placeLabel) { id }
}`
const SHARE = `mutation ($granteeCircleId: String, $shareLevel: String!) {
  ShareLocation(granteeCircleId: $granteeCircleId, shareLevel: $shareLevel) { id }
}`

// LocationSetup is the setup card. The page decides when it is shown.
export function LocationSetup({ onDismiss }: { onDismiss: () => void }) {
  const { t } = useTranslation()
  const toast = useToast()
  const session = useSession()
  const canUseCircles = hasPermission(session.permissions, 'circle:use')
  const [isBusy, setIsBusy] = useState(false)

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
      const position = await readBrowserLocation()
      if (position.latitudeDegrees === undefined || position.longitudeDegrees === undefined) {
        toast.failed(t('location.checkInNoPosition'))
        return
      }
      await graphql(SAVE_HOME, {
        latitudeDegrees: position.latitudeDegrees,
        longitudeDegrees: position.longitudeDegrees,
        placeLabel: 'home',
      })
      toast.done(t('location.setupHomeSaid'))
      await home.reload(true)
    } catch (failure) {
      toast.failure(failure, t('location.placeFailed'))
    } finally {
      setIsBusy(false)
    }
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
        title={<Link to="/settings/devices">{t('location.setupInstall')}</Link>}
        subtitle={t('location.setupInstallNote')}
        badge={badge(false)}
      />
      <SettingsRow
        title={t('location.setupHome')}
        subtitle={t('location.setupHomeNote')}
        badge={badge(isHomeDone)}
        actions={
          !isHomeDone && (
            <button type="button" disabled={isBusy} onClick={() => void markHome()}>
              {t('location.setupHomeButton')}
            </button>
          )
        }
      />
      {familyCircles.map((circle) => {
        const isShared = (shares.data ?? []).some((share) => share.granteeCircleId === circle.id)
        const share = (shareLevel: 'history' | 'current') =>
          void run(
            () => graphql(SHARE, { granteeCircleId: circle.id, shareLevel }),
            t('location.shareSaid', { name: t('location.shareEveryoneIn', { name: circle.name }) }),
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
                  <button type="button" disabled={isBusy} onClick={() => share('current')}>
                    {t('location.setupShareNow')}
                  </button>
                  <button
                    type="button"
                    className="primary"
                    disabled={isBusy}
                    aria-label={t('location.setupShare', { name: circle.name })}
                    onClick={() => share('history')}
                  >
                    {t('location.shareAdd')}
                  </button>
                </>
              )
            }
          />
        )
      })}
    </SettingsSection>
  )
}

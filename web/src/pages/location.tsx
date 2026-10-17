import { Fragment, useEffect, useMemo, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'

import { graphql, subscribe } from '../api'
import { Avatar } from '../components/avatar'
import { PageHeadingActions } from '../components/breadcrumb'
import { ErrorMessage, Tag } from '../components/common'
import { ConfirmDialog, FormDialog } from '../components/dialog'
import { MoreIcon } from '../components/icons'
import { LocationCheckInDialog } from '../components/locationCheckIn'
import {
  dayKeyOf,
  dayRange,
  LABEL_KEYS,
  LocationPoint,
  LocationView,
  PERSON_FIELDS,
  Place,
  PLACE_FIELDS,
  PlaceVisit,
  parseDayKey,
  placeTitle,
  VISIT_FIELDS,
} from '../components/locationData'
import { LocationMap, MapPerson, MapPlace } from '../components/locationMap'
import { LocationSetup } from '../components/locationSetup'
import { LocationSharing } from '../components/locationShare'
import { MenuButton } from '../components/menuButton'
import { Pager, usePageInAddress } from '../components/pager'
import { PLACE_LABELS, PlaceLabelIcon } from '../components/placeLabelIcon'
import { personName } from '../components/personLabel'
import { RelativeTime } from '../components/relativeTime'
import { SettingsEmpty, SettingsSection } from '../components/settingsList'
import { Tabs, TabItem } from '../components/tabs'
import { useToast } from '../components/toast'
import { useQuery } from '../components/useQuery'
import { Key, useTranslation } from '../i18n/i18n'
import { hasPermission, useSession } from '../session'

// Where the person is and has been, and who shares it with them: a map, the
// visit in progress under it, and three tabs. Visits are by day, newest first,
// in pages; Places are the ones the person named or labeled first; Sharing is
// who sees where they are. The tab and the day are in the address.

const TAB_IDS = ['visits', 'places', 'sharing'] as const
type TabID = (typeof TAB_IDS)[number]

const TABS: TabItem[] = [
  { id: 'visits', label: 'location.tabVisits' },
  { id: 'places', label: 'location.tabPlaces' },
  { id: 'sharing', label: 'location.tabSharing' },
]

// How often the people on the map are read again when the live feed says
// nothing: a feed that dropped, or a person who moved while it was down.
const PEOPLE_FALLBACK_REFRESH_MS = 60_000

// Remembered in this browser once the person has said "Not now" to the setup.
const SETUP_DISMISSED_KEY = 'teanode.location.setupDismissed'

function readIsSetupDismissed(): boolean {
  try {
    return window.localStorage.getItem(SETUP_DISMISSED_KEY) === 'true'
  } catch {
    return false
  }
}

const MINIMUM_RADIUS_METERS = 25
const MAXIMUM_RADIUS_METERS = 2000

const GET_LOCATION = `query ($userId: String) { GetLocation(userId: $userId) { ${PERSON_FIELDS} } }`
const LOCATION_EVENTS = `subscription { LocationEvents { userId } }`
const LIST_SHARED_LOCATIONS = `query { ListSharedLocations { ${PERSON_FIELDS} } }`
const LIST_VISITS = `query ($userId: String, $from: DateTime, $until: DateTime, $offset: Int, $limitCount: Int) {
  ListLocationVisits(userId: $userId, from: $from, until: $until, offset: $offset, limitCount: $limitCount) {
    placeVisits { ${VISIT_FIELDS} } nextOffset
  }
}`
const LIST_POINTS = `query ($userId: String, $from: DateTime!, $until: DateTime!) {
  ListLocationPoints(userId: $userId, from: $from, until: $until) { latitudeDegrees longitudeDegrees accuracyMeters measuredAt }
}`
const LIST_PLACES = `query ($userId: String, $offset: Int, $limitCount: Int) {
  ListPlaces(userId: $userId, offset: $offset, limitCount: $limitCount) { places { ${PLACE_FIELDS} } nextOffset }
}`
const SAVE_PLACE = `mutation ($placeId: String, $placeName: String, $placeLabel: String, $radiusMeters: Float) {
  SavePlace(placeId: $placeId, placeName: $placeName, placeLabel: $placeLabel, radiusMeters: $radiusMeters) { id }
}`
const FORGET_PLACE = `mutation ($placeId: String!) { ForgetPlace(placeId: $placeId) }`
const FORGET_LOCATION_HISTORY = `mutation { ForgetLocationHistory { pointCount visitCount placeCount } }`

type VisitPage = { placeVisits: PlaceVisit[]; nextOffset: number | null }
type PlacePage = { places: Place[]; nextOffset: number | null }

// LocationPage is /location.
export function LocationPage() {
  const session = useSession()
  const { t } = useTranslation()
  if (!hasPermission(session.permissions, 'location:use')) {
    return (
      <SettingsSection card title={t('location.title')}>
        <SettingsEmpty>{t('location.off')}</SettingsEmpty>
      </SettingsSection>
    )
  }
  return <LocationContent key={session.userId ?? ''} userId={session.userId ?? ''} />
}

function LocationContent({ userId }: { userId: string }) {
  const { t, plural, language } = useTranslation()
  const toast = useToast()
  const [parameters, setParameters] = useSearchParams()
  const requestedTab = parameters.get('tab')
  const tab: TabID = TAB_IDS.find((each) => each === requestedTab) ?? 'visits'
  const dayStart = parseDayKey(parameters.get('day'))
  const dayKey = dayStart ? dayKeyOf(dayStart) : null
  const visitsPaging = usePageInAddress()
  const placesPaging = usePageInAddress({ pageParameter: 'placesPage', pageSizeParameter: 'placesRows' })

  // The person whose visits and places are shown, in the address, when it is
  // somebody sharing with this person. The person themselves is not in it.
  const requestedPersonId = parameters.get('person')
  const [selectedUserId, setSelectedUserId] = useState<string | null>(null)
  const [isSetupDismissed, setIsSetupDismissed] = useState(readIsSetupDismissed)
  // What the live feed has read again about people who moved, until the next
  // full reading of everybody replaces it.
  const [movedViews, setMovedViews] = useState<Record<string, LocationView>>({})
  const [selectedPlaceId, setSelectedPlaceId] = useState<string | null>(null)
  const [isCheckingIn, setIsCheckingIn] = useState(false)
  const [editingPlace, setEditingPlace] = useState<Place | null>(null)
  const [forgettingPlace, setForgettingPlace] = useState<Place | null>(null)
  const [isErasing, setIsErasing] = useState(false)
  const [isBusy, setIsBusy] = useState(false)

  function changeParameters(changes: Record<string, string | null>) {
    setParameters((previous) => {
      const written = new URLSearchParams(previous)
      for (const [name, value] of Object.entries(changes)) {
        if (value === null) written.delete(name)
        else written.set(name, value)
      }
      return written
    })
  }
  const selectTab = (id: string) => changeParameters({ tab: id === 'visits' ? null : id })
  // A different day is a different list: back to its first page.
  const selectDay = (nextDayKey: string | null) => changeParameters({ day: nextDayKey, page: null })

  const range = useMemo(() => (dayStart ? dayRange(dayStart) : null), [dayStart])

  // Read again by the live feed and every minute, not by the page's default
  // ten-second timer.
  const me = useQuery(() => graphql<{ GetLocation: LocationView | null }>(GET_LOCATION), [userId], {
    refresh: false,
  })
  // The people sharing with this person arrive with milestone 4 of the
  // location plan. On a server that does not have the operation yet this
  // call fails, and that is the same as nobody sharing: the page works with
  // only the person's own position, without a word about it.
  const shared = useQuery(
    async () => {
      try {
        const answer = await graphql<{ ListSharedLocations: LocationView[] | null }>(LIST_SHARED_LOCATIONS)
        return answer.ListSharedLocations ?? []
      } catch {
        return [] as LocationView[]
      }
    },
    [userId],
    { refresh: false },
  )
  // Somebody else's visits and places are read only when they share their
  // history; at "where I am now" there is nothing of theirs to list. Until the
  // people are read, a person in the address is not known to be sharing.
  const views = useMemo(
    () => [
      ...(me.data?.GetLocation ? [me.data.GetLocation] : []),
      ...(shared.data ?? []).map((view) => movedViews[view.userId] ?? view),
    ],
    [me.data, shared.data, movedViews],
  )
  const isPersonPending = requestedPersonId !== null && shared.data === null
  const viewedPerson = useMemo(
    () => views.find((view) => view.userId === requestedPersonId && view.locationShareLevel !== 'owner') ?? null,
    [views, requestedPersonId],
  )
  const viewedUserId = viewedPerson?.userId ?? null
  const isReadOnly = viewedPerson !== null
  const canReadHistory = !isPersonPending && (viewedPerson === null || viewedPerson.locationShareLevel === 'history')

  const visits = useQuery(
    async () =>
      tab === 'visits' && canReadHistory
        ? (
            await graphql<{ ListLocationVisits: VisitPage }>(LIST_VISITS, {
              userId: viewedUserId,
              from: range?.from ?? null,
              until: range?.until ?? null,
              offset: visitsPaging.offset,
              limitCount: visitsPaging.pageSize,
            })
          ).ListLocationVisits
        : null,
    [tab, range?.from, visitsPaging.offset, visitsPaging.pageSize, userId, viewedUserId, canReadHistory],
  )
  const points = useQuery(
    async () =>
      range && canReadHistory
        ? (await graphql<{ ListLocationPoints: LocationPoint[] }>(LIST_POINTS, { userId: viewedUserId, ...range }))
            .ListLocationPoints
        : [],
    [range?.from, userId, viewedUserId, canReadHistory],
  )
  const places = useQuery(
    async () =>
      tab === 'places' && canReadHistory
        ? (
            await graphql<{ ListPlaces: PlacePage }>(LIST_PLACES, {
              userId: viewedUserId,
              offset: placesPaging.offset,
              limitCount: placesPaging.pageSize,
            })
          ).ListPlaces
        : null,
    [tab, placesPaging.offset, placesPaging.pageSize, userId, viewedUserId, canReadHistory],
  )

  const visitRows = useMemo(() => visits.data?.placeVisits ?? [], [visits.data])
  // The places a person named or labeled come first, then the ones the map
  // named; within each the server's order stands. A page is sorted as it
  // comes, so a labeled place on a later page waits for its page.
  const placeRows = useMemo(() => {
    const rank = (place: Place) => (place.placeLabel ? 0 : place.isNamedByPerson ? 1 : 2)
    return [...(places.data?.places ?? [])].sort((first, second) => rank(first) - rank(second))
  }, [places.data])

  const mapPeople: MapPerson[] = useMemo(
    () =>
      views.flatMap((view) =>
        view.latestPoint
          ? [
              {
                userId: view.userId,
                name: personName(view),
                avatarUrl: view.avatarUrl,
                avatarColorIndex: view.avatarColorIndex,
                latitudeDegrees: view.latestPoint.latitudeDegrees,
                longitudeDegrees: view.latestPoint.longitudeDegrees,
                accuracyMeters: view.latestPoint.accuracyMeters,
                isSelected: view.userId === (viewedUserId ?? selectedUserId),
              },
            ]
          : [],
      ),
    [views, selectedUserId, viewedUserId],
  )
  const mapPlaces: MapPlace[] = useMemo(() => {
    const shown =
      tab === 'places'
        ? placeRows
        : tab === 'visits'
          ? [...new Map(visitRows.map((visit) => [visit.place.id, visit.place])).values()]
          : []
    return shown.map((place) => ({
      id: place.id,
      placeName: placeTitle(place, t),
      placeLabel: place.placeLabel,
      latitudeDegrees: place.latitudeDegrees,
      longitudeDegrees: place.longitudeDegrees,
      radiusMeters: place.radiusMeters,
      isSelected: place.id === selectedPlaceId,
    }))
  }, [tab, placeRows, visitRows, selectedPlaceId, t])
  const path = useMemo(
    () => (points.data ?? []).map(({ latitudeDegrees, longitudeDegrees }) => ({ latitudeDegrees, longitudeDegrees })),
    [points.data],
  )

  const timeFormat = useMemo(
    () => new Intl.DateTimeFormat(language, { hour: 'numeric', minute: '2-digit' }),
    [language],
  )
  const dayFormat = useMemo(
    () => new Intl.DateTimeFormat(language, { weekday: 'long', year: 'numeric', month: 'long', day: 'numeric' }),
    [language],
  )
  const timeOf = (moment: string) => timeFormat.format(new Date(moment))

  // Choosing a person centers the map on them. Somebody else is put in the
  // address, with their visits and places; the person themselves is not.
  function selectPerson(view: LocationView) {
    setSelectedPlaceId(null)
    if (view.locationShareLevel === 'owner') {
      setSelectedUserId(view.userId)
      changeParameters({ person: null, page: null, placesPage: null })
    } else {
      setSelectedUserId(null)
      changeParameters({ person: view.userId, page: null, placesPage: null })
    }
  }

  function selectPlaceOnMap(placeId: string) {
    setSelectedPlaceId(placeId)
    setSelectedUserId(null)
    const place = placeRows.find((each) => each.id === placeId)
    if (tab === 'places' && place && !isReadOnly) setEditingPlace(place)
  }

  function reloadAll() {
    void me.reload()
    void visits.reload()
    void places.reload()
  }

  // Everybody on the map is read again: the person and the people sharing with
  // them. Quietly, so the page does not go back to loading.
  function refreshPeople() {
    setMovedViews({})
    return Promise.all([me.reload(true), shared.reload(true)])
  }

  // A person the live feed says moved is read again by themselves, and the
  // person (the viewer) with the page's own reading. A person who can no
  // longer be read, because they stopped sharing, is a reason to read
  // everybody again.
  async function refreshMovedPerson(movedUserId: string) {
    if (movedUserId === userId) {
      await me.reload(true)
      return
    }
    try {
      const answer = await graphql<{ GetLocation: LocationView | null }>(GET_LOCATION, { userId: movedUserId })
      const moved = answer.GetLocation
      if (moved) setMovedViews((previous) => ({ ...previous, [movedUserId]: moved }))
    } catch {
      await refreshPeople()
    }
  }

  // The latest of both, read by the feed without it starting again at every
  // render.
  const feedHandlers = useRef({ refreshMovedPerson, refreshPeople })
  feedHandlers.current = { refreshMovedPerson, refreshPeople }

  useEffect(() => {
    const stop = subscribe<{ LocationEvents: { userId: string } }>(
      LOCATION_EVENTS,
      {},
      (data) => void feedHandlers.current.refreshMovedPerson(data.LocationEvents.userId),
      // A feed that ended is covered by the minute refresh below.
      undefined,
      {
        // What happened while the socket was down is read again before the
        // feed resumes.
        beforeStart: async (isReconnecting) => {
          if (isReconnecting) await feedHandlers.current.refreshPeople()
        },
      },
    )
    const timer = window.setInterval(() => {
      if (document.visibilityState === 'visible') void feedHandlers.current.refreshPeople()
    }, PEOPLE_FALLBACK_REFRESH_MS)
    return () => {
      stop()
      window.clearInterval(timer)
    }
  }, [userId])

  async function act(work: () => Promise<unknown>, done: string, failed: string): Promise<boolean> {
    setIsBusy(true)
    try {
      await work()
      toast.done(done)
      reloadAll()
      return true
    } catch (failure) {
      toast.failure(failure, failed)
      return false
    } finally {
      setIsBusy(false)
    }
  }

  async function erase() {
    setIsBusy(true)
    try {
      const answer = await graphql<{
        ForgetLocationHistory: { pointCount: number; visitCount: number; placeCount: number }
      }>(FORGET_LOCATION_HISTORY)
      toast.done(t('location.erased', answer.ForgetLocationHistory))
      setIsErasing(false)
      setSelectedPlaceId(null)
      reloadAll()
      void points.reload()
    } catch (failure) {
      toast.failure(failure, t('location.eraseFailed'))
    } finally {
      setIsBusy(false)
    }
  }

  function durationText(visit: PlaceVisit): string {
    if (visit.durationSeconds === null || visit.durationSeconds === undefined) {
      return visit.departedAt ? '' : t('location.hereNow')
    }
    const minutes = Math.max(1, Math.round(visit.durationSeconds / 60))
    return minutes >= 60
      ? t('location.durationHours', { hours: Math.floor(minutes / 60), minutes: minutes % 60 })
      : t('location.durationMinutes', { minutes })
  }

  function rangeText(visit: PlaceVisit): string {
    if (visit.arrivedAt && visit.departedAt) {
      return t('location.timeRange', { from: timeOf(visit.arrivedAt), until: timeOf(visit.departedAt) })
    }
    if (visit.arrivedAt) return t('location.timeSince', { from: timeOf(visit.arrivedAt) })
    if (visit.departedAt) return t('location.timeUntil', { until: timeOf(visit.departedAt) })
    return ''
  }

  const visitDayKey = (visit: PlaceVisit) => {
    const moment = visit.arrivedAt ?? visit.departedAt
    return moment ? dayKeyOf(moment) : ''
  }

  // The first time, with no position recorded yet, a short setup stands where
  // the empty map would be, until it is dismissed.
  const isSetupShown = me.data !== null && !me.data.GetLocation?.latestPoint && !isSetupDismissed
  const viewedName = viewedPerson ? personName(viewedPerson) : ''
  const notSharedHistoryText = viewedPerson ? t('location.personSharesNowOnly', { name: viewedName }) : ''

  return (
    <>
      <PageHeadingActions>
        <button type="button" className="primary" onClick={() => setIsCheckingIn(true)}>
          {t('location.checkIn')}
        </button>
      </PageHeadingActions>

      <ErrorMessage error={me.error} />

      {isSetupShown && (
        <LocationSetup
          onDismiss={() => {
            try {
              window.localStorage.setItem(SETUP_DISMISSED_KEY, 'true')
            } catch {
              // Dismissed for this visit only.
            }
            setIsSetupDismissed(true)
          }}
        />
      )}

      {!isSetupShown && (
        <section className="card location-map-card">
          <LocationMap
            people={mapPeople}
            places={mapPlaces}
            path={path}
            onSelectPerson={(selected) => {
              const view = views.find((each) => each.userId === selected)
              if (view) selectPerson(view)
            }}
            onSelectPlace={selectPlaceOnMap}
          />
          <div className="location-people">
            {views.map((view) => {
              const name = personName(view)
              const isOwner = view.locationShareLevel === 'owner'
              return (
                <button
                  key={view.userId}
                  type="button"
                  className={
                    view.userId === (viewedUserId ?? selectedUserId) ? 'location-row is-selected' : 'location-row'
                  }
                  disabled={!view.latestPoint}
                  onClick={() => selectPerson(view)}
                >
                  <span className="location-row-icon">
                    <Avatar name={name} avatarUrl={view.avatarUrl} colorIndex={view.avatarColorIndex} size={28} />
                  </span>
                  <span className="location-row-body">
                    <strong>{name}</strong>
                    <span className="muted">
                      {view.openVisit ? (
                        t('location.atSince', {
                          place: placeTitle(view.openVisit.place, t),
                          time: view.openVisit.arrivedAt ? timeOf(view.openVisit.arrivedAt) : '',
                        })
                      ) : view.latestPoint ? (
                        <>
                          {t('location.lastSeen')} <RelativeTime value={view.latestPoint.measuredAt} />
                        </>
                      ) : (
                        t('location.nowhere')
                      )}
                    </span>
                  </span>
                  {isOwner && (
                    <span className="location-row-meta">
                      <Tag value={t('location.you')} />
                    </span>
                  )}
                </button>
              )
            })}
          </div>
        </section>
      )}

      <Tabs items={TABS} active={tab} onSelect={selectTab} />

      {tab === 'visits' && (
        <SettingsSection
          card
          title={viewedPerson ? t('location.personVisits', { name: viewedName }) : t('location.visits')}
          action={
            isReadOnly ? undefined : (
              <MenuButton
                className="icon-action"
                label={t('location.moreMenu')}
                icon={<MoreIcon size={16} />}
                render={(close) => (
                  <button
                    type="button"
                    role="menuitem"
                    onClick={() => {
                      close()
                      setIsErasing(true)
                    }}
                  >
                    {t('location.eraseHistory')}
                  </button>
                )}
              />
            )
          }
        >
          <div className="location-day-picker">
            <label>
              {t('location.day')}
              <input
                type="date"
                value={dayKey ?? ''}
                onChange={(event) => selectDay(parseDayKey(event.target.value) ? event.target.value : null)}
              />
            </label>
            {dayKey && (
              <button type="button" onClick={() => selectDay(null)}>
                {t('location.allDays')}
              </button>
            )}
          </div>
          <ErrorMessage error={visits.error} />
          {viewedPerson && !canReadHistory && <SettingsEmpty>{notSharedHistoryText}</SettingsEmpty>}
          {visits.data && visitRows.length === 0 && (
            <SettingsEmpty>
              {dayKey
                ? t('location.visitsEmptyDay')
                : t(isReadOnly ? 'location.personVisitsEmpty' : 'location.visitsEmpty')}
            </SettingsEmpty>
          )}
          {visitRows.map((visit, index) => {
            const key = visitDayKey(visit)
            const isFirstOfDay = index === 0 || visitDayKey(visitRows[index - 1]) !== key
            const title = placeTitle(visit.place, t)
            return (
              <Fragment key={visit.id}>
                {isFirstOfDay && key && (
                  <h4 className="location-day-head">
                    {dayKey === key ? (
                      dayFormat.format(parseDayKey(key) ?? new Date())
                    ) : (
                      <button
                        type="button"
                        className="location-day-button"
                        aria-label={t('location.showDay', { day: dayFormat.format(parseDayKey(key) ?? new Date()) })}
                        onClick={() => selectDay(key)}
                      >
                        {dayFormat.format(parseDayKey(key) ?? new Date())}
                      </button>
                    )}
                  </h4>
                )}
                <button
                  type="button"
                  className={visit.place.id === selectedPlaceId ? 'location-row is-selected' : 'location-row'}
                  onClick={() => {
                    setSelectedPlaceId(visit.place.id)
                    setSelectedUserId(null)
                  }}
                >
                  <span className="location-row-icon">
                    <PlaceLabelIcon placeLabel={visit.place.placeLabel} />
                  </span>
                  <span className="location-row-body">
                    <strong>{title}</strong>
                    <span className="muted">{rangeText(visit)}</span>
                  </span>
                  <span className="muted location-row-meta">{durationText(visit)}</span>
                </button>
              </Fragment>
            )
          })}
          {(visitsPaging.pageIndex > 0 || visits.data?.nextOffset != null) && (
            <div className="list-foot">
              <Pager
                pageIndex={visitsPaging.pageIndex}
                pageSize={visitsPaging.pageSize}
                shownCount={visitRows.length}
                totalCount={visitsPaging.offset + visitRows.length + (visits.data?.nextOffset != null ? 1 : 0)}
                isTotalCountLowerBound={visits.data?.nextOffset != null}
                hasNextPage={visits.data?.nextOffset != null}
                isLoading={visits.loading && !visits.data}
                onPageIndex={visitsPaging.setPageIndex}
                onPageSize={visitsPaging.setPageSize}
              />
            </div>
          )}
          <p className="muted location-credit">{t('location.credit')}</p>
        </SettingsSection>
      )}

      {tab === 'places' && (
        <SettingsSection
          card
          title={viewedPerson ? t('location.personPlaces', { name: viewedName }) : t('location.places')}
        >
          <ErrorMessage error={places.error} />
          {viewedPerson && !canReadHistory && <SettingsEmpty>{notSharedHistoryText}</SettingsEmpty>}
          {places.data && placeRows.length === 0 && (
            <SettingsEmpty>{t(isReadOnly ? 'location.personPlacesEmpty' : 'location.placesEmpty')}</SettingsEmpty>
          )}
          {placeRows.map((place) => (
            <button
              key={place.id}
              type="button"
              className={place.id === selectedPlaceId ? 'location-row is-selected' : 'location-row'}
              onClick={() => {
                setSelectedPlaceId(place.id)
                setSelectedUserId(null)
                if (!isReadOnly) setEditingPlace(place)
              }}
            >
              <span className="location-row-icon">
                <PlaceLabelIcon placeLabel={place.placeLabel} />
              </span>
              <span className="location-row-body">
                <strong>{placeTitle(place, t)}</strong>
                <span className="muted">
                  {plural(
                    place.visitCount,
                    { one: 'location.visitCountOne', other: 'location.visitCount' },
                    { count: place.visitCount },
                  )}
                  {place.lastVisitAt && (
                    <>
                      {' · '}
                      {t('location.lastVisit')} <RelativeTime value={place.lastVisitAt} />
                    </>
                  )}
                </span>
              </span>
              <span className="location-row-meta">
                {LABEL_KEYS[place.placeLabel] && <Tag value={t(LABEL_KEYS[place.placeLabel])} />}
              </span>
            </button>
          ))}
          {(placesPaging.pageIndex > 0 || places.data?.nextOffset != null) && (
            <div className="list-foot">
              <Pager
                pageIndex={placesPaging.pageIndex}
                pageSize={placesPaging.pageSize}
                shownCount={placeRows.length}
                totalCount={placesPaging.offset + placeRows.length + (places.data?.nextOffset != null ? 1 : 0)}
                isTotalCountLowerBound={places.data?.nextOffset != null}
                hasNextPage={places.data?.nextOffset != null}
                isLoading={places.loading && !places.data}
                onPageIndex={placesPaging.setPageIndex}
                onPageSize={placesPaging.setPageSize}
              />
            </div>
          )}
          <p className="muted location-credit">{t('location.credit')}</p>
        </SettingsSection>
      )}

      {tab === 'sharing' && <LocationSharing />}

      {isCheckingIn && <LocationCheckInDialog onClose={() => setIsCheckingIn(false)} onCheckedIn={reloadAll} />}

      {editingPlace && (
        <PlaceEditor
          key={editingPlace.id}
          place={editingPlace}
          isBusy={isBusy}
          onClose={() => setEditingPlace(null)}
          onForget={() => {
            setForgettingPlace(editingPlace)
            setEditingPlace(null)
          }}
          onSave={async (variables) => {
            const isDone = await act(
              () => graphql(SAVE_PLACE, { placeId: editingPlace.id, ...variables }),
              t('location.placeSaved', {
                place: placeTitle({ ...editingPlace, placeName: variables.placeName ?? editingPlace.placeName }, t),
              }),
              t('location.placeFailed'),
            )
            if (isDone) setEditingPlace(null)
          }}
        />
      )}

      {forgettingPlace && (
        <ConfirmDialog
          title={t('location.forgetTitle')}
          body={t('location.forgetBody', { place: placeTitle(forgettingPlace, t) })}
          confirmLabel={t('location.forget')}
          busy={isBusy}
          onClose={() => setForgettingPlace(null)}
          onConfirm={() =>
            void act(
              () => graphql(FORGET_PLACE, { placeId: forgettingPlace.id }),
              t('location.forgotten', { place: placeTitle(forgettingPlace, t) }),
              t('location.forgetFailed'),
            ).then((isDone) => {
              if (!isDone) return
              if (selectedPlaceId === forgettingPlace.id) setSelectedPlaceId(null)
              setForgettingPlace(null)
            })
          }
        />
      )}

      {isErasing && (
        <ConfirmDialog
          title={t('location.eraseTitle')}
          body={t('location.eraseBody')}
          confirmLabel={t('location.eraseConfirm')}
          busy={isBusy}
          onClose={() => setIsErasing(false)}
          onConfirm={() => void erase()}
        />
      )}
    </>
  )
}

type PlaceChanges = { placeName?: string; placeLabel?: string; radiusMeters?: number }

// PlaceEditor changes one place: what it is called, the label that gives it an
// icon and a meaning ("home"), and how far from its point still counts as
// being there. Only what the person changed is sent, so a name the map chose
// is not turned into one the person gave by saving the radius.
function PlaceEditor({
  place,
  isBusy,
  onClose,
  onForget,
  onSave,
}: {
  place: Place
  isBusy: boolean
  onClose: () => void
  onForget: () => void
  onSave: (changes: PlaceChanges) => Promise<void>
}) {
  const { t } = useTranslation()
  const [placeName, setPlaceName] = useState(place.placeName)
  const [placeLabel, setPlaceLabel] = useState(place.placeLabel)
  const [radiusText, setRadiusText] = useState(String(Math.round(place.radiusMeters)))
  const radiusMeters = Number(radiusText)
  const isRadiusValid =
    radiusText.trim() !== '' && radiusMeters >= MINIMUM_RADIUS_METERS && radiusMeters <= MAXIMUM_RADIUS_METERS

  const changes: PlaceChanges = {}
  if (placeName.trim() !== place.placeName.trim()) changes.placeName = placeName.trim()
  if (placeLabel !== place.placeLabel) changes.placeLabel = placeLabel
  if (isRadiusValid && radiusMeters !== Math.round(place.radiusMeters)) changes.radiusMeters = radiusMeters

  return (
    <FormDialog
      title={t('location.editPlace')}
      submitLabel={t('common.save')}
      busy={isBusy}
      canSubmit={isRadiusValid && Object.keys(changes).length > 0}
      onClose={onClose}
      onSubmit={() => void onSave(changes)}
      otherAction={
        <button type="button" className="danger" onClick={onForget}>
          {t('location.forget')}
        </button>
      }
    >
      <label>
        {t('location.placeName')}
        <input
          type="text"
          value={placeName}
          placeholder={place.osmName}
          onChange={(event) => setPlaceName(event.target.value)}
        />
      </label>
      <label>
        {t('location.placeLabel')}
        <select value={placeLabel} onChange={(event) => setPlaceLabel(event.target.value)}>
          <option value="">{t('location.label.none')}</option>
          {PLACE_LABELS.map((each) => (
            <option key={each} value={each}>
              {t(LABEL_KEYS[each] as Key)}
            </option>
          ))}
        </select>
      </label>
      <label>
        {t('location.placeRadius')}
        <input
          type="number"
          min={MINIMUM_RADIUS_METERS}
          max={MAXIMUM_RADIUS_METERS}
          step={5}
          value={radiusText}
          onChange={(event) => setRadiusText(event.target.value)}
        />
      </label>
      <input
        type="range"
        min={MINIMUM_RADIUS_METERS}
        max={MAXIMUM_RADIUS_METERS}
        step={25}
        value={isRadiusValid ? radiusMeters : MINIMUM_RADIUS_METERS}
        aria-label={t('location.placeRadius')}
        onChange={(event) => setRadiusText(event.target.value)}
      />
    </FormDialog>
  )
}

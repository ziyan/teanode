import { Fragment, useEffect, useMemo, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'

import { graphql, subscribe } from '../api'
import { Avatar } from '../components/avatar'
import { PageHeadingActions } from '../components/breadcrumb'
import { ErrorMessage, Loading, Tag } from '../components/common'
import { ConfirmDialog, FormDialog } from '../components/dialog'
import { MoreIcon } from '../components/icons'
import { isLocationPermissionGranted, readFreshBrowserLocation } from '../components/browserLocation'
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
  placeSubtitle,
  placeTitle,
  VISIT_FIELDS,
} from '../components/locationData'
import { LocationMap, MapPerson, MapPlace } from '../components/locationMap'
import { LocationSetup } from '../components/locationSetup'
import { LocationSharing } from '../components/locationShare'
import { MenuButton } from '../components/menuButton'
import { Pager, useKeepPageInRange, usePageInAddress } from '../components/pager'
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
const LIST_VISITS = `query ($userId: String, $from: DateTime, $until: DateTime, $placeId: String, $placeLabel: String,
  $offset: Int, $limitCount: Int) {
  ListLocationVisits(userId: $userId, from: $from, until: $until, placeId: $placeId, placeLabel: $placeLabel,
    offset: $offset, limitCount: $limitCount) {
    placeVisits { ${VISIT_FIELDS} } nextOffset
  }
}`
const LIST_POINTS = `query ($userId: String, $from: DateTime!, $until: DateTime!) {
  ListLocationPoints(userId: $userId, from: $from, until: $until) { latitudeDegrees longitudeDegrees accuracyMeters measuredAt }
}`
const LIST_PLACES = `query ($userId: String, $placeLabel: String, $query: String, $offset: Int, $limitCount: Int) {
  ListPlaces(userId: $userId, placeLabel: $placeLabel, query: $query, offset: $offset, limitCount: $limitCount) {
    places { ${PLACE_FIELDS} } nextOffset
  }
}`
const SAVE_PLACE = `mutation ($placeId: String, $placeName: String, $placeLabel: String, $radiusMeters: Float) {
  SavePlace(placeId: $placeId, placeName: $placeName, placeLabel: $placeLabel, radiusMeters: $radiusMeters) { id }
}`
const FORGET_PLACE = `mutation ($placeId: String!) { ForgetPlace(placeId: $placeId) }`
const ERASE_LOCATION_HISTORY = `mutation ($from: DateTime, $until: DateTime) {
  EraseLocationHistory(from: $from, until: $until) { pointCount visitCount placeCount }
}`

type VisitPage = { placeVisits: PlaceVisit[]; nextOffset: number | null }
type PlacePage = { places: Place[]; nextOffset: number | null }
type DayRange = { from: string; until: string }

// How long after the last keystroke the words typed to find a place are asked
// about, so the server is not asked for every letter.
const SEARCH_PATIENCE_MS = 300

// How many of a place's visits its editor lists.
const PLACE_VISITS_SHOWN_COUNT = 10

// What a query was asked, kept with its answer: a query keeps the previous
// answer while the next is on its way, and rows of another person, day or page
// must not stand under the new heading.
type Answered<Answer> = { requestKey: string; answer: Answer }
function answeredFor<Answer>(data: Answered<Answer> | null, requestKey: string): Answer | null {
  return data && data.requestKey === requestKey ? data.answer : null
}

type Translate = ReturnType<typeof useTranslation>['t']

// visitDurationText is how long a visit lasted, or that it is going on.
function visitDurationText(visit: PlaceVisit, t: Translate): string {
  if (visit.durationSeconds === null || visit.durationSeconds === undefined) {
    return visit.departedAt ? '' : t('location.hereNow')
  }
  const minutes = Math.max(1, Math.round(visit.durationSeconds / 60))
  return minutes >= 60
    ? t('location.durationHours', { hours: Math.floor(minutes / 60), minutes: minutes % 60 })
    : t('location.durationMinutes', { minutes })
}

// LabelFilter is the choice of one label, or all of them.
function LabelFilter({ value, onChange }: { value: string; onChange: (placeLabel: string) => void }) {
  const { t } = useTranslation()
  return (
    <label>
      {t('location.placeLabel')}
      <select value={value} onChange={(event) => onChange(event.target.value)}>
        <option value="">{t('location.filterAllLabels')}</option>
        {PLACE_LABELS.map((each) => (
          <option key={each} value={each}>
            {t(LABEL_KEYS[each] as Key)}
          </option>
        ))}
      </select>
    </label>
  )
}

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
  // The filters of the two lists, in the address with the page: a label for
  // the visits, a label and some words for the places.
  const isKnownLabel = (value: string | null) => PLACE_LABELS.some((each) => each === value)
  const visitsLabel = isKnownLabel(parameters.get('visitsLabel')) ? (parameters.get('visitsLabel') ?? '') : ''
  const placesLabel = isKnownLabel(parameters.get('placesLabel')) ? (parameters.get('placesLabel') ?? '') : ''
  const placesQuery = parameters.get('placesQuery') ?? ''

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
  // Where this browser is, for a viewer with no recorded position. It is only
  // drawn: nothing is stored, and it is gone when the page is.
  const [browserPosition, setBrowserPosition] = useState<MapPerson | null>(null)
  const [isReadingBrowser, setIsReadingBrowser] = useState(false)
  const [editingPlace, setEditingPlace] = useState<Place | null>(null)
  const [forgettingPlace, setForgettingPlace] = useState<Place | null>(null)
  // What the person is asked to erase: one day, or everything.
  const [erasing, setErasing] = useState<{ range: DayRange | null } | null>(null)
  const [placesSearchText, setPlacesSearchText] = useState(placesQuery)
  const [isBusy, setIsBusy] = useState(false)
  // Why the open dialog's work failed, shown in the dialog with what was typed
  // still in it.
  const [dialogProblem, setDialogProblem] = useState<string | null>(null)

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
  // The people sharing with this person. A failed first reading is shown above
  // the map, and a failed quiet one keeps what is on the map.
  const shared = useQuery(
    async () =>
      (await graphql<{ ListSharedLocations: LocationView[] | null }>(LIST_SHARED_LOCATIONS)).ListSharedLocations ?? [],
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
  const isPersonUnreadable = isPersonPending && shared.error !== null && shared.error !== undefined
  const viewedPerson = useMemo(
    () => views.find((view) => view.userId === requestedPersonId && view.shareLevel !== 'owner') ?? null,
    [views, requestedPersonId],
  )
  const viewedUserId = viewedPerson?.userId ?? null
  const isReadOnly = viewedPerson !== null
  const canReadHistory = !isPersonPending && (viewedPerson === null || viewedPerson.shareLevel === 'history')

  // A person in the address who is not sharing (a stale link, or they stopped)
  // is taken out of it once the people are read, and the person is told, rather
  // than their own visits standing under a link that named somebody else.
  const isPersonGone = requestedPersonId !== null && shared.data !== null && viewedPerson === null
  useEffect(() => {
    if (!isPersonGone) return
    if (requestedPersonId !== userId) toast.failed(t('location.personNotSharing'))
    setParameters(
      (previous) => {
        const written = new URLSearchParams(previous)
        written.delete('person')
        written.delete('page')
        written.delete('placesPage')
        return written
      },
      { replace: true },
    )
    // Once per link that is found stale.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isPersonGone, requestedPersonId])

  // Each answer carries what it was asked (the person, the day, the filters and
  // the page), so the rows of a different person, day or page are not shown
  // under the heading of the new one while its answer is on the way.
  const visitsRequestKey = JSON.stringify([
    viewedUserId,
    range?.from ?? null,
    visitsLabel,
    visitsPaging.offset,
    visitsPaging.pageSize,
  ])
  const visits = useQuery(
    async (): Promise<Answered<VisitPage> | null> =>
      tab === 'visits' && canReadHistory
        ? {
            requestKey: visitsRequestKey,
            answer: (
              await graphql<{ ListLocationVisits: VisitPage }>(LIST_VISITS, {
                userId: viewedUserId,
                from: range?.from ?? null,
                until: range?.until ?? null,
                placeLabel: visitsLabel || null,
                offset: visitsPaging.offset,
                limitCount: visitsPaging.pageSize,
              })
            ).ListLocationVisits,
          }
        : null,
    [tab, visitsRequestKey, userId, canReadHistory],
  )
  // The day's path is read when the day, the person or the page is, and again
  // when the live feed says the viewed person moved; not on a timer, since a
  // day holds thousands of positions and a day in the past does not change.
  const pointsRequestKey = JSON.stringify([viewedUserId, range?.from ?? null])
  const points = useQuery(
    async (): Promise<Answered<LocationPoint[]>> => ({
      requestKey: pointsRequestKey,
      answer:
        range && canReadHistory
          ? (await graphql<{ ListLocationPoints: LocationPoint[] }>(LIST_POINTS, { userId: viewedUserId, ...range }))
              .ListLocationPoints
          : [],
    }),
    [pointsRequestKey, userId, canReadHistory],
    { refresh: false },
  )
  const placesRequestKey = JSON.stringify([
    viewedUserId,
    placesLabel,
    placesQuery,
    placesPaging.offset,
    placesPaging.pageSize,
  ])
  const places = useQuery(
    async (): Promise<Answered<PlacePage> | null> =>
      tab === 'places' && canReadHistory
        ? {
            requestKey: placesRequestKey,
            answer: (
              await graphql<{ ListPlaces: PlacePage }>(LIST_PLACES, {
                userId: viewedUserId,
                placeLabel: placesLabel || null,
                query: placesQuery || null,
                offset: placesPaging.offset,
                limitCount: placesPaging.pageSize,
              })
            ).ListPlaces,
          }
        : null,
    [tab, placesRequestKey, userId, canReadHistory],
  )
  const visitPage = answeredFor(visits.data, visitsRequestKey)
  const placePage = answeredFor(places.data, placesRequestKey)
  const pointList = answeredFor(points.data, pointsRequestKey)

  // A page past the end, from a copied link or from the last row of the last
  // page having gone, steps back to the page before: an empty page is known
  // to be past the end, so the list holds no more than the rows before it.
  useKeepPageInRange(
    visitsPaging.pageIndex,
    visitsPaging.pageSize,
    visitPage && visitPage.placeVisits.length === 0 ? visitsPaging.offset : null,
    visitsPaging.setPageIndex,
  )
  useKeepPageInRange(
    placesPaging.pageIndex,
    placesPaging.pageSize,
    placePage && placePage.places.length === 0 ? placesPaging.offset : null,
    placesPaging.setPageIndex,
  )

  // The words typed to find a place are written to the address a moment after
  // the last keystroke, and start that list again from its first page.
  useEffect(() => {
    if (placesSearchText.trim() === placesQuery) return
    const timer = window.setTimeout(
      () => changeParameters({ placesQuery: placesSearchText.trim() || null, placesPage: null }),
      SEARCH_PATIENCE_MS,
    )
    return () => window.clearTimeout(timer)
    // changeParameters is new at every render and only writes the address.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [placesSearchText, placesQuery])

  const visitRows = useMemo(() => visitPage?.placeVisits ?? [], [visitPage])
  // The places a person named or labeled come first, then the ones the map
  // named; within each the server's order stands. A page is sorted as it
  // comes, so a labeled place on a later page waits for its page.
  const placeRows = useMemo(() => {
    const rank = (place: Place) => (place.placeLabel ? 0 : place.isNamedByPerson ? 1 : 2)
    return [...(placePage?.places ?? [])].sort((first, second) => rank(first) - rank(second))
  }, [placePage])

  // A viewer with no recorded position sees where this browser is, once the
  // person has allowed it or asks for it.
  const hasNoOwnPosition = me.data !== null && !me.data.GetLocation?.latestPoint

  const mapPeople: MapPerson[] = useMemo(
    () => [
      ...(hasNoOwnPosition && browserPosition ? [browserPosition] : []),
      ...views.flatMap((view) =>
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
    ],
    [views, selectedUserId, viewedUserId, hasNoOwnPosition, browserPosition],
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
    () => (pointList ?? []).map(({ latitudeDegrees, longitudeDegrees }) => ({ latitudeDegrees, longitudeDegrees })),
    [pointList],
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
    if (view.shareLevel === 'owner') {
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
  // them. Quietly, so the page does not go back to loading. What the live feed
  // read is dropped only once both answers are in, or every person would step
  // back to the older position for as long as the requests take.
  async function refreshPeople() {
    await Promise.all([me.reload(true), shared.reload(true)])
    setMovedViews({})
  }

  // A person the live feed says moved is read again by themselves, and the
  // person (the viewer) with the page's own reading. Somebody the page does not
  // know yet, who started sharing after the last full reading, and a person who
  // can no longer be read, because they stopped sharing, are each a reason to
  // read everybody again. The day's path is read again when it is the moved
  // person's.
  async function refreshMovedPerson(movedUserId: string) {
    if (movedUserId === (viewedUserId ?? userId)) void points.reload(true)
    if (movedUserId === userId) {
      await me.reload(true)
      return
    }
    if (!(shared.data ?? []).some((view) => view.userId === movedUserId)) {
      await refreshPeople()
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

  // Work done from a dialog. A failure stays in the dialog.
  async function act(work: () => Promise<unknown>, done: string, failed: string): Promise<boolean> {
    setIsBusy(true)
    setDialogProblem(null)
    try {
      await work()
      toast.done(done)
      reloadAll()
      return true
    } catch (failure) {
      setDialogProblem(failure instanceof Error && failure.message ? failure.message : failed)
      return false
    } finally {
      setIsBusy(false)
    }
  }

  // Erases everything, or the day when a range is given.
  async function erase(erasedRange: DayRange | null) {
    setIsBusy(true)
    setDialogProblem(null)
    try {
      const answer = await graphql<{
        EraseLocationHistory: { pointCount: number; visitCount: number; placeCount: number }
      }>(ERASE_LOCATION_HISTORY, erasedRange ?? {})
      toast.done(t('location.erased', answer.EraseLocationHistory))
      setErasing(null)
      setSelectedPlaceId(null)
      reloadAll()
      void points.reload()
    } catch (failure) {
      setDialogProblem(failure instanceof Error && failure.message ? failure.message : t('location.eraseFailed'))
    } finally {
      setIsBusy(false)
    }
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

  // The first time, with no position recorded yet, a short setup stands under
  // the map, until it is dismissed. It stands only while nobody shares with
  // the person either: a family that already shares is what the map is for.
  const isSetupShown =
    me.data !== null &&
    !me.data.GetLocation?.latestPoint &&
    !isSetupDismissed &&
    shared.data !== null &&
    shared.data.length === 0

  async function showBrowserPosition(isAsked: boolean) {
    setIsReadingBrowser(true)
    try {
      const answer = await readFreshBrowserLocation()
      if (answer.latitudeDegrees === undefined || answer.longitudeDegrees === undefined) {
        if (isAsked) toast.failed(t('location.checkInNoPosition'))
        return
      }
      // Drawn as the viewer, with their own picture or initial.
      const ownView = views.find((view) => view.userId === userId)
      setBrowserPosition({
        userId: 'browser',
        name: ownView?.name || t('location.you'),
        avatarUrl: ownView?.avatarUrl ?? undefined,
        avatarColorIndex: ownView?.avatarColorIndex ?? 3,
        latitudeDegrees: answer.latitudeDegrees,
        longitudeDegrees: answer.longitudeDegrees,
        accuracyMeters: answer.accuracyMeters ?? 0,
        isSelected: false,
      })
    } finally {
      setIsReadingBrowser(false)
    }
  }
  useEffect(() => {
    if (!hasNoOwnPosition) return
    let isCurrent = true
    void isLocationPermissionGranted().then((isGranted) => {
      if (isCurrent && isGranted) void showBrowserPosition(false)
    })
    return () => {
      isCurrent = false
    }
    // Asked when the page learns the person has no position of their own.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [hasNoOwnPosition])
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
      <ErrorMessage error={shared.error} />

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
        {hasNoOwnPosition && !browserPosition && (
          <div className="location-here">
            <span className="muted">{t('location.mapNoPosition')}</span>
            <button type="button" disabled={isReadingBrowser} onClick={() => void showBrowserPosition(true)}>
              {t('location.showWhereIAm')}
            </button>
          </div>
        )}
        <div className="location-people">
          {views.map((view) => {
            const name = personName(view)
            const isOwner = view.shareLevel === 'owner'
            return (
              <button
                key={view.userId}
                type="button"
                className={
                  view.userId === (viewedUserId ?? selectedUserId) ? 'location-row is-selected' : 'location-row'
                }
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
                    ) : isOwner && browserPosition ? (
                      t('location.browserHere', { meters: Math.round(browserPosition.accuracyMeters) })
                    ) : (
                      t(isOwner ? 'location.nowhere' : 'location.nowhereOther')
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
                  <>
                    {range && (
                      <button
                        type="button"
                        role="menuitem"
                        onClick={() => {
                          close()
                          setErasing({ range })
                        }}
                      >
                        {t('location.eraseDay')}
                      </button>
                    )}
                    <button
                      type="button"
                      role="menuitem"
                      onClick={() => {
                        close()
                        setErasing({ range: null })
                      }}
                    >
                      {t('location.eraseHistory')}
                    </button>
                  </>
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
            <LabelFilter
              value={visitsLabel}
              onChange={(placeLabel) => changeParameters({ visitsLabel: placeLabel || null, page: null })}
            />
          </div>
          <ErrorMessage error={visits.error} />
          {viewedPerson && !canReadHistory && <SettingsEmpty>{notSharedHistoryText}</SettingsEmpty>}
          {isPersonPending &&
            (isPersonUnreadable ? <SettingsEmpty>{t('location.personNotRead')}</SettingsEmpty> : <Loading />)}
          {canReadHistory && !visitPage && visits.loading && <Loading />}
          {visitPage && visitRows.length === 0 && (
            <SettingsEmpty>
              {visitsLabel
                ? t('location.visitsNoMatch')
                : dayKey
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
                  <span className="muted location-row-meta">{visitDurationText(visit, t)}</span>
                </button>
              </Fragment>
            )
          })}
          {(visitsPaging.pageIndex > 0 || visitPage?.nextOffset != null) && (
            <div className="list-foot">
              <Pager
                pageIndex={visitsPaging.pageIndex}
                pageSize={visitsPaging.pageSize}
                shownCount={visitRows.length}
                totalCount={visitsPaging.offset + visitRows.length + (visitPage?.nextOffset != null ? 1 : 0)}
                isTotalCountLowerBound={visitPage?.nextOffset != null}
                hasNextPage={visitPage?.nextOffset != null}
                isLoading={visits.loading && !visitPage}
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
          <div className="location-day-picker">
            <label>
              {t('location.searchPlaces')}
              <input
                type="search"
                value={placesSearchText}
                placeholder={t('location.searchPlaces')}
                onChange={(event) => setPlacesSearchText(event.target.value)}
              />
            </label>
            <LabelFilter
              value={placesLabel}
              onChange={(placeLabel) => changeParameters({ placesLabel: placeLabel || null, placesPage: null })}
            />
          </div>
          <ErrorMessage error={places.error} />
          {viewedPerson && !canReadHistory && <SettingsEmpty>{notSharedHistoryText}</SettingsEmpty>}
          {isPersonPending &&
            (isPersonUnreadable ? <SettingsEmpty>{t('location.personNotRead')}</SettingsEmpty> : <Loading />)}
          {canReadHistory && !placePage && places.loading && <Loading />}
          {placePage && placeRows.length === 0 && (
            <SettingsEmpty>
              {placesLabel || placesQuery
                ? t('location.placesNoMatch')
                : t(isReadOnly ? 'location.personPlacesEmpty' : 'location.placesEmpty')}
            </SettingsEmpty>
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
                {placeSubtitle(place, t) && <span className="muted">{placeSubtitle(place, t)}</span>}
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
                {LABEL_KEYS[place.placeLabel] && placeTitle(place, t) !== t(LABEL_KEYS[place.placeLabel]) && (
                  <Tag value={t(LABEL_KEYS[place.placeLabel])} />
                )}
              </span>
            </button>
          ))}
          {(placesPaging.pageIndex > 0 || placePage?.nextOffset != null) && (
            <div className="list-foot">
              <Pager
                pageIndex={placesPaging.pageIndex}
                pageSize={placesPaging.pageSize}
                shownCount={placeRows.length}
                totalCount={placesPaging.offset + placeRows.length + (placePage?.nextOffset != null ? 1 : 0)}
                isTotalCountLowerBound={placePage?.nextOffset != null}
                hasNextPage={placePage?.nextOffset != null}
                isLoading={places.loading && !placePage}
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
          error={dialogProblem}
          onClose={() => {
            setDialogProblem(null)
            setEditingPlace(null)
          }}
          onForget={() => {
            setDialogProblem(null)
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
          error={dialogProblem}
          onClose={() => {
            setDialogProblem(null)
            setForgettingPlace(null)
          }}
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

      {erasing && (
        <ConfirmDialog
          title={t(erasing.range ? 'location.eraseDayTitle' : 'location.eraseTitle')}
          body={
            erasing.range
              ? t('location.eraseDayBody', { day: dayFormat.format(dayStart ?? new Date()) })
              : t('location.eraseBody')
          }
          confirmLabel={t('location.eraseConfirm')}
          busy={isBusy}
          error={dialogProblem}
          onClose={() => {
            setDialogProblem(null)
            setErasing(null)
          }}
          onConfirm={() => void erase(erasing.range)}
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
  error,
  onClose,
  onForget,
  onSave,
}: {
  place: Place
  isBusy: boolean
  error: string | null
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
      error={error}
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
      <PlaceVisits placeId={place.id} />
    </FormDialog>
  )
}

// PlaceVisits lists the latest visits to one place, in the editor of the place.
function PlaceVisits({ placeId }: { placeId: string }) {
  const { t, language } = useTranslation()
  const visits = useQuery(
    async () =>
      (
        await graphql<{ ListLocationVisits: VisitPage }>(LIST_VISITS, {
          placeId,
          limitCount: PLACE_VISITS_SHOWN_COUNT,
        })
      ).ListLocationVisits,
    [placeId],
    { refresh: false },
  )
  const momentFormat = useMemo(
    () => new Intl.DateTimeFormat(language, { dateStyle: 'medium', timeStyle: 'short' }),
    [language],
  )
  const visitRows = visits.data?.placeVisits ?? []
  return (
    <section>
      <ErrorMessage error={visits.error} />
      <h4>{t('location.placeVisits')}</h4>
      {visits.data && visitRows.length === 0 && <p className="muted">{t('location.personVisitsEmpty')}</p>}
      {visitRows.length > 0 && (
        <ul className="location-place-visits">
          {visitRows.map((visit) => {
            const moment = visit.arrivedAt ?? visit.departedAt
            const duration = visitDurationText(visit, t)
            return (
              <li key={visit.id}>
                {moment ? momentFormat.format(new Date(moment)) : ''}
                {duration && <span className="muted"> · {duration}</span>}
              </li>
            )
          })}
        </ul>
      )}
      {visits.data?.nextOffset != null && (
        <p className="muted">{t('location.placeVisitsLatest', { visitCount: PLACE_VISITS_SHOWN_COUNT })}</p>
      )}
    </section>
  )
}

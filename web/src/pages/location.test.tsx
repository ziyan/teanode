import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useLocation, useNavigate } from 'react-router-dom'
import { act } from 'react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { APIError, graphql, subscribe } from '../api'
import { LocationPause, Place, PlaceVisit } from '../components/locationData'
import { ToastProvider } from '../components/toast'
import { TranslationProvider } from '../i18n/i18n'
import { SessionProvider } from '../session'
import { LocationPage } from './location'

vi.mock('../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../api')>()),
  graphql: vi.fn(),
  subscribe: vi.fn(),
}))
// WebGL is not there to draw a map in; the page's part is what it hands the map.
vi.mock('../components/locationMap', () => ({
  LocationMap: ({
    people,
    places,
    onPlaceMoved,
  }: {
    people: { name: string }[]
    places: { placeName: string }[]
    onPlaceMoved?: (latitudeDegrees: number, longitudeDegrees: number) => void
  }) => (
    <div data-testid="map" data-people={people.map((person) => person.name).join(',')}>
      {people.length} people, {places.length} places
      {onPlaceMoved && (
        <button type="button" onClick={() => onPlaceMoved(11.5, 21.5)}>
          Drop the pin at the test position
        </button>
      )}
    </div>
  ),
}))
// What the live feed was given: the function it calls with each event.
let feed: ((data: { LocationEvents: { userId: string } }) => void) | null = null
beforeEach(() => {
  feed = null
  window.localStorage.clear()
  vi.mocked(subscribe).mockImplementation(((_query: string, _variables: unknown, onData: typeof feed) => {
    feed = onData
    return () => {}
  }) as never)
})
afterEach(() => {
  cleanup()
  vi.resetAllMocks()
})

function place(overrides: Partial<Place> = {}): Place {
  return {
    id: 'place-one',
    placeName: 'Fixture Park',
    isNamedByPerson: false,
    placeLabel: '',
    latitudeDegrees: 10.5,
    longitudeDegrees: 20.5,
    radiusMeters: 100,
    osmName: 'Fixture Park',
    osmCategory: 'leisure',
    addressText: '',
    isAddressTyped: false,
    visitCount: 2,
    lastVisitAt: '2030-03-05T12:00:00Z',
    ...overrides,
  }
}

function visit(overrides: Partial<PlaceVisit> = {}): PlaceVisit {
  return {
    id: 'visit-one',
    place: place(),
    arrivedAt: '2030-03-05T12:00:00Z',
    departedAt: '2030-03-05T13:30:00Z',
    durationSeconds: 5400,
    ...overrides,
  }
}

const OWN_VIEW = {
  userId: 'person-a',
  username: 'person-a-login',
  name: 'Person A',
  avatarUrl: null,
  avatarColorIndex: 3,
  shareLevel: 'owner',
  latestPoint: {
    latitudeDegrees: 10.5,
    longitudeDegrees: 20.5,
    accuracyMeters: 20,
    measuredAt: '2030-03-05T12:00:00Z',
  },
  openVisit: null,
}

const PERSON_B_VIEW = {
  userId: 'person-b',
  username: 'person-b-login',
  name: 'Person B',
  avatarUrl: null,
  avatarColorIndex: 5,
  shareLevel: 'history',
  latestPoint: {
    latitudeDegrees: 11.5,
    longitudeDegrees: 21.5,
    accuracyMeters: 20,
    measuredAt: '2030-03-05T12:00:00Z',
  },
  openVisit: null,
}

function answerWith(shown: {
  own?: Record<string, unknown> | null
  sharedViews?: Record<string, unknown>[]
  movedView?: typeof PERSON_B_VIEW
  visits?: PlaceVisit[]
  places?: Place[]
  // The pauses ListLocationPauses answers with.
  pauses?: LocationPause[]
  failSharedLocations?: boolean
  // Answers the visits asked for, when what is asked changes the answer.
  visitsFor?: (variables: Record<string, unknown>) => PlaceVisit[] | Promise<PlaceVisit[]>
  people?: { userId: string; username: string; name: string }[]
  // The circles, for the members the person has a contact for.
  circles?: { members: { userId: string; linkedContactId: string }[] }[]
  // The places ReadPlace knows by id; any other id is not found.
  readablePlaces?: Place[]
}) {
  vi.mocked(graphql).mockImplementation(async (document, variables) => {
    if (document.includes('GetLocation')) {
      const askedUserId = (variables as { userId?: string } | undefined)?.userId
      if (askedUserId) return { GetLocation: shown.movedView ?? PERSON_B_VIEW } as never
      return { GetLocation: shown.own === undefined ? OWN_VIEW : shown.own && { ...OWN_VIEW, ...shown.own } } as never
    }
    if (document.includes('ListSharedLocations')) {
      if (shown.failSharedLocations) throw new Error('Cannot query field "ListSharedLocations"')
      return { ListSharedLocations: shown.sharedViews ?? [] } as never
    }
    if (document.includes('ListLocationVisits')) {
      const placeVisits = shown.visitsFor
        ? await shown.visitsFor((variables ?? {}) as Record<string, unknown>)
        : (shown.visits ?? [])
      return { ListLocationVisits: { placeVisits, nextOffset: null } } as never
    }
    if (document.includes('ReadPlace(')) {
      const askedPlaceId = (variables as { placeId: string }).placeId
      const found = (shown.readablePlaces ?? []).find((each) => each.id === askedPlaceId)
      if (!found) throw new APIError('api: not found')
      return { ReadPlace: found } as never
    }
    if (document.includes('ListLocationPoints')) return { ListLocationPoints: [] } as never
    if (document.includes('ListLocationPauses')) return { ListLocationPauses: shown.pauses ?? [] } as never
    if (document.includes('PauseLocation(')) {
      const endedAt = (variables as { endedAt: string | null }).endedAt
      return { PauseLocation: { id: 'pause-new', startedAt: new Date().toISOString(), endedAt } } as never
    }
    if (document.includes('ResumeLocation')) return { ResumeLocation: true } as never
    if (document.includes('ListPlaces'))
      return { ListPlaces: { places: shown.places ?? [], nextOffset: null } } as never
    if (document.includes('ListLocationShares')) return { ListLocationShares: [] } as never
    if (document.includes('ListCirclePeople')) return { ListCirclePeople: shown.people ?? [] } as never
    if (document.includes('ListCircles')) return { ListCircles: shown.circles ?? [] } as never
    if (document.includes('SearchAddresses')) {
      return {
        SearchAddresses: [
          {
            placeName: '',
            osmType: 'way',
            osmId: 77,
            osmCategory: 'building',
            addressText: '12 Elm Street, Exampleton',
            latitudeDegrees: 11.25,
            longitudeDegrees: 21.75,
            distanceMeters: 340,
            placeId: null,
            placeLabel: '',
            isNamedByPerson: false,
          },
        ],
      } as never
    }
    if (document.includes('EraseLocationHistory')) {
      return { EraseLocationHistory: { pointCount: 3, visitCount: 2, placeCount: 1 } } as never
    }
    return {} as never
  })
}

// The address the page is at, and a way back one entry, outside the page.
function AddressProbe() {
  const location = useLocation()
  const navigate = useNavigate()
  return (
    <>
      <span data-testid="address">{location.pathname + location.search}</span>
      <button type="button" onClick={() => navigate(-1)}>
        Step back
      </button>
    </>
  )
}

function renderAt(entry: string, options: { canUsePhotos?: boolean; earlierEntries?: string[] } = {}) {
  const earlierEntries = options.earlierEntries ?? []
  return render(
    <TranslationProvider>
      <ToastProvider>
        <SessionProvider
          value={{
            userId: 'person-a',
            authenticated: true,
            authenticationRequired: true,
            username: 'person-a-login',
            passkeysEnabled: false,
            permissions: {
              everywhere: ['location:use', 'circle:use', ...(options.canUsePhotos ? ['photos:use'] : [])],
              byDomain: [],
            },
            services: options.canUsePhotos ? ({ isPhotosEnabled: true } as never) : undefined,
          }}
        >
          <MemoryRouter initialEntries={[...earlierEntries, entry]} initialIndex={earlierEntries.length}>
            <Routes>
              <Route path="/location" element={<LocationPage />} />
              <Route path="/photos" element={<p>The photos page</p>} />
              <Route path="/people/:userId" element={<p>The card page</p>} />
              <Route path="/before" element={null} />
            </Routes>
            <AddressProbe />
          </MemoryRouter>
        </SessionProvider>
      </ToastProvider>
    </TranslationProvider>,
  )
}

function calls(operation: string) {
  return vi.mocked(graphql).mock.calls.filter(([document]) => document.includes(operation))
}

it('lists visits by day with how long each lasted', async () => {
  answerWith({
    visits: [
      visit(),
      visit({
        id: 'visit-two',
        place: place({ id: 'place-two', placeName: 'Fixture Library', isNamedByPerson: true, placeLabel: 'school' }),
        arrivedAt: '2030-03-02T12:00:00Z',
        departedAt: '2030-03-02T12:25:00Z',
        durationSeconds: 1500,
      }),
    ],
  })
  renderAt('/location')

  expect(await screen.findByText('Fixture Park')).toBeTruthy()
  expect(screen.getByText('Fixture Library')).toBeTruthy()
  expect(screen.getByText('1 h 30 min')).toBeTruthy()
  expect(screen.getByText('25 min')).toBeTruthy()
  // Two days, each with a heading that chooses it.
  expect(screen.getAllByRole('heading', { level: 4 })).toHaveLength(2)
  expect(screen.getByText('© OpenStreetMap contributors')).toBeTruthy()
})

it('saves the label chosen for a place', async () => {
  answerWith({ places: [place()] })
  renderAt('/location?tab=places')

  fireEvent.click(await screen.findByRole('button', { name: /Fixture Park/ }))
  const dialog = await screen.findByRole('dialog')
  fireEvent.click(within(dialog).getByRole('combobox', { name: 'Label' }))
  fireEvent.click(screen.getByRole('option', { name: 'Work' }))
  fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }))

  await waitFor(() => expect(calls('SavePlace')).toHaveLength(1))
  expect(calls('SavePlace')[0][1]).toEqual({ placeId: 'place-one', placeLabel: 'work' })
})

it('shares where the person has been with the circle member chosen', async () => {
  answerWith({ people: [{ userId: 'person-b', username: 'person-b-login', name: 'Person B' }] })
  renderAt('/location?tab=sharing')

  fireEvent.click(await screen.findByRole('combobox', { name: 'Share with' }))
  fireEvent.click(await screen.findByRole('option', { name: /Person B/ }))
  fireEvent.click(screen.getByRole('combobox', { name: /What they see/ }))
  fireEvent.click(screen.getByRole('option', { name: /where I have been/ }))
  fireEvent.click(screen.getByRole('button', { name: 'Share' }))

  await waitFor(() => expect(calls('ShareLocation(')).toHaveLength(1))
  expect(calls('ShareLocation(')[0][1]).toEqual({
    granteeUserId: 'person-b',
    granteeCircleId: null,
    shareLevel: 'history',
  })
})

it('says in a toast that the people sharing cannot be read, and still shows the person and their visits', async () => {
  answerWith({ visits: [visit()], failSharedLocations: true })
  renderAt('/location')

  expect(await screen.findByText('Fixture Park')).toBeTruthy()
  expect(screen.getByText('Person A')).toBeTruthy()
  expect(screen.getByTestId('map').textContent).toBe('1 people, 1 places')
  expect(await screen.findByText(/Cannot query field "ListSharedLocations"/)).toBeTruthy()
})

it("gives a person's arrival its day when it was not today", async () => {
  const now = new Date()
  const daysAgo = new Date(now.getTime() - 3 * 24 * 60 * 60 * 1000)
  answerWith({
    sharedViews: [
      {
        ...PERSON_B_VIEW,
        openVisit: { id: 'visit-old', arrivedAt: daysAgo.toISOString(), place: place({ placeName: 'Fixture Park' }) },
      },
      {
        ...PERSON_B_VIEW,
        userId: 'person-c',
        name: 'Person C',
        openVisit: { id: 'visit-today', arrivedAt: now.toISOString(), place: place({ placeName: 'Fixture Cafe' }) },
      },
    ] as never,
  })
  renderAt('/location')

  const weekday = new Intl.DateTimeFormat('en', { weekday: 'short' }).format(daysAgo)
  expect((await screen.findByText(/At Fixture Park since/)).textContent).toContain(weekday)
  const today = (await screen.findByText(/At Fixture Cafe since/)).textContent ?? ''
  expect(today).not.toContain(new Intl.DateTimeFormat('en', { weekday: 'short' }).format(now))
})

it('says a visit begun more than a week ago by its date alone', async () => {
  const weeksAgo = new Date(Date.now() - 20 * 24 * 60 * 60 * 1000)
  answerWith({
    sharedViews: [
      {
        ...PERSON_B_VIEW,
        openVisit: { id: 'visit-old', arrivedAt: weeksAgo.toISOString(), place: place({ placeName: 'Fixture Park' }) },
      },
    ] as never,
  })
  renderAt('/location')
  const shown = (await screen.findByText(/At Fixture Park since/)).textContent ?? ''
  expect(shown).toBe(
    `At Fixture Park since ${new Intl.DateTimeFormat('en', { month: 'short', day: 'numeric' }).format(weeksAgo)}`,
  )
})

it('reads a person again when the live feed says they moved', async () => {
  answerWith({
    sharedViews: [PERSON_B_VIEW],
    movedView: {
      ...PERSON_B_VIEW,
      openVisit: { id: 'visit-now', arrivedAt: '2030-03-05T14:05:00Z', place: place({ placeName: 'Fixture Cafe' }) },
    } as never,
  })
  renderAt('/location')

  expect(await screen.findByText('Person B')).toBeTruthy()
  expect(screen.queryByText(/Fixture Cafe/)).toBeNull()
  await waitFor(() => expect(feed).not.toBeNull())

  act(() => feed?.({ LocationEvents: { userId: 'person-b' } }))

  expect(await screen.findByText(/At Fixture Cafe since/)).toBeTruthy()
  const asked = calls('GetLocation').filter(([, variables]) => (variables as { userId?: string })?.userId)
  expect(asked).toHaveLength(1)
  expect(asked[0][1]).toEqual({ userId: 'person-b' })

  // The viewer's own movement reads the viewer's position again, not a person's.
  const before = calls('GetLocation').length
  act(() => feed?.({ LocationEvents: { userId: 'person-a' } }))
  await waitFor(() => expect(calls('GetLocation').length).toBe(before + 1))
  expect(calls('GetLocation').slice(-1)[0]?.[1]).toBeUndefined()
})

it('lists the visits of a person who shares their history, read only, with the person in the address', async () => {
  answerWith({ sharedViews: [PERSON_B_VIEW], visits: [visit()] })
  renderAt('/location')

  fireEvent.click(await screen.findByRole('button', { name: /Person B/ }))

  expect(await screen.findByText('Visits of Person B')).toBeTruthy()
  await waitFor(() =>
    expect(
      calls('ListLocationVisits').some(([, variables]) => (variables as { userId?: string }).userId === 'person-b'),
    ).toBe(true),
  )
  // Nothing of theirs can be erased.
  expect(screen.queryByRole('button', { name: 'More about visits' })).toBeNull()
})

it('says so when a person shares only where they are now', async () => {
  answerWith({ sharedViews: [{ ...PERSON_B_VIEW, shareLevel: 'current' }] })
  renderAt('/location?person=person-b')

  expect(await screen.findByText('Person B shares only where they are now.')).toBeTruthy()
  expect(calls('ListLocationVisits')).toHaveLength(0)
})

it('sets Location up when no position is recorded, and not again after Not now', async () => {
  answerWith({ own: { latestPoint: null } })
  const first = renderAt('/location')

  expect(await screen.findByText('Install the TeaNode app on your iPhone')).toBeTruthy()
  expect(screen.getByText('This is home')).toBeTruthy()
  // The map is there with the setup, which stands under it.
  const map = screen.getByTestId('map')
  expect(map).toBeTruthy()
  expect(map.compareDocumentPosition(screen.getByText('This is home')) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()

  fireEvent.click(screen.getByRole('button', { name: 'Not now' }))
  expect(screen.queryByText('Install the TeaNode app on your iPhone')).toBeNull()
  expect(screen.getByTestId('map')).toBeTruthy()

  first.unmount()
  renderAt('/location')
  expect(await screen.findByTestId('map')).toBeTruthy()
  expect(screen.queryByText('Install the TeaNode app on your iPhone')).toBeNull()
})

it('marks home from the browser position, and offers to share with a family', async () => {
  answerWith({ own: { latestPoint: null } })
  const base = vi.mocked(graphql).getMockImplementation()
  vi.mocked(graphql).mockImplementation(async (document, variables) => {
    if (document.includes('ListCircles')) {
      return { ListCircles: [{ id: 'circle-one', name: 'Fixture Home', circleKind: 'family' }] } as never
    }
    return base!(document, variables)
  })
  Object.defineProperty(window, 'isSecureContext', { value: true, configurable: true })
  Object.defineProperty(navigator, 'geolocation', {
    configurable: true,
    value: {
      getCurrentPosition: (success: (position: unknown) => void) =>
        success({ coords: { latitude: 10.5, longitude: 20.5, accuracy: 15 }, timestamp: 1_900_000_000_000 }),
    },
  })
  renderAt('/location')

  fireEvent.click(await screen.findByRole('button', { name: 'Use where I am now' }))
  await waitFor(() => expect(calls('SavePlace')).toHaveLength(1))
  expect(calls('SavePlace')[0][1]).toEqual({ latitudeDegrees: 10.5, longitudeDegrees: 20.5, placeLabel: 'home' })

  fireEvent.click(await screen.findByRole('button', { name: 'Share where I am now with Fixture Home' }))
  await waitFor(() => expect(calls('ShareLocation(')).toHaveLength(1))
  expect(calls('ShareLocation(')[0][1]).toEqual({ granteeCircleId: 'circle-one', shareLevel: 'current' })
})

const PERSON_C_VIEW = { ...PERSON_B_VIEW, userId: 'person-c', username: 'person-c-login', name: 'Person C' }

function stubBrowserPosition() {
  Object.defineProperty(window, 'isSecureContext', { value: true, configurable: true })
  Object.defineProperty(navigator, 'geolocation', {
    configurable: true,
    value: {
      getCurrentPosition: (success: (position: unknown) => void) =>
        success({ coords: { latitude: 10.5, longitude: 20.5, accuracy: 15 }, timestamp: 1_900_000_000_000 }),
    },
  })
}

it('lets a person open a row that has no position of its own, to leave or to see their visits', async () => {
  answerWith({ sharedViews: [{ ...PERSON_B_VIEW, latestPoint: null as never }], visits: [visit()] })
  renderAt('/location')

  const row = (await screen.findByRole('button', { name: /Person B/ })) as HTMLButtonElement
  expect(row.disabled).toBe(false)
  fireEvent.click(row)
  expect(await screen.findByText('Visits of Person B')).toBeTruthy()
})

it('does not leave the rows of one person under the heading of the next while theirs are on the way', async () => {
  let release: (rows: PlaceVisit[]) => void = () => {}
  const pending = new Promise<PlaceVisit[]>((resolve) => {
    release = resolve
  })
  answerWith({
    sharedViews: [PERSON_B_VIEW, PERSON_C_VIEW],
    visitsFor: (variables) =>
      variables.userId === 'person-b' ? [visit()] : variables.userId === 'person-c' ? pending : [],
  })
  renderAt('/location')

  fireEvent.click(await screen.findByRole('button', { name: /Person B/ }))
  expect(await screen.findByText('Fixture Park')).toBeTruthy()

  fireEvent.click(screen.getByRole('button', { name: /Person C/ }))
  expect(await screen.findByText('Visits of Person C')).toBeTruthy()
  expect(screen.queryByText('Fixture Park')).toBeNull()

  await act(async () => {
    release([visit({ id: 'visit-cafe', place: place({ id: 'place-cafe', placeName: 'Fixture Cafe' }) })])
  })
  expect(await screen.findByText('Fixture Cafe')).toBeTruthy()
})

it('steps back to the last page that has rows when the address names a page past the end', async () => {
  answerWith({
    visitsFor: (variables) => (variables.offset === 0 ? [visit()] : []),
  })
  renderAt('/location?page=3')

  expect(await screen.findByText('Fixture Park')).toBeTruthy()
  const offsets = calls('ListLocationVisits').map(([, variables]) => (variables as { offset: number }).offset)
  expect(offsets).toContain(100)
  expect(offsets).toContain(0)
})

it('reads everybody again when the live feed names a person the page does not know yet', async () => {
  answerWith({ sharedViews: [] })
  renderAt('/location')
  await screen.findByText('Person A')
  await waitFor(() => expect(feed).not.toBeNull())
  const before = calls('ListSharedLocations').length

  act(() => feed?.({ LocationEvents: { userId: 'person-z' } }))

  await waitFor(() => expect(calls('ListSharedLocations').length).toBe(before + 1))
  // Nobody is asked for by themselves: they are not known to share.
  expect(
    calls('GetLocation').filter(([, variables]) => (variables as { userId?: string })?.userId === 'person-z'),
  ).toHaveLength(0)
})

it('does not read the day on a timer, only when the page or the viewed person asks', async () => {
  answerWith({ visits: [visit()] })
  renderAt('/location?day=2030-03-05')
  await screen.findByText('Fixture Park')
  await waitFor(() => expect(calls('ListLocationPoints')).toHaveLength(1))

  // A moved viewer reads their path again; nobody else's move does.
  await waitFor(() => expect(feed).not.toBeNull())
  act(() => feed?.({ LocationEvents: { userId: 'person-a' } }))
  await waitFor(() => expect(calls('ListLocationPoints')).toHaveLength(2))
})

it("draws a day's path from the Map button on its heading", async () => {
  answerWith({ visits: [visit()] })
  renderAt('/location')
  await screen.findByText('Fixture Park')
  // Every day is listed and no path is read until a day is chosen.
  expect(calls('ListLocationPoints')).toHaveLength(0)

  fireEvent.click(screen.getByRole('button', { name: /^Show the path of / }))
  await waitFor(() => expect(calls('ListLocationPoints')).toHaveLength(1))
  // The chosen day's heading has no button: its path is the one drawn.
  expect(screen.queryByRole('button', { name: /^Show the path of / })).toBeNull()
})

it('erases the selected day, or everything, with the sentence that says what stays', async () => {
  answerWith({ visits: [visit()] })
  renderAt('/location?day=2030-03-05')

  await screen.findByText('Fixture Park')
  fireEvent.click(screen.getByRole('button', { name: 'More about visits' }))
  fireEvent.click(await screen.findByRole('menuitem', { name: 'Erase this day…' }))
  let dialog = await screen.findByRole('alertdialog')
  expect(within(dialog).getByText(/Places you named or labeled stay\./)).toBeTruthy()
  expect(within(dialog).getByText(/stays in its memory until you remove it/)).toBeTruthy()
  fireEvent.click(within(dialog).getByRole('button', { name: 'Erase' }))

  await waitFor(() => expect(calls('EraseLocationHistory')).toHaveLength(1))
  const asked = calls('EraseLocationHistory')[0][1] as { from: string; until: string }
  expect(new Date(asked.until).getTime()).toBeGreaterThan(new Date(asked.from).getTime())
  await waitFor(() => expect(screen.queryByRole('alertdialog')).toBeNull())

  fireEvent.click(screen.getByRole('button', { name: 'More about visits' }))
  fireEvent.click(await screen.findByRole('menuitem', { name: 'Erase positions and visits…' }))
  dialog = await screen.findByRole('alertdialog')
  fireEvent.click(within(dialog).getByRole('button', { name: 'Erase' }))
  await waitFor(() => expect(calls('EraseLocationHistory')).toHaveLength(2))
  expect(calls('EraseLocationHistory')[1][1]).toEqual({})
})

it('offers Erase this day only when a day is selected', async () => {
  answerWith({ visits: [visit()] })
  renderAt('/location')
  await screen.findByText('Fixture Park')
  fireEvent.click(screen.getByRole('button', { name: 'More about visits' }))
  expect(await screen.findByRole('menuitem', { name: 'Erase positions and visits…' })).toBeTruthy()
  expect(screen.queryByRole('menuitem', { name: 'Erase this day…' })).toBeNull()
})

it('narrows the places by label and by the words typed, and not the visits', async () => {
  answerWith({ visits: [visit()], places: [place()] })
  renderAt('/location')
  await screen.findByText('Fixture Park')
  expect(screen.queryByRole('combobox', { name: 'Label' })).toBeNull()

  cleanup()
  vi.mocked(graphql).mockClear()
  answerWith({ places: [place()] })
  renderAt('/location?tab=places')
  await screen.findByRole('button', { name: /Fixture Park/ })

  fireEvent.click(screen.getByRole('combobox', { name: 'Label' }))
  fireEvent.click(screen.getByRole('option', { name: 'Work' }))
  await waitFor(() =>
    expect(
      calls('ListPlaces(userId').some(([, variables]) => (variables as { placeLabel?: string }).placeLabel === 'work'),
    ).toBe(true),
  )
  fireEvent.change(screen.getByLabelText('Search places'), { target: { value: 'cafe' } })
  await waitFor(() =>
    expect(calls('ListPlaces(userId').some(([, variables]) => (variables as { query?: string }).query === 'cafe')).toBe(
      true,
    ),
  )
})

it('lists the visits to a place in its editor', async () => {
  answerWith({ places: [place()], visits: [visit()] })
  renderAt('/location?tab=places')

  fireEvent.click(await screen.findByRole('button', { name: /Fixture Park/ }))
  const dialog = await screen.findByRole('dialog')
  expect(await within(dialog).findByText('Visits to this place')).toBeTruthy()
  await waitFor(() =>
    expect(
      calls('ListLocationVisits').some(([, variables]) => (variables as { placeId?: string }).placeId === 'place-one'),
    ).toBe(true),
  )
  expect(within(dialog).getByText(/1 h 30 min/)).toBeTruthy()
})

it('labels the place that holds the browser position as home, instead of making a second one', async () => {
  answerWith({ own: { latestPoint: null } })
  const base = vi.mocked(graphql).getMockImplementation()
  vi.mocked(graphql).mockImplementation(async (document, variables) => {
    if (document.includes('ListPlaces') && document.includes('limitCount: 200')) {
      return {
        ListPlaces: {
          places: [
            { id: 'place-far', latitudeDegrees: 10.6, longitudeDegrees: 20.6, radiusMeters: 100 },
            { id: 'place-held', latitudeDegrees: 10.5003, longitudeDegrees: 20.5, radiusMeters: 100 },
          ],
        },
      } as never
    }
    return base!(document, variables)
  })
  stubBrowserPosition()
  renderAt('/location')

  fireEvent.click(await screen.findByRole('button', { name: 'Use where I am now' }))
  await waitFor(() => expect(calls('SavePlace')).toHaveLength(1))
  expect(calls('SavePlace')[0][1]).toEqual({ placeId: 'place-held', placeLabel: 'home' })
})

it('says where place names come from in the setup, and gives the install row no badge', async () => {
  answerWith({ own: { latestPoint: null } })
  renderAt('/location')
  expect(await screen.findByText(/Place names come from OpenStreetMap: your server sends the position/)).toBeTruthy()
  const installRow = screen.getByText('Install the TeaNode app on your iPhone').closest('.settings-row')
  expect(installRow?.querySelector('.tag')).toBeNull()
})

it('puts the setup row for the phone app behind a button, not a link', async () => {
  answerWith({ own: { latestPoint: null } })
  renderAt('/location')
  expect(await screen.findByRole('button', { name: 'Set up the iPhone app' })).toBeTruthy()
  expect(screen.queryByRole('link')).toBeNull()
})

it('shows where the browser is on the map when the person has no position, once they ask', async () => {
  answerWith({ own: { latestPoint: null } })
  stubBrowserPosition()
  renderAt('/location')

  expect((await screen.findByTestId('map')).getAttribute('data-people')).toBe('')
  fireEvent.click(await screen.findByRole('button', { name: 'Show where I am' }))
  await waitFor(() => expect(screen.getByTestId('map').getAttribute('data-people')).toBe('Person A'))
  expect(screen.queryByRole('button', { name: 'Show where I am' })).toBeNull()
})

it('reads the browser position at once when the permission is already granted', async () => {
  answerWith({ own: { latestPoint: null } })
  stubBrowserPosition()
  Object.defineProperty(navigator, 'permissions', {
    configurable: true,
    value: { query: async () => ({ state: 'granted' }) },
  })
  renderAt('/location')

  await waitFor(() => expect(screen.getByTestId('map').getAttribute('data-people')).toBe('Person A'))
  Object.defineProperty(navigator, 'permissions', { configurable: true, value: undefined })
})

it('does not draw the browser position for a person who has a position recorded', async () => {
  answerWith({})
  stubBrowserPosition()
  renderAt('/location')

  expect((await screen.findByTestId('map')).getAttribute('data-people')).toBe('Person A')
  expect(screen.queryByRole('button', { name: 'Show where I am' })).toBeNull()
})

it('checks in here with the coordinates alone, and says how precise the browser is', async () => {
  answerWith({})
  const base = vi.mocked(graphql).getMockImplementation()
  vi.mocked(graphql).mockImplementation(async (document, variables) => {
    if (document.includes('ListNearbyPlaces')) return { ListNearbyPlaces: [] } as never
    if (document.includes('CheckIn')) return { CheckIn: { id: 'visit-new' } } as never
    return base!(document, variables)
  })
  stubBrowserPosition()
  renderAt('/location')

  fireEvent.click(await screen.findByRole('button', { name: 'Check in' }))
  const dialog = await screen.findByRole('alertdialog')
  expect(await within(dialog).findByText('Your browser places you within 15 m.')).toBeTruthy()
  fireEvent.click(within(dialog).getByRole('button', { name: /Check in here/ }))
  await waitFor(() => expect(calls('CheckIn(')).toHaveLength(1))
  expect(calls('CheckIn(')[0][1]).toEqual({ latitudeDegrees: 10.5, longitudeDegrees: 20.5, accuracyMeters: 15 })
})

function stubBrowserAccuracy(accuracy: number) {
  Object.defineProperty(window, 'isSecureContext', { value: true, configurable: true })
  Object.defineProperty(navigator, 'geolocation', {
    configurable: true,
    value: {
      getCurrentPosition: (success: (position: unknown) => void) =>
        success({ coords: { latitude: 10.5, longitude: 20.5, accuracy }, timestamp: 1_900_000_000_000 }),
    },
  })
}

function answerCheckInWithNothingNearby() {
  const base = vi.mocked(graphql).getMockImplementation()
  vi.mocked(graphql).mockImplementation(async (document, variables) => {
    if (document.includes('ListNearbyPlaces')) return { ListNearbyPlaces: [] } as never
    if (document.includes('CheckIn')) return { CheckIn: { id: 'visit-new' } } as never
    return base!(document, variables)
  })
}

it('says plainly when the browser is sure only to 850 m, offers the typed address, and still lets them check in', async () => {
  answerWith({})
  answerCheckInWithNothingNearby()
  stubBrowserAccuracy(850)
  renderAt('/location')

  fireEvent.click(await screen.findByRole('button', { name: 'Check in' }))
  const dialog = await screen.findByRole('alertdialog')
  expect(
    await within(dialog).findByText(
      'Your browser can only place you within 850 m. A computer has no GPS; your iPhone is exact.',
    ),
  ).toBeTruthy()
  expect(within(dialog).getByRole('button', { name: 'Type my address' })).toBeTruthy()
  expect((within(dialog).getByRole('button', { name: /Check in here/ }) as HTMLButtonElement).disabled).toBe(false)
  fireEvent.click(within(dialog).getByRole('button', { name: /Check in here/ }))
  await waitFor(() => expect(calls('CheckIn(')).toHaveLength(1))
  expect(calls('CheckIn(')[0][1]).toMatchObject({ accuracyMeters: 850 })
})

it('turns Check in here off when the browser is sure only to 1500 m, and checks in at a typed address', async () => {
  answerWith({})
  answerCheckInWithNothingNearby()
  stubBrowserAccuracy(1500)
  renderAt('/location')

  fireEvent.click(await screen.findByRole('button', { name: 'Check in' }))
  const dialog = await screen.findByRole('alertdialog')
  expect(await within(dialog).findByText(/can only place you within 1500 m/)).toBeTruthy()
  expect((within(dialog).getByRole('button', { name: /Check in here/ }) as HTMLButtonElement).disabled).toBe(true)

  fireEvent.click(within(dialog).getByRole('button', { name: 'Type my address' }))
  const chooser = await screen.findByRole('alertdialog')
  fireEvent.change(within(chooser).getByLabelText('Type an address'), { target: { value: '12 Elm' } })
  fireEvent.click(within(chooser).getByRole('button', { name: 'Search' }))
  expect(calls('SearchAddresses')[0][1]).toEqual({ query: '12 Elm', placeId: undefined })
  fireEvent.click(await within(chooser).findByRole('button', { name: /12 Elm Street, Exampleton/ }))
  await waitFor(() => expect(calls('CheckIn(')).toHaveLength(1))
  expect(calls('CheckIn(')[0][1]).toMatchObject({
    latitudeDegrees: 11.25,
    longitudeDegrees: 21.75,
    addressText: '12 Elm Street, Exampleton',
  })
})

it('finds a place by words around the browser position and checks in to it', async () => {
  answerWith({})
  const base = vi.mocked(graphql).getMockImplementation()
  const found = {
    placeName: 'Fixture Bakery',
    osmType: 'node',
    osmId: 42,
    osmCategory: 'shop',
    addressText: '1 Fixture Street',
    latitudeDegrees: 10.5,
    longitudeDegrees: 20.5,
    distanceMeters: 30,
    placeId: null,
  }
  vi.mocked(graphql).mockImplementation(async (document, variables) => {
    if (document.includes('ListNearbyPlaces')) return { ListNearbyPlaces: [] } as never
    if (document.includes('SearchPlaces')) return { SearchPlaces: { foundPlaces: [found], nextOffset: null } } as never
    return base!(document, variables)
  })
  stubBrowserPosition()
  renderAt('/location')

  fireEvent.click(await screen.findByRole('button', { name: 'Check in' }))
  const dialog = await screen.findByRole('alertdialog')
  expect(await within(dialog).findByText('No place with a name is near you.')).toBeTruthy()
  fireEvent.change(within(dialog).getByLabelText('Find a place'), { target: { value: 'bakery' } })
  fireEvent.click(within(dialog).getByRole('button', { name: 'Search' }))

  expect(await within(dialog).findByText('Fixture Bakery')).toBeTruthy()
  expect(calls('SearchPlaces')[0][1]).toEqual({
    query: 'bakery',
    offset: 0,
    latitudeDegrees: 10.5,
    longitudeDegrees: 20.5,
  })
  fireEvent.click(within(dialog).getByRole('button', { name: /Fixture Bakery/ }))
  await waitFor(() => expect(calls('CheckIn(')).toHaveLength(1))
  expect(calls('CheckIn(')[0][1]).toMatchObject({ placeName: 'Fixture Bakery', osmType: 'node', osmId: 42 })
})

it('calls a labeled place by its label word, with the map name under it, and a named one by its name', async () => {
  answerWith({
    places: [
      place({ id: 'place-labeled', placeName: 'Fixture Cafe', osmName: 'Fixture Cafe', placeLabel: 'school' }),
      place({
        id: 'place-named',
        placeName: 'Piano teacher',
        isNamedByPerson: true,
        placeLabel: 'school',
        osmName: 'Fixture Hall',
      }),
    ],
  })
  renderAt('/location?tab=places')

  expect(await screen.findByRole('button', { name: /^School\s*Fixture Cafe/ })).toBeTruthy()
  expect(screen.getByRole('button', { name: /^Piano teacher/ })).toBeTruthy()
  expect(screen.queryByText('Fixture Hall')).toBeNull()
})

it('sets Location up for someone who joins a family that already shares, keeping the map and the people', async () => {
  answerWith({ own: { latestPoint: null }, sharedViews: [PERSON_B_VIEW] })
  renderAt('/location')

  expect(await screen.findByTestId('map')).toBeTruthy()
  expect(await screen.findByText('Install the TeaNode app on your iPhone')).toBeTruthy()
  expect(await screen.findByText('Person B')).toBeTruthy()
})

it("says that somebody with no position yet is not the viewer's phone to record", async () => {
  answerWith({ sharedViews: [{ ...PERSON_B_VIEW, latestPoint: null as never }] })
  renderAt('/location')

  expect(await screen.findByText('No position yet.')).toBeTruthy()
  expect(screen.queryByText(/The TeaNode app on your phone records where you go, and it appears here/)).toBeNull()
})

it('drops a person from the address who does not share, says so, and shows the own visits', async () => {
  answerWith({ visits: [visit()] })
  renderAt('/location?person=nobody')

  expect(await screen.findByText('That person does not share their location with you.')).toBeTruthy()
  expect(await screen.findByText('Fixture Park')).toBeTruthy()
  expect(screen.getByRole('heading', { name: 'Visits' })).toBeTruthy()
})

it('says the list cannot be shown when the people could not be read for a person in the address', async () => {
  answerWith({ failSharedLocations: true })
  renderAt('/location?person=person-b')

  expect(
    await screen.findByText('The people who share with you could not be read, so this list cannot be shown.'),
  ).toBeTruthy()
})

it('keeps the dialog open with what was typed when a save is refused', async () => {
  answerWith({ places: [place()] })
  const base = vi.mocked(graphql).getMockImplementation()
  vi.mocked(graphql).mockImplementation(async (document, variables) => {
    if (document.includes('SavePlace')) throw new Error('The name is too long')
    return base!(document, variables)
  })
  renderAt('/location?tab=places')

  fireEvent.click(await screen.findByRole('button', { name: /Fixture Park/ }))
  const dialog = await screen.findByRole('dialog')
  fireEvent.change(within(dialog).getByLabelText('Name'), { target: { value: 'Typed name' } })
  fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }))

  // The dialog's error is spoken by the page's toast, and the dialog stays.
  expect(await screen.findByText('The name is too long')).toBeTruthy()
  expect(screen.getByRole('dialog')).toBeTruthy()
  expect((within(dialog).getByLabelText('Name') as HTMLInputElement).value).toBe('Typed name')
})

it('does not make home from a browser that is only sure to 850 m, and says so beside Type my address', async () => {
  answerWith({ own: { latestPoint: null } })
  stubBrowserAccuracy(850)
  renderAt('/location')

  fireEvent.click(await screen.findByRole('button', { name: 'Use where I am now' }))
  expect(await screen.findByText(/can only place you within 850 m/)).toBeTruthy()
  expect(calls('SavePlace')).toHaveLength(0)
  expect(screen.getByRole('button', { name: 'Type my address' })).toBeTruthy()
  expect((screen.getByRole('button', { name: 'Use where I am now' }) as HTMLButtonElement).disabled).toBe(false)
})

it('turns Use where I am now off once the browser said it is only sure to 1500 m', async () => {
  answerWith({ own: { latestPoint: null } })
  stubBrowserAccuracy(1500)
  renderAt('/location')

  fireEvent.click(await screen.findByRole('button', { name: 'Use where I am now' }))
  expect(await screen.findByText(/can only place you within 1500 m/)).toBeTruthy()
  expect((screen.getByRole('button', { name: 'Use where I am now' }) as HTMLButtonElement).disabled).toBe(true)
  expect(calls('SavePlace')).toHaveLength(0)
})

it('shows the address of a place and moves the place to a typed address', async () => {
  answerWith({ places: [place({ addressText: '1 Old Road, Exampleton' })] })
  renderAt('/location?tab=places')

  fireEvent.click(await screen.findByRole('button', { name: /Fixture Park/ }))
  const editor = await screen.findByRole('dialog')
  expect(within(editor).getByText('1 Old Road, Exampleton')).toBeTruthy()
  fireEvent.click(within(editor).getByRole('button', { name: 'Change address' }))

  const chooser = await screen.findByRole('alertdialog')
  fireEvent.change(within(chooser).getByLabelText('Type an address'), { target: { value: '12 Elm' } })
  fireEvent.click(within(chooser).getByRole('button', { name: 'Search' }))
  expect(calls('SearchAddresses')[0][1]).toEqual({ query: '12 Elm', placeId: 'place-one' })
  fireEvent.click(await within(chooser).findByRole('button', { name: /12 Elm Street, Exampleton/ }))

  await waitFor(() => expect(calls('SavePlace')).toHaveLength(1))
  expect(calls('SavePlace')[0][1]).toEqual({
    placeId: 'place-one',
    latitudeDegrees: 11.25,
    longitudeDegrees: 21.75,
    addressText: '12 Elm Street, Exampleton',
  })
  expect(await screen.findByText('Moved to 12 Elm Street, Exampleton')).toBeTruthy()
  await waitFor(() => expect(screen.queryByRole('alertdialog')).toBeNull())
})

it('says there is no address for a place without one, and shows a failed address search in the chooser', async () => {
  answerWith({ places: [place()] })
  const base = vi.mocked(graphql).getMockImplementation()
  vi.mocked(graphql).mockImplementation(async (document, variables) => {
    if (document.includes('SearchAddresses')) throw new Error('place provider is off')
    return base!(document, variables)
  })
  renderAt('/location?tab=places')

  fireEvent.click(await screen.findByRole('button', { name: /Fixture Park/ }))
  const editor = await screen.findByRole('dialog')
  expect(within(editor).getByText('No address')).toBeTruthy()
  fireEvent.click(within(editor).getByRole('button', { name: 'Change address' }))
  const chooser = await screen.findByRole('alertdialog')
  fireEvent.change(within(chooser).getByLabelText('Type an address'), { target: { value: 'nowhere' } })
  fireEvent.click(within(chooser).getByRole('button', { name: 'Search' }))
  expect(await screen.findByText('place provider is off')).toBeTruthy()
  expect(calls('SavePlace')).toHaveLength(0)
})

it('makes home at a typed address from the setup', async () => {
  answerWith({ own: { latestPoint: null } })
  renderAt('/location')

  fireEvent.click(await screen.findByRole('button', { name: 'Type my address' }))
  const chooser = await screen.findByRole('alertdialog')
  fireEvent.change(within(chooser).getByLabelText('Type an address'), { target: { value: '12 Elm' } })
  fireEvent.click(within(chooser).getByRole('button', { name: 'Search' }))
  fireEvent.click(await within(chooser).findByRole('button', { name: /12 Elm Street, Exampleton/ }))

  await waitFor(() => expect(calls('SavePlace')).toHaveLength(1))
  expect(calls('SavePlace')[0][1]).toEqual({
    latitudeDegrees: 11.25,
    longitudeDegrees: 21.75,
    placeLabel: 'home',
    addressText: '12 Elm Street, Exampleton',
  })
})

it('keeps an address as typed for a place, without moving it', async () => {
  answerWith({ places: [place({ addressText: '1 Old Road, Exampleton' })] })
  const base = vi.mocked(graphql).getMockImplementation()
  vi.mocked(graphql).mockImplementation(async (document, variables) => {
    if (document.includes('SearchAddresses')) return { SearchAddresses: [] } as never
    return base!(document, variables)
  })
  renderAt('/location?tab=places')

  fireEvent.click(await screen.findByRole('button', { name: /Fixture Park/ }))
  fireEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Change address' }))
  const chooser = await screen.findByRole('alertdialog')
  expect(within(chooser).queryByRole('button', { name: 'Keep this address as typed' })).toBeNull()
  fireEvent.change(within(chooser).getByLabelText('Type an address'), { target: { value: '9 Nowhere Lane' } })
  fireEvent.click(within(chooser).getByRole('button', { name: 'Search' }))
  fireEvent.click(await within(chooser).findByRole('button', { name: 'Keep this address as typed' }))

  await waitFor(() => expect(calls('SavePlace')).toHaveLength(1))
  expect(calls('SavePlace')[0][1]).toEqual({ placeId: 'place-one', addressText: '9 Nowhere Lane' })
  expect(await screen.findByText('Kept the address 9 Nowhere Lane')).toBeTruthy()
})

it('gives a typed address back to the map, and offers that only for a typed address', async () => {
  answerWith({ places: [place({ addressText: '9 Nowhere Lane', isAddressTyped: true })] })
  renderAt('/location?tab=places')

  fireEvent.click(await screen.findByRole('button', { name: /Fixture Park/ }))
  fireEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: "Use the map's address" }))

  await waitFor(() => expect(calls('SavePlace')).toHaveLength(1))
  expect(calls('SavePlace')[0][1]).toEqual({ placeId: 'place-one', addressText: '' })
  expect(await screen.findByText('The map will name this place again.')).toBeTruthy()
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
})

it('does not offer the map address for a place the map already names', async () => {
  answerWith({ places: [place({ addressText: '1 Old Road, Exampleton' })] })
  renderAt('/location?tab=places')

  fireEvent.click(await screen.findByRole('button', { name: /Fixture Park/ }))
  const editor = await screen.findByRole('dialog')
  expect(within(editor).queryByRole('button', { name: "Use the map's address" })).toBeNull()
})

it('does not offer to keep a typed address for home, and says to use the phone or move the pin', async () => {
  answerWith({ own: { latestPoint: null } })
  const base = vi.mocked(graphql).getMockImplementation()
  vi.mocked(graphql).mockImplementation(async (document, variables) => {
    if (document.includes('SearchAddresses')) return { SearchAddresses: [] } as never
    return base!(document, variables)
  })
  renderAt('/location')

  fireEvent.click(await screen.findByRole('button', { name: 'Type my address' }))
  const chooser = await screen.findByRole('alertdialog')
  fireEvent.change(within(chooser).getByLabelText('Type an address'), { target: { value: '9 Nowhere Lane' } })
  fireEvent.click(within(chooser).getByRole('button', { name: 'Search' }))
  expect(
    await within(chooser).findByText(
      'Not found on the map. Use where I am now on your iPhone, or make home and move its pin.',
    ),
  ).toBeTruthy()
  expect(within(chooser).queryByRole('button', { name: 'Keep this address as typed' })).toBeNull()
})

it('moves a place by its pin in the editor and saves the new position with Save', async () => {
  answerWith({ places: [place()] })
  renderAt('/location?tab=places')

  fireEvent.click(await screen.findByRole('button', { name: /Fixture Park/ }))
  const editor = await screen.findByRole('dialog')
  expect((within(editor).getByRole('button', { name: 'Save' }) as HTMLButtonElement).disabled).toBe(true)
  fireEvent.click(within(editor).getByRole('button', { name: 'Drop the pin at the test position' }))
  fireEvent.click(within(editor).getByRole('button', { name: 'Save' }))

  await waitFor(() => expect(calls('SavePlace')).toHaveLength(1))
  expect(calls('SavePlace')[0][1]).toEqual({ placeId: 'place-one', latitudeDegrees: 11.5, longitudeDegrees: 21.5 })
})

it('keeps the address buttons off while the editor has unsaved changes', async () => {
  answerWith({ places: [place({ addressText: '9 Nowhere Lane', isAddressTyped: true })] })
  renderAt('/location?tab=places')

  fireEvent.click(await screen.findByRole('button', { name: /Fixture Park/ }))
  const editor = await screen.findByRole('dialog')
  const changeButton = within(editor).getByRole('button', { name: 'Change address' }) as HTMLButtonElement
  const mapButton = within(editor).getByRole('button', { name: "Use the map's address" }) as HTMLButtonElement
  expect(changeButton.disabled).toBe(false)
  expect(mapButton.disabled).toBe(false)

  fireEvent.change(within(editor).getByLabelText('Name'), { target: { value: 'Unsaved name' } })
  expect(changeButton.disabled).toBe(true)
  expect(mapButton.disabled).toBe(true)
  expect(within(editor).getByText(/Save your changes first/)).toBeTruthy()
  fireEvent.click(mapButton)
  expect(calls('SavePlace')).toHaveLength(0)
})

it('leaves Photos of a shared person to their card', async () => {
  answerWith({
    sharedViews: [PERSON_B_VIEW],
    circles: [{ members: [{ userId: 'person-b', linkedContactId: 'contact-1' }] }],
  })
  renderAt('/location?person=person-b', { canUsePhotos: true })
  await screen.findByText('Visits of Person B')
  expect(screen.queryByRole('link', { name: /^Photos of / })).toBeNull()
  expect(screen.getByRole('link', { name: 'Profile' })).toBeTruthy()
})

it("opens the card of the person whose location is shown, and none for the person's own", async () => {
  answerWith({ sharedViews: [PERSON_B_VIEW] })
  renderAt('/location?person=person-b')
  const profile = await screen.findByRole('link', { name: 'Profile' })
  expect(profile.getAttribute('href')).toBe(`/people/${encodeURIComponent(PERSON_B_VIEW.userId)}`)
  fireEvent.click(profile)
  expect(await screen.findByText('The card page')).toBeTruthy()
  cleanup()

  answerWith({})
  renderAt('/location')
  await screen.findByRole('heading', { name: 'Visits' })
  expect(screen.queryByRole('link', { name: 'Profile' })).toBeNull()
})

const photoOf = (id: string) => ({ id, smallPreviewUrl: `/photos/${id}/small` })

it('shows up to four pictures taken during a visit, and the number of the rest', async () => {
  answerWith({
    visits: [
      visit({
        photoCount: 6,
        photos: ['photo-1', 'photo-2', 'photo-3', 'photo-4'].map(photoOf),
      }),
    ],
  })
  renderAt('/location')
  const first = (await screen.findAllByRole('link', { name: /^Photo taken during the visit to Fixture Park/ }))[0]
  expect(first.getAttribute('href')).toBe(
    '/photos/photo-1?after=2030-03-05T12%3A00%3A00.000Z&before=2030-03-05T13%3A30%3A00.000Z',
  )
  expect(screen.getAllByRole('link', { name: /^Photo taken during the visit to Fixture Park/ })).toHaveLength(4)
  const more = screen.getByRole('link', { name: /^Photos taken during the visit to Fixture Park.*all 6/ })
  expect(more.textContent).toBe('+2')
  expect(more.getAttribute('href')).toBe(
    '/photos?after=2030-03-05T12%3A00%3A00.000Z&before=2030-03-05T13%3A30%3A00.000Z',
  )
  // The row is still a button of its own, with the pictures beside it.
  expect(first.closest('button')).toBeNull()
})

it('shows no count tile when every picture of the visit is shown', async () => {
  answerWith({ visits: [visit({ photoCount: 2, photos: [photoOf('photo-1'), photoOf('photo-2')] })] })
  renderAt('/location')
  await screen.findAllByRole('link', { name: /^Photo taken during the visit/ })
  expect(screen.queryByText(/^\+\d/)).toBeNull()
})

it('shows no pictures under the visits of another person', async () => {
  answerWith({
    sharedViews: [PERSON_B_VIEW],
    visits: [visit({ photoCount: null, photos: null })],
  })
  renderAt('/location?person=person-b')
  await screen.findByText('Fixture Park')
  expect(screen.queryByRole('link', { name: /taken during the visit/ })).toBeNull()
})

it('selects the place the address names, on the Places tab', async () => {
  answerWith({
    places: [
      place({ id: 'place-one', placeName: 'Fixture Park' }),
      place({ id: 'place-two', placeName: 'Fixture Cafe' }),
    ],
  })
  renderAt('/location?tab=places&place=place-two')
  const row = (await screen.findByText('Fixture Cafe')).closest('button')!
  expect(row.className).toContain('is-selected')
  expect(screen.getByText('Fixture Park').closest('button')!.className).not.toContain('is-selected')
})

it('writes the place chosen to the address by replacing the entry, so Back leaves the page', async () => {
  answerWith({
    places: [
      place({ id: 'place-one', placeName: 'Fixture Park' }),
      place({ id: 'place-two', placeName: 'Fixture Cafe' }),
    ],
  })
  renderAt('/location?tab=places', { earlierEntries: ['/before'] })
  fireEvent.click((await screen.findByText('Fixture Cafe')).closest('button')!)
  await waitFor(() => expect(screen.getByTestId('address').textContent).toBe('/location?tab=places&place=place-two'))
  fireEvent.click(screen.getByText('Fixture Park').closest('button')!)
  await waitFor(() => expect(screen.getByTestId('address').textContent).toBe('/location?tab=places&place=place-one'))
  fireEvent.click(screen.getByRole('button', { name: 'Step back' }))
  await waitFor(() => expect(screen.getByTestId('address').textContent).toBe('/before'))
})

it('drops the place from the address when the viewed person changes', async () => {
  answerWith({ sharedViews: [PERSON_B_VIEW], places: [place({ id: 'place-one' })] })
  renderAt('/location?tab=places&place=place-one')
  await screen.findAllByText('Fixture Park')
  fireEvent.click(await screen.findByRole('button', { name: /Person B/ }))
  await waitFor(() => expect(screen.getByTestId('address').textContent).toBe('/location?tab=places&person=person-b'))
})

it('reads a place named in the address that is not on the loaded page, and selects it on the map', async () => {
  answerWith({
    places: [place({ id: 'place-one', placeName: 'Fixture Park' })],
    readablePlaces: [place({ id: 'place-far', placeName: 'Fixture Far' })],
  })
  renderAt('/location?tab=places&place=place-far')
  await screen.findByText('Fixture Park')
  await waitFor(() => expect(screen.getByTestId('map').textContent).toContain('2 places'))
  expect(calls('ReadPlace(')).toHaveLength(1)
  expect(screen.getByTestId('address').textContent).toBe('/location?tab=places&place=place-far')
})

it('takes a place nobody has out of the address', async () => {
  answerWith({ places: [place({ id: 'place-one' })] })
  renderAt('/location?tab=places&place=place-gone&day=2030-03-05')
  await screen.findByText('Fixture Park')
  await waitFor(() => expect(screen.getByTestId('address').textContent).toBe('/location?tab=places&day=2030-03-05'))
})

it("ends a visit's photo window where the server counted its photos", async () => {
  answerWith({
    visits: [
      visit({
        departedAt: null,
        durationSeconds: 0,
        photoCount: 1,
        photos: [photoOf('photo-1')],
        photosTakenBefore: '2030-03-05T11:00:00Z',
      }),
    ],
  })
  renderAt('/location')
  const link = await screen.findByRole('link', { name: /^Photo taken during the visit/ })
  expect(link.getAttribute('href')).toContain('before=2030-03-05T11%3A00%3A00.000Z')
})

it('ends the photo window of an open visit at one moment per answer, not at every render', async () => {
  answerWith({
    visits: [
      visit({
        departedAt: null,
        durationSeconds: 0,
        photoCount: 1,
        photos: [photoOf('photo-1')],
      }),
    ],
  })
  renderAt('/location')
  const link = await screen.findByRole('link', { name: /^Photo taken during the visit/ })
  const href = link.getAttribute('href')
  expect(href).toMatch(/before=/)
  // Choosing the row renders the page again; the visits are the same answer.
  await new Promise((resolve) => setTimeout(resolve, 20))
  fireEvent.click(screen.getByText('Fixture Park').closest('button')!)
  await waitFor(() => expect(screen.getByTestId('address').textContent).toContain('place=place-one'))
  expect(screen.getByRole('link', { name: /^Photo taken during the visit/ }).getAttribute('href')).toBe(href)
})

it("offers neither checking in nor Sharing while somebody else's location is shown", async () => {
  answerWith({ sharedViews: [PERSON_B_VIEW] })
  renderAt('/location?person=person-b&tab=sharing')
  await screen.findByText('Visits of Person B')
  expect(screen.queryByRole('button', { name: 'Check in' })).toBeNull()
  expect(screen.queryByRole('button', { name: 'Sharing' })).toBeNull()
  expect(screen.getByRole('button', { name: 'Places' })).toBeTruthy()
})

it('opens on the visits of somebody chosen from the Sharing tab, and comes back to Visits', async () => {
  answerWith({ sharedViews: [PERSON_B_VIEW] })
  renderAt('/location?tab=sharing')
  await screen.findByRole('combobox', { name: 'Share with' })
  fireEvent.click(await screen.findByRole('button', { name: /Person B/ }))
  expect(await screen.findByText('Visits of Person B')).toBeTruthy()
  expect(screen.queryByRole('combobox', { name: 'Share with' })).toBeNull()
  expect(screen.queryByRole('button', { name: 'Sharing' })).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: /You/ }))
  expect(await screen.findByRole('heading', { name: 'Visits' })).toBeTruthy()
  expect(screen.queryByRole('combobox', { name: 'Share with' })).toBeNull()
})

it('puts somebody who shares only the place they are at on the map, at that place', async () => {
  answerWith({
    sharedViews: [
      {
        ...PERSON_B_VIEW,
        latestPoint: null,
        openVisit: {
          id: 'visit-now',
          arrivedAt: new Date().toISOString(),
          place: place({ placeName: 'Fixture Park' }),
        },
      },
    ] as never,
  })
  renderAt('/location')
  await waitFor(() => expect(screen.getByTestId('map').getAttribute('data-people')).toContain('Person B'))
})

const OPEN_PAUSE = { id: 'pause-open', startedAt: '2030-03-05T11:00:00Z', endedAt: null }

it('pauses recording for an hour, with the end an hour from now', async () => {
  answerWith({})
  renderAt('/location')
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(new Date('2030-03-05T12:00:00Z'))
  try {
    fireEvent.click(await screen.findByRole('button', { name: 'Pause recording' }))
    fireEvent.click(screen.getByRole('menuitem', { name: 'For an hour' }))
    await waitFor(() => expect(calls('PauseLocation(')).toHaveLength(1))
  } finally {
    vi.useRealTimers()
  }
  expect(calls('PauseLocation(')[0][1]).toEqual({ endedAt: '2030-03-05T13:00:00.000Z' })
  expect(await screen.findByText(/^Recording paused until/)).toBeTruthy()
})

it('pauses until 08:00 the next morning on the browser clock, or until resumed', async () => {
  answerWith({})
  renderAt('/location')
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(new Date(2030, 2, 5, 22, 15))
  try {
    fireEvent.click(await screen.findByRole('button', { name: 'Pause recording' }))
    fireEvent.click(screen.getByRole('menuitem', { name: 'Until tomorrow morning' }))
    await waitFor(() => expect(calls('PauseLocation(')).toHaveLength(1))
    fireEvent.click(await screen.findByRole('button', { name: 'Pause recording' }))
    fireEvent.click(screen.getByRole('menuitem', { name: 'Until I resume' }))
    await waitFor(() => expect(calls('PauseLocation(')).toHaveLength(2))
  } finally {
    vi.useRealTimers()
  }
  expect(calls('PauseLocation(')[0][1]).toEqual({ endedAt: new Date(2030, 2, 6, 8, 0).toISOString() })
  expect(calls('PauseLocation(')[1][1]).toEqual({ endedAt: null })
})

it('ends the pause this morning when it is asked for before 05:00, and tomorrow from 05:00', async () => {
  answerWith({})
  renderAt('/location')
  vi.useFakeTimers({ toFake: ['Date'] })
  try {
    for (const [index, asked] of [new Date(2030, 2, 6, 0, 30), new Date(2030, 2, 6, 5, 0)].entries()) {
      vi.setSystemTime(asked)
      fireEvent.click(await screen.findByRole('button', { name: 'Pause recording' }))
      fireEvent.click(screen.getByRole('menuitem', { name: 'Until tomorrow morning' }))
      await waitFor(() => expect(calls('PauseLocation(')).toHaveLength(index + 1))
    }
  } finally {
    vi.useRealTimers()
  }
  expect(calls('PauseLocation(')[0][1]).toEqual({ endedAt: new Date(2030, 2, 6, 8, 0).toISOString() })
  expect(calls('PauseLocation(')[1][1]).toEqual({ endedAt: new Date(2030, 2, 7, 8, 0).toISOString() })
})

it('says in a toast when pausing failed, and offers the pause again', async () => {
  answerWith({})
  const answered = vi.mocked(graphql).getMockImplementation()!
  vi.mocked(graphql).mockImplementation(async (document, variables) => {
    if (document.includes('PauseLocation(')) throw new Error('Pausing is refused')
    return answered(document, variables)
  })
  renderAt('/location')

  fireEvent.click(await screen.findByRole('button', { name: 'Pause recording' }))
  fireEvent.click(screen.getByRole('menuitem', { name: 'Until I resume' }))

  expect(await screen.findByText('Pausing is refused')).toBeTruthy()
  expect(screen.getByRole('button', { name: 'Pause recording' })).toBeTruthy()
})

it('shows the pause in effect with Resume, which ends it with a toast', async () => {
  answerWith({ own: { pause: OPEN_PAUSE } })
  renderAt('/location')

  expect(await screen.findByText('Recording paused until you resume')).toBeTruthy()
  // Only one thing at a time: the pause is not offered again while it is on.
  expect(screen.queryByRole('button', { name: 'Pause recording' })).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: 'Resume' }))

  await waitFor(() => expect(calls('ResumeLocation')).toHaveLength(1))
  expect(await screen.findByText('Recording resumed.')).toBeTruthy()
})

it('reads the person again when a timed pause ends while the page is open', async () => {
  const endsAt = Date.now() + 200
  answerWith({
    own: { pause: { id: 'pause-timed', startedAt: '2030-03-05T11:00:00Z', endedAt: new Date(endsAt).toISOString() } },
  })
  const answered = vi.mocked(graphql).getMockImplementation()!
  vi.mocked(graphql).mockImplementation(async (document, variables) => {
    const isOwnLocation = document.includes('GetLocation') && !(variables as { userId?: string } | undefined)?.userId
    if (isOwnLocation && Date.now() >= endsAt) return { GetLocation: OWN_VIEW } as never
    return answered(document, variables)
  })
  renderAt('/location')

  expect(await screen.findByText(/^Recording paused until/)).toBeTruthy()
  await waitFor(() => expect(screen.queryByText(/^Recording paused until/)).toBeNull(), { timeout: 4000 })
  expect(screen.getByRole('button', { name: 'Pause recording' })).toBeTruthy()
})

it('reads the person again at the end of a pause further ahead than a timer can wait', async () => {
  // The minute refresh stays on the real clock, so only the end of the pause
  // reads the people again here, and the month is not stepped through a
  // minute at a time.
  vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout', 'Date'], shouldAdvanceTime: true })
  try {
    vi.setSystemTime(new Date('2030-03-05T12:00:00Z'))
    const endsAt = Date.now() + 30 * 24 * 60 * 60 * 1000
    answerWith({
      own: { pause: { id: 'pause-far', startedAt: '2030-03-05T11:00:00Z', endedAt: new Date(endsAt).toISOString() } },
    })
    const answered = vi.mocked(graphql).getMockImplementation()!
    vi.mocked(graphql).mockImplementation(async (document, variables) => {
      const isOwnLocation = document.includes('GetLocation') && !(variables as { userId?: string } | undefined)?.userId
      if (isOwnLocation && Date.now() >= endsAt) return { GetLocation: OWN_VIEW } as never
      return answered(document, variables)
    })
    renderAt('/location')
    expect(await screen.findByText(/^Recording paused until/)).toBeTruthy()

    // The longest a timer waits (about 24.8 days) is over, and the end is not.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(2 ** 31)
    })
    expect(screen.queryByText(/^Recording paused until/)).toBeTruthy()
    // The timer was set again for what is left.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(6 * 24 * 60 * 60 * 1000)
    })
    await waitFor(() => expect(screen.queryByText(/^Recording paused until/)).toBeNull())
    expect(screen.getByRole('button', { name: 'Pause recording' })).toBeTruthy()
  } finally {
    vi.useRealTimers()
  }
})

it('reads the day of the end of a pause that is not today, and the date alone from six days ahead', async () => {
  vi.useFakeTimers({ toFake: ['Date'] })
  try {
    vi.setSystemTime(new Date(2030, 2, 5, 12, 0))
    answerWith({
      sharedViews: [
        {
          ...PERSON_B_VIEW,
          latestPoint: null,
          openVisit: null,
          pause: {
            id: 'pause-soon',
            startedAt: new Date(2030, 2, 5, 9, 0).toISOString(),
            endedAt: new Date(2030, 2, 7, 15, 0).toISOString(),
          },
        },
        {
          ...PERSON_B_VIEW,
          userId: 'person-c',
          name: 'Person C',
          latestPoint: null,
          openVisit: null,
          pause: {
            id: 'pause-far',
            startedAt: new Date(2030, 2, 5, 9, 0).toISOString(),
            endedAt: new Date(2030, 2, 15, 15, 0).toISOString(),
          },
        },
      ],
    })
    renderAt('/location')
    const weekdayAndTime = new Intl.DateTimeFormat('en', { weekday: 'short', hour: 'numeric', minute: '2-digit' })
    const dateOnly = new Intl.DateTimeFormat('en', { month: 'short', day: 'numeric' })
    expect(
      await screen.findByText(`Location paused until ${weekdayAndTime.format(new Date(2030, 2, 7, 15, 0))}`),
    ).toBeTruthy()
    expect(screen.getByText(`Location paused until ${dateOnly.format(new Date(2030, 2, 15, 15, 0))}`)).toBeTruthy()
  } finally {
    vi.useRealTimers()
  }
})

it('gives the days of a pause that began on one day and ended on another', async () => {
  vi.useFakeTimers({ toFake: ['Date'] })
  try {
    vi.setSystemTime(new Date(2030, 2, 5, 12, 0))
    answerWith({
      visits: [visit()],
      pauses: [
        {
          id: 'pause-night',
          startedAt: new Date(2030, 2, 3, 17, 0).toISOString(),
          endedAt: new Date(2030, 2, 4, 8, 0).toISOString(),
        },
        {
          id: 'pause-day',
          startedAt: new Date(2030, 2, 3, 10, 0).toISOString(),
          endedAt: new Date(2030, 2, 3, 11, 0).toISOString(),
        },
      ],
    })
    const { container } = renderAt('/location')
    const weekdayAndTime = new Intl.DateTimeFormat('en', { weekday: 'short', hour: 'numeric', minute: '2-digit' })
    const timeOnly = new Intl.DateTimeFormat('en', { hour: 'numeric', minute: '2-digit' })
    await waitFor(() => expect(screen.getAllByText('Paused')).toHaveLength(2))
    const pauseTexts = [...container.querySelectorAll('.location-row')]
      .filter((row) => row.textContent?.startsWith('Paused'))
      .map((row) => row.querySelector('.muted')?.textContent)
    expect(pauseTexts).toContain(
      `${weekdayAndTime.format(new Date(2030, 2, 3, 17, 0))} to ${weekdayAndTime.format(new Date(2030, 2, 4, 8, 0))}`,
    )
    expect(pauseTexts).toContain(
      `${timeOnly.format(new Date(2030, 2, 3, 10, 0))} to ${timeOnly.format(new Date(2030, 2, 3, 11, 0))}`,
    )
  } finally {
    vi.useRealTimers()
  }
})

it('reads Location paused for a person who paused, and draws no pin for them', async () => {
  answerWith({
    sharedViews: [
      { ...PERSON_B_VIEW, latestPoint: null, openVisit: null, pause: OPEN_PAUSE },
      {
        ...PERSON_B_VIEW,
        userId: 'person-c',
        name: 'Person C',
        pause: {
          id: 'pause-timed',
          startedAt: '2030-03-05T11:00:00Z',
          endedAt: new Date(Date.now() + 3600_000).toISOString(),
        },
      },
    ],
  })
  renderAt('/location')

  expect(await screen.findByText('Location paused')).toBeTruthy()
  expect(screen.getByText(/^Location paused until/)).toBeTruthy()
  // Person C still carries a position (a stale read), and still has no pin.
  expect(screen.getByTestId('map').getAttribute('data-people')).toBe('Person A')
})

it('keeps the pin of the person themselves while they are paused', async () => {
  answerWith({ own: { pause: OPEN_PAUSE } })
  renderAt('/location')

  await screen.findByText('Recording paused until you resume')
  expect(screen.getByTestId('map').getAttribute('data-people')).toBe('Person A')
})

it('lists a pause among the visits, between the visits around it, with how long it lasted', async () => {
  answerWith({
    visits: [
      visit(),
      visit({
        id: 'visit-two',
        place: place({ id: 'place-two', placeName: 'Fixture Library', isNamedByPerson: true }),
        arrivedAt: '2030-03-02T12:00:00Z',
        departedAt: '2030-03-02T12:25:00Z',
        durationSeconds: 1500,
      }),
    ],
    pauses: [{ id: 'pause-past', startedAt: '2030-03-04T14:00:00Z', endedAt: '2030-03-04T15:00:00Z' }],
  })
  const { container } = renderAt('/location')

  expect(await screen.findByText('Paused')).toBeTruthy()
  const rows = [...container.querySelectorAll('.location-row')].map((row) => row.textContent ?? '')
  const indexOf = (words: string) => rows.findIndex((row) => row.includes(words))
  expect(indexOf('Fixture Park')).toBeLessThan(indexOf('Paused'))
  expect(indexOf('Paused')).toBeLessThan(indexOf('Fixture Library'))
  expect(rows[indexOf('Paused')]).toContain('1 h 0 min')
  // Three days, each with its heading.
  expect(screen.getAllByRole('heading', { level: 4 })).toHaveLength(3)
  expect(calls('ListLocationPauses')[0][1]).toMatchObject({ userId: null, from: '2000-01-01T00:00:00Z' })
  expect(calls('ListLocationPauses')[0][1]).toHaveProperty('until')
  expect(calls('ListLocationPauses')[0][1]).not.toHaveProperty('to')
  expect(calls('ListLocationPauses')[0][0]).toContain('$until: DateTime!')
})

it('shows the pause in effect as the newest row of the timeline, and reads the pauses again for its person', async () => {
  answerWith({ visits: [], pauses: [OPEN_PAUSE], sharedViews: [PERSON_B_VIEW] })
  const { container } = renderAt('/location?person=person-b')

  expect(await screen.findByText('Paused now')).toBeTruthy()
  expect(container.querySelectorAll('.location-row').length).toBeGreaterThan(0)
  expect(screen.queryByText(/No visits yet/)).toBeNull()
  expect(calls('ListLocationPauses')[0][1]).toMatchObject({ userId: 'person-b' })
})

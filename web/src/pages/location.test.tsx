import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { act } from 'react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { graphql, subscribe } from '../api'
import { Place, PlaceVisit } from '../components/locationData'
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
  LocationMap: ({ people, places }: { people: { name: string }[]; places: { placeName: string }[] }) => (
    <div data-testid="map" data-people={people.map((person) => person.name).join(',')}>
      {people.length} people, {places.length} places
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
  own?: typeof OWN_VIEW | { latestPoint: null } | null
  sharedViews?: (typeof PERSON_B_VIEW)[]
  movedView?: typeof PERSON_B_VIEW
  visits?: PlaceVisit[]
  places?: Place[]
  failSharedLocations?: boolean
  // Answers the visits asked for, when what is asked changes the answer.
  visitsFor?: (variables: Record<string, unknown>) => PlaceVisit[] | Promise<PlaceVisit[]>
  people?: { userId: string; username: string; name: string }[]
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
    if (document.includes('ListLocationPoints')) return { ListLocationPoints: [] } as never
    if (document.includes('ListPlaces'))
      return { ListPlaces: { places: shown.places ?? [], nextOffset: null } } as never
    if (document.includes('ListLocationShares')) return { ListLocationShares: [] } as never
    if (document.includes('ListCirclePeople')) return { ListCirclePeople: shown.people ?? [] } as never
    if (document.includes('ListCircles')) return { ListCircles: [] } as never
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

function renderAt(entry: string) {
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
            permissions: { everywhere: ['location:use', 'circle:use'], byDomain: [] },
          }}
        >
          <MemoryRouter initialEntries={[entry]}>
            <Routes>
              <Route path="/location" element={<LocationPage />} />
            </Routes>
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
  fireEvent.change(within(dialog).getByLabelText('Label'), { target: { value: 'work' } })
  fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }))

  await waitFor(() => expect(calls('SavePlace')).toHaveLength(1))
  expect(calls('SavePlace')[0][1]).toEqual({ placeId: 'place-one', placeLabel: 'work' })
})

it('shares where the person has been with the circle member chosen', async () => {
  answerWith({ people: [{ userId: 'person-b', username: 'person-b-login', name: 'Person B' }] })
  renderAt('/location?tab=sharing')

  const choice = await screen.findByLabelText('Share with')
  await screen.findByRole('option', { name: /Person B/ })
  fireEvent.change(choice, { target: { value: 'user:person-b' } })
  fireEvent.change(screen.getByLabelText(/What they see/), { target: { value: 'history' } })
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

it('narrows the visits by label and the places by label and by the words typed', async () => {
  answerWith({ visits: [visit()], places: [place()] })
  renderAt('/location')
  await screen.findByText('Fixture Park')

  fireEvent.change(screen.getByLabelText('Label'), { target: { value: 'home' } })
  await waitFor(() =>
    expect(
      calls('ListLocationVisits').some(([, variables]) => (variables as { placeLabel?: string }).placeLabel === 'home'),
    ).toBe(true),
  )

  cleanup()
  vi.mocked(graphql).mockClear()
  answerWith({ places: [place()] })
  renderAt('/location?tab=places')
  await screen.findByRole('button', { name: /Fixture Park/ })

  fireEvent.change(screen.getByLabelText('Label'), { target: { value: 'work' } })
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
  expect(calls('CheckIn(')[0][1]).toEqual({ latitudeDegrees: 10.5, longitudeDegrees: 20.5 })
})

it('says plainly when the browser could only find the person roughly, and still lets them check in', async () => {
  answerWith({})
  const base = vi.mocked(graphql).getMockImplementation()
  vi.mocked(graphql).mockImplementation(async (document, variables) => {
    if (document.includes('ListNearbyPlaces')) return { ListNearbyPlaces: [] } as never
    return base!(document, variables)
  })
  Object.defineProperty(window, 'isSecureContext', { value: true, configurable: true })
  Object.defineProperty(navigator, 'geolocation', {
    configurable: true,
    value: {
      getCurrentPosition: (success: (position: unknown) => void) =>
        success({ coords: { latitude: 10.5, longitude: 20.5, accuracy: 1500 }, timestamp: 1_900_000_000_000 }),
    },
  })
  renderAt('/location')

  fireEvent.click(await screen.findByRole('button', { name: 'Check in' }))
  const dialog = await screen.findByRole('alertdialog')
  expect(await within(dialog).findByText(/could only find you roughly, within 1500 m/)).toBeTruthy()
  expect((within(dialog).getByRole('button', { name: /Check in here/ }) as HTMLButtonElement).disabled).toBe(false)
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

it('keeps the map and the people when nobody has a position of their own but others share', async () => {
  answerWith({ own: { latestPoint: null }, sharedViews: [PERSON_B_VIEW] })
  renderAt('/location')

  expect(await screen.findByTestId('map')).toBeTruthy()
  expect(screen.queryByText('Install the TeaNode app on your iPhone')).toBeNull()
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

it('does not label the browser position as home when the browser is only sure to a kilometer', async () => {
  answerWith({ own: { latestPoint: null } })
  Object.defineProperty(window, 'isSecureContext', { value: true, configurable: true })
  Object.defineProperty(navigator, 'geolocation', {
    configurable: true,
    value: {
      getCurrentPosition: (success: (position: unknown) => void) =>
        success({ coords: { latitude: 10.5, longitude: 20.5, accuracy: 1500 }, timestamp: 1_900_000_000_000 }),
    },
  })
  renderAt('/location')

  fireEvent.click(await screen.findByRole('button', { name: 'Use where I am now' }))
  expect(await screen.findByText(/only knows where you are to within 1500 m/)).toBeTruthy()
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
  expect(calls('SearchAddresses')[0][1]).toEqual({ query: '12 Elm' })
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

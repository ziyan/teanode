import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, expect, it, vi } from 'vitest'

import { graphql } from '../api'
import { Place, PlaceVisit } from '../components/locationData'
import { ToastProvider } from '../components/toast'
import { TranslationProvider } from '../i18n/i18n'
import { SessionProvider } from '../session'
import { LocationPage } from './location'

vi.mock('../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../api')>()),
  graphql: vi.fn(),
}))
// WebGL is not there to draw a map in; the page's part is what it hands the map.
vi.mock('../components/locationMap', () => ({
  LocationMap: ({ people, places }: { people: { name: string }[]; places: { placeName: string }[] }) => (
    <div data-testid="map">
      {people.length} people, {places.length} places
    </div>
  ),
}))
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
  locationShareLevel: 'owner',
  latestPoint: {
    latitudeDegrees: 10.5,
    longitudeDegrees: 20.5,
    accuracyMeters: 20,
    measuredAt: '2030-03-05T12:00:00Z',
  },
  openVisit: null,
}

function answerWith(shown: {
  visits?: PlaceVisit[]
  places?: Place[]
  failSharedLocations?: boolean
  people?: { userId: string; username: string; name: string }[]
}) {
  vi.mocked(graphql).mockImplementation(async (document) => {
    if (document.includes('GetLocation')) return { GetLocation: OWN_VIEW } as never
    if (document.includes('ListSharedLocations')) {
      if (shown.failSharedLocations) throw new Error('Cannot query field "ListSharedLocations"')
      return { ListSharedLocations: [] } as never
    }
    if (document.includes('ListLocationVisits')) {
      return { ListLocationVisits: { placeVisits: shown.visits ?? [], nextOffset: null } } as never
    }
    if (document.includes('ListLocationPoints')) return { ListLocationPoints: [] } as never
    if (document.includes('ListPlaces'))
      return { ListPlaces: { places: shown.places ?? [], nextOffset: null } } as never
    if (document.includes('ListLocationShares')) return { ListLocationShares: [] } as never
    if (document.includes('ListCirclePeople')) return { ListCirclePeople: shown.people ?? [] } as never
    if (document.includes('ListCircles')) return { ListCircles: [] } as never
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
        place: place({ id: 'place-two', placeName: 'Fixture Library', placeLabel: 'school' }),
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
  fireEvent.change(screen.getByLabelText(/How much/), { target: { value: 'history' } })
  fireEvent.click(screen.getByRole('button', { name: 'Share' }))

  await waitFor(() => expect(calls('ShareLocation(')).toHaveLength(1))
  expect(calls('ShareLocation(')[0][1]).toEqual({
    granteeUserId: 'person-b',
    granteeCircleId: null,
    shareLevel: 'history',
  })
})

it('still shows the person and their visits when the people sharing with them cannot be asked', async () => {
  answerWith({ visits: [visit()], failSharedLocations: true })
  renderAt('/location')

  expect(await screen.findByText('Fixture Park')).toBeTruthy()
  expect(screen.getByText('Person A')).toBeTruthy()
  expect(screen.getByTestId('map').textContent).toBe('1 people, 1 places')
})

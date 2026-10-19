import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { graphql } from '../../api'
import { ToastProvider } from '../../components/toast'
import { TranslationProvider } from '../../i18n/i18n'
import { SessionProvider } from '../../session'
import { addDays, dayText } from './healthApi'
import { HealthPage } from './healthPage'

vi.mock('../../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../api')>()),
  graphql: vi.fn(),
}))
// WebGL is not there to draw a map in; the page's part is the path it hands the map.
vi.mock('../../components/locationMap', () => ({
  LocationMap: ({ path }: { path?: { latitudeDegrees: number; longitudeDegrees: number }[] }) => (
    <div data-testid="map">{(path ?? []).length} points</div>
  ),
}))

// A chart draws nothing at a width of zero, which is what a page without a
// layout has.
beforeEach(() => {
  window.localStorage.clear()
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  )
  vi.spyOn(Element.prototype, 'getBoundingClientRect').mockReturnValue({
    width: 640,
    height: 220,
    top: 0,
    left: 0,
    right: 640,
    bottom: 220,
    x: 0,
    y: 0,
    toJSON: () => ({}),
  })
})
afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  vi.resetAllMocks()
})

const TODAY = dayText(new Date())
const DAYS_AGO = (count: number) => addDays(TODAY, -count)

function healthType(overrides: Record<string, unknown> = {}) {
  return {
    healthType: 'stepCount',
    healthCategory: 'activity',
    typeName: 'Step count',
    valueKind: 'quantity',
    quantityUnit: 'count',
    summaryKind: 'sum',
    firstDay: DAYS_AGO(40),
    lastDay: TODAY,
    sampleCount: 500,
    latestDaySummary: {
      summaryDay: TODAY,
      sumValue: 8402,
      averageValue: null,
      minimumValue: null,
      maximumValue: null,
      sampleCount: 20,
      durationSeconds: null,
    },
    ...overrides,
  }
}

const HEART_RATE = healthType({
  healthType: 'heartRate',
  healthCategory: 'heart',
  typeName: 'Heart rate',
  quantityUnit: 'count/min',
  summaryKind: 'average',
  latestDaySummary: {
    summaryDay: TODAY,
    sumValue: null,
    averageValue: 71,
    minimumValue: 52,
    maximumValue: 118,
    sampleCount: 300,
    durationSeconds: null,
  },
})

const SLEEP = healthType({
  healthType: 'sleepAnalysis',
  healthCategory: 'sleep',
  typeName: 'Sleep analysis',
  valueKind: 'category',
  quantityUnit: '',
  summaryKind: 'duration',
  latestDaySummary: {
    summaryDay: TODAY,
    sumValue: null,
    averageValue: null,
    minimumValue: null,
    maximumValue: null,
    sampleCount: 6,
    durationSeconds: 27000,
  },
})

const UNKNOWN_TYPE = healthType({
  healthType: 'madeUpMeasure',
  healthCategory: 'other',
  typeName: 'Made up measure',
  quantityUnit: 'kg',
  summaryKind: 'average',
  latestDaySummary: {
    summaryDay: TODAY,
    sumValue: null,
    averageValue: 3.5,
    minimumValue: 3,
    maximumValue: 4,
    sampleCount: 2,
    durationSeconds: null,
  },
})

const WORKOUT = healthType({
  healthType: 'workout',
  healthCategory: 'activity',
  typeName: 'Workout',
  valueKind: 'workout',
  quantityUnit: '',
  summaryKind: 'count',
  latestDaySummary: {
    summaryDay: TODAY,
    sumValue: null,
    averageValue: null,
    minimumValue: null,
    maximumValue: null,
    sampleCount: 1,
    durationSeconds: 1800,
  },
})

function period(day: string, overrides: Record<string, unknown> = {}) {
  return {
    startDay: day,
    endDay: day,
    sumValue: null,
    averageValue: null,
    minimumValue: null,
    maximumValue: null,
    sampleCount: 1,
    dayCount: 1,
    durationSeconds: null,
    categorySeconds: {},
    ...overrides,
  }
}

type Shown = {
  types?: unknown[]
  sharedTypes?: unknown[]
  shares?: unknown[]
  samples?: unknown[]
  totalSamples?: number
  periods?: (healthType: string) => unknown[]
  canAgentReadHealth?: boolean
  modelProviderName?: string
  sampleCountKept?: () => number
  characteristics?: { healthType: string; characteristicValue: string }[]
}

function answerWith(shown: Shown = {}) {
  vi.mocked(graphql).mockImplementation(async (document, variables) => {
    const asked = (variables ?? {}) as Record<string, unknown>
    if (document.includes('ListHealthTypes')) {
      return { ListHealthTypes: asked.ownerUserId ? (shown.sharedTypes ?? []) : (shown.types ?? []) } as never
    }
    if (document.includes('ReadHealthSummary')) {
      const healthType = asked.healthType as string
      return {
        ReadHealthSummary: {
          healthType,
          quantityUnit: '',
          summaryKind: 'sum',
          periods: shown.periods
            ? shown.periods(healthType)
            : [period(DAYS_AGO(2), { sumValue: 5000 }), period(DAYS_AGO(1), { sumValue: 7000 })],
        },
      } as never
    }
    if (document.includes('ListHealthSamples')) {
      return {
        ListHealthSamples: {
          samples: shown.samples ?? [],
          totalCount: shown.totalSamples ?? (shown.samples ?? []).length,
          nextOffset: null,
        },
      } as never
    }
    if (document.includes('ReadHealthRoute')) {
      return {
        ReadHealthRoute: {
          healthSampleId: asked.healthSampleId,
          routePoints: [
            [0, 10.5, 20.5, 5],
            [60, 10.6, 20.6, 6],
            [120, 10.7, 20.7, 7],
          ],
        },
      } as never
    }
    if (document.includes('ReadHealthSettings') || document.includes('UpdateHealthSettings')) {
      return {
        ReadHealthSettings: {
          canAgentReadHealth: shown.canAgentReadHealth ?? false,
          healthErasedAt: null,
          sampleCount: shown.sampleCountKept ? shown.sampleCountKept() : 500,
          lastReceivedAt: new Date().toISOString(),
          modelProviderName: shown.modelProviderName ?? '',
          characteristics: shown.characteristics ?? [],
        },
        UpdateHealthSettings: { canAgentReadHealth: true },
      } as never
    }
    if (document.includes('ListHealthShares')) return { ListHealthShares: shown.shares ?? [] } as never
    if (document.includes('ListCirclePeople')) {
      return { ListCirclePeople: [{ userId: 'person-c', username: 'person-c-login', name: 'Person C' }] } as never
    }
    if (document.includes('ListCircles')) {
      return { ListCircles: [{ id: 'circle-one', name: 'Fixture Family', members: [{ userId: 'person-c' }] }] } as never
    }
    if (document.includes('EraseHealthData')) return { EraseHealthData: { erasedSampleCount: 500 } } as never
    if (document.includes('SetHealthShare')) return { SetHealthShare: { id: 'share-one' } } as never
    return {} as never
  })
}

function Address() {
  const location = useLocation()
  return <output data-testid="address">{location.search}</output>
}

function renderAt(entry: string, permissions = ['health:use', 'circle:use']) {
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
            permissions: { everywhere: permissions, byDomain: [] },
          }}
        >
          <MemoryRouter initialEntries={[entry]}>
            <Routes>
              <Route
                path="/health"
                element={
                  <>
                    <HealthPage />
                    <Address />
                  </>
                }
              />
              <Route path="/settings/devices" element={<p>Devices page</p>} />
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

it('lists the types by Health category with their latest day, translated, and a server name for unknown ones', async () => {
  answerWith({ types: [UNKNOWN_TYPE, HEART_RATE, SLEEP, healthType()] })
  renderAt('/health')

  const list = await screen.findByRole('group', { name: 'Kinds of Health data' })
  const headings = within(list)
    .getAllByRole('heading', { level: 4 })
    .map((heading) => heading.textContent)
  // The Health app's order, with the types it has no group for last.
  expect(headings).toEqual(['Activity', 'Heart', 'Sleep', 'Other'])
  const steps = within(list).getByRole('button', { name: /Steps/ })
  expect(steps.textContent).toContain('8,402')
  expect(within(list).getByRole('button', { name: /Heart rate/ }).textContent).toContain('71 bpm')
  expect(within(list).getByRole('button', { name: /Sleep/ }).textContent).toContain('7 h 30 min')
  expect(within(list).getByRole('button', { name: /Made up measure/ }).textContent).toContain('3.5 kg')
})

it('draws the first type with its numbers, and reads the chosen one by the range and period in the address', async () => {
  answerWith({ types: [healthType(), HEART_RATE] })
  renderAt('/health')

  // Steps is first; the default is 30 days by day.
  await screen.findByRole('img', { name: /Steps by day/ })
  const first = calls('ReadHealthSummary')[0][1] as Record<string, unknown>
  expect(first).toMatchObject({ healthType: 'stepCount', period: 'day', toDay: TODAY, fromDay: DAYS_AGO(29) })
  // The average per day is over the days that have steps.
  expect(screen.getByText('6,000 steps')).toBeTruthy()
  expect(screen.getByText('12,000 steps')).toBeTruthy()

  fireEvent.click(screen.getByRole('button', { name: '90 days' }))
  await waitFor(() => expect(screen.getByTestId('address').textContent).toBe('?range=90'))
  await waitFor(() =>
    expect(
      calls('ReadHealthSummary').some(([, variables]) => (variables as { fromDay: string }).fromDay === DAYS_AGO(89)),
    ).toBe(true),
  )

  fireEvent.click(screen.getByRole('button', { name: 'Week' }))
  await waitFor(() => expect(screen.getByTestId('address').textContent).toBe('?range=90&period=week'))
  await waitFor(() =>
    expect(
      calls('ReadHealthSummary').some(([, variables]) => (variables as { period: string }).period === 'week'),
    ).toBe(true),
  )

  fireEvent.click(screen.getByRole('button', { name: /Heart rate/ }))
  await waitFor(() => expect(screen.getByTestId('address').textContent).toContain('type=heartRate'))
  expect(screen.getByTestId('address').textContent).toContain('range=90')
})

it('reads the type, range and period from the address', async () => {
  answerWith({ types: [healthType(), HEART_RATE] })
  renderAt('/health?type=heartRate&range=365&period=month')

  await waitFor(() => expect(calls('ReadHealthSummary')).toHaveLength(1))
  expect(calls('ReadHealthSummary')[0][1]).toMatchObject({
    healthType: 'heartRate',
    period: 'month',
    fromDay: DAYS_AGO(364),
  })
  expect(screen.getByRole('button', { name: '30 days' }).getAttribute('aria-pressed')).toBe('false')
  expect(screen.getByRole('button', { name: 'Year' }).getAttribute('aria-pressed')).toBe('true')
  expect(screen.getByRole('button', { name: 'Month' }).getAttribute('aria-pressed')).toBe('true')
})

it('draws sleep as stacked stages', async () => {
  answerWith({
    types: [SLEEP],
    periods: () => [
      period(DAYS_AGO(1), {
        durationSeconds: 27000,
        sampleCount: 6,
        categorySeconds: { asleepCore: 12600, asleepDeep: 3600, asleepREM: 5400, awake: 600 },
      }),
    ],
  })
  renderAt('/health')

  const chart = await screen.findByRole('img', { name: /Sleep by day/ })
  expect(chart.getAttribute('aria-label')).toContain('Deep 1 h')
  expect(chart.getAttribute('aria-label')).toContain('Core 3 h 30 min')
  expect(chart.getAttribute('aria-label')).toContain('REM 1 h 30 min')
  expect(screen.getByText('Awake')).toBeTruthy()
})

it('draws a line with the lowest and highest reading of each day', async () => {
  answerWith({
    types: [HEART_RATE],
    periods: () => [
      period(DAYS_AGO(2), { averageValue: 70, minimumValue: 50, maximumValue: 120, sampleCount: 100 }),
      period(DAYS_AGO(1), { averageValue: 74, minimumValue: 55, maximumValue: 110, sampleCount: 300 }),
    ],
  })
  const { container } = renderAt('/health')

  await screen.findByRole('img', { name: /Heart rate by day/ })
  expect(container.querySelector('.series-chart-band')).not.toBeNull()
  // Weighted by how many readings each day holds: (70*100 + 74*300) / 400.
  expect(screen.getByText('73 bpm')).toBeTruthy()
  expect(screen.getByText('50 bpm')).toBeTruthy()
  expect(screen.getByText('120 bpm')).toBeTruthy()
})

it('pages the readings fifty at a time, with the page in the address', async () => {
  const samples = [
    {
      healthSampleId: 'sample-one',
      healthType: 'stepCount',
      startedAt: new Date().toISOString(),
      endedAt: new Date().toISOString(),
      quantityValue: 120,
      quantityUnit: 'count',
      categoryValue: null,
      workoutActivityType: '',
      sourceName: 'Fixture phone',
      hasRoute: false,
    },
  ]
  answerWith({ types: [healthType()], samples, totalSamples: 120 })
  renderAt('/health')

  await screen.findByText('Fixture phone')
  expect(calls('ListHealthSamples')[0][1]).toMatchObject({ healthType: 'stepCount', limitCount: 50, offset: 0 })
  fireEvent.click(screen.getByRole('button', { name: 'Next page' }))
  await waitFor(() => expect(screen.getByTestId('address').textContent).toContain('page=2'))
  await waitFor(() =>
    expect(calls('ListHealthSamples').some(([, variables]) => (variables as { offset: number }).offset === 50)).toBe(
      true,
    ),
  )
})

it('opens a workout route on the map', async () => {
  const samples = [
    {
      healthSampleId: 'workout-one',
      healthType: 'workout',
      startedAt: new Date(Date.now() - 1_800_000).toISOString(),
      endedAt: new Date().toISOString(),
      quantityValue: null,
      quantityUnit: '',
      categoryValue: null,
      workoutActivityType: 'running',
      sourceName: 'Fixture watch',
      hasRoute: true,
    },
  ]
  answerWith({ types: [WORKOUT], samples })
  renderAt('/health')

  fireEvent.click(await screen.findByRole('button', { name: /Running, 30 min: Show the route/ }))

  expect(await screen.findByTestId('map')).toBeTruthy()
  expect(screen.getByTestId('map').textContent).toBe('3 points')
  expect(calls('ReadHealthRoute')[0][1]).toEqual({ healthSampleId: 'workout-one' })
  expect(screen.getByTestId('address').textContent).toContain('route=workout-one')
  // The card is titled with the workout's own line, and how far it went.
  expect(await screen.findByRole('heading', { name: /^Running, .+, 30 min, \d+\.\d km$/ })).toBeTruthy()
})

it('chooses somebody who shares Health and reads only their types and charts', async () => {
  answerWith({
    types: [healthType()],
    sharedTypes: [healthType({ healthType: 'sleepAnalysis', healthCategory: 'sleep', typeName: 'Sleep analysis' })],
    shares: [
      {
        id: 'share-in',
        ownerUserId: 'person-b',
        ownerUsername: 'person-b-login',
        ownerName: 'Person B',
        granteeUserId: 'person-a',
        granteeUsername: 'person-a-login',
        granteeCircleId: null,
        granteeCircleName: '',
        healthCategories: ['sleep'],
      },
    ],
  })
  renderAt('/health')

  await screen.findByRole('button', { name: /^Steps/ })
  fireEvent.click(await screen.findByRole('combobox', { name: 'Showing' }))
  fireEvent.click(screen.getByRole('option', { name: 'Person B' }))

  await waitFor(() => expect(screen.getByTestId('address').textContent).toBe('?person=person-b'))
  await waitFor(() =>
    expect(
      calls('ListHealthTypes').some(
        ([, variables]) => (variables as { ownerUserId?: string }).ownerUserId === 'person-b',
      ),
    ).toBe(true),
  )
  await waitFor(() =>
    expect(
      calls('ReadHealthSummary').some(
        ([, variables]) => (variables as { ownerUserId?: string }).ownerUserId === 'person-b',
      ),
    ).toBe(true),
  )
  // Sleep is theirs; nothing of the person's own is listed, and there are no
  // readings and no settings.
  expect(screen.queryByRole('button', { name: /^Steps/ })).toBeNull()
  expect(
    calls('ListHealthSamples').some(
      ([, variables]) => (variables as { healthType: string }).healthType === 'sleepAnalysis',
    ),
  ).toBe(false)
  expect(screen.queryByRole('button', { name: 'Settings' })).toBeNull()
  expect(screen.queryByText('Recent readings')).toBeNull()
})

it('drops a person who does not share from the address', async () => {
  answerWith({ types: [healthType()] })
  renderAt('/health?person=person-z')

  await waitFor(() => expect(screen.getByTestId('address').textContent).toBe(''))
  expect(await screen.findByText('That person does not share Health data with you.')).toBeTruthy()
})

it('turns on the agent reading Health data, naming the provider', async () => {
  answerWith({ types: [healthType()], modelProviderName: 'Fixture Models' })
  renderAt('/health?tab=settings')

  expect(await screen.findByText(/What the agent reads goes to Fixture Models\./)).toBeTruthy()
  // Only the person's own Health, never what others share.
  expect(screen.getByText(/reads only your own Health data, never what other people share with you/)).toBeTruthy()
  const box = screen.getByRole('checkbox', { name: 'Let the agent read Health data' })
  await waitFor(() => expect((box as HTMLInputElement).disabled).toBe(false))
  fireEvent.click(box)

  await waitFor(() => expect(calls('UpdateHealthSettings')).toHaveLength(1))
  expect(calls('UpdateHealthSettings')[0][1]).toEqual({ canAgentReadHealth: true })
  expect(await screen.findByText('The agent can read your Health data now.')).toBeTruthy()
})

it('shares the Health categories ticked with a circle', async () => {
  answerWith({ types: [healthType()] })
  renderAt('/health?tab=settings')

  fireEvent.click(await screen.findByRole('combobox', { name: 'Share with' }))
  fireEvent.click(await screen.findByRole('option', { name: 'Fixture Family' }))
  fireEvent.click(screen.getByRole('button', { name: 'Add' }))

  const group = await screen.findByRole('group', { name: 'Categories shared with Everyone in Fixture Family' })
  fireEvent.click(within(group).getByRole('checkbox', { name: 'Sleep' }))

  await waitFor(() => expect(calls('SetHealthShare')).toHaveLength(1))
  expect(calls('SetHealthShare')[0][1]).toEqual({
    granteeUserId: null,
    granteeCircleId: 'circle-one',
    healthCategories: ['sleep'],
  })
})

it('asks before erasing everything, and says how much went', async () => {
  answerWith({ types: [healthType()] })
  renderAt('/health?tab=settings')

  fireEvent.click(await screen.findByRole('button', { name: 'Erase all Health data' }))
  const dialog = await screen.findByRole('alertdialog')
  expect(within(dialog).getByText(/This cannot be undone/)).toBeTruthy()
  // Nothing is erased until it is confirmed.
  expect(calls('EraseHealthData')).toHaveLength(0)
  fireEvent.click(within(dialog).getByRole('button', { name: 'Erase' }))

  await waitFor(() => expect(calls('EraseHealthData')).toHaveLength(1))
  expect(calls('EraseHealthData')[0][1]).toEqual({})
  expect(await screen.findByText('500 readings erased.')).toBeTruthy()
  await waitFor(() => expect(screen.queryByRole('alertdialog')).toBeNull())
})

it('reads the numbers on the Settings tab again after everything is erased', async () => {
  let kept = 2578
  answerWith({ types: [healthType()], sampleCountKept: () => kept })
  vi.mocked(graphql).mockImplementation(
    ((implementation) => async (document: string, variables?: unknown) => {
      if (document.includes('EraseHealthData')) {
        kept = 0
        return { EraseHealthData: { erasedSampleCount: 2578 } } as never
      }
      return implementation(document, variables as never)
    })(vi.mocked(graphql).getMockImplementation() as (document: string, variables?: never) => Promise<never>),
  )
  renderAt('/health?tab=settings')

  // Written for the language, with a thousands separator.
  expect(await screen.findByText(/2,578 readings kept/)).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: 'Erase all Health data' }))
  const dialog = await screen.findByRole('alertdialog')
  fireEvent.click(within(dialog).getByRole('button', { name: 'Erase' }))

  expect(await screen.findByText('2,578 readings erased.')).toBeTruthy()
  expect(await screen.findByText(/No readings kept/)).toBeTruthy()
  expect(screen.queryByText(/2,578 readings kept/)).toBeNull()
})

it('hides the person picker on the Settings tab, where it means nothing', async () => {
  const shares = [
    {
      id: 'share-in',
      ownerUserId: 'person-b',
      ownerUsername: 'person-b-login',
      ownerName: 'Person B',
      granteeUserId: 'person-a',
      granteeUsername: 'person-a-login',
      granteeCircleId: null,
      granteeCircleName: '',
      healthCategories: ['sleep'],
    },
  ]
  answerWith({ types: [healthType()], shares })
  renderAt('/health?tab=settings')

  await screen.findByRole('checkbox', { name: 'Let the agent read Health data' })
  expect(screen.queryByRole('combobox', { name: 'Showing' })).toBeNull()

  fireEvent.click(screen.getByRole('button', { name: 'Data' }))
  expect(await screen.findByRole('combobox', { name: 'Showing' })).toBeTruthy()
})

it('shows the facts Health holds about the person, read only', async () => {
  answerWith({
    types: [healthType()],
    characteristics: [
      { healthType: 'dateOfBirth', characteristicValue: '1990-05-12' },
      { healthType: 'biologicalSex', characteristicValue: 'female' },
      { healthType: 'bloodType', characteristicValue: 'abNegative' },
    ],
  })
  renderAt('/health?tab=settings')

  const about = (await screen.findByRole('heading', { name: 'About you' })).closest('section') as HTMLElement
  expect(within(about).getByText('Date of birth')).toBeTruthy()
  expect(within(about).getByText(/May 12, 1990/)).toBeTruthy()
  expect(within(about).getByText('Female')).toBeTruthy()
  expect(within(about).getByText('AB-')).toBeTruthy()
  // Read only: nothing in it can be edited.
  expect(within(about).queryAllByRole('textbox')).toHaveLength(0)
  expect(within(about).queryAllByRole('button')).toHaveLength(0)
})

it('leaves the facts out when Health holds none', async () => {
  answerWith({ types: [healthType()] })
  renderAt('/health?tab=settings')

  await screen.findByRole('checkbox', { name: 'Let the agent read Health data' })
  expect(screen.queryByRole('heading', { name: 'About you' })).toBeNull()
})

it('erases one type from its own view', async () => {
  answerWith({ types: [healthType(), HEART_RATE] })
  renderAt('/health?type=heartRate')

  fireEvent.click(await screen.findByRole('button', { name: 'Actions for Heart rate' }))
  fireEvent.click(await screen.findByRole('menuitem', { name: 'Erase Heart rate' }))
  const dialog = await screen.findByRole('alertdialog')
  // It says what goes, that the iPhone keeps sending, and what the agent keeps.
  expect(within(dialog).getByText(/that this server keeps/)).toBeTruthy()
  expect(within(dialog).getByText(/keeps sending new readings of it while Health is on/)).toBeTruthy()
  fireEvent.click(within(dialog).getByRole('button', { name: 'Erase' }))

  await waitFor(() => expect(calls('EraseHealthData')).toHaveLength(1))
  expect(calls('EraseHealthData')[0][1]).toEqual({ healthType: 'heartRate' })
  await waitFor(() => expect(screen.getByTestId('address').textContent).toBe(''))
})

it('says what Health is when there is nothing yet, and points to the iPhone app', async () => {
  answerWith({ types: [] })
  renderAt('/health')

  expect(await screen.findByText('No Health data yet')).toBeTruthy()
  expect(screen.getByText(/copies your Apple Health data to your own server/)).toBeTruthy()
  fireEvent.click(screen.getByRole('link', { name: 'Set up the iPhone app' }))
  expect(await screen.findByText('Devices page')).toBeTruthy()
  expect(calls('ListHealthSamples')).toHaveLength(0)
})

it('is not offered without the permission', async () => {
  answerWith({})
  renderAt('/health', ['circle:use'])

  expect(await screen.findByText(/Your account is not allowed to copy or read Apple Health data/)).toBeTruthy()
  expect(calls('ListHealthTypes')).toHaveLength(0)
})

const OXYGEN = healthType({
  healthType: 'oxygenSaturation',
  healthCategory: 'respiratory',
  typeName: 'Blood oxygen',
  quantityUnit: '%',
  summaryKind: 'average',
  latestDaySummary: {
    summaryDay: TODAY,
    sumValue: null,
    averageValue: 0.97,
    minimumValue: 0.94,
    maximumValue: 0.99,
    sampleCount: 40,
    durationSeconds: null,
  },
})

function axisLabels(container: HTMLElement): string[] {
  return [...container.querySelectorAll('.usage-chart-grid + text')].map((each) => each.textContent ?? '')
}

it('writes the axis of a percentage as the figures are, not as the fractions HealthKit keeps', async () => {
  answerWith({
    types: [OXYGEN],
    periods: () => [
      period(DAYS_AGO(2), { averageValue: 0.96, minimumValue: 0.93, maximumValue: 0.99, sampleCount: 20 }),
      period(DAYS_AGO(1), { averageValue: 0.98, minimumValue: 0.95, maximumValue: 1, sampleCount: 20 }),
    ],
  })
  const { container } = renderAt('/health')

  await screen.findByRole('img', { name: /Blood oxygen by day/ })
  // The headline is 97%, so the axis reads 93, 94 ... 100 and never 0.93.
  expect(screen.getAllByText('97%').length).toBeGreaterThan(0)
  const labels = axisLabels(container)
  expect(labels.length).toBeGreaterThan(1)
  for (const label of labels) {
    expect(Number(label)).toBeGreaterThanOrEqual(90)
    expect(Number(label)).toBeLessThanOrEqual(100)
  }
})

it('reads a week of steps per day over the days that have a summary', async () => {
  answerWith({
    types: [healthType()],
    periods: () => [
      period(DAYS_AGO(13), {
        endDay: DAYS_AGO(7),
        sumValue: 21000,
        sampleCount: 30,
        dayCount: 3,
      }),
    ],
  })
  renderAt('/health?period=week')

  await screen.findByRole('img', { name: /Steps by week/ })
  // Three days with steps: 7,000 a day, not 3,000 over the seven calendar days.
  expect(screen.getByText('7,000 steps')).toBeTruthy()
  expect(screen.getByText('21,000 steps')).toBeTruthy()
  expect(calls('ReadHealthSummary')[0][0]).toContain('dayCount')
})

it('reads a month of sleep per night over the nights that have a summary', async () => {
  answerWith({
    types: [SLEEP],
    periods: () => [
      period(DAYS_AGO(30), {
        endDay: DAYS_AGO(1),
        durationSeconds: 3 * 7 * 3600,
        sampleCount: 18,
        dayCount: 3,
        categorySeconds: { asleepCore: 3 * 4 * 3600, asleepDeep: 3 * 1 * 3600, asleepREM: 3 * 2 * 3600, awake: 3600 },
      }),
    ],
  })
  renderAt('/health?period=month')

  await screen.findByRole('img', { name: /Sleep by month/ })
  // Seven hours a night, from three nights of seven hours asleep.
  expect(screen.getAllByText('7 h').length).toBeGreaterThan(0)
})

it('keeps the buttons and the chart while another range is read, and keeps the focus', async () => {
  answerWith({ types: [healthType()] })
  renderAt('/health')
  await screen.findByRole('img', { name: /Steps by day/ })

  // The next read does not come until it is told to.
  let answer: (value: unknown) => void = () => {}
  const waiting = new Promise((resolve) => {
    answer = resolve
  })
  const previous = vi.mocked(graphql).getMockImplementation() as (document: string, variables?: never) => Promise<never>
  vi.mocked(graphql).mockImplementation(async (document, variables) =>
    document.includes('ReadHealthSummary') ? ((await waiting) as never) : previous(document, variables as never),
  )

  const button = screen.getByRole('button', { name: '90 days' })
  button.focus()
  fireEvent.click(button)
  await waitFor(() => expect(screen.getByTestId('address').textContent).toBe('?range=90'))

  expect(screen.getByRole('img', { name: /Steps by day/ })).toBeTruthy()
  expect(screen.getByRole('button', { name: '90 days' })).toBe(button)
  expect(document.activeElement).toBe(button)
  expect(button.getAttribute('aria-pressed')).toBe('true')

  answer({
    ReadHealthSummary: {
      healthType: 'stepCount',
      quantityUnit: '',
      summaryKind: 'sum',
      periods: [period(DAYS_AGO(3), { sumValue: 1000 })],
    },
  })
  await waitFor(() => expect(screen.getAllByText('1,000 steps').length).toBeGreaterThan(0))
  expect(screen.getByRole('button', { name: '90 days' })).toBe(button)
})

it('keeps the buttons and offers another try when the chart cannot be read', async () => {
  answerWith({ types: [healthType()] })
  const answerBefore = vi.mocked(graphql).getMockImplementation() as (
    document: string,
    variables?: never,
  ) => Promise<never>
  let isBroken = true
  vi.mocked(graphql).mockImplementation(async (document, variables) => {
    if (document.includes('ReadHealthSummary') && isBroken) throw new Error('the server is down')
    return answerBefore(document, variables as never)
  })
  renderAt('/health')

  expect(await screen.findByText(/The chart could not be read/)).toBeTruthy()
  expect(screen.getByRole('button', { name: '90 days' })).toBeTruthy()
  expect(screen.getByRole('button', { name: 'Month' })).toBeTruthy()

  isBroken = false
  fireEvent.click(screen.getByRole('button', { name: 'Try again' }))
  await screen.findByRole('img', { name: /Steps by day/ })
  expect(screen.queryByText(/The chart could not be read/)).toBeNull()
})

it('draws a workout chart in minutes', async () => {
  const workout = healthType({
    healthType: 'workout',
    healthCategory: 'activity',
    typeName: 'Workouts',
    valueKind: 'workout',
    quantityUnit: '',
    summaryKind: 'duration',
  })
  answerWith({
    types: [workout],
    periods: () => [
      period(DAYS_AGO(2), { durationSeconds: 36 * 60 }),
      period(DAYS_AGO(1), { durationSeconds: 60 * 60 }),
    ],
  })
  const { container } = renderAt('/health')

  await screen.findByRole('img', { name: /Workouts by day/ })
  const labels = axisLabels(container)
  expect(labels.length).toBeGreaterThan(1)
  expect(labels.every((label) => /^\d+ min$/.test(label))).toBe(true)
})

it('writes a long distance in kilometers on the axis and in the figures', async () => {
  const distance = healthType({
    healthType: 'distanceWalkingRunning',
    typeName: 'Walking and running distance',
    quantityUnit: 'm',
    summaryKind: 'sum',
  })
  answerWith({
    types: [distance],
    periods: () => [period(DAYS_AGO(2), { sumValue: 8000 }), period(DAYS_AGO(1), { sumValue: 6000 })],
  })
  const { container } = renderAt('/health')

  await screen.findByRole('img', { name: /Walking and running distance by day/ })
  expect(screen.getByText('7 km')).toBeTruthy()
  expect(axisLabels(container).every((label) => Number(label) <= 8)).toBe(true)
})

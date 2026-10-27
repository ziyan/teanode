import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import { beforeEach, expect, it, vi } from 'vitest'

import { graphql } from '../../api'
import { type Language, loadCatalog } from '../../i18n/i18n'
import type { JournalLink, JournalNarrative } from './journalApi'
import {
  NO_NARRATIVE,
  SHOPPING_TRIP,
  SHOPPING_TRIP_MOMENT,
  answerWith,
  calls,
  journalDay,
  journalPeriod,
  renderAt,
  setUpJournalTestWorld,
} from './journalFixtures'

// The Shopping section of a day, pinning a moment to the day, and removing a
// story. The data is all invented.

// The pages ask matchMedia for the width; jsdom has none, and a test that does
// not care about the width is a desktop.
beforeEach(() => {
  if (typeof window.matchMedia !== 'function') {
    vi.stubGlobal('matchMedia', () => ({ matches: true, addEventListener() {}, removeEventListener() {} }))
  }
})

vi.mock('../../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../api')>()),
  graphql: vi.fn(),
}))
vi.mock('../../components/locationMap', () => ({
  LocationMap: ({ places }: { places: unknown[] }) => <div data-testid="map">{places.length} places</div>,
}))

setUpJournalTestWorld()

const THURSDAY = '2026-10-22'

const STORY: JournalNarrative = {
  ...NO_NARRATIVE,
  headline: 'A quiet errand day',
  narrative: 'You picked up groceries.',
  writtenAt: '2026-10-23T03:12:00Z',
  sourceCount: 3,
}

// The day's shopping

it('lists the shopping trips of the day with the store and the lists, each linking to its page', async () => {
  answerWith({
    day: (day) =>
      journalDay(day, {
        shoppingTrips: [
          SHOPPING_TRIP,
          {
            ...SHOPPING_TRIP,
            tripId: 'trip-2',
            storeName: '',
            listNames: [],
            startedAt: '2026-10-22T19:10:00Z',
            dashboardPath: '/shopping/stores/store-2',
          },
        ],
        moments: [SHOPPING_TRIP_MOMENT],
      }),
  })
  renderAt(`/journal/${THURSDAY}`)
  const section = (await screen.findByRole('heading', { name: 'Shopping' })).closest('section') as HTMLElement
  const store = within(section).getByRole('link', { name: 'Shopping at Corner Market' })
  expect(store.getAttribute('href')).toBe('/shopping/stores/store-1')
  expect(within(section).getByText('for Groceries, Hardware')).toBeTruthy()
  // A trip with no store is only "Shopping", and says no lists.
  const unnamed = within(section).getByRole('link', { name: 'Shopping' })
  expect(unnamed.getAttribute('href')).toBe('/shopping/stores/store-2')
  expect(within(section).getAllByText(/^for /)).toHaveLength(1)
})

it('has no Shopping section on a day without trips', async () => {
  answerWith({ day: (day) => journalDay(day, { moments: [] }) })
  renderAt(`/journal/${THURSDAY}`)
  await screen.findByText('Nothing is linked to this day yet', { exact: false })
  expect(screen.queryByRole('heading', { name: 'Shopping' })).toBeNull()
})

// Pin to this day

const CHOSEN: JournalLink = {
  linkId: 'link-1',
  record: {
    recordKind: 'agent_document',
    recordId: 'doc-2',
    reference: 'agent_document:doc-2',
    title: 'Freeze date for the platform team',
    happenedAt: '2026-10-22T15:52:00Z',
    dashboardPath: '/knowledge/work',
    isUnavailable: false,
  },
  linkSource: 'agent',
  linkReason: 'you were asked to decide',
  isHidden: false,
}

it('pins a moment to the day, says so, and reads the day again', async () => {
  answerWith({
    day: (day) =>
      journalDay(day, {
        moments: [
          SHOPPING_TRIP_MOMENT,
          {
            ...SHOPPING_TRIP_MOMENT,
            recordKind: '',
            recordId: '',
            title: 'A moment with no record',
            dashboardPath: '',
          },
          {
            ...SHOPPING_TRIP_MOMENT,
            recordKind: 'agent_document',
            recordId: 'doc-2',
            title: 'Freeze date for the platform team',
          },
        ],
        choices: [CHOSEN],
      }),
  })
  renderAt(`/journal/${THURSDAY}`)
  const pin = await screen.findByRole('button', { name: 'Pin to this day: Shopping at Corner Market' })
  // Only the moment with a record that is not already on the day has the button.
  expect(screen.getAllByRole('button', { name: /^Pin to this day/ })).toHaveLength(1)
  const readsBefore = calls('ReadJournalDay').length
  fireEvent.click(pin)
  await waitFor(() => expect(calls('PinJournalRecord')).toHaveLength(1))
  expect(calls('PinJournalRecord')[0][1]).toEqual({
    day: THURSDAY,
    recordKind: 'shopping_trip',
    recordId: 'trip-1',
  })
  expect(await screen.findByText('Pinned to this day')).toBeTruthy()
  await waitFor(() => expect(calls('ReadJournalDay').length).toBeGreaterThan(readsBefore))
})

it('has no pin button on a moment already pinned, and the pinned moment shows with its remove button', async () => {
  answerWith({
    day: (day) =>
      journalDay(day, {
        moments: [SHOPPING_TRIP_MOMENT],
        pins: [
          {
            linkId: 'pin-1',
            record: {
              recordKind: 'shopping_trip',
              recordId: 'trip-1',
              reference: 'shopping_trip:trip-1',
              title: 'Shopping at Corner Market',
              happenedAt: '2026-10-22T17:30:00Z',
              dashboardPath: '/shopping/stores/store-1',
              isUnavailable: false,
            },
            linkSource: 'person',
            linkReason: '',
            isHidden: false,
          },
        ],
      }),
  })
  renderAt(`/journal/${THURSDAY}`)
  expect(await screen.findByText('Pinned')).toBeTruthy()
  expect(screen.getByRole('button', { name: 'Remove: Shopping at Corner Market' })).toBeTruthy()
  expect(screen.queryByRole('button', { name: /^Pin to this day/ })).toBeNull()
})

it('tells the person when the pin could not be made', async () => {
  answerWith({
    day: (day) => journalDay(day, { moments: [SHOPPING_TRIP_MOMENT] }),
    failures: { PinJournalRecord: 'a day keeps at most 50 pins' },
  })
  renderAt(`/journal/${THURSDAY}`)
  fireEvent.click(await screen.findByRole('button', { name: 'Pin to this day: Shopping at Corner Market' }))
  expect(await screen.findByText(/a day keeps at most 50 pins/)).toBeTruthy()
})

// Remove this story

it('asks before removing a day story, then removes it and shows the empty card', async () => {
  let isRemoved = false
  answerWith({
    day: (day) => journalDay(day, { narrative: isRemoved ? NO_NARRATIVE : STORY }),
  })
  // The day is read without a story from the moment the removal is asked for.
  const answer = vi.mocked(graphql).getMockImplementation()!
  vi.mocked(graphql).mockImplementation(async (document, variables) => {
    if (document.includes('DeleteJournalNarrative')) isRemoved = true
    return answer(document, variables)
  })
  renderAt(`/journal/${THURSDAY}`)
  expect(await screen.findByText('You picked up groceries.')).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: 'Remove this story' }))
  const dialog = await screen.findByRole('alertdialog', { name: 'Remove this story?' })
  expect(within(dialog).getByText(/what TeaNode chose for it, the records marked Chosen/)).toBeTruthy()
  // Nothing is sent until the person confirms.
  expect(calls('DeleteJournalNarrative')).toHaveLength(0)
  fireEvent.click(within(dialog).getByRole('button', { name: 'Remove this story' }))
  await waitFor(() => expect(calls('DeleteJournalNarrative')).toHaveLength(1))
  expect(calls('DeleteJournalNarrative')[0][1]).toEqual({ day: THURSDAY, periodKind: null })
  expect(await screen.findByText('Story removed')).toBeTruthy()
  expect(await screen.findByRole('button', { name: "Write this day's story" })).toBeTruthy()
  expect(screen.queryByText('You picked up groceries.')).toBeNull()
  expect(screen.queryByRole('alertdialog')).toBeNull()
})

it('keeps the story when the person cancels the removal', async () => {
  answerWith({ day: (day) => journalDay(day, { narrative: STORY }) })
  renderAt(`/journal/${THURSDAY}`)
  fireEvent.click(await screen.findByRole('button', { name: 'Remove this story' }))
  const dialog = await screen.findByRole('alertdialog', { name: 'Remove this story?' })
  fireEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }))
  await waitFor(() => expect(screen.queryByRole('alertdialog')).toBeNull())
  expect(calls('DeleteJournalNarrative')).toHaveLength(0)
  expect(screen.getByText('You picked up groceries.')).toBeTruthy()
})

it("removes a week's story with the week's start and kind", async () => {
  answerWith({ period: () => journalPeriod('week', '2026-10-19', '2026-10-25', { narrative: STORY }) })
  renderAt('/journal/week/2026-10-21')
  fireEvent.click(await screen.findByRole('button', { name: 'Remove this story' }))
  const dialog = await screen.findByRole('alertdialog', { name: 'Remove this story?' })
  fireEvent.click(within(dialog).getByRole('button', { name: 'Remove this story' }))
  await waitFor(() => expect(calls('DeleteJournalNarrative')).toHaveLength(1))
  expect(calls('DeleteJournalNarrative')[0][1]).toEqual({ day: '2026-10-19', periodKind: 'week' })
  expect(await screen.findByText('Story removed')).toBeTruthy()
})

it('says when the story could not be removed, and keeps the dialog', async () => {
  answerWith({
    day: (day) => journalDay(day, { narrative: STORY }),
    failures: { DeleteJournalNarrative: 'the server is busy' },
  })
  renderAt(`/journal/${THURSDAY}`)
  fireEvent.click(await screen.findByRole('button', { name: 'Remove this story' }))
  const dialog = await screen.findByRole('alertdialog', { name: 'Remove this story?' })
  fireEvent.click(within(dialog).getByRole('button', { name: 'Remove this story' }))
  expect(await screen.findByText(/the server is busy/)).toBeTruthy()
  expect(screen.getByText('You picked up groceries.')).toBeTruthy()
})

// What the story's correction asks and answers, in each language.
const SOMETHING_WRONG: Record<string, string> = { en: 'Something wrong?', zh: '有不对的地方？', ja: '間違いがありますか？' }
const WRITE_IT_AGAIN: Record<string, string> = { en: 'Write it again', zh: '重新撰写', ja: '書き直す' }

function dialogSubmitName(language: string) {
  return WRITE_IT_AGAIN[language]
}

async function openTheCorrectionDialog(language = 'en') {
  fireEvent.click(await screen.findByRole('button', { name: SOMETHING_WRONG[language] }))
  return screen.findByRole('dialog')
}

// A correction the story keeps already adds nothing, and the toast says so in
// the reader's language rather than that the story is being written again.
it.each([
  ['en', 'That correction is already kept, so nothing was added.'],
  ['zh', '这条更正已经保存过，没有重复添加。'],
  ['ja', 'この訂正はすでに保存されているため、追加していません。'],
])('says in %s when a correction is already kept', async (language, toastText) => {
  // The app loads the reader's catalog before it draws; so does the test.
  window.localStorage.setItem('teanode.language', language)
  await loadCatalog(language as Language)
  try {
    answerWith({ day: (day) => journalDay(day, { narrative: STORY }), isCorrectionAlreadyKept: true })
    renderAt(`/journal/${THURSDAY}`)
    const dialog = await openTheCorrectionDialog(language)
    fireEvent.change(within(dialog).getByRole('textbox'), { target: { value: 'The walk was 25 minutes.' } })
    fireEvent.click(within(dialog).getByRole('button', { name: dialogSubmitName(language) }))
    await waitFor(() => expect(calls('CorrectJournalNarrative')).toHaveLength(1))
    expect(await screen.findByText(toastText)).toBeTruthy()
  } finally {
    window.localStorage.removeItem('teanode.language')
  }
})

it('says a new correction is saved and the story written again', async () => {
  answerWith({ day: (day) => journalDay(day, { narrative: STORY }) })
  renderAt(`/journal/${THURSDAY}`)
  const dialog = await openTheCorrectionDialog()
  fireEvent.change(within(dialog).getByRole('textbox'), { target: { value: 'The walk was 25 minutes.' } })
  fireEvent.click(within(dialog).getByRole('button', { name: 'Write it again' }))
  expect(await screen.findByText('Saved. Writing the story again.')).toBeTruthy()
  expect(screen.queryByText(/already kept/)).toBeNull()
})

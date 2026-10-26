import { cleanup, render } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { afterEach, beforeEach, vi } from 'vitest'

import { graphql } from '../../api'
import { ToastProvider } from '../../components/toast'
import { TranslationProvider } from '../../i18n/i18n'
import { SessionProvider } from '../../session'
import type {
  JournalDay,
  JournalDaySummary,
  JournalFacts,
  JournalNarrative,
  JournalPeriod,
  JournalSettings,
  JournalWords,
} from './journalApi'
import { JournalDayPage } from './journalDay'
import { JournalMonthPage, JournalWeekPage, JournalYearPage } from './journalPeriods'
import { JournalTimelinePage } from './journalTimeline'

// What the Journal's tests share: a server answered with invented data, and
// the pages rendered at an address inside the providers they need. The data is
// all made up; nothing in it is a real person, place or day of anyone's. The
// test files mock the API and the map themselves, before they import this.

// Monday, October 26, 2026, at noon: the "today" of every test.
export const TODAY = '2026-10-26'
const NOW = new Date(2026, 9, 26, 12, 0, 0)

export function setUpJournalTestWorld() {
  beforeEach(() => {
    window.localStorage.clear()
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(NOW)
  })
  afterEach(() => {
    cleanup()
    vi.useRealTimers()
    vi.resetAllMocks()
  })
}

export const NO_FACTS: JournalFacts = {
  photoCount: 0,
  placeCount: 0,
  walkedMeters: 0,
  stepCount: 0,
  sleepMinutes: 0,
  workoutMinutes: 0,
  codingSessionCount: 0,
  codingMinutes: 0,
  meetingCount: 0,
  meetingMinutes: 0,
  recordingCount: 0,
  conversationCount: 0,
  chatMessageCount: 0,
  mailReceivedCount: 0,
  mailSentCount: 0,
  documentCount: 0,
  transactionCount: 0,
  spentMinorUnits: 0,
  spentCurrency: 'USD',
  shoppingTripCount: 0,
  memoryPageCount: 0,
  mainPlaceName: '',
}

export const NO_WORDS: JournalWords = {
  original: '',
  modifiedAt: null,
  isTouchUpOn: false,
  touchedUp: '',
  touchUpHow: '',
  isTouchedUpStale: false,
  touchUpPending: false,
  journalNotes: [],
}

export const NO_NARRATIVE: JournalNarrative = {
  headline: '',
  narrative: '',
  references: [],
  writtenAt: null,
  isStale: false,
  narrativePending: false,
  isBeyondDepth: false,
  failure: '',
  corrections: [],
  sourceCount: 0,
}

export function summary(day: string, overrides: Partial<JournalDaySummary> = {}): JournalDaySummary {
  return {
    day,
    isToday: day === TODAY,
    mood: null,
    weather: null,
    headline: '',
    wordsExcerpt: '',
    hasWords: false,
    facts: NO_FACTS,
    coverPhotoId: '',
    photoIds: [],
    hasNarrative: false,
    ...overrides,
  }
}

export function journalDay(day: string, overrides: Partial<JournalDay> = {}): JournalDay {
  return {
    day,
    timeZone: 'UTC',
    isToday: day === TODAY,
    weekday: 'thursday',
    weekStart: '2026-10-19',
    words: NO_WORDS,
    mood: null,
    weather: null,
    facts: NO_FACTS,
    narrative: NO_NARRATIVE,
    choices: [],
    pins: [],
    coverPhotoId: '',
    codingSessions: [],
    documents: [],
    documentTotal: 0,
    conversations: [],
    chats: [],
    mail: [],
    mailTotal: 0,
    recordings: [],
    photos: [],
    photoTotal: 0,
    visits: [],
    events: [],
    reminders: [],
    transactions: [],
    health: null,
    shoppingTrips: [],
    notes: [],
    memoryPages: [],
    unavailableSections: [],
    moments: [],
    ...overrides,
  }
}

export function journalPeriod(
  periodKind: 'week' | 'month' | 'year',
  periodStart: string,
  periodEnd: string,
  overrides: Partial<JournalPeriod> = {},
): JournalPeriod {
  return {
    periodKind,
    periodStart,
    periodEnd,
    isCurrent: false,
    lookingBack: NO_WORDS,
    narrative: NO_NARRATIVE,
    choices: [],
    days: [],
    months: [],
    totals: NO_FACTS,
    moodCounts: [0, 0, 0, 0, 0],
    dayCount: 0,
    chapters: [],
    ...overrides,
  }
}

export const SETTINGS: JournalSettings = {
  isWeatherOn: true,
  isEveningQuestionOn: false,
  eveningQuestionTime: '21:00',
  weekStartsOn: 'monday',
  narrativeDepthDays: 90,
  modelProviderName: 'Example Models',
}

export type Server = {
  days?: JournalDaySummary[]
  totalCount?: number
  day?: (day: string) => JournalDay
  period?: (periodKind: string, day: string) => JournalPeriod
  settings?: JournalSettings
  // An operation that fails, by its name, with the message it fails with.
  failures?: Record<string, string>
}

// answerWith answers every Journal operation from the data given and the
// plain defaults for the rest. Mutations answer with the little they return.
export function answerWith(server: Server = {}) {
  vi.mocked(graphql).mockImplementation(async (document, variables) => {
    const asked = (variables ?? {}) as Record<string, unknown>
    for (const [name, message] of Object.entries(server.failures ?? {})) {
      if (document.includes(name)) throw new Error(message)
    }
    if (document.includes('ListJournalDays')) {
      const days = (server.days ?? []).filter(
        (each) =>
          (typeof asked.fromDay !== 'string' || each.day >= asked.fromDay) &&
          (typeof asked.untilDay !== 'string' || each.day <= asked.untilDay),
      )
      return {
        ListJournalDays: { days, totalCount: server.totalCount ?? days.length, today: TODAY, firstDay: '2026-10-01' },
      } as never
    }
    if (document.includes('ReadJournalDay')) {
      const day = asked.day as string
      return { ReadJournalDay: (server.day ?? ((each) => journalDay(each)))(day) } as never
    }
    if (document.includes('ReadJournalPeriod')) {
      const kind = asked.periodKind as string
      const day = asked.day as string
      return {
        ReadJournalPeriod: (server.period ?? ((periodKind) => journalPeriod(periodKind as 'week', day, day)))(
          kind,
          day,
        ),
      } as never
    }
    if (document.includes('ReadJournalSettings')) return { ReadJournalSettings: server.settings ?? SETTINGS } as never
    if (document.includes('UpdateJournalSettings'))
      return { UpdateJournalSettings: server.settings ?? SETTINGS } as never
    if (document.includes('SearchJournal')) {
      return { SearchJournal: { hits: [], totalCount: 0 } } as never
    }
    if (document.includes('EraseJournal')) {
      return { EraseJournal: { dayCount: 3, periodCount: 1, linkCount: 2 } } as never
    }
    if (document.includes('RemoveJournalLink')) return { RemoveJournalLink: true } as never
    return { Done: { day: asked.day } } as never
  })
}

function Address() {
  const location = useLocation()
  return <output data-testid="address">{location.pathname + location.search}</output>
}

export function renderAt(entry: string, permissions = ['journal:use']) {
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
              <Route path="/journal" element={<JournalTimelinePage />} />
              <Route path="/journal/week/:day" element={<JournalWeekPage />} />
              <Route path="/journal/month/:month" element={<JournalMonthPage />} />
              <Route path="/journal/year/:year" element={<JournalYearPage />} />
              <Route path="/journal/:day" element={<JournalDayPage />} />
            </Routes>
            <Address />
          </MemoryRouter>
        </SessionProvider>
      </ToastProvider>
    </TranslationProvider>,
  )
}

// The calls made to one operation, by a word of its document.
export function calls(operation: string) {
  return vi.mocked(graphql).mock.calls.filter(([document]) => document.includes(operation))
}

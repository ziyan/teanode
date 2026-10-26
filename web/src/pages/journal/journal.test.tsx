import { act, cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { expect, it, vi } from 'vitest'

import { graphql, openAgentConversation } from '../../api'
import type { JournalDay, JournalLink, JournalNarrative, JournalPeriod } from './journalApi'
import {
  NO_FACTS,
  NO_NARRATIVE,
  NO_WORDS,
  SETTINGS,
  TODAY,
  answerWith,
  calls,
  journalDay,
  journalPeriod,
  renderAt,
  summary,
  setUpJournalTestWorld,
} from './journalFixtures'

vi.mock('../../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../api')>()),
  graphql: vi.fn(),
  openAgentConversation: vi.fn(),
}))
// WebGL is not there to draw a map in; the page's part is the places it hands the map.
vi.mock('../../components/locationMap', () => ({
  LocationMap: ({ places }: { places: unknown[] }) => <div data-testid="map">{places.length} places</div>,
}))

setUpJournalTestWorld()

const THURSDAY = '2026-10-22'

const FACTS = {
  ...NO_FACTS,
  stepCount: 9412,
  sleepMinutes: 370,
  codingSessionCount: 2,
  workProjectCount: 1,
  codingMinutes: 340,
  placeCount: 2,
  photoCount: 3,
  spentMinorUnits: 8640,
  transactionCount: 2,
  mainPlaceName: 'Office',
}

const NARRATIVE: JournalNarrative = {
  ...NO_NARRATIVE,
  headline: 'Release day',
  narrative:
    'A release day. You worked on the [[agent_document:doc-1]] and kept a [[photo:gone-1]] from the walk.\n\nYou slept 6 h 10 min.',
  references: [
    {
      recordKind: 'agent_document',
      recordId: 'doc-1',
      reference: 'agent_document:doc-1',
      title: 'Release branch for the sample app',
      happenedAt: '2026-10-22T09:20:00Z',
      dashboardPath: '/coding',
      isUnavailable: false,
    },
    {
      recordKind: 'photo',
      recordId: 'gone-1',
      reference: 'photo:gone-1',
      title: '',
      happenedAt: null,
      dashboardPath: '',
      isUnavailable: true,
    },
  ],
  writtenAt: '2026-10-23T03:12:00Z',
  sourceCount: 12,
}

const CHOICE: JournalLink = {
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

function fullDay(overrides: Partial<JournalDay> = {}): JournalDay {
  return journalDay(THURSDAY, {
    facts: FACTS,
    mood: { moodLevel: 4, moodWords: 'Tired, but it is out', moodSource: 'person' },
    weather: {
      weatherCode: 2,
      weatherKind: 'partlyCloudy',
      highCelsius: 14.4,
      lowCelsius: 8,
      precipitationMM: 1,
      placeName: 'Home',
    },
    words: {
      ...NO_WORDS,
      original: 'shipped the release. nothing broke.',
      modifiedAt: '2026-10-22T22:48:00Z',
    },
    narrative: NARRATIVE,
    choices: [CHOICE],
    workProjects: [
      {
        name: 'sample-app',
        firstAt: '2026-10-22T09:20:00Z',
        lastAt: '2026-10-22T17:05:00Z',
        activeMinutes: 130,
        stretchCount: 3,
        topics: [
          'Release branch for the sample app',
          'Fix the flaky upload test',
          'Rename the config keys',
          'Update the changelog',
          'Tidy the build scripts',
          'Remove the old fixtures',
        ],
        shipped: [
          {
            documentId: 'request-1',
            title: 'Fix the flaky upload test',
            address: 'https://git.example.com/sample/sample-app/pulls/12',
            happenedAt: '2026-10-22T15:40:00Z',
            isDraft: false,
          },
          {
            documentId: 'request-2',
            title: 'Rename the config keys',
            address: 'https://git.example.com/sample/sample-app/pulls/13',
            happenedAt: '2026-10-22T16:10:00Z',
            isDraft: true,
          },
          {
            documentId: 'request-3',
            title: 'A request with an address that is not a web page',
            address: 'javascript:alert(1)',
            happenedAt: '2026-10-22T16:20:00Z',
            isDraft: false,
          },
        ],
        summary: 'Cut the release branch and fixed the upload test that kept failing.',
        documentId: 'doc-1',
      },
    ],
    codingSessions: [
      {
        documentId: 'doc-1',
        title: 'Release branch for the sample app',
        directory: '/work/sample-app',
        assistant: 'claude-code',
        startedAt: '2026-10-22T09:20:00Z',
        endedAt: '2026-10-22T12:20:00Z',
        linkReason: 'Fixed the flaky upload test.',
        stretchCount: 3,
      },
    ],
    documents: [
      {
        documentId: 'doc-2',
        title: 'Freeze date for the platform team',
        sourceName: 'Team chat',
        happenedAt: '2026-10-22T15:52:00Z',
        isChosen: true,
        linkReason: 'you were asked to decide',
      },
      {
        documentId: 'doc-3',
        title: 'Lunch order',
        sourceName: 'Team chat',
        happenedAt: '2026-10-22T11:00:00Z',
        isChosen: false,
        linkReason: '',
      },
    ],
    documentTotal: 4,
    mail: [
      {
        itemId: 'mail-1',
        fromName: 'Ada',
        subject: 'Onboarding mockups',
        receivedAt: '2026-10-22T14:05:00Z',
        isSent: false,
        isImportant: true,
        isChosen: false,
        linkReason: '',
        repeatCount: 1,
      },
    ],
    mailTotal: 44,
    photos: [
      { photoId: 'photo-1', takenAt: '2026-10-22T08:12:00Z', placeName: 'Park', isVideo: false },
      { photoId: 'photo-2', takenAt: '2026-10-22T08:40:00Z', placeName: 'Park', isVideo: false },
    ],
    photoTotal: 3,
    recordings: [
      {
        recordingId: 'rec-1',
        title: 'Design review',
        recordedAt: '2026-10-22T13:00:00Z',
        durationMS: 6_480_000,
        summary: 'Decided on one onboarding screen.',
      },
    ],
    conversations: [
      {
        conversationId: 'conv-1',
        title: 'Draft the release announcement',
        isMain: false,
        firstAt: '2026-10-22T11:05:00Z',
        lastAt: '2026-10-22T11:30:00Z',
        messageCount: 4,
      },
      {
        conversationId: 'conv-main',
        title: 'Main chat',
        isMain: true,
        firstAt: '2026-10-22T18:00:00Z',
        lastAt: '2026-10-22T18:20:00Z',
        messageCount: 6,
      },
    ],
    transactions: [
      {
        transactionId: 'tx-1',
        postedOn: THURSDAY,
        transactedAt: '2026-10-22T12:31:00Z',
        description: 'Noodle shop',
        accountName: 'Checking',
        amountMinorUnits: -1420,
        currency: 'USD',
      },
    ],
    visits: [
      {
        visitId: 'visit-1',
        placeId: 'place-1',
        placeName: 'Office',
        arrivedAt: '2026-10-22T09:05:00Z',
        departedAt: '2026-10-22T17:20:00Z',
        latitude: 10.5,
        longitude: 20.5,
      },
    ],
    events: [
      {
        eventId: 'event-1',
        title: 'Standup',
        calendarName: 'Work',
        startsAt: '2026-10-22T09:30:00Z',
        endsAt: '2026-10-22T09:45:00Z',
        isAllDay: false,
        location: '',
        reference: 'calendar_occurrence:event-1@x',
      },
    ],
    health: {
      stepCount: 9412,
      stepsByHour: [
        0, 0, 0, 0, 0, 0, 0, 120, 640, 980, 210, 160, 830, 420, 260, 300, 380, 520, 2410, 1240, 380, 160, 90, 0,
      ],
      sleepMinutes: 370,
      sleepStages: [
        { sleepStage: 'deep', minutes: 52 },
        { sleepStage: 'core', minutes: 220 },
        { sleepStage: 'rem', minutes: 78 },
        { sleepStage: 'awake', minutes: 20 },
      ],
      restingHeartRate: 61,
      workouts: [
        {
          sampleId: 'workout-1',
          activityType: 'walking',
          startedAt: '2026-10-22T17:28:00Z',
          minutes: 32,
          meters: 2574,
        },
      ],
    },
    memoryPages: [{ nodeId: 'node-1', path: 'projects/sample-app', title: 'Sample app release', factCount: 3 }],
    moments: [
      {
        at: '2026-10-22T09:20:00Z',
        recordKind: 'agent_document',
        recordId: 'doc-1',
        title: 'Release branch for the sample app',
        detail: 'Claude Code, 3 h',
        dashboardPath: '/coding',
      },
    ],
    ...overrides,
  })
}

// the timeline

it('lists the days newest first in weeks, with the summary of a finished week and a button for a day without words', async () => {
  answerWith({
    days: [
      summary(TODAY, {
        headline: '',
        hasWords: false,
        facts: { ...NO_FACTS, codingMinutes: 130, workProjectCount: 3, placeCount: 4, photoCount: 3 },
      }),
      summary('2026-10-25', {
        headline: 'A slow Sunday by the lake.',
        wordsExcerpt: 'Best day in weeks.',
        hasWords: true,
        mood: { moodLevel: 5, moodWords: '', moodSource: 'person' },
        facts: { ...NO_FACTS, stepCount: 15920 },
      }),
      summary(THURSDAY, { headline: 'Release day', hasWords: true, wordsExcerpt: 'Shipped it.' }),
    ],
    period: (_periodKind, day) =>
      journalPeriod('week', day, '2026-10-25', {
        narrative: {
          ...NO_NARRATIVE,
          headline: 'A week that bent around one day.',
          narrative: 'Harbor shipped on Thursday after the [[agent_document:doc-1]].',
          references: NARRATIVE.references,
        },
      }),
  })
  renderAt('/journal')

  expect(await screen.findByText('A slow Sunday by the lake.')).toBeTruthy()
  expect(screen.getByText('Release day')).toBeTruthy()
  expect(screen.getByText('Today so far')).toBeTruthy()
  expect(screen.getByText('This week, October 26')).toBeTruthy()
  expect(screen.getByText('Week of October 19')).toBeTruthy()
  // Only today's day has no words, and says how to add them.
  expect(screen.getAllByRole('link', { name: 'Add your words' })).toHaveLength(1)
  // A day with no headline says its numbers, and not its date a second time.
  expect(screen.getByRole('link', { name: '3 projects, 4 places, 3 photos' }).getAttribute('href')).toBe(
    '/journal/2026-10-26',
  )
  expect(screen.queryByRole('heading', { level: 3, name: 'October 26' })).toBeNull()
  // The finished week is asked for to head its days. The week that holds today is read once,
  // for the card of the week so far, and not for a summary it has no story for.
  await waitFor(() => expect(screen.getByText(/Harbor shipped on Thursday after the Release branch/)).toBeTruthy())
  expect(screen.getByText('A week that bent around one day.')).toBeTruthy()
  const weekCalls = calls('ReadJournalPeriod')
    .map(([, variables]) => variables as { periodKind: string; day: string })
    .sort((first, second) => first.day.localeCompare(second.day))
  expect(weekCalls).toEqual([
    { periodKind: 'week', day: '2026-10-19' },
    { periodKind: 'week', day: '2026-10-26' },
  ])
  // The newest page has nothing newer.
  expect(screen.getByRole('button', { name: 'Newer' }).hasAttribute('disabled')).toBe(true)
  expect(screen.getByRole('button', { name: 'Older' }).hasAttribute('disabled')).toBe(true)
  // A day of the month calendar is a link to the day, with its mood said in words.
  expect(screen.getByRole('link', { name: 'October 25: Great' }).getAttribute('href')).toBe('/journal/2026-10-25')
})

it('opens today in the size asked for from the heading', async () => {
  answerWith({ days: [summary(THURSDAY)] })
  renderAt('/journal')
  await screen.findByText('Week of October 19')
  const group = screen.getByRole('group', { name: 'Look at' })
  expect(within(group).getByRole('link', { name: 'Day' }).getAttribute('href')).toBe('/journal/2026-10-26')
  expect(within(group).getByRole('link', { name: 'Week' }).getAttribute('href')).toBe('/journal/week/2026-10-26')
  expect(within(group).getByRole('link', { name: 'Month' }).getAttribute('href')).toBe('/journal/month/2026-10')
  expect(within(group).getByRole('link', { name: 'Year' }).getAttribute('href')).toBe('/journal/year/2026')
  expect(screen.getByRole('link', { name: 'Write today' }).getAttribute('href')).toBe('/journal/2026-10-26')
})

it('offers to add words only on today, and a day with nothing to say says so', async () => {
  answerWith({
    days: [
      summary(TODAY, { hasWords: false }),
      summary('2026-10-24', { hasWords: false, headline: 'A day out.' }),
      summary('2026-10-23', { hasWords: false, facts: { ...NO_FACTS, photoCount: 1 } }),
    ],
  })
  renderAt('/journal')
  expect(await screen.findByText('A day out.')).toBeTruthy()
  expect(screen.getAllByRole('link', { name: 'Add your words' })).toHaveLength(1)
  expect(screen.getByRole('link', { name: 'A quiet day' })).toBeTruthy()
  expect(screen.getByRole('link', { name: '1 photo' }).getAttribute('href')).toBe('/journal/2026-10-23')
})

it('adds up the week so far in hours and projects, with the way to the week at its foot', async () => {
  answerWith({
    days: [summary(TODAY)],
    period: (_kind, day) =>
      journalPeriod('week', day, '2026-11-01', {
        dayCount: 2,
        totals: { ...NO_FACTS, codingMinutes: 370, workProjectCount: 3, codingSessionCount: 179 },
      }),
  })
  renderAt('/journal')
  const card = (await screen.findByRole('heading', { name: 'This week so far' })).closest('section') as HTMLElement
  expect(await within(card).findByText('6 h 10 min, 3 projects')).toBeTruthy()
  expect(within(card).queryByText(/179/)).toBeNull()
  const link = within(card).getByRole('link', { name: 'Read the week' })
  expect(link.getAttribute('href')).toBe('/journal/week/2026-10-26')
  // After the numbers, not between the heading and them.
  const numbers = card.querySelector('dl') as HTMLElement
  expect(numbers.compareDocumentPosition(link) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
})

it('searches the journal and lists the days and periods that match', async () => {
  answerWith({ days: [summary(THURSDAY)] })
  renderAt('/journal')
  fireEvent.change(await screen.findByRole('searchbox', { name: 'Search the journal' }), {
    target: { value: 'harbor' },
  })
  vi.mocked(graphql).mockImplementation(async (document) => {
    if (document.includes('SearchJournal')) {
      return {
        SearchJournal: {
          hits: [
            { day: THURSDAY, periodKind: '', headline: 'Release day', snippet: '...shipped the harbor release...' },
            { day: '2026-10-19', periodKind: 'week', headline: 'A week that bent', snippet: '' },
          ],
          totalCount: 2,
        },
      } as never
    }
    return { ListJournalDays: { days: [], totalCount: 0, today: TODAY, firstDay: '' } } as never
  })
  fireEvent.submit(screen.getByRole('search'))
  const hit = await screen.findByRole('link', { name: /Release day/ })
  expect(hit.getAttribute('href')).toBe('/journal/2026-10-22')
  expect(screen.getByRole('link', { name: /A week that bent/ }).getAttribute('href')).toBe('/journal/week/2026-10-19')
  expect(screen.getByTestId('address').textContent).toBe('/journal?q=harbor')
})

// the day

// The units come from the person's account, and from the browser's region only
// while they have chosen none. The browser here is en-US, which reads as
// Fahrenheit and miles.
it.each([
  ['metric', '14°', '8°', '5.4 km walked'],
  ['us', '58°', '46°', '3.4 mi walked'],
  ['', '58°', '46°', '3.4 mi walked'],
])(
  'writes a day in the units of a person who chose %j, in an en-US browser',
  async (measurementSystem, high, low, walked) => {
    answerWith({ day: () => fullDay({ facts: { ...FACTS, walkedMeters: 5400 } }) })
    renderAt('/journal/2026-10-22', ['journal:use'], measurementSystem)

    expect(await screen.findByText(high)).toBeTruthy()
    expect(screen.getByText(`/ ${low}`)).toBeTruthy()
    expect(screen.getByText(walked)).toBeTruthy()
  },
)

it('writes the weather of a metric browser in Celsius until the person chooses US units', async () => {
  const browserLanguage = vi.spyOn(window.navigator, 'language', 'get').mockReturnValue('en-GB')
  try {
    answerWith({ day: () => fullDay() })
    renderAt('/journal/2026-10-22')
    expect(await screen.findByText('14°')).toBeTruthy()
    cleanup()

    renderAt('/journal/2026-10-22', ['journal:use'], 'us')
    expect(await screen.findByText('58°')).toBeTruthy()
  } finally {
    browserLanguage.mockRestore()
  }
})

it('shows the sections of a day, each with something to show, and the narrative with its mentions as chips', async () => {
  answerWith({ day: () => fullDay() })
  renderAt('/journal/2026-10-22')

  // The numbers and the weather.
  expect(await screen.findByText('9,412')).toBeTruthy()
  expect(screen.getByText('58°')).toBeTruthy()
  expect(screen.getByText('Partly cloudy, at Home')).toBeTruthy()
  // The person's own words.
  expect(screen.getByText('shipped the release. nothing broke.')).toBeTruthy()
  // A mention the reader can open is a chip with the record's title; one that is gone is plain words.
  const chip = screen
    .getAllByRole('link', { name: 'Release branch for the sample app' })
    .find((link) => link.className.includes('journal-chip'))
  expect(chip?.getAttribute('href')).toBe('/coding')
  const story = screen.getByText(/You worked on the/).closest('p') as HTMLElement
  expect(story.textContent).toContain('and kept a a photo from the walk.')
  expect(within(story).queryByRole('link', { name: /photo/ })).toBeNull()
  expect(screen.getByText('Written from 12 things. Each mention opens the thing itself.')).toBeTruthy()

  // Work: one card for the project, the sessions and chosen documents behind Details, and the
  // rest counted in a section of their own.
  expect(screen.getByRole('heading', { name: 'What you worked on' })).toBeTruthy()
  expect(screen.queryByText('sample-app, Claude Code, 3 stretches of work')).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: 'Details' }))
  expect(await screen.findByText('sample-app, Claude Code, 3 stretches of work')).toBeTruthy()
  expect(screen.getByText('Chosen: you were asked to decide')).toBeTruthy()
  expect(screen.getByText('3 more documents and messages were left out as routine.')).toBeTruthy()
  expect(screen.queryByText('Lunch order')).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: 'Expand Work chat and documents' }))
  expect(await screen.findByText('Lunch order')).toBeTruthy()

  // Mail that mattered, with the way to all of the day's mail.
  expect(screen.getByText('Mail that mattered')).toBeTruthy()
  expect(screen.getByText('Marked important')).toBeTruthy()
  expect(screen.getByRole('link', { name: 'All 44' }).getAttribute('href')).toBe(
    '/mailbox?everywhere=true&since=2026-10-22&before=2026-10-22',
  )
  // Photos are links to their page that open in a lightbox.
  expect(screen.getByRole('link', { name: 'Photo at 8:12 AM' }).getAttribute('href')).toBe('/photos/photo-1')
  // The rest of the left column.
  expect(screen.getByText('Design review')).toBeTruthy()
  expect(screen.getByText('Draft the release announcement')).toBeTruthy()
  expect(screen.getByText('Noodle shop')).toBeTruthy()
  expect(screen.getByText('$14.20')).toBeTruthy()
  // Knowledge is one line until it is opened.
  expect(screen.getByText('TeaNode wrote or changed 1 memory page from this day')).toBeTruthy()
  expect(screen.queryByText('Sample app release')).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: 'Expand Knowledge' }))
  expect(await screen.findByText('Sample app release')).toBeTruthy()
  // The right column: the map, the day in order, Health and the calendar.
  expect(screen.getByTestId('map').textContent).toBe('1 places')
  expect(screen.getByText('Through the day')).toBeTruthy()
  expect(screen.getByRole('img', { name: 'Steps by hour, most at 6 PM' })).toBeTruthy()
  expect(screen.getByRole('img', { name: 'Sleep stages' })).toBeTruthy()
  expect(screen.getByText('Walking, 32 min')).toBeTruthy()
  expect(screen.getByText('61 beats a minute')).toBeTruthy()
  expect(screen.getByText('Standup')).toBeTruthy()
})

it('shows only the sections a day has, and says what to do on a day with nothing', async () => {
  answerWith({ day: () => journalDay('2026-10-24') })
  renderAt('/journal/2026-10-24')
  expect(await screen.findByText(/Nothing is linked to this day yet/)).toBeTruthy()
  for (const heading of [
    'What you worked on',
    'Work chat and documents',
    'Mail that mattered',
    'Photos',
    'Recordings',
    'Money',
    'Places',
    'Health',
    'Calendar',
  ]) {
    expect(screen.queryByRole('heading', { name: heading })).toBeNull()
  }
  // Writing is open at once on a day with no words.
  expect(screen.getByRole('textbox', { name: 'What you wrote' })).toBeTruthy()
  // And there is no story to write for a day that is not over, only for a finished one.
  expect(screen.getByText(/TeaNode has not written this yet/)).toBeTruthy()
  expect(screen.getByRole('button', { name: "Write this day's story" })).toBeTruthy()
})

it('offers to write today so far, and a day older than the stories TeaNode writes on its own', async () => {
  answerWith({
    day: (day) =>
      day === TODAY ? journalDay(day) : journalDay(day, { narrative: { ...NO_NARRATIVE, isBeyondDepth: true } }),
  })
  renderAt('/journal/2026-10-26')
  expect(
    await screen.findByText('TeaNode writes the story of a day after it ends. You can ask for the day so far.'),
  ).toBeTruthy()
  expect(screen.queryByRole('button', { name: "Write this day's story" })).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: 'Write today so far' }))
  await waitFor(() => expect(calls('WriteJournalNarrative')).toHaveLength(1))
  expect(calls('WriteJournalNarrative')[0][1]).toEqual({ day: TODAY, periodKind: null })
})

it('shows that today is being written, and the day so far once it is', async () => {
  answerWith({ day: (day) => journalDay(day, { narrative: { ...NO_NARRATIVE, narrativePending: true } }) })
  renderAt('/journal/2026-10-26')
  expect(await screen.findByText('TeaNode is writing this now. It takes about a minute.')).toBeTruthy()
  expect(screen.queryByRole('button', { name: 'Write today so far' })).toBeNull()
  cleanup()

  answerWith({
    day: (day) =>
      journalDay(day, {
        narrative: {
          ...NO_NARRATIVE,
          narrative: 'So far you wrote the release notes.',
          writtenAt: '2026-10-26T10:00:00Z',
        },
      }),
  })
  renderAt('/journal/2026-10-26')
  expect(await screen.findByText('So far you wrote the release notes.')).toBeTruthy()
  expect(screen.getByRole('button', { name: 'Write again' })).toBeTruthy()
})

it('writes a day older than the depth when asked', async () => {
  answerWith({ day: (day) => journalDay(day, { narrative: { ...NO_NARRATIVE, isBeyondDepth: true } }) })
  renderAt('/journal/2025-03-02')
  expect(await screen.findByText(/older than the stories TeaNode writes on its own/)).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: "Write this day's story" }))
  await waitFor(() => expect(calls('WriteJournalNarrative')).toHaveLength(1))
  expect(calls('WriteJournalNarrative')[0][1]).toEqual({ day: '2025-03-02', periodKind: null })
})

it('picking a mood saves it, and the words under the faces save when the field is left', async () => {
  answerWith({ day: () => fullDay({ mood: { moodLevel: 4, moodWords: 'Tired', moodSource: 'person' } }) })
  renderAt('/journal/2026-10-22')
  const great = await screen.findByRole('radio', { name: 'Great' })
  expect(screen.getByRole('radio', { name: 'Good' }).getAttribute('aria-checked')).toBe('true')
  fireEvent.click(great)
  await waitFor(() => expect(calls('SetJournalMood')).toHaveLength(1))
  expect(calls('SetJournalMood')[0][1]).toEqual({ day: THURSDAY, moodLevel: 5, moodWords: 'Tired' })

  const words = screen.getByLabelText('In a few words')
  fireEvent.change(words, { target: { value: 'Tired, but it is out' } })
  fireEvent.blur(words)
  await waitFor(() => expect(calls('SetJournalMood')).toHaveLength(2))
  expect(calls('SetJournalMood')[1][1]).toEqual({ day: THURSDAY, moodLevel: 4, moodWords: 'Tired, but it is out' })
})

it('marks a mood that came from Apple Health, and keeps the words field closed until a face is chosen', async () => {
  answerWith({ day: () => fullDay({ mood: { moodLevel: 3, moodWords: '', moodSource: 'health' } }) })
  renderAt('/journal/2026-10-22')
  expect(await screen.findByText('From Apple Health')).toBeTruthy()
  expect(screen.getByRole('radio', { name: 'Okay' }).getAttribute('aria-checked')).toBe('true')

  cleanup()
  answerWith({ day: () => journalDay(THURSDAY) })
  renderAt('/journal/2026-10-22')
  expect((await screen.findByLabelText('In a few words')).hasAttribute('disabled')).toBe(true)
})

it('moves the faces with the arrow keys', async () => {
  answerWith({ day: () => fullDay({ mood: { moodLevel: 4, moodWords: '', moodSource: 'person' } }) })
  renderAt('/journal/2026-10-22')
  const group = await screen.findByRole('radiogroup', { name: 'Mood' })
  fireEvent.keyDown(group, { key: 'ArrowRight' })
  await waitFor(() => expect(calls('SetJournalMood')).toHaveLength(1))
  expect(calls('SetJournalMood')[0][1]).toMatchObject({ moodLevel: 5 })
})

// the person's words

const TOUCHED_UP_WORDS = {
  ...NO_WORDS,
  original: 'shipped the release. nothing broke.',
  modifiedAt: '2026-10-22T22:48:00Z',
  isTouchUpOn: true,
  touchedUp: 'Shipped the release at last. Nothing broke.',
  touchUpHow: 'Warmer',
}

it('shows the touched-up words with the original one switch away, and saving the original sends the time it was read', async () => {
  answerWith({ day: () => fullDay({ words: TOUCHED_UP_WORDS }) })
  renderAt('/journal/2026-10-22')

  expect(await screen.findByText('Shipped the release at last. Nothing broke.')).toBeTruthy()
  expect(screen.getByText('Touched up by TeaNode from your original: Warmer.')).toBeTruthy()
  expect(screen.getByRole('button', { name: 'Touched up' }).getAttribute('aria-pressed')).toBe('true')

  fireEvent.click(screen.getByRole('button', { name: 'Your original' }))
  const original = (await screen.findByRole('textbox', { name: /What you wrote/ })) as HTMLTextAreaElement
  expect(original.value).toBe('shipped the release. nothing broke.')
  expect(screen.getByTestId('address').textContent).toBe('/journal/2026-10-22?text=original')
  // Nothing to save until something changes.
  expect(screen.getByRole('button', { name: 'Save and touch up again' }).hasAttribute('disabled')).toBe(true)

  fireEvent.change(original, { target: { value: 'shipped the release. nothing broke. walked home.' } })
  fireEvent.click(screen.getByRole('button', { name: 'Save and touch up again' }))
  await waitFor(() => expect(calls('SetJournalWords')).toHaveLength(1))
  expect(calls('SetJournalWords')[0][1]).toEqual({
    day: THURSDAY,
    words: 'shipped the release. nothing broke. walked home.',
    baseModifiedAt: '2026-10-22T22:48:00Z',
  })
  // The instruction did not change, so the server's remaking from the new words is all there is.
  expect(calls('TouchUpJournalWords')).toHaveLength(0)
  // And the touched-up text is the one shown again.
  await waitFor(() => expect(screen.getByTestId('address').textContent).toBe('/journal/2026-10-22'))
})

it('asks for another touch up when the instruction changes with the save, and for another version on request', async () => {
  answerWith({ day: () => fullDay({ words: TOUCHED_UP_WORDS }) })
  renderAt('/journal/2026-10-22?text=original')
  const how = (await screen.findByLabelText('How')) as HTMLInputElement
  expect(how.value).toBe('Warmer')
  fireEvent.change(how, { target: { value: 'Shorter' } })
  fireEvent.click(screen.getByRole('button', { name: 'Save and touch up again' }))
  await waitFor(() => expect(calls('TouchUpJournalWords')).toHaveLength(1))
  expect(calls('SetJournalWords')).toHaveLength(1)
  expect(calls('TouchUpJournalWords')[0][1]).toEqual({ day: THURSDAY, periodKind: null, how: 'Shorter' })

  fireEvent.click(await screen.findByRole('button', { name: 'Another version' }))
  await waitFor(() => expect(calls('TouchUpJournalWords')).toHaveLength(2))
  expect(calls('TouchUpJournalWords')[1][1]).toEqual({ day: THURSDAY, periodKind: null, how: null })
})

it('stops touching up a day', async () => {
  answerWith({ day: () => fullDay({ words: TOUCHED_UP_WORDS }) })
  renderAt('/journal/2026-10-22?text=original')
  fireEvent.click(await screen.findByRole('button', { name: 'Stop touching up this day' }))
  await waitFor(() => expect(calls('StopJournalTouchUp')).toHaveLength(1))
  expect(calls('StopJournalTouchUp')[0][1]).toEqual({ day: THURSDAY, periodKind: null })
})

it('edits the original in place when touching up is off, and offers to touch it up', async () => {
  answerWith({ day: () => fullDay() })
  renderAt('/journal/2026-10-22')
  expect(await screen.findByText('shipped the release. nothing broke.')).toBeTruthy()
  expect(screen.queryByRole('button', { name: 'Touched up' })).toBeNull()

  fireEvent.click(screen.getByRole('button', { name: 'Edit' }))
  const box = (await screen.findByRole('textbox', { name: 'What you wrote' })) as HTMLTextAreaElement
  fireEvent.change(box, { target: { value: 'shipped it' } })
  fireEvent.click(screen.getByRole('button', { name: 'Save' }))
  await waitFor(() => expect(calls('SetJournalWords')).toHaveLength(1))
  expect(calls('SetJournalWords')[0][1]).toMatchObject({ words: 'shipped it', baseModifiedAt: '2026-10-22T22:48:00Z' })

  fireEvent.click(await screen.findByRole('button', { name: 'Touch up' }))
  const dialog = await screen.findByRole('dialog', { name: 'Touch up your words' })
  fireEvent.change(within(dialog).getByLabelText('How'), { target: { value: 'Warmer' } })
  fireEvent.click(within(dialog).getByRole('button', { name: 'Touch up' }))
  await waitFor(() => expect(calls('TouchUpJournalWords')).toHaveLength(1))
  expect(calls('TouchUpJournalWords')[0][1]).toEqual({ day: THURSDAY, periodKind: null, how: 'Warmer' })
})

it('a first save on a day with no words sends no time to compare', async () => {
  answerWith({ day: () => journalDay('2026-10-24') })
  renderAt('/journal/2026-10-24')
  fireEvent.change(await screen.findByRole('textbox', { name: 'What you wrote' }), { target: { value: 'a quiet day' } })
  fireEvent.click(screen.getByRole('button', { name: 'Save' }))
  await waitFor(() => expect(calls('SetJournalWords')).toHaveLength(1))
  expect(calls('SetJournalWords')[0][1]).toEqual({ day: '2026-10-24', words: 'a quiet day', baseModifiedAt: null })
})

it('says a day was saved somewhere else when the save is refused, and reads it again', async () => {
  answerWith({ day: () => fullDay(), failures: { SetJournalWords: 'api: conflict' } })
  renderAt('/journal/2026-10-22?text=original')
  fireEvent.change(await screen.findByRole('textbox', { name: 'What you wrote' }), {
    target: { value: 'changed here' },
  })
  fireEvent.click(screen.getByRole('button', { name: 'Save' }))
  expect(await screen.findByText('This was saved somewhere else first. Nothing was lost.')).toBeTruthy()
  await waitFor(() => expect(calls('ReadJournalDay')).toHaveLength(2))
  // What the person typed is still there to save again, under the newer text.
  expect(await screen.findByText(/Saved somewhere else meanwhile/)).toBeTruthy()
  expect((screen.getByRole('textbox', { name: 'What you wrote' }) as HTMLTextAreaElement).value).toBe('changed here')
})

it('shows notes from the notes folder as read only, marked as such', async () => {
  answerWith({
    day: () =>
      fullDay({
        words: {
          ...NO_WORDS,
          journalNotes: [{ documentId: 'note-1', title: 'Morning pages', text: 'Slept badly.' }],
        },
      }),
  })
  renderAt('/journal/2026-10-22')
  expect(await screen.findByText('From your notes')).toBeTruthy()
  expect(screen.getByText('Morning pages')).toBeTruthy()
  expect(screen.getByText('Slept badly.')).toBeTruthy()
})

it('asks again every three seconds while words are being touched up, and stops after two minutes', async () => {
  vi.useFakeTimers({ toFake: ['Date', 'setInterval', 'clearInterval'] })
  vi.setSystemTime(new Date(2026, 9, 26, 12, 0, 0))
  answerWith({ day: () => fullDay({ words: { ...TOUCHED_UP_WORDS, touchedUp: '', touchUpPending: true } }) })
  renderAt('/journal/2026-10-22')
  expect(await screen.findByText(/TeaNode is touching up your words/)).toBeTruthy()
  expect(calls('ReadJournalDay')).toHaveLength(1)
  await act(async () => {
    await vi.advanceTimersByTimeAsync(3000)
  })
  expect(calls('ReadJournalDay')).toHaveLength(2)
  await act(async () => {
    await vi.advanceTimersByTimeAsync(3000)
  })
  expect(calls('ReadJournalDay')).toHaveLength(3)
  await act(async () => {
    await vi.advanceTimersByTimeAsync(180_000)
  })
  const atTheEnd = calls('ReadJournalDay').length
  // 2 minutes at 3 seconds: it stopped asking, however long the page stays open.
  expect(atTheEnd).toBeLessThanOrEqual(1 + 40)
  await act(async () => {
    await vi.advanceTimersByTimeAsync(60_000)
  })
  expect(calls('ReadJournalDay')).toHaveLength(atTheEnd)
})

// the story

it('opens a dialog for what is wrong with the story, and sends the correction', async () => {
  answerWith({ day: () => fullDay() })
  renderAt('/journal/2026-10-22')
  fireEvent.click(await screen.findByRole('button', { name: 'Something wrong?' }))
  const dialog = await screen.findByRole('dialog', { name: 'What did it get wrong?' })
  const submit = within(dialog).getByRole('button', { name: 'Write it again' })
  expect(submit.hasAttribute('disabled')).toBe(true)
  fireEvent.change(within(dialog).getByLabelText('What should it say?'), {
    target: { value: 'The walk was 25 minutes.' },
  })
  fireEvent.click(submit)
  await waitFor(() => expect(calls('CorrectJournalNarrative')).toHaveLength(1))
  expect(calls('CorrectJournalNarrative')[0][1]).toEqual({
    day: THURSDAY,
    periodKind: null,
    correctionText: 'The walk was 25 minutes.',
  })
  await waitFor(() => expect(screen.queryByRole('dialog', { name: 'What did it get wrong?' })).toBeNull())
})

it('lists the corrections and removes one by its place in the list', async () => {
  answerWith({
    day: () =>
      fullDay({
        narrative: {
          ...NARRATIVE,
          corrections: [
            { correctionText: 'The walk was 25 minutes.', createdAt: '2026-10-23T07:00:00Z' },
            { correctionText: 'A friend came along.', createdAt: '2026-10-23T07:01:00Z' },
          ],
        },
      }),
  })
  renderAt('/journal/2026-10-22')
  expect(await screen.findByText('The walk was 25 minutes.')).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: 'Remove: A friend came along.' }))
  await waitFor(() => expect(calls('RemoveJournalCorrection')).toHaveLength(1))
  expect(calls('RemoveJournalCorrection')[0][1]).toEqual({ day: THURSDAY, periodKind: null, correctionIndex: 1 })
})

it('writes the story again, and shows when it is being written and why a try did not work', async () => {
  answerWith({ day: () => fullDay() })
  renderAt('/journal/2026-10-22')
  fireEvent.click(await screen.findByRole('button', { name: 'Write again' }))
  await waitFor(() => expect(calls('WriteJournalNarrative')).toHaveLength(1))
  expect(calls('WriteJournalNarrative')[0][1]).toEqual({ day: THURSDAY, periodKind: null })

  cleanup()
  answerWith({
    day: () =>
      fullDay({ narrative: { ...NARRATIVE, isStale: true, failure: 'It named something that is not in the day.' } }),
  })
  renderAt('/journal/2026-10-22')
  expect(await screen.findByText(/Something changed since this was written/)).toBeTruthy()
  expect(screen.getByText('The last try did not work: It named something that is not in the day.')).toBeTruthy()
})

it("hides one of the agent's choices", async () => {
  answerWith({ day: () => fullDay() })
  renderAt('/journal/2026-10-22')
  fireEvent.click(await screen.findByRole('button', { name: 'Details' }))
  fireEvent.click(await screen.findByRole('button', { name: 'Hide this choice: Freeze date for the platform team' }))
  await waitFor(() => expect(calls('RemoveJournalLink')).toHaveLength(1))
  expect(calls('RemoveJournalLink')[0][1]).toEqual({ linkId: 'link-1' })
})

it('shows what the person pinned, with a way to take it off', async () => {
  answerWith({
    day: () =>
      fullDay({
        pins: [{ ...CHOICE, linkId: 'pin-1', linkSource: 'person', linkReason: 'for the book' }],
      }),
  })
  renderAt('/journal/2026-10-22')
  expect(await screen.findByText('Pinned')).toBeTruthy()
  expect(screen.getByText('for the book')).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: 'Remove: Freeze date for the platform team' }))
  await waitFor(() => expect(calls('RemoveJournalLink')).toHaveLength(1))
  expect(calls('RemoveJournalLink')[0][1]).toEqual({ linkId: 'pin-1' })
})

it('steps to the day before and after, but not past today, and out to the week', async () => {
  answerWith({ day: (day) => journalDay(day, { weekStart: '2026-10-19' }) })
  renderAt('/journal/2026-10-26')
  const before = await screen.findByRole('link', { name: 'The day before' })
  expect(before.getAttribute('href')).toBe('/journal/2026-10-25')
  expect(screen.getByRole('button', { name: 'The day after' }).hasAttribute('disabled')).toBe(true)
  expect(screen.getByRole('link', { name: 'Today' }).getAttribute('href')).toBe('/journal/2026-10-26')
  expect(screen.getByRole('link', { name: 'Timeline' }).getAttribute('href')).toBe('/journal')
})

it('sends a day that is not a date back to the timeline', async () => {
  answerWith({ days: [summary(THURSDAY)] })
  renderAt('/journal/yesterday')
  await waitFor(() => expect(screen.getByTestId('address').textContent).toBe('/journal'))
})

it('shows nothing of the journal to somebody without the permission', async () => {
  answerWith()
  renderAt('/journal', [])
  expect(await screen.findByText('The journal is not turned on for your account.')).toBeTruthy()
  expect(calls('ListJournalDays')).toHaveLength(0)
})

// work, folding, and opening records in place

it('shows the work as a card for each project: its time, what was done, what it was about and what was sent for review', async () => {
  answerWith({ day: () => fullDay() })
  renderAt('/journal/2026-10-22')

  const project = (await screen.findByRole('heading', { name: 'sample-app' })).closest('article') as HTMLElement
  expect(within(project).getByText('2 h 10 min, 9:20 AM to 5:05 PM')).toBeTruthy()
  expect(within(project).getByText('Cut the release branch and fixed the upload test that kept failing.')).toBeTruthy()
  // Four topics and a count of the rest, never all six.
  expect(within(project).getByText('Update the changelog')).toBeTruthy()
  expect(within(project).queryByText('Tidy the build scripts')).toBeNull()
  expect(within(project).getByText('and 2 more')).toBeTruthy()
  // Requests open in a new tab, and a draft says so. An address that is not a web page is only words.
  const request = within(project).getByRole('link', { name: 'Fix the flaky upload test' })
  expect(request.getAttribute('href')).toBe('https://git.example.com/sample/sample-app/pulls/12')
  expect(request.getAttribute('target')).toBe('_blank')
  expect(request.getAttribute('rel')).toBe('noopener noreferrer')
  expect(within(project).getByRole('link', { name: 'Rename the config keys' })).toBeTruthy()
  expect(within(project).getByText('Draft')).toBeTruthy()
  expect(within(project).getByText('A request with an address that is not a web page')).toBeTruthy()
  expect(within(project).queryByRole('link', { name: /not a web page/ })).toBeNull()
  // The work tile counts projects, not sessions.
  expect(screen.getByText('1 project')).toBeTruthy()
  expect(screen.queryByText(/session/)).toBeNull()
})

it('folds a section up to its head, remembers it, and works without anywhere to remember', async () => {
  answerWith({ day: () => fullDay() })
  renderAt('/journal/2026-10-22')

  const fold = await screen.findByRole('button', { name: 'Collapse Photos' })
  expect(fold.getAttribute('aria-expanded')).toBe('true')
  expect(document.getElementById(fold.getAttribute('aria-controls') as string)).toBeTruthy()
  expect(screen.getByRole('link', { name: 'Photo at 8:12 AM' })).toBeTruthy()
  fireEvent.click(fold)
  expect(screen.queryByRole('link', { name: 'Photo at 8:12 AM' })).toBeNull()
  expect(screen.getByRole('button', { name: 'Expand Photos' }).getAttribute('aria-expanded')).toBe('false')
  expect(window.localStorage.getItem('teanode.journal.collapsed.card:photos')).toBe('1')

  // Another reading of the page finds it folded still, on another day too.
  cleanup()
  renderAt('/journal/2026-10-22')
  expect(await screen.findByRole('button', { name: 'Expand Photos' })).toBeTruthy()
  expect(screen.queryByRole('link', { name: 'Photo at 8:12 AM' })).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: 'Expand Photos' }))
  expect(screen.getByRole('link', { name: 'Photo at 8:12 AM' })).toBeTruthy()
  expect(window.localStorage.getItem('teanode.journal.collapsed.card:photos')).toBe('0')
})

it('starts the long sections folded, and folds without storage', async () => {
  const read = vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
    throw new Error('blocked')
  })
  const write = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
    throw new Error('blocked')
  })
  try {
    answerWith({ day: () => fullDay() })
    renderAt('/journal/2026-10-22')
    // Folded by default: the details of the work, the work chat, and Knowledge.
    expect((await screen.findByRole('button', { name: 'Details' })).getAttribute('aria-expanded')).toBe('false')
    expect(screen.getByRole('button', { name: 'Expand Work chat and documents' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Expand Knowledge' })).toBeTruthy()
    // Folding still works for as long as the page is open.
    fireEvent.click(screen.getByRole('button', { name: 'Collapse Money' }))
    expect(screen.queryByText('Noodle shop')).toBeNull()
  } finally {
    read.mockRestore()
    write.mockRestore()
  }
})

it('shows the first six rows of a long list, and the rest on request', async () => {
  const pages = Array.from({ length: 8 }, (_, index) => ({
    nodeId: `node-${index}`,
    path: `projects/page-${index}`,
    title: `Memory page ${index}`,
    factCount: 1,
  }))
  window.localStorage.setItem('teanode.journal.collapsed.card:knowledge', '0')
  answerWith({ day: () => fullDay({ memoryPages: pages }) })
  renderAt('/journal/2026-10-22')

  expect(await screen.findByText('Memory page 5')).toBeTruthy()
  expect(screen.queryByText('Memory page 6')).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: 'Show all 8' }))
  expect(screen.getByText('Memory page 7')).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: 'Show fewer' }))
  expect(screen.queryByText('Memory page 6')).toBeNull()
})

it('shows the first twelve moments of the day, and the rest on request', async () => {
  const moments = Array.from({ length: 14 }, (_, index) => ({
    at: `2026-10-22T${String(8 + index).padStart(2, '0')}:00:00Z`,
    recordKind: 'agent_document',
    recordId: `moment-${index}`,
    title: `Moment number ${index}`,
    detail: '',
    dashboardPath: '/coding',
  }))
  answerWith({ day: () => fullDay({ moments }) })
  renderAt('/journal/2026-10-22')
  expect(await screen.findByText('Moment number 11')).toBeTruthy()
  expect(screen.queryByText('Moment number 12')).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: 'Show all 14' }))
  expect(screen.getByText('Moment number 13')).toBeTruthy()
})

it('opens a conversation in the drawer, over the day, and names the main one', async () => {
  vi.mocked(openAgentConversation).mockReturnValue(true)
  answerWith({ day: () => fullDay() })
  renderAt('/journal/2026-10-22')

  fireEvent.click(await screen.findByRole('link', { name: 'Draft the release announcement' }))
  expect(openAgentConversation).toHaveBeenCalledWith('conv-1')
  // The address is still the day: the chat page was not visited.
  expect(screen.getByTestId('address').textContent).toBe('/journal/2026-10-22')

  // The server's English name is the person's main chat, which is named in their language.
  expect(screen.queryByText('Main chat', { selector: 'a' })).toBeTruthy()
  fireEvent.click(screen.getByRole('link', { name: 'Main chat' }))
  expect(openAgentConversation).toHaveBeenLastCalledWith('conv-main')
  expect(screen.getByTestId('address').textContent).toBe('/journal/2026-10-22')
})

it('follows the link to the chats when there is no drawer to open it in', async () => {
  vi.mocked(openAgentConversation).mockReturnValue(false)
  answerWith({ day: () => fullDay() })
  renderAt('/journal/2026-10-22')
  fireEvent.click(await screen.findByRole('link', { name: 'Draft the release announcement' }))
  expect(screen.getByTestId('address').textContent).toBe('/chat?conversation=conv-1')
})

it('opens a photo in a lightbox over the day, moves through the day’s photos, and closes back to the day', async () => {
  answerWith({ day: () => fullDay() })
  renderAt('/journal/2026-10-22')

  fireEvent.click(await screen.findByRole('link', { name: 'Photo at 8:12 AM' }))
  const lightbox = await screen.findByRole('dialog', { name: 'Photo at 8:12 AM' })
  expect(within(lightbox).getByRole('img').getAttribute('src')).toBe('/api/v1/photos/photo-1/previews/large')
  expect(within(lightbox).getByText('1 of 2')).toBeTruthy()
  expect(screen.getByTestId('address').textContent).toBe('/journal/2026-10-22')

  fireEvent.click(within(lightbox).getByRole('button', { name: 'Next picture' }))
  expect(screen.getByRole('dialog', { name: 'Photo at 8:40 AM' })).toBeTruthy()
  expect(screen.getByText('2 of 2')).toBeTruthy()
  fireEvent.keyDown(document, { key: 'ArrowLeft' })
  expect(screen.getByRole('dialog', { name: 'Photo at 8:12 AM' })).toBeTruthy()

  fireEvent.keyDown(document, { key: 'Escape' })
  expect(screen.queryByRole('dialog')).toBeNull()
  expect(screen.getByTestId('address').textContent).toBe('/journal/2026-10-22')
})

it('opens a photo that the story mentions in the same lightbox', async () => {
  answerWith({
    day: () =>
      fullDay({
        narrative: {
          ...NARRATIVE,
          narrative: 'You walked in the [[photo:photo-2]].',
          references: [
            {
              recordKind: 'photo',
              recordId: 'photo-2',
              reference: 'photo:photo-2',
              title: 'Morning in the park',
              happenedAt: '2026-10-22T08:40:00Z',
              dashboardPath: '/photos/photo-2',
              isUnavailable: false,
            },
          ],
        },
      }),
  })
  renderAt('/journal/2026-10-22')
  fireEvent.click(await screen.findByRole('link', { name: 'Morning in the park' }))
  expect(await screen.findByRole('dialog', { name: 'Photo at 8:40 AM' })).toBeTruthy()
  expect(screen.getByTestId('address').textContent).toBe('/journal/2026-10-22')
})

it('says how many times mail of the same sender and subject came', async () => {
  answerWith({
    day: () =>
      fullDay({
        mail: [
          {
            itemId: 'mail-9',
            fromName: 'Build robot',
            subject: 'Nightly build failed',
            receivedAt: '2026-10-22T03:00:00Z',
            isSent: false,
            isImportant: true,
            isChosen: false,
            linkReason: '',
            repeatCount: 4,
          },
        ],
      }),
  })
  renderAt('/journal/2026-10-22')
  expect(await screen.findByText('4 times')).toBeTruthy()
  expect(screen.queryByText('1 times')).toBeNull()
})

// the week, the month and the year

const WEEK_DAYS = [
  summary('2026-10-19', {
    headline: 'Release branch cut.',
    facts: { ...NO_FACTS, stepCount: 6100, codingMinutes: 390 },
  }),
  summary('2026-10-21', {
    headline: 'A late night chasing a test.',
    mood: { moodLevel: 2, moodWords: '', moodSource: 'person' },
    facts: { ...NO_FACTS, sleepMinutes: 340, stepCount: 4200, codingMinutes: 440 },
  }),
  summary(THURSDAY, {
    headline: 'Harbor 2.4 shipped.',
    mood: { moodLevel: 4, moodWords: '', moodSource: 'person' },
    weather: {
      weatherCode: 3,
      weatherKind: 'cloudy',
      highCelsius: 14,
      lowCelsius: 8,
      precipitationMM: 0,
      placeName: 'Home',
    },
    facts: {
      ...NO_FACTS,
      sleepMinutes: 420,
      stepCount: 9412,
      codingMinutes: 340,
      spentMinorUnits: 8640,
      transactionCount: 4,
      mainPlaceName: 'Office',
    },
  }),
]

function weekPeriod(): JournalPeriod {
  return journalPeriod('week', '2026-10-19', '2026-10-25', {
    narrative: {
      ...NARRATIVE,
      narrative: 'A week that bent around one day, ending in the [[agent_document:doc-1]].',
      sourceCount: 7,
    },
    lookingBack: { ...NO_WORDS, original: 'Too much of the week at the desk.', modifiedAt: '2026-10-25T20:00:00Z' },
    days: WEEK_DAYS,
    totals: {
      ...NO_FACTS,
      stepCount: 19712,
      codingMinutes: 1170,
      codingSessionCount: 6,
      spentMinorUnits: 8640,
      transactionCount: 4,
      placeCount: 3,
    },
    choices: [{ ...CHOICE, record: { ...CHOICE.record, happenedAt: '2026-10-22T12:00:00Z' } }],
    dayCount: 3,
  })
}

it('shows a week: its story, the looking back, seven day columns, numbers and the moments chosen', async () => {
  answerWith({ period: () => weekPeriod() })
  renderAt('/journal/week/2026-10-21')

  expect(await screen.findByText('The week, as TeaNode saw it')).toBeTruthy()
  expect(calls('ReadJournalPeriod')[0][1]).toEqual({ periodKind: 'week', day: '2026-10-21' })
  expect(screen.getByRole('link', { name: 'Release branch for the sample app' })).toBeTruthy()
  expect(screen.getByText('Too much of the week at the desk.')).toBeTruthy()
  // Seven columns: three days with something, four without.
  const columns = within(screen.getByRole('region', { name: 'The seven days' })).getAllByRole('link')
  expect(columns).toHaveLength(7)
  expect(columns[3].getAttribute('href')).toBe('/journal/2026-10-22')
  expect(screen.getByText('Harbor 2.4 shipped.')).toBeTruthy()
  // The week in numbers, each with its bars.
  expect(screen.getByRole('img', { name: 'Steps each day of the week' })).toBeTruthy()
  expect(screen.getByRole('img', { name: 'Sleep each night of the week' })).toBeTruthy()
  expect(screen.getByText('19,712')).toBeTruthy()
  expect(screen.getByText('Most on Thursday, 9,412')).toBeTruthy()
  // What the agent chose, and where the days were spent.
  expect(screen.getByText('Moments of the week')).toBeTruthy()
  expect(screen.getByText('you were asked to decide')).toBeTruthy()
  expect(screen.getByRole('link', { name: 'Office' })).toBeTruthy()
  // The way out and along.
  expect(screen.getByRole('link', { name: 'The week before' }).getAttribute('href')).toBe('/journal/week/2026-10-12')
  expect(screen.getByRole('link', { name: 'The week after' }).getAttribute('href')).toBe('/journal/week/2026-10-26')
  const sizes = screen.getByRole('group', { name: 'Look at' })
  expect(within(sizes).getByRole('link', { name: 'Day' }).getAttribute('href')).toBe('/journal/2026-10-19')
  expect(within(sizes).getByRole('link', { name: 'Month' }).getAttribute('href')).toBe('/journal/month/2026-10')
})

it("writes a week's looking back with the time it was read, and corrects a week's story", async () => {
  answerWith({ period: () => weekPeriod() })
  renderAt('/journal/week/2026-10-19')
  fireEvent.click(await screen.findByRole('button', { name: 'Edit' }))
  fireEvent.change(await screen.findByRole('textbox', { name: 'What you wrote' }), {
    target: { value: 'Keep Saturdays free.' },
  })
  fireEvent.click(screen.getByRole('button', { name: 'Save' }))
  await waitFor(() => expect(calls('SetJournalLookingBack')).toHaveLength(1))
  expect(calls('SetJournalLookingBack')[0][1]).toEqual({
    periodKind: 'week',
    day: '2026-10-19',
    lookingBack: 'Keep Saturdays free.',
    baseModifiedAt: '2026-10-25T20:00:00Z',
  })

  fireEvent.click(screen.getByRole('button', { name: 'Something wrong?' }))
  const dialog = await screen.findByRole('dialog', { name: 'What did it get wrong?' })
  fireEvent.change(within(dialog).getByLabelText('What should it say?'), { target: { value: 'It ended on Friday.' } })
  fireEvent.click(within(dialog).getByRole('button', { name: 'Write it again' }))
  await waitFor(() => expect(calls('CorrectJournalNarrative')).toHaveLength(1))
  expect(calls('CorrectJournalNarrative')[0][1]).toEqual({
    day: '2026-10-19',
    periodKind: 'week',
    correctionText: 'It ended on Friday.',
  })
})

it('shows a month as a calendar of its days with the mood, the high and the headline, and the side cards', async () => {
  answerWith({
    period: () =>
      journalPeriod('month', '2026-10-01', '2026-10-31', {
        days: WEEK_DAYS,
        narrative: { ...NARRATIVE, narrative: 'The month of the release.', headline: 'October' },
        totals: {
          ...NO_FACTS,
          sleepMinutes: 1160,
          photoCount: 312,
          spentMinorUnits: 218460,
          codingMinutes: 700,
          codingSessionCount: 4,
        },
        moodCounts: [0, 1, 0, 3, 1],
        dayCount: 3,
        chapters: [
          { fromDay: '2026-10-19', untilDay: '2026-10-22', title: 'The release', detail: 'Four days of release work.' },
        ],
      }),
  })
  renderAt('/journal/month/2026-10')

  expect(await screen.findByText('The month of the release.')).toBeTruthy()
  expect(calls('ReadJournalPeriod')[0][1]).toEqual({ periodKind: 'month', day: '2026-10-01' })
  const cell = screen.getByRole('link', { name: 'October 22: Good' })
  expect(cell.getAttribute('href')).toBe('/journal/2026-10-22')
  expect(within(cell).getByText('Harbor 2.4 shipped.')).toBeTruthy()
  expect(within(cell).getByText('57°')).toBeTruthy()
  // Days that are still to come are not links.
  expect(screen.queryByRole('link', { name: 'October 30' })).toBeNull()
  expect(screen.getByRole('img', { name: '1 low, 3 good, 1 great' })).toBeTruthy()
  expect(screen.getByText('The release')).toBeTruthy()
  expect(screen.getByRole('button', { name: 'Write about October' })).toBeTruthy()
  expect(screen.getByRole('link', { name: 'The month before' }).getAttribute('href')).toBe('/journal/month/2026-09')
  // This month is the latest there is.
  expect(screen.getByRole('button', { name: 'The month after' }).hasAttribute('disabled')).toBe(true)
})

it("shows a month day's first photo when no cover was chosen, and the numbers without a mood bar when no day has a mood", async () => {
  answerWith({
    period: () =>
      journalPeriod('month', '2026-10-01', '2026-10-31', {
        days: [
          summary(THURSDAY, { photoIds: ['photo-first', 'photo-second'] }),
          summary('2026-10-21', { coverPhotoId: 'photo-chosen', photoIds: ['photo-other'] }),
        ],
        totals: { ...NO_FACTS, codingMinutes: 21_000, workProjectCount: 12, codingSessionCount: 10764 },
        moodCounts: [0, 0, 0, 0, 0],
      }),
  })
  renderAt('/journal/month/2026-10')
  expect(await screen.findByText('The month in numbers')).toBeTruthy()
  expect(screen.queryByText('Mood across the month')).toBeNull()
  expect(screen.getByText('350 h, 12 projects')).toBeTruthy()
  expect(screen.queryByText(/sessions/)).toBeNull()
  expect(document.querySelector('.journal-mood-bar')).toBeNull()
  const sources = [...document.querySelectorAll('.journal-month-photo')].map((image) => image.getAttribute('src'))
  expect(sources).toContain('/api/v1/photos/photo-first/previews/small')
  expect(sources).toContain('/api/v1/photos/photo-chosen/previews/small')
  expect(sources).not.toContain('/api/v1/photos/photo-other/previews/small')
  // The five moods are one unit, so a narrow legend moves them together.
  expect(document.querySelectorAll('.journal-legend-items .journal-legend-item')).toHaveLength(5)
})

it('shows a year as twelve small months, its numbers and its chapters', async () => {
  answerWith({
    period: () =>
      journalPeriod('year', '2026-01-01', '2026-12-31', {
        narrative: { ...NARRATIVE, narrative: 'The year the sample app grew up.' },
        months: [
          {
            periodStart: '2026-01-01',
            headline: 'A prototype and snow.',
            dayMoods: Array.from({ length: 31 }, (_, index) => (index % 5) + 1),
            placeCount: 18,
            photoCount: 40,
          },
          {
            periodStart: '2026-10-01',
            headline: 'The release.',
            dayMoods: [4, 4, 0, 5],
            placeCount: 29,
            photoCount: 312,
          },
        ],
        totals: { ...NO_FACTS, placeCount: 142, photoCount: 3912, stepCount: 2600000, recordingCount: 96 },
        chapters: [
          {
            fromDay: '2026-01-01',
            untilDay: '2026-03-31',
            title: 'Building the first version',
            detail: 'Most evenings at the desk.',
          },
        ],
        dayCount: 299,
        isCurrent: true,
      }),
  })
  renderAt('/journal/year/2026')

  expect(await screen.findByText('The year the sample app grew up.')).toBeTruthy()
  expect(calls('ReadJournalPeriod')[0][1]).toEqual({ periodKind: 'year', day: '2026-01-01' })
  const months = within(screen.getByRole('region', { name: 'Months of 2026' })).getAllByRole('link')
  expect(months).toHaveLength(12)
  expect(months[0].getAttribute('href')).toBe('/journal/month/2026-01')
  expect(within(months[0]).getByText('A prototype and snow.')).toBeTruthy()
  expect(within(months[9]).getByText('The release.')).toBeTruthy()
  expect(screen.getByText('The year in numbers')).toBeTruthy()
  expect(screen.getByText('3,912')).toBeTruthy()
  expect(screen.getByText('Building the first version')).toBeTruthy()
  expect(screen.getByRole('button', { name: 'Write about 2026' })).toBeTruthy()
  expect(screen.getByRole('button', { name: 'The year after' }).hasAttribute('disabled')).toBe(true)
  expect(screen.getByRole('link', { name: 'The year before' }).getAttribute('href')).toBe('/journal/year/2025')
})

// settings

it('turns the weather and the evening question on and off, and sets the time', async () => {
  answerWith({ settings: { ...SETTINGS, isEveningQuestionOn: true } })
  renderAt('/journal?tab=settings')

  const weather = await screen.findByRole('checkbox', { name: "Add each day's weather" })
  await waitFor(() => expect((weather as HTMLInputElement).disabled).toBe(false))
  expect((weather as HTMLInputElement).checked).toBe(true)
  expect(screen.getByText('TeaNode reads your journal to write it. What it reads goes to Example Models.')).toBeTruthy()

  fireEvent.click(weather)
  await waitFor(() => expect(calls('UpdateJournalSettings')).toHaveLength(1))
  expect(calls('UpdateJournalSettings')[0][1]).toEqual({ isWeatherOn: false })

  fireEvent.click(screen.getByRole('checkbox', { name: 'Ask how the day was each evening' }))
  await waitFor(() => expect(calls('UpdateJournalSettings')).toHaveLength(2))
  expect(calls('UpdateJournalSettings')[1][1]).toEqual({ isEveningQuestionOn: false })

  const time = screen.getByLabelText('Ask at') as HTMLInputElement
  expect(time.value).toBe('21:00')
  fireEvent.change(time, { target: { value: '20:30' } })
  fireEvent.click(screen.getByRole('button', { name: 'Save' }))
  await waitFor(() => expect(calls('UpdateJournalSettings')).toHaveLength(3))
  expect(calls('UpdateJournalSettings')[2][1]).toEqual({ eveningQuestionTime: '20:30' })
})

it('erases the whole journal only after it is confirmed', async () => {
  answerWith()
  renderAt('/journal?tab=settings')
  fireEvent.click(await screen.findByRole('button', { name: 'Erase the journal' }))
  const dialog = await screen.findByRole('alertdialog', { name: 'Erase the journal' })
  expect(calls('EraseJournal')).toHaveLength(0)
  fireEvent.click(within(dialog).getByRole('button', { name: 'Erase the journal' }))
  await waitFor(() => expect(calls('EraseJournal')).toHaveLength(1))
  expect(await screen.findByText('Erased the journal: 3 days.')).toBeTruthy()
})

it('keeps the tab in the address', async () => {
  answerWith()
  renderAt('/journal')
  fireEvent.click(await screen.findByRole('button', { name: 'Settings' }))
  await waitFor(() => expect(screen.getByTestId('address').textContent).toBe('/journal?tab=settings'))
})

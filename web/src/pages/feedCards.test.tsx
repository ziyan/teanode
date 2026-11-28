import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { graphql, subscribe } from '../api'
import { TranslationProvider } from '../i18n/i18n'
import { FeedItem, FeedPage } from './feed'

const notices = vi.hoisted(() => ({ done: vi.fn(), failed: vi.fn(), failure: vi.fn(), clear: vi.fn() }))
vi.mock('../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../api')>()),
  graphql: vi.fn(),
  subscribe: vi.fn(),
  openAgentConversation: vi.fn(() => true),
  askAgentAbout: vi.fn(() => true),
}))
// The version check asks the server on its own; the feed's tests answer for the feed alone.
vi.mock('../components/freshness', () => ({ useFreshness: () => ({ staleBundle: false, upgradeAvailable: false }) }))
vi.mock('../components/toast', () => ({ useToast: () => notices }))
const execute = vi.mocked(graphql)

function card(overrides: Partial<FeedItem> = {}): FeedItem {
  return {
    feedItemId: 'card-one',
    feedItemKind: 'update',
    feedSourceKind: 'goal',
    feedSourceId: 'goal-garden',
    feedSourceTitle: 'Garden watch',
    runConversationId: '',
    title: 'Greenhouse door open',
    body: 'The door has been open since noon.',
    pictureSymbol: 'house',
    factRows: [],
    sourceLinks: [],
    recordReferences: [],
    choices: [],
    chosenChoiceKey: null,
    chosenAt: null,
    outcomeText: '',
    isOutcomePending: false,
    feedUrgency: 'today',
    isLiked: false,
    isRead: true,
    isDismissed: false,
    dismissedAt: null,
    postedAt: '2030-01-01T08:00:00Z',
    updatedAt: '2030-01-01T09:00:00Z',
    feedStackKey: '',
    stackItemCount: 1,
    stackUnreadCount: 0,
    feedStackTitle: '',
    stackPosition: null,
    feedCardLayout: 'text',
    layoutContent: '',
    feedMedia: [],
    ...overrides,
  }
}

function listing(feedItems: FeedItem[]) {
  return { ListAgentFeed: { feedItems, nextCursor: null, unreadCount: 0 } }
}

beforeEach(() => {
  execute.mockReset()
  Object.values(notices).forEach((notice) => notice.mockReset())
  vi.mocked(subscribe).mockImplementation((() => () => {}) as never)
})
afterEach(() => cleanup())

function draw() {
  return render(
    <TranslationProvider>
      <MemoryRouter initialEntries={['/feed']}>
        <Routes>
          <Route path="/feed" element={<FeedPage />} />
        </Routes>
      </MemoryRouter>
    </TranslationProvider>,
  )
}

const resolvedAt = '2030-01-01T10:41:00Z'

it('asks for the new fields in the feed documents', async () => {
  execute.mockResolvedValue(listing([card()]))
  draw()
  await screen.findByText('Greenhouse door open')
  const document = execute.mock.calls[0][0] as string
  for (const field of [
    'isResolved',
    'resolvedAt',
    'resolutionNote',
    'resolvedByKind',
    'chartBlocks',
    'seriesRole',
    'markerAt',
    'barValue',
    'cameraName',
    'capturedAt',
    'stillReason',
    'playbackUrl',
    'liveUrl',
  ])
    expect(document).toContain(field)
})

it('draws a resolved card muted, with the chip in the person zone and the note under the body', async () => {
  execute.mockResolvedValue(
    listing([
      card({ isResolved: true, resolvedAt, resolutionNote: 'The door was shut at 10:41.', resolvedByKind: 'person' }),
    ]),
  )
  const { container } = draw()
  await screen.findByText('Greenhouse door open')
  const expectedClock = new Date(resolvedAt).toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' })
  const chip = container.querySelector('.feed-resolved-chip')
  expect(chip?.textContent).toBe(`Resolved ${expectedClock}`)
  expect(container.querySelector('.feed-card.is-resolved')).toBeTruthy()
  expect(screen.getByText('The door was shut at 10:41.').className).toContain('muted')
})

it('draws an open card without the chip or the note', async () => {
  execute.mockResolvedValue(listing([card({ resolutionNote: 'stale note' })]))
  const { container } = draw()
  await screen.findByText('Greenhouse door open')
  expect(container.querySelector('.feed-resolved-chip')).toBeNull()
  expect(container.querySelector('.is-resolved')).toBeNull()
  expect(screen.queryByText('stale note')).toBeNull()
})

it('resolves from the info sheet with an optional note', async () => {
  execute.mockImplementation(async (document: string, variables?: unknown) => {
    if (document.includes('ResolveAgentFeedItem')) {
      return {
        ResolveAgentFeedItem: card({
          isResolved: true,
          resolvedAt,
          resolutionNote: (variables as { resolutionNote: string }).resolutionNote,
        }),
      }
    }
    return listing([card()])
  })
  const { container } = draw()
  fireEvent.click(await screen.findByRole('button', { name: /Why this is here/ }))
  fireEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Resolve' }))
  const dialog = await screen.findByRole('dialog', { name: 'Resolve this card' })
  fireEvent.change(within(dialog).getByRole('textbox'), { target: { value: 'Shut it myself' } })
  fireEvent.click(within(dialog).getByRole('button', { name: 'Resolve' }))
  await waitFor(() =>
    expect(execute).toHaveBeenCalledWith(expect.stringContaining('ResolveAgentFeedItem'), {
      feedItemId: 'card-one',
      resolutionNote: 'Shut it myself',
    }),
  )
  expect(await screen.findByText('Shut it myself')).toBeTruthy()
  expect(container.querySelector('.feed-resolved-chip')).toBeTruthy()
  expect(notices.done).toHaveBeenCalledWith('Marked resolved.')
})

it('unresolves from the info sheet', async () => {
  execute.mockImplementation(async (document: string) =>
    document.includes('UnresolveAgentFeedItem')
      ? { UnresolveAgentFeedItem: card() }
      : listing([card({ isResolved: true, resolvedAt, resolutionNote: 'Shut' })]),
  )
  const { container } = draw()
  fireEvent.click(await screen.findByRole('button', { name: /Why this is here/ }))
  const sheet = await screen.findByRole('dialog')
  expect(within(sheet).queryByRole('button', { name: 'Resolve' })).toBeNull()
  fireEvent.click(within(sheet).getByRole('button', { name: 'Unresolve' }))
  await waitFor(() =>
    expect(execute).toHaveBeenCalledWith(expect.stringContaining('UnresolveAgentFeedItem'), {
      feedItemId: 'card-one',
    }),
  )
  await waitFor(() => expect(container.querySelector('.feed-resolved-chip')).toBeNull())
  expect(notices.done).toHaveBeenCalledWith('Marked unresolved.')
})

it('draws a series and bars on the card', async () => {
  execute.mockResolvedValue(
    listing([
      card({
        chartBlocks: [
          {
            chartKind: 'series',
            chartTitle: 'Door temperature',
            unit: 'C',
            provenance: 'Read from the sensor at 12:05',
            xLabels: ['12:00', '12:05'],
            series: [{ seriesName: 'Inside', seriesRole: 'measured', values: [18, 21] }],
            markers: [{ markerAt: '12:05', markerLabel: 'Door opened' }],
            bars: [],
          },
          {
            chartKind: 'bars',
            chartTitle: 'Opens by day',
            unit: 'times',
            provenance: 'Read from the log at 12:05',
            xLabels: [],
            series: [],
            markers: [],
            bars: [{ barLabel: 'Monday', barValue: 3 }],
          },
        ],
      }),
    ]),
  )
  const { container } = draw()
  await screen.findByText('Door temperature')
  expect(container.querySelectorAll('.feed-chart')).toHaveLength(2)
  expect(screen.getByText('3 times')).toBeTruthy()
  expect(screen.getByText('Door opened')).toBeTruthy()
})

it('draws a camera still with its caption, camera time, reason, playback and a Watch live button', async () => {
  execute.mockResolvedValue(
    listing([
      card({
        feedMedia: [
          {
            mediaKey: 'still-1',
            feedMediaKind: 'camera_still',
            imageUrl: '/media/still-1.jpg',
            videoUrl: '',
            linkUrl: '',
            siteName: '',
            mediaCaption: 'A parcel on the step',
            widthPixels: null,
            heightPixels: null,
            listRowIndex: null,
            cameraName: 'Front door',
            capturedAt: '2030-01-01T14:02:11+09:00',
            stillReason: 'The camera at the front door, where the parcel was left',
            playbackUrl: 'https://cameras.example.test/playback/1',
            liveUrl: 'https://cameras.example.test/live/1',
          },
        ],
      }),
    ]),
  )
  const { container } = draw()
  await screen.findByText('A parcel on the step')
  const line = container.querySelector('.feed-still-line')
  expect(line?.textContent).toMatch(/^Front door · /)
  expect(line?.textContent).toMatch(/2:02:11/)
  expect(screen.getByText('The camera at the front door, where the parcel was left')).toBeTruthy()
  const playback = screen.getByRole('link', { name: 'Open the recording from Front door' })
  expect(playback.getAttribute('href')).toBe('https://cameras.example.test/playback/1')
  const live = screen.getByRole('link', { name: 'Watch live' })
  expect(live.getAttribute('href')).toBe('https://cameras.example.test/live/1')
  expect(live.className).toContain('button')
})

it('leaves out Watch live and the link when the camera gave no address, and refuses other schemes', async () => {
  execute.mockResolvedValue(
    listing([
      card({
        feedMedia: [
          {
            mediaKey: 'still-2',
            feedMediaKind: 'camera_still',
            imageUrl: '/media/still-2.jpg',
            videoUrl: '',
            linkUrl: '',
            siteName: '',
            mediaCaption: 'Quiet yard',
            widthPixels: null,
            heightPixels: null,
            listRowIndex: null,
            cameraName: 'Back yard',
            capturedAt: '2030-01-01T14:02:11+09:00',
            stillReason: '',
            playbackUrl: 'javascript:alert(1)',
            liveUrl: '',
          },
        ],
      }),
    ]),
  )
  const { container } = draw()
  await screen.findByText('Quiet yard')
  expect(screen.queryByRole('link', { name: 'Watch live' })).toBeNull()
  expect(container.querySelector('a.feed-media-link')).toBeNull()
})

it('names the zone on an agenda time that is not in the viewer zone', async () => {
  // Written at a zone no viewer is likely to be in, so the zone is named wherever the test runs.
  const far = '2030-01-01T09:00:00+13:45'
  execute.mockResolvedValue(
    listing([
      card({
        feedCardLayout: 'agenda',
        layoutContent: JSON.stringify({
          agendaEntries: [{ startsAt: far, endsAt: '', agendaTitle: 'Harbor tour', agendaEntryKind: 'meeting' }],
        }),
      }),
    ]),
  )
  const { container } = draw()
  await screen.findByText('Harbor tour')
  const zoneName = new Date().getTimezoneOffset() === -(13 * 60 + 45) ? '' : 'UTC+13:45'
  expect(container.querySelector('.feed-agenda-time')?.textContent).toContain(zoneName)
  expect(container.querySelector('.feed-agenda-time')?.textContent).toMatch(/9:00/)
})

it('folds an answered card to its title and one muted line, and shows the card again', async () => {
  execute.mockResolvedValue(
    listing([
      card({
        choices: [{ choiceKey: 'close', choiceLabel: 'Close it', choiceRole: 'action' }],
        chosenChoiceKey: 'close',
        chosenAt: '2030-01-01T10:00:00Z',
        outcomeText: 'The door is shut.',
      }),
    ]),
  )
  const { container } = draw()
  await screen.findByText('Greenhouse door open')
  expect(screen.getByText('You chose Close it')).toBeTruthy()
  expect(screen.getByText('The door is shut.').closest('.feed-folded-line')?.className).toContain('muted')
  expect(screen.queryByText('The door has been open since noon.')).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: 'Show the card' }))
  expect(await screen.findByText('The door has been open since noon.')).toBeTruthy()
  expect(container.querySelector('.feed-folded')).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: 'Fold the card' }))
  expect(screen.queryByText('The door has been open since noon.')).toBeNull()
})

it('keeps the choices of an unanswered card in one row of equal buttons', async () => {
  execute.mockResolvedValue(
    listing([
      card({
        choices: [
          { choiceKey: 'close', choiceLabel: 'Close it', choiceRole: 'action' },
          { choiceKey: 'later', choiceLabel: 'Later', choiceRole: 'dismiss' },
        ],
      }),
    ]),
  )
  const { container } = draw()
  await screen.findByText('Greenhouse door open')
  expect(container.querySelectorAll('.feed-choices button')).toHaveLength(2)
  // The width rule is in the stylesheet, which jsdom does not load: read it as text.
  const { readFileSync } = await import('node:fs')
  const stylesheet = readFileSync(`${process.cwd()}/src/style.css`, 'utf8')
  expect(stylesheet).toMatch(/\.feed-choices button \{\s*flex: 1 1 0;/)
})

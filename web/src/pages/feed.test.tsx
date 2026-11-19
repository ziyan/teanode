import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { askAgentAbout, graphql, openAgentConversation, subscribe } from '../api'
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
vi.mock('../components/toast', () => ({ useToast: () => notices }))
const execute = vi.mocked(graphql)
const openDrawer = vi.mocked(openAgentConversation)
const askAbout = vi.mocked(askAgentAbout)

// What the page gave the subscription: the function it calls on each event.
let announce: (() => void) | null = null

function feedItem(feedItemId: string, overrides: Partial<FeedItem> = {}): FeedItem {
  return {
    feedItemId,
    feedItemKind: 'update',
    feedSourceKind: 'goal',
    feedSourceId: 'goal-garden',
    feedSourceTitle: 'Garden watch',
    runConversationId: 'run-one',
    title: `Frost tonight ${feedItemId}`,
    body: 'Cover the **tomatoes** before dusk. See [the forecast](https://example.com/forecast).',
    pictureSymbol: 'cloud.sun',
    factRows: [{ factLabel: 'Low', factValue: '-2 C' }],
    sourceLinks: [{ sourceTitle: 'Forecast page', sourceUrl: 'https://example.com/forecast' }],
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

function listing(feedItems: FeedItem[], nextCursor: string | null = null, unreadCount = 0) {
  return { ListAgentFeed: { feedItems, nextCursor, unreadCount } }
}

beforeEach(() => {
  announce = null
  execute.mockReset()
  openDrawer.mockClear()
  Object.values(notices).forEach((notice) => notice.mockReset())
  vi.mocked(subscribe).mockImplementation(((_query: string, _variables: unknown, onData: () => void) => {
    announce = onData
    return () => {}
  }) as never)
})
afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

function drawFeed() {
  return render(
    <TranslationProvider>
      <MemoryRouter initialEntries={['/feed']}>
        <Routes>
          <Route path="/feed" element={<FeedPage />} />
          <Route path="/coding" element={<p>The Coding page</p>} />
          <Route path="/settings/agent/goals" element={<p>The goals tab</p>} />
        </Routes>
      </MemoryRouter>
    </TranslationProvider>,
  )
}

const choiceCard = feedItem('card-signin', {
  title: 'New sign-in from a laptop',
  pictureSymbol: 'lock.shield',
  feedSourceKind: 'alert',
  feedSourceId: '',
  feedSourceTitle: '',
  choices: [
    { choiceKey: 'mine', choiceLabel: "That's me", choiceRole: 'dismiss' },
    { choiceKey: 'secure', choiceLabel: 'Secure my account', choiceRole: 'action' },
  ],
})

it('draws a card with its picture, body, facts and choices', async () => {
  execute.mockResolvedValue(listing([choiceCard, feedItem('card-frost')]))
  const { container } = drawFeed()
  expect(await screen.findByText('New sign-in from a laptop')).toBeTruthy()
  expect(screen.getByText('Frost tonight card-frost')).toBeTruthy()
  // The body is Markdown, with its link.
  expect(screen.getAllByText('tomatoes')[0].tagName).toBe('STRONG')
  expect(screen.getAllByRole('link', { name: 'the forecast' })[0].getAttribute('href')).toBe(
    'https://example.com/forecast',
  )
  // A label at the left and a value at the right.
  expect(screen.getAllByRole('rowheader', { name: 'Low' })).toHaveLength(2)
  expect(screen.getAllByRole('cell', { name: '-2 C' })).toHaveLength(2)
  expect(screen.getByRole('button', { name: "That's me" })).toBeTruthy()
  expect(screen.getByRole('button', { name: 'Secure my account' }).className).toContain('primary')
  // One picture per card, drawn as an icon.
  expect(container.querySelectorAll('.feed-picture svg').length).toBe(2)
})

it('falls back to a newspaper for a symbol it does not know and draws an emoji as itself', async () => {
  execute.mockResolvedValue(
    listing([feedItem('card-odd', { pictureSymbol: 'not.a.symbol' }), feedItem('card-emoji', { pictureSymbol: '🌧' })]),
  )
  const { container } = drawFeed()
  await screen.findByText('Frost tonight card-odd')
  expect(container.querySelectorAll('.feed-picture svg').length).toBe(1)
  expect(container.querySelector('.feed-picture-emoji')?.textContent).toBe('🌧')
})

it('chooses an action and shows what was chosen and the outcome', async () => {
  execute.mockImplementation(async (document: string) => {
    if (document.includes('ChooseAgentFeedItem'))
      return {
        ChooseAgentFeedItem: {
          ...choiceCard,
          chosenChoiceKey: 'secure',
          chosenAt: '2030-01-01T10:00:00Z',
          outcomeText: 'Signed out the other session.',
        },
      }
    return listing([choiceCard])
  })
  drawFeed()
  fireEvent.click(await screen.findByRole('button', { name: 'Secure my account' }))
  expect(await screen.findByText('You chose Secure my account')).toBeTruthy()
  expect(screen.getByText('Signed out the other session.')).toBeTruthy()
  expect(execute).toHaveBeenCalledWith(expect.stringContaining('ChooseAgentFeedItem'), {
    feedItemId: 'card-signin',
    choiceKey: 'secure',
  })
  expect(screen.queryByRole('button', { name: "That's me" })).toBeNull()
})

it('shows a spinner while the turn after a choice is still running', async () => {
  execute.mockResolvedValue(
    listing([{ ...choiceCard, chosenChoiceKey: 'secure', isOutcomePending: true, outcomeText: '' }]),
  )
  const { container } = drawFeed()
  expect(await screen.findByText('You chose Secure my account')).toBeTruthy()
  expect(screen.getByText('Working on it')).toBeTruthy()
  expect(container.querySelector('.agent-spinner')).not.toBeNull()
})

it('toggles the heart', async () => {
  execute.mockImplementation(async (document: string, variables?: Record<string, unknown>) => {
    if (document.includes('LikeAgentFeedItem'))
      return { LikeAgentFeedItem: feedItem('card-frost', { isLiked: Boolean(variables?.isLiked) }) }
    return listing([feedItem('card-frost')])
  })
  drawFeed()
  const heart = await screen.findByRole('button', { name: 'Like' })
  expect(heart.getAttribute('aria-pressed')).toBe('false')
  fireEvent.click(heart)
  await waitFor(() =>
    expect(screen.getByRole('button', { name: 'Remove like' }).getAttribute('aria-pressed')).toBe('true'),
  )
  expect(execute).toHaveBeenCalledWith(expect.stringContaining('LikeAgentFeedItem'), {
    feedItemId: 'card-frost',
    isLiked: true,
  })
})

it('puts the heart back and says so when the server refuses it', async () => {
  execute.mockImplementation(async (document: string) => {
    if (document.includes('LikeAgentFeedItem')) throw new Error('refused')
    return listing([feedItem('card-frost')])
  })
  drawFeed()
  fireEvent.click(await screen.findByRole('button', { name: 'Like' }))
  await waitFor(() => expect(notices.failure).toHaveBeenCalled())
  expect(screen.getByRole('button', { name: 'Like' }).getAttribute('aria-pressed')).toBe('false')
})

it('opens the drawer on the conversation Discuss returns with the card on a chip', async () => {
  execute.mockImplementation(async (document: string) => {
    if (document.includes('DiscussAgentFeedItem')) return { DiscussAgentFeedItem: 'main-conversation' }
    return listing([feedItem('card-frost')])
  })
  drawFeed()
  fireEvent.click(await screen.findByRole('button', { name: 'Discuss' }))
  await waitFor(() => expect(openDrawer).toHaveBeenCalledWith('main-conversation'))
  expect(execute).toHaveBeenCalledWith(expect.stringContaining('DiscussAgentFeedItem'), { feedItemId: 'card-frost' })
  await waitFor(() =>
    expect(askAbout).toHaveBeenCalledWith({ feedItemId: 'card-frost', feedItemTitle: expect.any(String) as string }),
  )
})

it('opens the coding session page for an open choice', async () => {
  execute.mockResolvedValue(
    listing([
      feedItem('card-coding', {
        feedSourceKind: 'coding_session',
        feedSourceId: 'laptop/pane-3',
        choices: [{ choiceKey: 'open', choiceLabel: 'Open session', choiceRole: 'open' }],
      }),
    ]),
  )
  drawFeed()
  fireEvent.click(await screen.findByRole('button', { name: 'Open session' }))
  expect(await screen.findByText('The Coding page')).toBeTruthy()
  expect(execute).not.toHaveBeenCalledWith(expect.stringContaining('ChooseAgentFeedItem'), expect.anything())
})

it('says where a card came from, with its links, and can show less of it', async () => {
  execute.mockImplementation(async (document: string) => {
    if (document.includes('ShowLessLikeAgentFeedItem'))
      return { ShowLessLikeAgentFeedItem: feedItem('card-frost', { isDismissed: true }) }
    return listing([feedItem('card-frost')])
  })
  drawFeed()
  fireEvent.click(await screen.findByRole('button', { name: /Why this is here/ }))
  const dialog = await screen.findByRole('dialog')
  expect(within(dialog).getByText(/Posted by the goal/)).toBeTruthy()
  const goalLink = within(dialog).getByRole('link', { name: 'Garden watch' })
  expect(goalLink.getAttribute('href')).toBe('/settings/agent/goals?goal=goal-garden')
  expect(within(dialog).getByRole('link', { name: 'Forecast page' }).getAttribute('href')).toBe(
    'https://example.com/forecast',
  )
  expect(within(dialog).getByText('Posted')).toBeTruthy()
  expect(within(dialog).getByText('Updated')).toBeTruthy()
  fireEvent.click(within(dialog).getByRole('button', { name: 'Show less like this' }))
  await waitFor(() => expect(screen.queryByText('Frost tonight card-frost')).toBeNull())
  expect(execute).toHaveBeenCalledWith(expect.stringContaining('ShowLessLikeAgentFeedItem'), {
    feedItemId: 'card-frost',
  })
})

it('dismisses a card from its info dialog', async () => {
  execute.mockImplementation(async (document: string) => {
    if (document.includes('DismissAgentFeedItem')) return { DismissAgentFeedItem: feedItem('card-frost') }
    return listing([feedItem('card-frost')])
  })
  drawFeed()
  fireEvent.click(await screen.findByRole('button', { name: /Why this is here/ }))
  fireEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Dismiss' }))
  await waitFor(() => expect(screen.queryByText('Frost tonight card-frost')).toBeNull())
})

it('does not draw a source link that is not a web address', async () => {
  execute.mockResolvedValue(
    listing([feedItem('card-frost', { sourceLinks: [{ sourceTitle: 'Sneaky', sourceUrl: 'javascript:alert(1)' }] })]),
  )
  drawFeed()
  fireEvent.click(await screen.findByRole('button', { name: /Why this is here/ }))
  const dialog = await screen.findByRole('dialog')
  expect(within(dialog).queryByText('Sneaky')).toBeNull()
})

it('says what the feed is when it is empty', async () => {
  execute.mockResolvedValue(listing([]))
  drawFeed()
  expect(await screen.findByText(/Goals, schedules and alerts post here/)).toBeTruthy()
})

it('reads the feed again when the feed says something changed', async () => {
  execute.mockResolvedValueOnce(listing([feedItem('card-first')]))
  drawFeed()
  await screen.findByText('Frost tonight card-first')
  execute.mockResolvedValueOnce(listing([feedItem('card-second'), feedItem('card-first')]))
  act(() => announce?.())
  expect(await screen.findByText('Frost tonight card-second')).toBeTruthy()
  expect(screen.getByText('Frost tonight card-first')).toBeTruthy()
})

it('loads the next page under the first with More', async () => {
  execute.mockResolvedValueOnce(listing([feedItem('card-first')], 'cursor-one'))
  drawFeed()
  await screen.findByText('Frost tonight card-first')
  execute.mockResolvedValueOnce(listing([feedItem('card-older')], null))
  fireEvent.click(screen.getByRole('button', { name: 'More' }))
  expect(await screen.findByText('Frost tonight card-older')).toBeTruthy()
  expect(execute).toHaveBeenLastCalledWith(expect.stringContaining('ListAgentFeed'), {
    first: 20,
    after: 'cursor-one',
  })
  expect(screen.queryByRole('button', { name: 'More' })).toBeNull()
})

it('marks unread cards read once they have been on screen, in one request', async () => {
  const watched: { callback: IntersectionObserverCallback; element: Element }[] = []
  vi.stubGlobal(
    'IntersectionObserver',
    class {
      callback: IntersectionObserverCallback
      constructor(callback: IntersectionObserverCallback) {
        this.callback = callback
      }
      observe(element: Element) {
        watched.push({ callback: this.callback, element })
      }
      disconnect() {}
    },
  )
  execute.mockImplementation(async (document: string) => {
    if (document.includes('MarkAgentFeedRead')) return { MarkAgentFeedRead: 2 }
    return listing([feedItem('card-one', { isRead: false }), feedItem('card-two', { isRead: false })], null, 2)
  })
  drawFeed()
  await screen.findByText('Frost tonight card-two')
  expect(screen.getAllByRole('img', { name: 'Unread' })).toHaveLength(2)
  act(() => {
    for (const { callback, element } of watched)
      callback([{ isIntersecting: true, target: element } as IntersectionObserverEntry], {} as IntersectionObserver)
  })
  await waitFor(
    () =>
      expect(execute).toHaveBeenCalledWith(expect.stringContaining('MarkAgentFeedRead'), {
        feedItemIds: ['card-one', 'card-two'],
      }),
    { timeout: 4000 },
  )
  // They keep their mark while the page is open, so what was new can still be told.
  expect(screen.getAllByRole('img', { name: 'Unread' })).toHaveLength(2)
})

function stackListing(feedItems: FeedItem[], nextCursor: string | null = null) {
  return { ListAgentFeedStack: listing(feedItems, nextCursor).ListAgentFeed }
}

const stackTop = feedItem('news-top', {
  title: 'Newest story',
  feedStackKey: 'goal:news',
  stackItemCount: 3,
  stackUnreadCount: 2,
  feedSourceTitle: 'News watch',
})

it('draws a stack as its top card with a row saying how many more', async () => {
  execute.mockImplementation(async (document: string) =>
    document.includes('ListAgentFeedStack')
      ? stackListing([stackTop, feedItem('news-second', { title: 'Second story' })])
      : listing([stackTop, feedItem('alone')]),
  )
  const { container } = drawFeed()
  const row = await screen.findByRole('button', { name: /\+2 more from News watch/ })
  expect(row.textContent).toContain('2 unread')
  expect(row.getAttribute('aria-expanded')).toBe('false')
  expect(container.querySelectorAll('.feed-stack.is-closed')).toHaveLength(1)
  expect(screen.getAllByRole('button', { name: /more from/ })).toHaveLength(1)
  // The deck under the top card is the stack's own next card, tucked away:
  // drawn, but neither read out nor pressed.
  const tucked = await screen.findByText('Second story')
  const room = tucked.closest('.feed-stack-rest') as HTMLElement
  expect(room.getAttribute('aria-hidden')).toBe('true')
  expect(room.inert).toBe(true)
})

it('opens a stack in place with its other cards, pages it and collapses it', async () => {
  execute.mockImplementation(async (document: string, variables?: Record<string, unknown>) => {
    if (document.includes('ListAgentFeedStack')) {
      if (variables?.after === 'stack-cursor') return stackListing([feedItem('news-third', { title: 'Third story' })])
      return stackListing([stackTop, feedItem('news-second', { title: 'Second story' })], 'stack-cursor')
    }
    return listing([stackTop])
  })
  drawFeed()
  fireEvent.click(await screen.findByRole('button', { name: /\+2 more from News watch/ }))
  expect(await screen.findByText('Second story')).toBeTruthy()
  // The top card is not drawn twice.
  expect(screen.getAllByText('Newest story')).toHaveLength(1)
  expect(execute).toHaveBeenCalledWith(expect.stringContaining('ListAgentFeedStack'), {
    feedStackKey: 'goal:news',
    first: 20,
  })
  fireEvent.click(screen.getByRole('button', { name: 'More' }))
  expect(await screen.findByText('Third story')).toBeTruthy()
  expect(execute).toHaveBeenLastCalledWith(expect.stringContaining('ListAgentFeedStack'), {
    feedStackKey: 'goal:news',
    first: 20,
    after: 'stack-cursor',
  })
  fireEvent.click(screen.getByRole('button', { name: 'Show less' }))
  expect(screen.getByText('Second story').closest('.feed-stack-rest')?.getAttribute('aria-hidden')).toBe('true')
})

it('gathers an open stack back into its deck at a press outside it, and not at one inside it', async () => {
  execute.mockImplementation(async (document: string) => {
    if (document.includes('ListAgentFeedStack')) {
      return stackListing([stackTop, feedItem('news-second', { title: 'Second story' })])
    }
    return listing([stackTop, feedItem('alone', { title: 'A card alone' })])
  })
  drawFeed()
  fireEvent.click(await screen.findByRole('button', { name: /\+2 more from News watch/ }))
  const second = await screen.findByText('Second story')
  fireEvent.pointerDown(second)
  expect(screen.getByText('Second story')).toBeTruthy()
  fireEvent.pointerDown(screen.getByText('A card alone'))
  await waitFor(() =>
    expect(screen.getByText('Second story').closest('.feed-stack-rest')?.getAttribute('aria-hidden')).toBe('true'),
  )
})

it('marks the cards of an open stack read as they are seen and reads the stack on an event', async () => {
  const watched: { callback: IntersectionObserverCallback; element: Element }[] = []
  vi.stubGlobal(
    'IntersectionObserver',
    class {
      callback: IntersectionObserverCallback
      constructor(callback: IntersectionObserverCallback) {
        this.callback = callback
      }
      observe(element: Element) {
        watched.push({ callback: this.callback, element })
      }
      disconnect() {}
    },
  )
  let stackReads = 0
  execute.mockImplementation(async (document: string) => {
    if (document.includes('MarkAgentFeedRead')) return { MarkAgentFeedRead: 1 }
    if (document.includes('ListAgentFeedStack')) {
      stackReads += 1
      return stackListing([stackTop, feedItem('news-second', { title: 'Second story', isRead: false })])
    }
    return listing([stackTop], null, 1)
  })
  drawFeed()
  fireEvent.click(await screen.findByRole('button', { name: /\+2 more from News watch/ }))
  await screen.findByText('Second story')
  act(() => {
    for (const { callback, element } of watched)
      callback([{ isIntersecting: true, target: element } as IntersectionObserverEntry], {} as IntersectionObserver)
  })
  await waitFor(
    () =>
      expect(execute).toHaveBeenCalledWith(expect.stringContaining('MarkAgentFeedRead'), {
        feedItemIds: ['news-second'],
      }),
    { timeout: 4000 },
  )
  const readsBefore = stackReads
  act(() => announce?.())
  await waitFor(() => expect(stackReads).toBeGreaterThan(readsBefore))
})

it('names a titled stack above its top card and in the row, and keeps the server order', async () => {
  const titled = feedItem('brief-one', {
    title: 'Weather',
    feedStackKey: 'brief:2030-01-01',
    feedStackTitle: 'Morning brief, Tue Jan 1',
    stackItemCount: 3,
    stackPosition: 1,
    feedSourceTitle: 'Briefs',
  })
  execute.mockImplementation(async (document: string) => {
    if (document.includes('ListAgentFeedStack'))
      return stackListing([
        titled,
        feedItem('brief-three', { title: 'Zebra news', stackPosition: 2 }),
        feedItem('brief-two', { title: 'Apple news', stackPosition: 3 }),
      ])
    return listing([titled])
  })
  drawFeed()
  expect(await screen.findByText('Morning brief, Tue Jan 1')).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: '+2 more from Morning brief, Tue Jan 1' }))
  await screen.findByText('Zebra news')
  const titles = screen.getAllByRole('heading', { level: 3 }).map((heading) => heading.textContent)
  expect(titles).toEqual(['Weather', 'Zebra news', 'Apple news'])
})

it('draws a list layout as rows that link out, and only to web addresses', async () => {
  execute.mockResolvedValue(
    listing([
      feedItem('card-list', {
        feedCardLayout: 'list',
        body: 'Fallback words',
        layoutContent: JSON.stringify({
          listRows: [
            {
              rowTitle: 'Robots ship more',
              rowDetail: 'Warehouse fleets grow',
              rowMeta: 'Daily Bots',
              rowUrl: 'https://example.com/story',
            },
            { rowTitle: 'Unsafe row', rowUrl: 'javascript:alert(1)' },
            { rowTitle: 'A mail', rowRecordReference: { itemId: 'mail1' } },
          ],
        }),
      }),
    ]),
  )
  drawFeed()
  const link = await screen.findByRole('link', { name: /Robots ship more/ })
  expect(link.getAttribute('href')).toBe('https://example.com/story')
  expect(link.getAttribute('target')).toBe('_blank')
  expect(screen.getByText('Warehouse fleets grow')).toBeTruthy()
  expect(screen.getByText('Daily Bots')).toBeTruthy()
  expect(screen.queryByRole('link', { name: /Unsafe row/ })).toBeNull()
  expect(screen.getByRole('link', { name: /A mail/ }).getAttribute('href')).toBe('/mailbox/starred/mail1')
  expect(screen.queryByText('Fallback words')).toBeNull()
})

it('draws a weather layout with the temperature, high and low and an hourly strip', async () => {
  execute.mockResolvedValue(
    listing([
      feedItem('card-weather', {
        feedCardLayout: 'weather',
        layoutContent: JSON.stringify({
          placeName: 'Testville',
          currentTemperature: 17.4,
          temperatureUnit: 'C',
          conditionText: 'Partly cloudy',
          conditionSymbol: 'cloud.sun',
          highTemperature: 21,
          lowTemperature: 9,
          hourlyForecast: [
            {
              forecastHour: '2030-01-01T10:00:00Z',
              forecastTemperature: 18,
              forecastSymbol: 'cloud.rain',
              precipitationChancePercent: 40,
            },
            {
              forecastHour: '2030-01-01T11:00:00Z',
              forecastTemperature: 19,
              forecastSymbol: 'sun.max',
              precipitationChancePercent: 0,
            },
          ],
        }),
      }),
    ]),
  )
  const { container } = drawFeed()
  expect(await screen.findByText('17°C')).toBeTruthy()
  expect(screen.getByText('Testville')).toBeTruthy()
  expect(screen.getByText('Partly cloudy')).toBeTruthy()
  expect(screen.getByText('High 21°, low 9°')).toBeTruthy()
  expect(container.querySelectorAll('.feed-weather-hours li')).toHaveLength(2)
  // Rain is shown only where there is a chance of it.
  expect(screen.getAllByText('40%')).toHaveLength(1)
  expect(container.querySelectorAll('.feed-weather-rain')).toHaveLength(1)
})

it('draws an agenda layout as a timeline and links a calendar record', async () => {
  execute.mockResolvedValue(
    listing([
      feedItem('card-agenda', {
        feedCardLayout: 'agenda',
        layoutContent: JSON.stringify({
          agendaEntries: [
            {
              startsAt: '2030-01-01T09:00:00Z',
              endsAt: '2030-01-01T09:30:00Z',
              agendaTitle: 'Standup',
              agendaEntryKind: 'meeting',
              agendaDetail: 'Room two',
              agendaRecordReference: { itemId: 'event1' },
            },
            {
              startsAt: '2030-01-01T10:00:00Z',
              endsAt: '2030-01-01T11:00:00Z',
              agendaTitle: 'Deep work',
              agendaEntryKind: 'focus',
            },
          ],
        }),
      }),
    ]),
  )
  const { container } = drawFeed()
  expect(await screen.findByRole('link', { name: /Standup/ })).toBeTruthy()
  expect(screen.getByRole('link', { name: /Standup/ }).getAttribute('href')).toBe('/calendar')
  expect(screen.getByText('Room two')).toBeTruthy()
  expect(screen.getByText('Deep work')).toBeTruthy()
  expect(container.querySelector('.feed-agenda-entry.is-meeting')).not.toBeNull()
  expect(container.querySelector('.feed-agenda-entry.is-focus')).not.toBeNull()
})

it('falls back to the body when the layout content is broken or of the wrong shape', async () => {
  execute.mockResolvedValue(
    listing([
      feedItem('card-broken', { feedCardLayout: 'list', body: 'Broken fallback', layoutContent: '{not json' }),
      feedItem('card-shape', { feedCardLayout: 'weather', body: 'Shape fallback', layoutContent: '{"listRows": []}' }),
      feedItem('card-unknown', { feedCardLayout: 'carousel', body: 'Unknown fallback', layoutContent: '{}' }),
    ]),
  )
  drawFeed()
  expect(await screen.findByText('Broken fallback')).toBeTruthy()
  expect(screen.getByText('Shape fallback')).toBeTruthy()
  expect(screen.getByText('Unknown fallback')).toBeTruthy()
})

it('draws one picture at the top of the card with its site name above the headline, and opens its page', async () => {
  execute.mockResolvedValue(
    listing([
      feedItem('with-picture', {
        feedMedia: [
          {
            mediaKey: 'picture-one',
            feedMediaKind: 'picture',
            imageUrl: '/api/v1/feed/items/with-picture/media/picture-one',
            videoUrl: '',
            linkUrl: 'https://example.com/story',
            siteName: 'Example Daily',
            mediaCaption: 'A robot in a warehouse',
            widthPixels: 1200,
            heightPixels: 630,
            listRowIndex: null,
          },
        ],
      }),
    ]),
  )
  const { container } = drawFeed()
  await screen.findByText('Frost tonight with-picture')
  const image = container.querySelector('.feed-media-image') as HTMLImageElement
  expect(image.getAttribute('src')).toBe('/api/v1/feed/items/with-picture/media/picture-one')
  expect(container.querySelector('.feed-media-tile.is-wide')).toBeTruthy()
  expect(container.querySelector('.feed-media-link')?.getAttribute('target')).toBe('_blank')
  expect(screen.getByText('A robot in a warehouse')).toBeTruthy()
  expect(container.querySelector('.feed-kicker')?.textContent).toBe('Example Daily')
})

it('plays a video in place and puts a list row thumbnail at the row start', async () => {
  execute.mockResolvedValue(
    listing([
      feedItem('with-video', {
        feedCardLayout: 'list',
        layoutContent: JSON.stringify({ listRows: [{ rowTitle: 'First row' }, { rowTitle: 'Second row' }] }),
        feedMedia: [
          {
            mediaKey: 'video-one',
            feedMediaKind: 'video',
            imageUrl: '/poster',
            videoUrl: '/video',
            linkUrl: '',
            siteName: '',
            mediaCaption: '',
            widthPixels: null,
            heightPixels: null,
            listRowIndex: null,
          },
          {
            mediaKey: 'thumbnail-one',
            feedMediaKind: 'picture',
            imageUrl: '/thumbnail',
            videoUrl: '',
            linkUrl: '',
            siteName: '',
            mediaCaption: '',
            widthPixels: null,
            heightPixels: null,
            listRowIndex: 1,
          },
        ],
      }),
    ]),
  )
  const { container } = drawFeed()
  fireEvent.click(await screen.findByRole('button', { name: 'Play video' }))
  expect(container.querySelector('video')?.getAttribute('src')).toBe('/video')
  const thumbnails = container.querySelectorAll('.feed-row-thumbnail')
  expect(thumbnails).toHaveLength(1)
  expect(thumbnails[0].closest('li')?.textContent).toContain('Second row')
})

it('draws a weather card on the gradient of its condition', async () => {
  execute.mockResolvedValue(
    listing([
      feedItem('sky', {
        feedCardLayout: 'weather',
        layoutContent: JSON.stringify({
          placeName: 'Somewhere',
          currentTemperature: 3,
          temperatureUnit: 'C',
          conditionText: 'Light snow',
          conditionSymbol: 'cloud.snow',
        }),
      }),
    ]),
  )
  const { container } = drawFeed()
  await screen.findByText('Frost tonight sky')
  expect(container.querySelector('.feed-card.is-weather.is-snow')).toBeTruthy()
})

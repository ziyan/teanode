import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { graphql, subscribe } from '../api'
import { TranslationProvider } from '../i18n/i18n'
import { DashboardUpdateCard, DismissedFeedPage, FeedItem, FeedPage, groupDismissedItems, UnreadFeedPage } from './feed'

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

function dismissedItem(feedItemId: string, overrides: Partial<FeedItem> = {}): FeedItem {
  return {
    feedItemId,
    feedItemKind: 'update',
    feedSourceKind: 'goal',
    feedSourceId: 'goal-garden',
    feedSourceTitle: 'Garden watch',
    runConversationId: '',
    title: `Frost tonight ${feedItemId}`,
    body: 'Cover the tomatoes before dusk.',
    pictureSymbol: 'cloud.sun',
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
    isDismissed: true,
    dismissedAt: '2030-01-02T08:00:00Z',
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

function drawAt(path: string) {
  return render(
    <TranslationProvider>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route path="/feed" element={<FeedPage />} />
          <Route path="/feed/unread" element={<UnreadFeedPage />} />
          <Route path="/feed/dismissed" element={<DismissedFeedPage />} />
        </Routes>
      </MemoryRouter>
    </TranslationProvider>,
  )
}

// Which reads asked for the dismissed cards.
function dismissedReads() {
  return execute.mock.calls.filter(
    ([document, variables]) =>
      document.includes('ListAgentFeed(') &&
      (variables as { isDismissedOnly?: boolean } | undefined)?.isDismissedOnly === true,
  )
}

it('opens the Dismissed view from the feed header, reads only dismissed cards there, and goes back', async () => {
  execute.mockImplementation(async (_document, variables) =>
    (variables as { isDismissedOnly?: boolean } | undefined)?.isDismissedOnly
      ? listing([dismissedItem('card-old')])
      : listing([dismissedItem('card-new', { isDismissed: false, dismissedAt: null })]),
  )
  drawAt('/feed')
  await screen.findByText('Frost tonight card-new')
  expect(dismissedReads()).toHaveLength(0)
  fireEvent.click(screen.getByRole('button', { name: 'Dismissed' }))
  expect(await screen.findByText('Frost tonight card-old')).toBeTruthy()
  expect(screen.queryByText('Frost tonight card-new')).toBeNull()
  expect(dismissedReads()).toHaveLength(1)
  // Nothing is dimmed or disabled, and a dismissed card has no Dismiss button.
  expect(screen.queryByRole('button', { name: /: Dismiss$/ })).toBeNull()
  expect(screen.getByRole('button', { name: 'Frost tonight card-old: Restore' }).hasAttribute('disabled')).toBe(false)
  fireEvent.click(screen.getByRole('button', { name: 'All' }))
  expect(await screen.findByText('Frost tonight card-new')).toBeTruthy()
})

it('draws the Dismissed view at its own address', async () => {
  execute.mockResolvedValue(listing([dismissedItem('card-old')]))
  drawAt('/feed/dismissed')
  expect(await screen.findByText('Frost tonight card-old')).toBeTruthy()
  expect(execute).toHaveBeenCalledWith(expect.stringContaining('isDismissedOnly'), {
    first: 20,
    isDismissedOnly: true,
  })
})

it('groups consecutive entries of one stack into a deck and leaves others alone', () => {
  const rows = groupDismissedItems([
    dismissedItem('a-1', { feedStackKey: 'stack-a', isRead: false }),
    dismissedItem('a-2', { feedStackKey: 'stack-a' }),
    dismissedItem('solo'),
    dismissedItem('b-1', { feedStackKey: 'stack-b' }),
    dismissedItem('a-3', { feedStackKey: 'stack-a' }),
    dismissedItem('c-1', { feedStackKey: '' }),
  ])
  expect(rows.map((row) => row.entries.map((entry) => entry.feedItemId))).toEqual([
    ['a-1', 'a-2'],
    ['solo'],
    ['b-1'],
    ['a-3'],
    ['c-1'],
  ])
  expect(rows[0].item.feedItemId).toBe('a-1')
  expect(rows[0].item.stackItemCount).toBe(2)
  expect(rows[0].item.stackUnreadCount).toBe(1)
  expect(rows[1].item.stackItemCount).toBe(1)
})

it('draws a group as a deck from the entries already read, without reading the stack', async () => {
  execute.mockResolvedValue(
    listing([
      dismissedItem('deck-1', { feedStackKey: 'stack-news', feedSourceTitle: 'News watch' }),
      dismissedItem('deck-2', { feedStackKey: 'stack-news', feedSourceTitle: 'News watch' }),
      dismissedItem('solo'),
    ]),
  )
  const { container } = drawAt('/feed/dismissed')
  const row = await screen.findByRole('button', { name: /\+1 more from News watch/ })
  expect(container.querySelectorAll('.feed-stack.is-closed')).toHaveLength(1)
  expect(screen.getByText('Frost tonight deck-2')).toBeTruthy()
  // The server's stack read lists only cards that are not dismissed.
  expect(execute.mock.calls.some(([document]) => document.includes('ListAgentFeedStack'))).toBe(false)
  // No Dismiss all in this view.
  expect(screen.queryByRole('button', { name: /Dismiss all/ })).toBeNull()
  fireEvent.click(row)
  expect(row.getAttribute('aria-expanded')).toBe('true')
})

it('restores a card with its footer button, at once, and says so', async () => {
  let isRestored = false
  execute.mockImplementation(async (document: string) => {
    if (document.includes('RestoreAgentFeedItems')) {
      isRestored = true
      return { RestoreAgentFeedItems: ['card-one'] }
    }
    return listing(isRestored ? [dismissedItem('card-two')] : [dismissedItem('card-one'), dismissedItem('card-two')])
  })
  drawAt('/feed/dismissed')
  fireEvent.click(await screen.findByRole('button', { name: 'Frost tonight card-one: Restore' }))
  await waitFor(() => expect(screen.queryByText('Frost tonight card-one')).toBeNull())
  expect(execute).toHaveBeenCalledWith(expect.stringContaining('RestoreAgentFeedItems'), {
    feedItemIds: ['card-one'],
  })
  expect(notices.done).toHaveBeenCalledWith('Card restored')
  expect(screen.getByText('Frost tonight card-two')).toBeTruthy()
})

it('restores a card from its info sheet, where Restore replaces Show less and Dismiss', async () => {
  execute.mockImplementation(async (document: string) =>
    document.includes('RestoreAgentFeedItems')
      ? { RestoreAgentFeedItems: ['card-one'] }
      : listing([dismissedItem('card-one')]),
  )
  drawAt('/feed/dismissed')
  fireEvent.click(await screen.findByRole('button', { name: /Why this is here/ }))
  const sheet = await screen.findByRole('dialog')
  expect(within(sheet).queryByRole('button', { name: 'Show less like this' })).toBeNull()
  expect(within(sheet).queryByRole('button', { name: 'Dismiss' })).toBeNull()
  fireEvent.click(within(sheet).getByRole('button', { name: 'Restore' }))
  await waitFor(() =>
    expect(execute).toHaveBeenCalledWith(expect.stringContaining('RestoreAgentFeedItems'), {
      feedItemIds: ['card-one'],
    }),
  )
  expect(notices.done).toHaveBeenCalledWith('Card restored')
})

it('says nothing is dismissed only after a read says so, and keeps a restored card hidden meanwhile', async () => {
  let reread: ((value: unknown) => void) | null = null
  execute.mockImplementation(async (document: string) => {
    if (document.includes('RestoreAgentFeedItems')) return { RestoreAgentFeedItems: ['card-one'] }
    if (reread === null && execute.mock.calls.filter(([entry]) => entry.includes('ListAgentFeed(')).length > 1)
      return new Promise((resolve) => {
        reread = resolve
      })
    return listing([dismissedItem('card-one')])
  })
  drawAt('/feed/dismissed')
  fireEvent.click(await screen.findByRole('button', { name: 'Frost tonight card-one: Restore' }))
  await waitFor(() => expect(reread).not.toBeNull())
  // The list is empty, but the read has not come back: no empty text yet.
  expect(screen.queryByText('Nothing dismissed')).toBeNull()
  expect(screen.queryByText('Frost tonight card-one')).toBeNull()
  // The read comes back with the card still in it (it was already on the way): still hidden.
  await act(async () => {
    reread?.(listing([dismissedItem('card-one')]))
  })
  expect(await screen.findByText('Nothing dismissed')).toBeTruthy()
  expect(screen.getByText('Dismissed cards are kept until they expire.')).toBeTruthy()
  expect(screen.queryByText('Frost tonight card-one')).toBeNull()
})

it('reads the feed again when a dismissal leaves it empty, and shows what that read brings', async () => {
  let reread: ((value: unknown) => void) | null = null
  execute.mockImplementation(async (document: string) => {
    if (document.includes('DismissAgentFeedItems')) return { DismissAgentFeedItems: ['card-one'] }
    if (execute.mock.calls.some(([entry]) => entry.includes('DismissAgentFeedItems')))
      return new Promise((resolve) => {
        reread = resolve
      })
    return listing([dismissedItem('card-one', { isDismissed: false, dismissedAt: null })])
  })
  drawAt('/feed')
  fireEvent.click(await screen.findByRole('button', { name: 'Frost tonight card-one: Dismiss' }))
  await waitFor(() => expect(reread).not.toBeNull())
  expect(screen.queryByText(/Goals and alerts post here/)).toBeNull()
  // A card posted meanwhile arrives with the read, beside the stale one that stays hidden.
  await act(async () => {
    reread?.(
      listing([
        dismissedItem('card-one', { isDismissed: false, dismissedAt: null }),
        dismissedItem('card-new', { isDismissed: false, dismissedAt: null }),
      ]),
    )
  })
  expect(await screen.findByText('Frost tonight card-new')).toBeTruthy()
  expect(screen.queryByText('Frost tonight card-one')).toBeNull()
})

it('says the feed is empty when the read after the last dismissal is empty', async () => {
  execute.mockImplementation(async (document: string) => {
    if (document.includes('DismissAgentFeedItems')) return { DismissAgentFeedItems: ['card-one'] }
    return listing(
      execute.mock.calls.some(([entry]) => entry.includes('DismissAgentFeedItems'))
        ? []
        : [dismissedItem('card-one', { isDismissed: false, dismissedAt: null })],
    )
  })
  drawAt('/feed')
  fireEvent.click(await screen.findByRole('button', { name: 'Frost tonight card-one: Dismiss' }))
  expect(await screen.findByText(/Goals and alerts post here/)).toBeTruthy()
})

it('reads only the cards not yet read in the Unread view, which the views name as current', async () => {
  execute.mockImplementation(async (_document, variables) =>
    (variables as { isUnreadOnly?: boolean } | undefined)?.isUnreadOnly
      ? listing([dismissedItem('card-unread', { isDismissed: false, dismissedAt: null, isRead: false })])
      : listing([dismissedItem('card-any', { isDismissed: false, dismissedAt: null })]),
  )
  drawAt('/feed')
  await screen.findByText('Frost tonight card-any')
  expect(screen.getByRole('button', { name: 'All' }).getAttribute('aria-pressed')).toBe('true')
  fireEvent.click(screen.getByRole('button', { name: 'Unread' }))
  expect(await screen.findByText('Frost tonight card-unread')).toBeTruthy()
  expect(screen.queryByText('Frost tonight card-any')).toBeNull()
  expect(screen.getByRole('button', { name: 'Unread' }).getAttribute('aria-pressed')).toBe('true')
})

it('offers a newer dashboard as a card that reloads, or is put off', () => {
  const onReload = vi.fn()
  const onSetAside = vi.fn()
  render(
    <TranslationProvider>
      <DashboardUpdateCard onReload={onReload} onSetAside={onSetAside} />
    </TranslationProvider>,
  )
  expect(screen.getByText('A new version of the dashboard is ready')).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: 'Reload' }))
  fireEvent.click(screen.getByRole('button', { name: 'Not now' }))
  expect(onReload).toHaveBeenCalledTimes(1)
  expect(onSetAside).toHaveBeenCalledTimes(1)
})

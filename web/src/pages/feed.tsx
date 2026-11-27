import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'

import { AgentReference, askAgentAbout, graphql, openAgentConversation, subscribe } from '../api'
import { ErrorMessage, Loading } from '../components/common'
import { FormDialog } from '../components/dialog'
import { AGENT_FEED_CHANGED, announceFeedUnread } from '../components/feedUnread'
import { codingPagePath } from '../components/herdrSessions'
import {
  BellIcon,
  CalendarIcon,
  ChatIcon,
  CarIcon,
  CartIcon,
  CloseIcon,
  CodeIcon,
  HeartIcon,
  HomeIcon,
  InfoIcon,
  LightbulbIcon,
  LocationIcon,
  LockIcon,
  MailIcon,
  NewspaperIcon,
  PictureIcon,
  PlaneIcon,
  RestoreIcon,
  ShieldIcon,
  TerminalIcon,
  TrendingUpIcon,
  UserIcon,
  WalletIcon,
  WarningIcon,
  WeatherIcon,
} from '../components/icons'
import { feedClockTextOf } from '../components/feedClock'
import { chartBlocksOf, FeedChartBlocks, type FeedChartBlockData } from '../components/feedChart'
import { FeedLayoutContent, parseFeedLayout, webAddressOf } from '../components/feedLayouts'
import { FeedMediaGallery } from '../components/feedMedia'
import {
  feedHeaderLabelOf,
  isMotionReduced,
  splitFeedMedia,
  weatherGradientOf,
  type FeedMedia,
} from '../components/feedMediaLayout'
import { Markdown } from '../components/markdown'
import { ReferenceChips } from '../components/agentReferenceChips'
import { RelativeTime } from '../components/relativeTime'
import { SettingsEmpty } from '../components/settingsList'
import { SWIPE_ACTION_PX, SWIPE_STARTS_PX, swipeOffset } from '../components/swipeableLine'
import { Tooltip } from '../components/tooltip'
import { useToast } from '../components/toast'
import { Key, Trans, useTranslation } from '../i18n/i18n'

// The Feed page: what the agent posted without being asked, newest first, as
// cards. A card the agent needs an answer on carries the choices as buttons;
// choosing answers the agent without opening a conversation. The list grows
// with a More button rather than pages in the address, because the server
// pages it by a cursor that only goes forward, and because the newest card
// changes at any moment: a page number would point at different cards a
// minute later.

// How many cards one read brings.
const PAGE_SIZE = 20

// The most a refresh asks for at once, so a person who has loaded many pages
// does not lose the later ones when an event makes the list read again.
const MAXIMUM_REFRESH_SIZE = 100

// How long a card must stay on screen before it counts as seen, and how long
// seen cards wait for company so one request marks several.
const SEEN_FRACTION = 0.6
const READ_BATCH_DELAY_MS = 1200

// How often a card whose follow-up turn is still running is read again, in case
// the event that says it finished was missed.
const PENDING_RECHECK_MS = 8000

// How long a stack takes to open and to close. Every card of the stack and the
// room under the top card move on the same curve, so the cards spread out of
// the deck evenly and never cross. Neither curve overshoots: a room that grew
// past its cards and came back would push the card below it down and up again.
const STACK_OPEN_MS = 440
const STACK_CLOSE_MS = 340
const STACK_OPEN_EASING = 'cubic-bezier(0.45, 0, 0.2, 1)'
const STACK_CLOSE_EASING = 'cubic-bezier(0.5, 0, 0.3, 1)'
// A closed stack is its top card with the next cards tucked under it: the
// first shows this much of its foot below the top card and the second as much
// again below that, each narrower by these insets at either side. The cards
// further down hide behind the second. Each inset is more than a card's corner
// radius (12 px), so a tucked card's edge never shows beside the top card's
// rounded top corners.
const DECK_PEEK_PX = 8
const DECK_INSET_PX = [14, 24]
const DECK_PEEK_MOST = DECK_INSET_PX.length

// ignoreSeen stands in for marking a card read while it is tucked in its deck,
// where only its foot shows.
function ignoreSeen() {}

type FeedChoiceRole = 'action' | 'dismiss' | 'discuss' | 'open'

interface FeedChoice {
  choiceKey: string
  choiceLabel: string
  choiceRole: FeedChoiceRole
}

export interface FeedItem {
  feedItemId: string
  feedItemKind: string
  feedSourceKind: string
  feedSourceId: string
  feedSourceTitle: string
  runConversationId: string
  title: string
  body: string
  pictureSymbol: string
  factRows: { factLabel: string; factValue: string }[]
  sourceLinks: { sourceTitle: string; sourceUrl: string }[]
  recordReferences: AgentReference[]
  choices: FeedChoice[]
  chosenChoiceKey: string | null
  chosenAt: string | null
  outcomeText: string
  isOutcomePending: boolean
  feedUrgency: string
  isLiked: boolean
  isRead: boolean
  isDismissed: boolean
  dismissedAt: string | null
  postedAt: string
  updatedAt: string
  feedStackKey: string
  stackItemCount: number
  stackUnreadCount: number
  feedStackTitle: string
  stackPosition: number | null
  feedCardLayout: string
  layoutContent: string
  feedMedia: FeedMedia[]
  // A card whose problem cleared is marked resolved, with a one-line note.
  isResolved?: boolean
  resolvedAt?: string | null
  resolutionNote?: string
  resolvedByKind?: string
  // Charts as the server sends them; read with chartBlocksOf.
  chartBlocks?: FeedChartBlockData[]
}

interface FeedPageAnswer {
  feedItems: FeedItem[]
  nextCursor: string | null
  unreadCount: number
}

const ITEM_FIELDS = `feedItemId feedItemKind feedSourceKind feedSourceId feedSourceTitle runConversationId title body
  pictureSymbol factRows { factLabel factValue } sourceLinks { sourceTitle sourceUrl }
  recordReferences { itemId threadId subject from path name recordingId recordingTitle photoId photoTitle financeTransactionId postedOn amount currencyCode merchantName description }
  choices { choiceKey choiceLabel choiceRole } chosenChoiceKey chosenAt outcomeText isOutcomePending
  feedUrgency isLiked isRead isDismissed dismissedAt postedAt updatedAt feedStackKey stackItemCount stackUnreadCount feedStackTitle stackPosition feedCardLayout layoutContent
  feedMedia { mediaKey feedMediaKind imageUrl videoUrl linkUrl siteName mediaCaption widthPixels heightPixels listRowIndex cameraName capturedAt stillReason playbackUrl liveUrl }
  isResolved resolvedAt resolutionNote resolvedByKind
  chartBlocks { chartKind chartTitle unit provenance xLabels series { seriesName seriesRole values } markers { markerAt markerLabel } bars { barLabel barValue } }`

const LIST_FEED = `query ($first: Int, $after: String, $isDismissedOnly: Boolean) {
  ListAgentFeed(first: $first, after: $after, isDismissedOnly: $isDismissedOnly) { feedItems { ${ITEM_FIELDS} } nextCursor unreadCount }
}`
const LIST_STACK = `query ($feedStackKey: String!, $first: Int, $after: String) {
  ListAgentFeedStack(feedStackKey: $feedStackKey, first: $first, after: $after) { feedItems { ${ITEM_FIELDS} } nextCursor unreadCount }
}`
const CHOOSE_ITEM = `mutation ($feedItemId: String!, $choiceKey: String!) {
  ChooseAgentFeedItem(feedItemId: $feedItemId, choiceKey: $choiceKey) { ${ITEM_FIELDS} }
}`
const LIKE_ITEM = `mutation ($feedItemId: String!, $isLiked: Boolean!) {
  LikeAgentFeedItem(feedItemId: $feedItemId, isLiked: $isLiked) { ${ITEM_FIELDS} }
}`
const SHOW_LESS_ITEM = `mutation ($feedItemId: String!) {
  ShowLessLikeAgentFeedItem(feedItemId: $feedItemId) { ${ITEM_FIELDS} }
}`
const DISMISS_ITEMS = `mutation ($feedItemIds: [String!], $feedStackKey: String, $isEveryReadCard: Boolean) {
  DismissAgentFeedItems(feedItemIds: $feedItemIds, feedStackKey: $feedStackKey, isEveryReadCard: $isEveryReadCard)
}`
const RESTORE_ITEMS = `mutation ($feedItemIds: [String!]!) {
  RestoreAgentFeedItems(feedItemIds: $feedItemIds)
}`

// Which cards a dismissal closes: the ones named, a stack's, or every read one.
// Exactly one is given, as the operation wants.
type DismissSelection = { feedItemIds: string[] } | { feedStackKey: string } | { isEveryReadCard: true }
const RESOLVE_ITEM = `mutation ($feedItemId: String!, $resolutionNote: String) {
  ResolveAgentFeedItem(feedItemId: $feedItemId, resolutionNote: $resolutionNote) { ${ITEM_FIELDS} }
}`
const UNRESOLVE_ITEM = `mutation ($feedItemId: String!) {
  UnresolveAgentFeedItem(feedItemId: $feedItemId) { ${ITEM_FIELDS} }
}`
const MARK_READ = `mutation ($feedItemIds: [String!]!) { MarkAgentFeedRead(feedItemIds: $feedItemIds) }`
const DISCUSS_ITEM = `mutation ($feedItemId: String!) { DiscussAgentFeedItem(feedItemId: $feedItemId) }`

// pictureIcon draws a card's picture from the symbol name the agent chose
// (an SF Symbol, as the iPhone draws it). A name the dashboard does not know
// is a newspaper; an emoji, which the agent may choose instead, is drawn as
// itself.
export function FeedPicture({ pictureSymbol }: { pictureSymbol: string }) {
  const isSymbolName = /^[a-z][a-z0-9.]*$/i.test(pictureSymbol)
  if (pictureSymbol && !isSymbolName) {
    return (
      <span className="feed-picture-emoji" aria-hidden="true">
        {pictureSymbol}
      </span>
    )
  }
  const base = pictureSymbol.split('.')[0]
  switch (pictureSymbol) {
    case 'cloud.sun':
      return <WeatherIcon kind="partlyCloudy" size={22} />
    case 'chart.line.uptrend.xyaxis':
      return <TrendingUpIcon size={22} />
    case 'lock.shield':
      return <ShieldIcon size={22} />
    case 'terminal':
      return <TerminalIcon size={22} />
    case 'lightbulb':
      return <LightbulbIcon size={22} />
  }
  switch (base) {
    case 'cloud':
    case 'sun':
    case 'rain':
      return <WeatherIcon kind="partlyCloudy" size={22} />
    case 'chart':
    case 'dollarsign':
      return <TrendingUpIcon size={22} />
    case 'lock':
    case 'key':
      return <LockIcon size={22} />
    case 'shield':
      return <ShieldIcon size={22} />
    case 'envelope':
    case 'mail':
      return <MailIcon size={22} />
    case 'calendar':
      return <CalendarIcon size={22} />
    case 'creditcard':
    case 'wallet':
    case 'banknote':
      return <WalletIcon size={22} />
    case 'location':
    case 'mappin':
    case 'map':
      return <LocationIcon size={22} />
    case 'heart':
      return <HeartIcon size={22} />
    case 'exclamationmark':
      return <WarningIcon size={22} />
    case 'bell':
      return <BellIcon size={22} />
    case 'person':
      return <UserIcon size={22} />
    case 'cart':
      return <CartIcon size={22} />
    case 'photo':
      return <PictureIcon size={22} />
    case 'house':
      return <HomeIcon size={22} />
    case 'airplane':
      return <PlaneIcon size={22} />
    case 'car':
      return <CarIcon size={22} />
    case 'chevron':
      return <CodeIcon size={22} />
    case 'bubble':
      return <ChatIcon size={22} />
    default:
      return <NewspaperIcon size={22} />
  }
}

// codingSessionPath is the Coding page on the session a card came from. The
// card's source is the computer and the pane joined by a slash.
function codingSessionPath(feedSourceId: string): string {
  const slash = feedSourceId.indexOf('/')
  if (slash < 0) return '/coding'
  return codingPagePath({ computer: feedSourceId.slice(0, slash), paneId: feedSourceId.slice(slash + 1) })
}

// A dismissed card that sits beside the others of its stack in the list.
interface DismissedRow {
  // The card drawn on top: the first entry of the group, with the group's size
  // and unread count, as the server gives a stack's top card in the feed.
  item: FeedItem
  // Every entry of the group, the top one first. One entry for a card alone.
  entries: FeedItem[]
}

// groupDismissedItems joins consecutive entries with the same non-empty
// feedStackKey into one row, which is drawn as a deck. The server lists every
// dismissed card on its own, so the grouping is the page's.
export function groupDismissedItems(feedItems: FeedItem[]): DismissedRow[] {
  const rows: DismissedRow[] = []
  for (const item of feedItems) {
    const last = rows[rows.length - 1]
    if (last && item.feedStackKey !== '' && last.entries[0].feedStackKey === item.feedStackKey) {
      last.entries.push(item)
    } else {
      rows.push({ item, entries: [item] })
    }
  }
  return rows.map((row) => ({
    item: {
      ...row.entries[0],
      stackItemCount: row.entries.length,
      stackUnreadCount: row.entries.filter((entry) => !entry.isRead).length,
    },
    entries: row.entries,
  }))
}

// The Dismissed view: the same page, reading the dismissed cards.
export function DismissedFeedPage() {
  return <FeedPage isDismissedView />
}

export function FeedPage({ isDismissedView = false }: { isDismissedView?: boolean }) {
  const { t, plural } = useTranslation()
  const toast = useToast()
  const navigate = useNavigate()
  const [feedItems, setFeedItems] = useState<FeedItem[] | null>(null)
  const [nextCursor, setNextCursor] = useState<string | null>(null)
  const [error, setError] = useState<unknown>(null)
  const [isLoadingMore, setIsLoadingMore] = useState(false)
  // The cards that were unread when they arrived on this page. They keep their
  // mark until the page is left, so what is new can still be told apart after
  // it has been seen and counted read.
  const [freshItemIds, setFreshItemIds] = useState<Set<string>>(() => new Set())
  // The stacks opened in place, by their key, and which cards have been
  // marked read since, so an open stack shows them read without being read again.
  const [openStackKeys, setOpenStackKeys] = useState<Set<string>>(() => new Set())
  const [readItemIds, setReadItemIds] = useState<Set<string>>(() => new Set())
  // Counted at every read of the list, so an open stack reads itself again too.
  const [loadCount, setLoadCount] = useState(0)
  // The Dismissed view reads the dismissed cards; the feed asks for neither.
  const dismissedVariables = useMemo(() => (isDismissedView ? { isDismissedOnly: true } : {}), [isDismissedView])
  const feedItemCountRef = useRef(0)
  // The list as last drawn, for the code that decides whether a removal left
  // it empty, and the ids this page took off the list with the number of the
  // read that was latest then. A read that was already on the way when a card
  // was taken off may still carry it, so every read drops these ids; an id is
  // forgotten once a read that began after it was taken off comes back without it.
  const feedItemsRef = useRef<FeedItem[] | null>(null)
  feedItemsRef.current = feedItems
  const hiddenItemNumbersRef = useRef<Map<string, number>>(new Map())
  const unreadCountRef = useRef(0)
  const requestNumberRef = useRef(0)
  const pendingReadRef = useRef<Set<string>>(new Set())
  const readTimerRef = useRef<number | undefined>(undefined)
  const toastRef = useRef(toast)
  toastRef.current = toast

  const applyUnreadCount = useCallback((unreadCount: number) => {
    unreadCountRef.current = unreadCount
    announceFeedUnread(unreadCount)
  }, [])

  const rememberFresh = useCallback((items: FeedItem[]) => {
    const unread = items.filter((item) => !item.isRead).map((item) => item.feedItemId)
    if (unread.length === 0) return
    setFreshItemIds((previous) =>
      unread.every((id) => previous.has(id)) ? previous : new Set([...previous, ...unread]),
    )
  }, [])

  // load reads the list from the top again, as far down as it had been read
  // (up to a limit), so a card that changed or went away is right wherever it
  // is. isQuiet is for a refresh the person did not ask for: a failure of that
  // keeps what is shown.
  const load = useCallback(
    async (isQuiet: boolean) => {
      const requestNumber = ++requestNumberRef.current
      try {
        const first = Math.min(Math.max(PAGE_SIZE, feedItemCountRef.current), MAXIMUM_REFRESH_SIZE)
        const answer = await graphql<{ ListAgentFeed: FeedPageAnswer }>(LIST_FEED, { first, ...dismissedVariables })
        // A newer read has been asked for, so this one is out of date.
        if (requestNumber !== requestNumberRef.current) return
        const page = answer.ListAgentFeed
        const hiddenNumbers = hiddenItemNumbersRef.current
        const returnedIds = new Set(page.feedItems.map((item) => item.feedItemId))
        hiddenNumbers.forEach((hiddenNumber, feedItemId) => {
          if (requestNumber > hiddenNumber && !returnedIds.has(feedItemId)) hiddenNumbers.delete(feedItemId)
        })
        const visibleItems = page.feedItems.filter((item) => !hiddenNumbers.has(item.feedItemId))
        feedItemCountRef.current = visibleItems.length
        feedItemsRef.current = visibleItems
        setFeedItems(visibleItems)
        setNextCursor(page.nextCursor)
        rememberFresh(visibleItems)
        applyUnreadCount(page.unreadCount)
        setError(null)
        setLoadCount((count) => count + 1)
      } catch (caught) {
        if (requestNumber !== requestNumberRef.current) return
        if (!isQuiet) setError(caught)
      }
    },
    [applyUnreadCount, rememberFresh, dismissedVariables],
  )

  useEffect(() => {
    void load(false)
  }, [load])

  // The feed's own event: a card was posted, replaced, settled or dismissed.
  // The list is read again rather than patched, because a replaced card moves
  // to the top and the unread count changes with it.
  const loadRef = useRef(load)
  loadRef.current = load
  // A batch of changes (a stack or every read card dismissed, a restore)
  // arrives as one event a card; they are read once, a moment after the last.
  const reloadTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const scheduleReload = useCallback(() => {
    if (reloadTimerRef.current) clearTimeout(reloadTimerRef.current)
    reloadTimerRef.current = setTimeout(() => {
      reloadTimerRef.current = null
      void loadRef.current(true)
    }, 250)
  }, [])
  useEffect(
    () => () => {
      if (reloadTimerRef.current) clearTimeout(reloadTimerRef.current)
    },
    [],
  )
  useEffect(
    () =>
      subscribe<{ AgentFeedChanged: { feedItemId: string; feedChange: string; unreadCount: number } }>(
        AGENT_FEED_CHANGED,
        {},
        () => scheduleReload(),
        undefined,
        {
          // What changed while the socket was down is read before the event
          // stream resumes.
          beforeStart: async (isReconnecting) => {
            if (isReconnecting) await loadRef.current(true)
          },
        },
      ),
    [scheduleReload],
  )

  // A turn that follows a choice may still be running when the card is first
  // drawn; its end arrives as an event, and this is the fallback.
  const hasPendingOutcome = feedItems?.some((item) => item.isOutcomePending) ?? false
  useEffect(() => {
    if (!hasPendingOutcome) return
    const timer = window.setInterval(() => void loadRef.current(true), PENDING_RECHECK_MS)
    return () => window.clearInterval(timer)
  }, [hasPendingOutcome])

  const loadMore = async () => {
    if (!nextCursor || isLoadingMore) return
    setIsLoadingMore(true)
    try {
      const answer = await graphql<{ ListAgentFeed: FeedPageAnswer }>(LIST_FEED, {
        first: PAGE_SIZE,
        after: nextCursor,
        ...dismissedVariables,
      })
      const page = answer.ListAgentFeed
      setFeedItems((previous) => {
        const known = new Set((previous ?? []).map((item) => item.feedItemId))
        const merged = [
          ...(previous ?? []),
          ...page.feedItems.filter(
            (item) => !known.has(item.feedItemId) && !hiddenItemNumbersRef.current.has(item.feedItemId),
          ),
        ]
        feedItemCountRef.current = merged.length
        return merged
      })
      setNextCursor(page.nextCursor)
      rememberFresh(page.feedItems)
      applyUnreadCount(page.unreadCount)
    } catch (caught) {
      toast.failure(caught, t('feed.loadFailed'))
    } finally {
      setIsLoadingMore(false)
    }
  }

  const flushRead = useCallback(async () => {
    window.clearTimeout(readTimerRef.current)
    readTimerRef.current = undefined
    const feedItemIds = [...pendingReadRef.current]
    pendingReadRef.current.clear()
    if (feedItemIds.length === 0) return
    try {
      const answer = await graphql<{ MarkAgentFeedRead: number }>(MARK_READ, { feedItemIds })
      const readIds = new Set(feedItemIds)
      setReadItemIds((previous) => new Set([...previous, ...feedItemIds]))
      setFeedItems((previous) =>
        previous ? previous.map((item) => (readIds.has(item.feedItemId) ? { ...item, isRead: true } : item)) : previous,
      )
      applyUnreadCount(Math.max(0, unreadCountRef.current - answer.MarkAgentFeedRead))
    } catch (caught) {
      toastRef.current.failure(caught, t('feed.actionFailed'))
    }
    // The translation function changes with the language only.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [applyUnreadCount])

  // A card seen is marked read in a batch a moment later; one request for
  // the cards that were scrolled past together.
  const markSeen = useCallback(
    (feedItemId: string) => {
      pendingReadRef.current.add(feedItemId)
      if (readTimerRef.current === undefined) {
        readTimerRef.current = window.setTimeout(() => void flushRead(), READ_BATCH_DELAY_MS)
      }
    },
    [flushRead],
  )
  useEffect(() => () => void flushRead(), [flushRead])

  // closeStack puts a stack's cards back into its deck.
  const closeStack = useCallback((feedStackKey: string) => {
    setOpenStackKeys((keys) => {
      const next = new Set(keys)
      next.delete(feedStackKey)
      return next
    })
  }, [])

  // toggleStack opens a stack in place, or closes it.
  const toggleStack = (feedStackKey: string) => {
    if (openStackKeys.has(feedStackKey)) {
      closeStack(feedStackKey)
      return
    }
    setOpenStackKeys((keys) => new Set([...keys, feedStackKey]))
  }

  // A press anywhere outside an open stack gathers it back into its deck. A
  // press in a dialog, a menu or a toast, which are drawn outside the stack but may
  // have been opened from one of its cards, leaves it open.
  useEffect(() => {
    if (openStackKeys.size === 0) return
    const onPointerDown = (event: PointerEvent) => {
      const target = event.target as Element | null
      if (
        !target ||
        !target.isConnected ||
        target.closest('[aria-modal="true"], [role="dialog"], [role="menu"], [role="listbox"], .toasts, .lightbox')
      )
        return
      const pressedKey = target.closest<HTMLElement>('[data-feed-stack-key]')?.dataset.feedStackKey
      openStackKeys.forEach((feedStackKey) => {
        if (feedStackKey !== pressedKey) closeStack(feedStackKey)
      })
    }
    document.addEventListener('pointerdown', onPointerDown)
    return () => document.removeEventListener('pointerdown', onPointerDown)
  }, [openStackKeys, closeStack])

  const replaceItem = useCallback((updated: FeedItem) => {
    setFeedItems((previous) =>
      previous ? previous.map((item) => (item.feedItemId === updated.feedItemId ? updated : item)) : previous,
    )
  }, [])
  // hideItems takes cards off the list at once and keeps them off it, even
  // from a read that was already on the way.
  const hideItems = useCallback((feedItemIds: string[]) => {
    for (const feedItemId of feedItemIds) hiddenItemNumbersRef.current.set(feedItemId, requestNumberRef.current)
    const gone = new Set(feedItemIds)
    const remaining = feedItemsRef.current ? feedItemsRef.current.filter((item) => !gone.has(item.feedItemId)) : null
    feedItemsRef.current = remaining
    feedItemCountRef.current = remaining?.length ?? 0
    setFeedItems(remaining)
  }, [])
  // readAfterRemoval reads the list again after cards were taken off. When
  // none are left the loading mark is shown meanwhile, so that empty is said
  // only when the read says so.
  const readAfterRemoval = useCallback(() => {
    if (feedItemsRef.current !== null && feedItemsRef.current.length === 0) {
      feedItemsRef.current = null
      setFeedItems(null)
      void loadRef.current(false)
      return
    }
    void loadRef.current(true)
  }, [])
  const removeItem = useCallback(
    (feedItemId: string) => {
      hideItems([feedItemId])
      readAfterRemoval()
    },
    [hideItems, readAfterRemoval],
  )

  // restoreItem puts a dismissed card back. It leaves the list at once; a
  // failure brings it back.
  const restoreItem = useCallback(
    async (feedItemId: string) => {
      hideItems([feedItemId])
      let restoredIds: string[]
      try {
        const answer = await graphql<{ RestoreAgentFeedItems: string[] }>(RESTORE_ITEMS, { feedItemIds: [feedItemId] })
        restoredIds = answer.RestoreAgentFeedItems
      } catch (caught) {
        hiddenItemNumbersRef.current.delete(feedItemId)
        toastRef.current.failure(caught, t('feed.actionFailed'))
        void loadRef.current(true)
        return false
      }
      // The server leaves out a card a newer card on its subject replaced,
      // or one that expired: it is shown again and the person told.
      if (!restoredIds.includes(feedItemId)) {
        hiddenItemNumbersRef.current.delete(feedItemId)
        toastRef.current.failed(t('feed.notRestored'))
        void loadRef.current(true)
        return false
      }
      toastRef.current.done(t('feed.restored'))
      readAfterRemoval()
      return true
    },
    // The translation function changes with the language only.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [hideItems, readAfterRemoval],
  )

  // dismissItems closes cards and offers Undo, which brings back exactly the
  // cards this call closed. The list is read again, so a stack whose top card
  // went shows its next card, and the events that follow say the same.
  const dismissItems = useCallback(
    async (selection: DismissSelection) => {
      try {
        const answer = await graphql<{ DismissAgentFeedItems: string[] }>(DISMISS_ITEMS, selection)
        const dismissedIds = answer.DismissAgentFeedItems
        if (dismissedIds.length === 0) {
          toastRef.current.done(t('feed.nothingToDismiss'))
          return true
        }
        hideItems(dismissedIds)
        readAfterRemoval()
        toastRef.current.done(plural(dismissedIds.length, { one: 'feed.dismissedOne', other: 'feed.dismissedOther' }), {
          label: t('common.undo'),
          run: async () => {
            try {
              await graphql(RESTORE_ITEMS, { feedItemIds: dismissedIds })
              for (const feedItemId of dismissedIds) hiddenItemNumbersRef.current.delete(feedItemId)
              await loadRef.current(true)
            } catch (caught) {
              toastRef.current.failure(caught, t('feed.actionFailed'))
            }
          },
        })
        return true
      } catch (caught) {
        toastRef.current.failure(caught, t('feed.actionFailed'))
        return false
      }
    },
    // The translation functions change with the language only.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [hideItems, readAfterRemoval],
  )

  if (feedItems === null) {
    return (
      <>
        <ErrorMessage error={error} />
        {!error && <Loading />}
      </>
    )
  }
  const rows: DismissedRow[] = isDismissedView
    ? groupDismissedItems(feedItems)
    : feedItems.map((item) => ({ item, entries: [item] }))
  return (
    <div className="feed-page">
      {isDismissedView ? (
        <div className="feed-header is-dismissed-view">
          <button type="button" onClick={() => navigate('/feed')}>
            {t('feed.backToFeed')}
          </button>
        </div>
      ) : (
        <div className="feed-header">
          <button type="button" onClick={() => navigate('/feed/dismissed')}>
            {t('feed.dismissedView')}
          </button>
          {feedItems.length > 0 && (
            <button type="button" onClick={() => void dismissItems({ isEveryReadCard: true })}>
              {t('feed.dismissRead')}
            </button>
          )}
        </div>
      )}
      {feedItems.length === 0 ? (
        isDismissedView ? (
          <>
            <SettingsEmpty>{t('feed.dismissedEmpty')}</SettingsEmpty>
            <SettingsEmpty>{t('feed.dismissedEmptyNote')}</SettingsEmpty>
          </>
        ) : (
          <SettingsEmpty>{t('feed.empty')}</SettingsEmpty>
        )
      ) : (
        <ul className="feed-list">
          {rows.map(({ item, entries }) => {
            const isStack = item.stackItemCount > 1 && item.feedStackKey !== ''
            const isOpen = isStack && openStackKeys.has(item.feedStackKey)
            return (
              <li
                key={item.feedItemId}
                data-feed-stack-key={isStack ? item.feedStackKey : undefined}
                className={[
                  'feed-stack',
                  item.stackItemCount > 1 ? 'is-deck' : '',
                  item.stackItemCount > 1 && !isOpen ? 'is-closed' : '',
                ]
                  .filter(Boolean)
                  .join(' ')}
                onClick={
                  isStack && !isOpen
                    ? (event) => {
                        // A tap on the deck itself opens it; a tap on something
                        // in the card does what that thing does.
                        if ((event.target as HTMLElement).closest('a, button, video, input, summary')) return
                        toggleStack(item.feedStackKey)
                      }
                    : undefined
                }
              >
                {item.stackItemCount > 1 && item.feedStackTitle && (
                  <p className="feed-stack-title muted">{item.feedStackTitle}</p>
                )}
                <FeedCard
                  item={item}
                  isFresh={freshItemIds.has(item.feedItemId) || !item.isRead}
                  onSeen={markSeen}
                  onChanged={replaceItem}
                  onRemoved={removeItem}
                  onDismissItems={dismissItems}
                  onRestore={isDismissedView ? restoreItem : undefined}
                  stack={isStack ? { isOpen, onToggle: () => toggleStack(item.feedStackKey) } : undefined}
                />
                {isStack && (
                  <FeedStack
                    feedStackKey={item.feedStackKey}
                    topFeedItemId={item.feedItemId}
                    stackItemCount={item.stackItemCount}
                    isOpen={isOpen}
                    loadCount={loadCount}
                    readItemIds={readItemIds}
                    onSeen={markSeen}
                    onDismissItems={dismissItems}
                    entries={isDismissedView ? entries : undefined}
                    onEntryChanged={replaceItem}
                    onEntryRemoved={removeItem}
                    onRestore={isDismissedView ? restoreItem : undefined}
                  />
                )}
              </li>
            )
          })}
        </ul>
      )}
      {nextCursor && (
        <div className="list-foot feed-foot">
          <button type="button" disabled={isLoadingMore} onClick={() => void loadMore()}>
            {t('feed.more')}
          </button>
        </div>
      )}
    </div>
  )
}

function FeedCard({
  item,
  isFresh,
  onSeen,
  onChanged,
  onRemoved,
  onDismissItems,
  onRestore,
  stack,
}: {
  item: FeedItem
  isFresh: boolean
  onSeen: (feedItemId: string) => void
  onChanged: (item: FeedItem) => void
  onRemoved: (feedItemId: string) => void
  onDismissItems: (selection: DismissSelection) => Promise<boolean>
  // Given in the Dismissed view: the card is put back by the restore button
  // in its footer, by the info sheet's Restore, and by a swipe on a touch screen.
  onRestore?: (feedItemId: string) => Promise<boolean>
  // Present on the top card of a stack: whether the rest is shown and how to toggle it.
  stack?: { isOpen: boolean; onToggle: () => void }
}) {
  const { t } = useTranslation()
  const toast = useToast()
  const navigate = useNavigate()
  const cardRef = useRef<HTMLElement>(null)
  const [isBusy, setIsBusy] = useState(false)
  const [isInfoOpen, setIsInfoOpen] = useState(false)
  const [isResolveOpen, setIsResolveOpen] = useState(false)
  // An answered card is folded to its title and one line; this opens it again.
  const [isUnfolded, setIsUnfolded] = useState(false)
  // Where a finger came down and how far across a swipe has taken the card.
  const touchRef = useRef<{ pointerId: number; startX: number; startY: number; isSwiping: boolean } | null>(null)
  const [swipeAcrossPx, setSwipeAcrossPx] = useState(0)

  // Seen once most of the card has been on screen.
  const isUnread = !item.isRead
  useEffect(() => {
    const element = cardRef.current
    if (!isUnread || !element || typeof IntersectionObserver === 'undefined') return
    const observer = new IntersectionObserver(
      (entries) => {
        if (entries.some((entry) => entry.isIntersecting)) {
          observer.disconnect()
          onSeen(item.feedItemId)
        }
      },
      { threshold: SEEN_FRACTION },
    )
    observer.observe(element)
    return () => observer.disconnect()
  }, [isUnread, item.feedItemId, onSeen])

  const run = async (work: () => Promise<void>) => {
    setIsBusy(true)
    try {
      await work()
    } catch (caught) {
      toast.failure(caught, t('feed.actionFailed'))
    } finally {
      setIsBusy(false)
    }
  }

  const discuss = () =>
    run(async () => {
      const answer = await graphql<{ DiscussAgentFeedItem: string }>(DISCUSS_ITEM, { feedItemId: item.feedItemId })
      // The main conversation opens with the card on a chip in its box, so
      // what the person says next is about the card and the agent reads it.
      if (!openAgentConversation(answer.DiscussAgentFeedItem)) {
        toast.failed(t('feed.agentUnavailable'))
        return
      }
      askAgentAbout({ feedItemId: item.feedItemId, feedItemTitle: item.title })
    })

  const open = () => {
    if (item.feedSourceKind === 'coding_session') {
      navigate(codingSessionPath(item.feedSourceId))
      return
    }
    if (item.runConversationId && !openAgentConversation(item.runConversationId)) {
      toast.failed(t('feed.agentUnavailable'))
    }
  }

  const choose = (choice: FeedChoice) => {
    if (choice.choiceRole === 'discuss') return void discuss()
    if (choice.choiceRole === 'open') return open()
    void run(async () => {
      const answer = await graphql<{ ChooseAgentFeedItem: FeedItem }>(CHOOSE_ITEM, {
        feedItemId: item.feedItemId,
        choiceKey: choice.choiceKey,
      })
      onChanged(answer.ChooseAgentFeedItem)
    })
  }

  // The heart answers at once and goes back if the server refuses.
  const toggleLike = async () => {
    const isLiked = !item.isLiked
    onChanged({ ...item, isLiked })
    try {
      const answer = await graphql<{ LikeAgentFeedItem: FeedItem }>(LIKE_ITEM, {
        feedItemId: item.feedItemId,
        isLiked,
      })
      onChanged(answer.LikeAgentFeedItem)
    } catch (caught) {
      onChanged(item)
      toast.failure(caught, t('feed.actionFailed'))
    }
  }

  const dismiss = () =>
    run(async () => {
      // The sheet closes once the card is gone; a failure leaves it open
      // beside the message saying so.
      if (await onDismissItems({ feedItemIds: [item.feedItemId] })) setIsInfoOpen(false)
    })

  const dismissStack = () =>
    run(async () => {
      await onDismissItems({ feedStackKey: item.feedStackKey })
    })

  const restore = () =>
    run(async () => {
      if (onRestore && (await onRestore(item.feedItemId))) setIsInfoOpen(false)
    })

  // A horizontal swipe of a finger past SWIPE_ACTION_PX restores the card; a
  // move more down than across is a scroll, left to the browser. A mouse has
  // the button.
  const swipeHandlers = onRestore
    ? {
        onPointerDown: (event: React.PointerEvent) => {
          if (event.pointerType !== 'touch') return
          touchRef.current = {
            pointerId: event.pointerId,
            startX: event.clientX,
            startY: event.clientY,
            isSwiping: false,
          }
        },
        onPointerMove: (event: React.PointerEvent) => {
          const touch = touchRef.current
          if (!touch || touch.pointerId !== event.pointerId) return
          const acrossPx = event.clientX - touch.startX
          const downPx = event.clientY - touch.startY
          if (!touch.isSwiping) {
            if (Math.abs(downPx) >= SWIPE_STARTS_PX && Math.abs(downPx) > Math.abs(acrossPx)) {
              touchRef.current = null
              return
            }
            if (Math.abs(acrossPx) < SWIPE_STARTS_PX) return
            touch.isSwiping = true
            event.currentTarget.setPointerCapture?.(event.pointerId)
          }
          setSwipeAcrossPx(swipeOffset(acrossPx))
        },
        onPointerUp: (event: React.PointerEvent) => {
          const touch = touchRef.current
          if (!touch || touch.pointerId !== event.pointerId) return
          touchRef.current = null
          const acrossPx = event.clientX - touch.startX
          setSwipeAcrossPx(0)
          if (touch.isSwiping && Math.abs(acrossPx) >= SWIPE_ACTION_PX) void restore()
        },
        onPointerCancel: () => {
          touchRef.current = null
          setSwipeAcrossPx(0)
        },
      }
    : {}

  const resolve = (resolutionNote: string) =>
    run(async () => {
      const answer = await graphql<{ ResolveAgentFeedItem: FeedItem }>(RESOLVE_ITEM, {
        feedItemId: item.feedItemId,
        resolutionNote: resolutionNote.trim(),
      })
      onChanged(answer.ResolveAgentFeedItem)
      setIsResolveOpen(false)
      toast.done(t('feed.resolvedDone'))
    })

  const unresolve = () =>
    run(async () => {
      const answer = await graphql<{ UnresolveAgentFeedItem: FeedItem }>(UNRESOLVE_ITEM, {
        feedItemId: item.feedItemId,
      })
      onChanged(answer.UnresolveAgentFeedItem)
      setIsInfoOpen(false)
      toast.done(t('feed.unresolvedDone'))
    })

  const showLess = () =>
    run(async () => {
      await graphql(SHOW_LESS_ITEM, { feedItemId: item.feedItemId })
      setIsInfoOpen(false)
      onRemoved(item.feedItemId)
      toast.done(t('feed.shownLess'))
    })

  const chosen = item.choices.find((choice) => choice.choiceKey === item.chosenChoiceKey)
  const layout = parseFeedLayout(item.feedCardLayout, item.layoutContent)
  const { cardMedia, rowMediaByIndex } = splitFeedMedia(item.feedMedia)
  const hasCardMedia = cardMedia.length > 0
  const headerLabel = feedHeaderLabelOf(cardMedia, item.feedSourceTitle)
  const weatherGradient =
    layout?.kind === 'weather' ? weatherGradientOf(layout.weather.conditionSymbol, layout.weather.conditionText) : null
  const cardClassName = [
    'card',
    'feed-card',
    isFresh ? 'is-fresh' : '',
    onRestore ? 'is-swipeable' : '',
    weatherGradient ? `is-weather is-${weatherGradient}` : '',
    item.isResolved ? 'is-resolved' : '',
  ]
    .filter(Boolean)
    .join(' ')
  const isChosen = item.chosenChoiceKey !== null && item.chosenChoiceKey !== ''
  const isFolded = isChosen && !isUnfolded
  const chartBlocks = useMemo(() => chartBlocksOf(item.chartBlocks), [item.chartBlocks])
  const resolvedClock =
    item.isResolved && item.resolvedAt ? feedClockTextOf(item.resolvedAt, { isHourPadded: true }) : ''

  return (
    <article
      ref={cardRef}
      className={cardClassName}
      style={swipeAcrossPx !== 0 ? { transform: `translateX(${swipeAcrossPx}px)` } : undefined}
      {...swipeHandlers}
    >
      {isFresh && <span className="feed-fresh-dot" role="img" aria-label={t('feed.unread')} />}
      {hasCardMedia && !isFolded && <FeedMediaGallery cardMedia={cardMedia} />}
      {/* With pictures on top the symbol moves into the header line. */}
      {(!hasCardMedia || isFolded) && (
        <div className="feed-picture">
          <FeedPicture pictureSymbol={item.pictureSymbol} />
        </div>
      )}
      <div className="feed-content">
        {(headerLabel || (hasCardMedia && !isFolded)) && (
          <p className="feed-kicker muted">
            {hasCardMedia && !isFolded && (
              <span className="feed-kicker-symbol" aria-hidden="true">
                <FeedPicture pictureSymbol={item.pictureSymbol} />
              </span>
            )}
            {headerLabel}
          </p>
        )}
        <div className="feed-head">
          <h3 className="feed-title">{item.title}</h3>
          {!onRestore && (
            <Tooltip label={t('feed.dismiss')}>
              <button
                type="button"
                className="icon-action feed-dismiss"
                aria-label={`${item.title}: ${t('feed.dismiss')}`}
                disabled={isBusy}
                onClick={() => void dismiss()}
              >
                <CloseIcon size={16} />
              </button>
            </Tooltip>
          )}
        </div>
        {isFolded ? (
          <div className="feed-folded">
            <p className="muted feed-folded-line">
              <span className="feed-folded-part">
                {t('feed.chose', { choice: chosen?.choiceLabel ?? item.chosenChoiceKey ?? '' })}
              </span>
              {item.isOutcomePending ? (
                <span className="feed-folded-part">
                  <span className="agent-spinner" aria-hidden="true" /> {t('feed.working')}
                </span>
              ) : (
                item.outcomeText && <span className="feed-folded-part">{item.outcomeText}</span>
              )}
            </p>
            <button type="button" className="feed-unfold" onClick={() => setIsUnfolded(true)}>
              {t('feed.showCard')}
            </button>
          </div>
        ) : (
          <>
            {layout ? (
              <FeedLayoutContent layout={layout} rowMediaByIndex={rowMediaByIndex} />
            ) : (
              item.body && (
                <div className="feed-body">
                  <Markdown text={item.body} />
                </div>
              )
            )}
            {item.isResolved && item.resolutionNote && (
              <p className="muted feed-resolution-note">{item.resolutionNote}</p>
            )}
            <FeedChartBlocks blocks={chartBlocks} />
            {item.factRows.length > 0 && (
              <table className="feed-facts">
                <tbody>
                  {item.factRows.map((row, index) => (
                    <tr key={`${row.factLabel}-${index}`}>
                      <th scope="row">{row.factLabel}</th>
                      <td>{row.factValue}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
            {isChosen ? (
              <div className="feed-outcome" aria-live="polite">
                <p className="feed-chosen">
                  {t('feed.chose', { choice: chosen?.choiceLabel ?? item.chosenChoiceKey ?? '' })}
                </p>
                {item.isOutcomePending ? (
                  <p className="muted feed-working">
                    <span className="agent-spinner" aria-hidden="true" /> {t('feed.working')}
                  </p>
                ) : (
                  item.outcomeText && <p className="muted">{item.outcomeText}</p>
                )}
                <button type="button" className="feed-unfold" onClick={() => setIsUnfolded(false)}>
                  {t('feed.foldCard')}
                </button>
              </div>
            ) : (
              item.choices.length > 0 && (
                <div className="feed-choices">
                  {item.choices.map((choice) => (
                    <button
                      key={choice.choiceKey}
                      type="button"
                      className={choice.choiceRole === 'action' ? 'primary' : undefined}
                      disabled={isBusy}
                      onClick={() => choose(choice)}
                    >
                      {choice.choiceLabel}
                    </button>
                  ))}
                </div>
              )
            )}
          </>
        )}
        <div className="feed-footer">
          <Tooltip label={item.isLiked ? t('feed.unlike') : t('feed.like')}>
            <button
              type="button"
              className={item.isLiked ? 'icon-action feed-like is-liked' : 'icon-action feed-like'}
              aria-label={item.isLiked ? t('feed.unlike') : t('feed.like')}
              aria-pressed={item.isLiked}
              onClick={() => void toggleLike()}
            >
              <HeartIcon size={16} />
            </button>
          </Tooltip>
          <button type="button" className="feed-discuss" disabled={isBusy} onClick={() => void discuss()}>
            <ChatIcon size={14} /> {t('feed.discuss')}
          </button>
          <span className="feed-footer-end">
            {item.isResolved && (
              <span className="feed-resolved-chip">
                {resolvedClock ? t('feed.resolvedAt', { time: resolvedClock }) : t('feed.resolved')}
              </span>
            )}
            <span className="muted feed-date">
              <RelativeTime value={item.updatedAt} />
            </span>
            {onRestore && (
              <Tooltip label={t('feed.restore')}>
                <button
                  type="button"
                  className="icon-action feed-restore"
                  aria-label={`${item.title}: ${t('feed.restore')}`}
                  disabled={isBusy}
                  onClick={() => void restore()}
                >
                  <RestoreIcon size={16} />
                </button>
              </Tooltip>
            )}
            <Tooltip label={t('feed.aboutCard')}>
              <button
                type="button"
                className="icon-action"
                aria-label={`${item.title}: ${t('feed.aboutCard')}`}
                onClick={() => setIsInfoOpen(true)}
              >
                <InfoIcon size={16} />
              </button>
            </Tooltip>
          </span>
        </div>
      </div>
      {stack && item.stackItemCount > 1 && (
        <div className="feed-stack-actions">
          <button type="button" className="feed-stack-row" aria-expanded={stack.isOpen} onClick={stack.onToggle}>
            <span>
              {stack.isOpen
                ? t('feed.stackCollapse')
                : item.feedStackTitle || item.feedSourceTitle
                  ? t('feed.stackMore', {
                      count: item.stackItemCount - 1,
                      source: item.feedStackTitle || item.feedSourceTitle,
                    })
                  : t('feed.stackMoreUnnamed', { count: item.stackItemCount - 1 })}
            </span>
            {!stack.isOpen && item.stackUnreadCount > 0 && (
              <span className="muted feed-stack-unread">{t('feed.unreadCount', { count: item.stackUnreadCount })}</span>
            )}
          </button>
          {!onRestore && (
            <button type="button" className="feed-dismiss-all" disabled={isBusy} onClick={() => void dismissStack()}>
              {t('feed.dismissAll', { count: item.stackItemCount })}
            </button>
          )}
        </div>
      )}
      {isInfoOpen && (
        <FeedInfoDialog
          item={item}
          isBusy={isBusy}
          onResolve={() => {
            setIsInfoOpen(false)
            setIsResolveOpen(true)
          }}
          onUnresolve={() => void unresolve()}
          onShowLess={() => void showLess()}
          onDismiss={() => void dismiss()}
          onRestore={onRestore ? () => void restore() : undefined}
          onClose={() => setIsInfoOpen(false)}
        />
      )}
      {isResolveOpen && (
        <FeedResolveDialog
          isBusy={isBusy}
          onResolve={(note) => void resolve(note)}
          onClose={() => setIsResolveOpen(false)}
        />
      )}
    </article>
  )
}

// FeedResolveDialog asks for the optional note a resolution carries: one line,
// the length the server keeps.
const RESOLUTION_NOTE_LIMIT = 160

function FeedResolveDialog({
  isBusy,
  onResolve,
  onClose,
}: {
  isBusy: boolean
  onResolve: (resolutionNote: string) => void
  onClose: () => void
}) {
  const { t } = useTranslation()
  const [resolutionNote, setResolutionNote] = useState('')
  return (
    <FormDialog
      title={t('feed.resolveTitle')}
      submitLabel={t('feed.resolve')}
      busy={isBusy}
      onSubmit={() => onResolve(resolutionNote)}
      onClose={onClose}
    >
      <p className="muted">{t('feed.resolveHint')}</p>
      <label>
        <span>{t('feed.resolutionNoteLabel')}</span>
        <input
          type="text"
          value={resolutionNote}
          maxLength={RESOLUTION_NOTE_LIMIT}
          placeholder={t('feed.resolutionNotePlaceholder')}
          onChange={(event) => setResolutionNote(event.target.value)}
        />
      </label>
    </FormDialog>
  )
}

// FeedInfoDialog says why a card is here: what posted it (linking to the goal or
// coding session when there is one), when, what it was based on,
// and the two ways to be rid of it.
function FeedInfoDialog({
  item,
  isBusy,
  onResolve,
  onUnresolve,
  onShowLess,
  onDismiss,
  onRestore,
  onClose,
}: {
  item: FeedItem
  isBusy: boolean
  // Resolve opens a small dialog for the note; Unresolve takes the mark off.
  onResolve: () => void
  onUnresolve: () => void
  onShowLess: () => void
  onDismiss: () => void
  // Given for a dismissed card: Restore is the one way out, in place of
  // Show less and Dismiss.
  onRestore?: () => void
  onClose: () => void
}) {
  const { t } = useTranslation()
  const sourceName = item.feedSourceTitle || t('feed.sourceUnnamed')
  const sourcePath =
    item.feedSourceKind === 'goal' && item.feedSourceId
      ? `/settings/agent/goals?goal=${encodeURIComponent(item.feedSourceId)}`
      : item.feedSourceKind === 'coding_session' && item.feedSourceId
        ? codingSessionPath(item.feedSourceId)
        : ''
  const sourceNode = sourcePath ? (
    <Link to={sourcePath} onClick={onClose}>
      {sourceName}
    </Link>
  ) : (
    sourceName
  )
  const knownKinds = ['goal', 'alert', 'speak_first', 'coding_session', 'agent']
  const sourceKey = `feed.source.${knownKinds.includes(item.feedSourceKind) ? item.feedSourceKind : 'agent'}` as Key
  const sourceLinks = item.sourceLinks
    .map((link) => ({ ...link, address: webAddressOf(link.sourceUrl) }))
    .filter((link) => link.address !== '')
  return (
    <FormDialog
      title={t('feed.infoTitle')}
      submitLabel={onRestore ? t('feed.restore') : t('feed.showLess')}
      closeLabel={t('feed.close')}
      busy={isBusy}
      onSubmit={onRestore ?? onShowLess}
      onClose={onClose}
      otherAction={
        onRestore ? undefined : (
          <button type="button" disabled={isBusy} onClick={onDismiss}>
            {t('feed.dismiss')}
          </button>
        )
      }
    >
      <p>
        <Trans k={sourceKey} nodes={{ source: sourceNode }} />
      </p>
      <table className="feed-facts">
        <tbody>
          <tr>
            <th scope="row">{t('feed.postedAt')}</th>
            <td>
              <RelativeTime value={item.postedAt} />
            </td>
          </tr>
          <tr>
            <th scope="row">{t('feed.updatedAt')}</th>
            <td>
              <RelativeTime value={item.updatedAt} />
            </td>
          </tr>
        </tbody>
      </table>
      {sourceLinks.length > 0 && (
        <>
          <h4>{t('feed.sources')}</h4>
          <ul className="feed-source-links">
            {sourceLinks.map((link, index) => (
              <li key={`${link.address}-${index}`}>
                <a href={link.address} target="_blank" rel="noopener noreferrer">
                  {link.sourceTitle || link.address}
                </a>
              </li>
            ))}
          </ul>
        </>
      )}
      {item.recordReferences.length > 0 && (
        <>
          <h4>{t('feed.records')}</h4>
          <ReferenceChips references={item.recordReferences} />
        </>
      )}
      <p>
        {item.isResolved ? (
          <button type="button" disabled={isBusy} onClick={onUnresolve}>
            {t('feed.unresolve')}
          </button>
        ) : (
          <button type="button" disabled={isBusy} onClick={onResolve}>
            {t('feed.resolve')}
          </button>
        )}
      </p>
      {item.runConversationId && (
        <p>
          <button
            type="button"
            onClick={() => {
              if (openAgentConversation(item.runConversationId)) onClose()
            }}
          >
            {t('feed.showWork')}
          </button>
        </p>
      )}
    </FormDialog>
  )
}

// FeedStack is the rest of an opened stack, under its top card: full cards,
// newest first, read from ListAgentFeedStack with its own More. It reads
// again whenever the page's list has been read again, which every feed event
// causes, so a card posted to the stack shows up while it is open.
function FeedStack({
  feedStackKey,
  topFeedItemId,
  stackItemCount,
  isOpen,
  loadCount,
  readItemIds,
  onSeen,
  onDismissItems,
  entries,
  onEntryChanged,
  onEntryRemoved,
  onRestore,
}: {
  feedStackKey: string
  topFeedItemId: string
  // How many cards the stack has, the top one too, which says how many feet
  // the deck shows before its cards are read.
  stackItemCount: number
  // False while the cards are tucked under the top card as its deck.
  isOpen: boolean
  loadCount: number
  readItemIds: Set<string>
  onSeen: (feedItemId: string) => void
  onDismissItems: (selection: DismissSelection) => Promise<boolean>
  // Given in the Dismissed view: the stack's cards are these entries, already
  // read, because the server's stack read lists only cards that are not
  // dismissed. Changes to them go to the page's list.
  entries?: FeedItem[]
  onEntryChanged?: (item: FeedItem) => void
  onEntryRemoved?: (feedItemId: string) => void
  onRestore?: (feedItemId: string) => Promise<boolean>
}) {
  const { t } = useTranslation()
  const toast = useToast()
  const [readItems, setReadItems] = useState<FeedItem[] | null>(null)
  const feedItems = entries ?? readItems
  const [nextCursor, setNextCursor] = useState<string | null>(null)
  const [isLoadingMore, setIsLoadingMore] = useState(false)
  const restRef = useRef<HTMLDivElement>(null)
  const isDealtRef = useRef(false)
  const countRef = useRef(0)
  const toastRef = useRef(toast)
  toastRef.current = toast

  // The deck under a closed stack's top card is the stack's own cards, tucked
  // under it with only the feet of the first two showing, so opening the stack
  // moves those very cards out and closing moves them back. The room under the
  // top card grows from the feet's height to the cards' height while every card
  // slides from its tucked place into its own on the same curve, so they fan
  // out evenly. The cards stay opaque and each is drawn over the one under it,
  // as in a real deck. A stack turned back midway starts from where its cards
  // are. Each card's place is read from the layout, which a transform does not
  // change, so no height has to be known in advance.
  useLayoutEffect(() => {
    const room = restRef.current
    const topCard = room?.previousElementSibling
    if (!room || !(topCard instanceof HTMLElement)) return
    room.inert = !isOpen
    const cards = Array.from(room.querySelectorAll<HTMLElement>(':scope > ul > li'))
    const peekCount = Math.min(DECK_PEEK_MOST, cards.length > 0 ? cards.length : stackItemCount - 1)
    const isMoving = isOpen !== isDealtRef.current
    isDealtRef.current = isOpen
    const isAnimated = isMoving && !isMotionReduced() && typeof room.animate === 'function'
    // Where everything is drawn now, before the animations are stopped.
    const fromHeight = room.getBoundingClientRect().height
    const fromTransforms = cards.map((card) => getComputedStyle(card).transform)
    for (const element of [room, ...cards]) element.getAnimations?.().forEach((animation) => animation.cancel())

    // The deck: each card's foot just under the top card, centered under it.
    room.style.height = ''
    const gap = room.offsetTop - (topCard.offsetTop + topCard.offsetHeight)
    const topCenter = topCard.offsetLeft + topCard.offsetWidth / 2
    const tucked = (card: HTMLElement, index: number) => {
      const level = Math.min(index, peekCount - 1)
      const scale = (topCard.offsetWidth - 2 * DECK_INSET_PX[level]) / card.offsetWidth
      const left = topCenter - (room.offsetLeft + card.offsetLeft + card.offsetWidth / 2)
      const down = -gap + DECK_PEEK_PX * (level + 1) - (card.offsetTop + card.offsetHeight)
      return `translate(${left}px, ${down}px) scale(${scale})`
    }
    // The cards are cut off under the top card's rounded top edge, so a tucked
    // card taller than the top card stays behind it rather than showing above
    // it, through its border or beside its corners, and at the room's bottom
    // edge, so a card shows only as it slides out. The band the cut skips is
    // covered by the top card anyway.
    const cornerRadius = parseFloat(getComputedStyle(topCard).borderTopLeftRadius) || 0
    room.style.clipPath = `inset(${-(gap + topCard.offsetHeight) + cornerRadius}px -24px 0 -24px)`
    const toHeight = isOpen ? room.offsetHeight : DECK_PEEK_PX * peekCount
    const toTransforms = cards.map((card, index) => (isOpen ? 'none' : tucked(card, index)))
    room.style.height = isOpen ? '' : `${toHeight}px`
    cards.forEach((card, index) => {
      card.style.transform = isOpen ? '' : toTransforms[index]
    })
    if (!isAnimated) return
    const timing: KeyframeAnimationOptions = isOpen
      ? { duration: STACK_OPEN_MS, easing: STACK_OPEN_EASING }
      : { duration: STACK_CLOSE_MS, easing: STACK_CLOSE_EASING }
    room.animate([{ height: `${fromHeight}px` }, { height: `${toHeight}px` }], timing)
    cards.forEach((card, index) =>
      card.animate([{ transform: fromTransforms[index] }, { transform: toTransforms[index] }], timing),
    )
  }, [isOpen, feedItems, stackItemCount])

  useEffect(() => {
    if (entries) return
    let isStale = false
    const first = Math.min(Math.max(PAGE_SIZE, countRef.current), MAXIMUM_REFRESH_SIZE)
    graphql<{ ListAgentFeedStack: FeedPageAnswer }>(LIST_STACK, { feedStackKey, first })
      .then((answer) => {
        if (isStale) return
        countRef.current = answer.ListAgentFeedStack.feedItems.length
        setReadItems(answer.ListAgentFeedStack.feedItems)
        setNextCursor(answer.ListAgentFeedStack.nextCursor)
      })
      .catch((caught) => {
        if (!isStale && countRef.current === 0) toastRef.current.failure(caught, t('feed.loadFailed'))
      })
    return () => {
      isStale = true
    }
    // The translation function changes with the language only, and entries
    // only says which of the two sources the stack has, which does not change
    // while it is drawn.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [feedStackKey, loadCount])

  const loadMore = async () => {
    if (!nextCursor || isLoadingMore) return
    setIsLoadingMore(true)
    try {
      const answer = await graphql<{ ListAgentFeedStack: FeedPageAnswer }>(LIST_STACK, {
        feedStackKey,
        first: PAGE_SIZE,
        after: nextCursor,
      })
      setReadItems((previous) => {
        const known = new Set((previous ?? []).map((item) => item.feedItemId))
        const merged = [
          ...(previous ?? []),
          ...answer.ListAgentFeedStack.feedItems.filter((item) => !known.has(item.feedItemId)),
        ]
        countRef.current = merged.length
        return merged
      })
      setNextCursor(answer.ListAgentFeedStack.nextCursor)
    } catch (caught) {
      toast.failure(caught, t('feed.loadFailed'))
    } finally {
      setIsLoadingMore(false)
    }
  }

  const rest = (feedItems ?? []).filter((item) => item.feedItemId !== topFeedItemId)
  return (
    <div ref={restRef} className={isOpen ? 'feed-stack-rest' : 'feed-stack-rest is-tucked'} aria-hidden={!isOpen}>
      <ul className="feed-list">
        {rest.map((item, index) => (
          // A card is drawn over the cards under it, as in a deck.
          <li key={item.feedItemId} style={{ zIndex: rest.length - index }}>
            <FeedCard
              item={readItemIds.has(item.feedItemId) ? { ...item, isRead: true } : item}
              isFresh={!item.isRead}
              onDismissItems={onDismissItems}
              onRestore={onRestore}
              onSeen={isOpen ? onSeen : ignoreSeen}
              onChanged={(updated) =>
                onEntryChanged
                  ? onEntryChanged(updated)
                  : setReadItems((previous) =>
                      previous
                        ? previous.map((entry) => (entry.feedItemId === updated.feedItemId ? updated : entry))
                        : previous,
                    )
              }
              onRemoved={(feedItemId) =>
                onEntryRemoved
                  ? onEntryRemoved(feedItemId)
                  : setReadItems((previous) =>
                      previous ? previous.filter((entry) => entry.feedItemId !== feedItemId) : previous,
                    )
              }
            />
          </li>
        ))}
      </ul>
      {nextCursor && (
        <div className="list-foot feed-foot">
          <button type="button" disabled={isLoadingMore} onClick={() => void loadMore()}>
            {t('feed.more')}
          </button>
        </div>
      )}
    </div>
  )
}

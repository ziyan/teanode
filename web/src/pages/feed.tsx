import { useCallback, useEffect, useRef, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'

import { AgentReference, graphql, openAgentConversation, subscribe } from '../api'
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
  ShieldIcon,
  TerminalIcon,
  TrendingUpIcon,
  UserIcon,
  WalletIcon,
  WarningIcon,
  WeatherIcon,
} from '../components/icons'
import { FeedLayoutContent, parseFeedLayout, webAddressOf } from '../components/feedLayouts'
import { Markdown } from '../components/markdown'
import { ReferenceChips } from '../components/agentReferenceChips'
import { RelativeTime } from '../components/relativeTime'
import { SettingsEmpty } from '../components/settingsList'
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
  postedAt: string
  updatedAt: string
  feedStackKey: string
  stackItemCount: number
  stackUnreadCount: number
  feedStackTitle: string
  stackPosition: number | null
  feedCardLayout: string
  layoutContent: string
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
  feedUrgency isLiked isRead isDismissed postedAt updatedAt feedStackKey stackItemCount stackUnreadCount feedStackTitle stackPosition feedCardLayout layoutContent`

const LIST_FEED = `query ($first: Int, $after: String) {
  ListAgentFeed(first: $first, after: $after) { feedItems { ${ITEM_FIELDS} } nextCursor unreadCount }
}`
const LIST_STACK = `query ($feedStackKey: String!, $first: Int, $after: String) {
  ListAgentFeedStack(feedStackKey: $feedStackKey, first: $first, after: $after) { feedItems { ${ITEM_FIELDS} } nextCursor unreadCount }
}`
const CHOOSE_ITEM = `mutation ($feedItemId: String, $choiceKey: String) {
  ChooseAgentFeedItem(feedItemId: $feedItemId, choiceKey: $choiceKey) { ${ITEM_FIELDS} }
}`
const LIKE_ITEM = `mutation ($feedItemId: String, $isLiked: Boolean) {
  LikeAgentFeedItem(feedItemId: $feedItemId, isLiked: $isLiked) { ${ITEM_FIELDS} }
}`
const SHOW_LESS_ITEM = `mutation ($feedItemId: String) {
  ShowLessLikeAgentFeedItem(feedItemId: $feedItemId) { ${ITEM_FIELDS} }
}`
const DISMISS_ITEM = `mutation ($feedItemId: String) {
  DismissAgentFeedItem(feedItemId: $feedItemId) { ${ITEM_FIELDS} }
}`
const MARK_READ = `mutation ($feedItemIds: [String]) { MarkAgentFeedRead(feedItemIds: $feedItemIds) }`
const DISCUSS_ITEM = `mutation ($feedItemId: String) { DiscussAgentFeedItem(feedItemId: $feedItemId) }`

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

export function FeedPage() {
  const { t } = useTranslation()
  const toast = useToast()
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
  const feedItemCountRef = useRef(0)
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
        const answer = await graphql<{ ListAgentFeed: FeedPageAnswer }>(LIST_FEED, { first })
        // A newer read has been asked for, so this one is out of date.
        if (requestNumber !== requestNumberRef.current) return
        const page = answer.ListAgentFeed
        feedItemCountRef.current = page.feedItems.length
        setFeedItems(page.feedItems)
        setNextCursor(page.nextCursor)
        rememberFresh(page.feedItems)
        applyUnreadCount(page.unreadCount)
        setError(null)
        setLoadCount((count) => count + 1)
      } catch (caught) {
        if (requestNumber !== requestNumberRef.current) return
        if (!isQuiet) setError(caught)
      }
    },
    [applyUnreadCount, rememberFresh],
  )

  useEffect(() => {
    void load(false)
  }, [load])

  // The feed's own event: a card was posted, replaced, settled or dismissed.
  // The list is read again rather than patched, because a replaced card moves
  // to the top and the unread count changes with it.
  const loadRef = useRef(load)
  loadRef.current = load
  useEffect(
    () =>
      subscribe<{ AgentFeedChanged: { feedItemId: string; feedChange: string; unreadCount: number } }>(
        AGENT_FEED_CHANGED,
        {},
        () => void loadRef.current(true),
        undefined,
        {
          // What changed while the socket was down is read before the event
          // stream resumes.
          beforeStart: async (isReconnecting) => {
            if (isReconnecting) await loadRef.current(true)
          },
        },
      ),
    [],
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
      })
      const page = answer.ListAgentFeed
      setFeedItems((previous) => {
        const known = new Set((previous ?? []).map((item) => item.feedItemId))
        const merged = [...(previous ?? []), ...page.feedItems.filter((item) => !known.has(item.feedItemId))]
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

  const replaceItem = useCallback((updated: FeedItem) => {
    setFeedItems((previous) =>
      previous ? previous.map((item) => (item.feedItemId === updated.feedItemId ? updated : item)) : previous,
    )
  }, [])
  const removeItem = useCallback((feedItemId: string) => {
    setFeedItems((previous) => {
      const remaining = previous ? previous.filter((item) => item.feedItemId !== feedItemId) : previous
      feedItemCountRef.current = remaining?.length ?? 0
      return remaining
    })
  }, [])

  if (feedItems === null) {
    return (
      <>
        <ErrorMessage error={error} />
        {!error && <Loading />}
      </>
    )
  }
  return (
    <div className="feed-page">
      {feedItems.length === 0 ? (
        <SettingsEmpty>{t('feed.empty')}</SettingsEmpty>
      ) : (
        <ul className="feed-list">
          {feedItems.map((item) => (
            <li
              key={item.feedItemId}
              className={
                item.stackItemCount > 1 && !openStackKeys.has(item.feedStackKey) ? 'feed-stack is-closed' : 'feed-stack'
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
                stack={
                  item.stackItemCount > 1 && item.feedStackKey
                    ? {
                        isOpen: openStackKeys.has(item.feedStackKey),
                        onToggle: () =>
                          setOpenStackKeys((previous) => {
                            const next = new Set(previous)
                            if (!next.delete(item.feedStackKey)) next.add(item.feedStackKey)
                            return next
                          }),
                      }
                    : undefined
                }
              />
              {item.stackItemCount > 1 && openStackKeys.has(item.feedStackKey) && (
                <FeedStack
                  feedStackKey={item.feedStackKey}
                  topFeedItemId={item.feedItemId}
                  loadCount={loadCount}
                  readItemIds={readItemIds}
                  onSeen={markSeen}
                />
              )}
            </li>
          ))}
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
  stack,
}: {
  item: FeedItem
  isFresh: boolean
  onSeen: (feedItemId: string) => void
  onChanged: (item: FeedItem) => void
  onRemoved: (feedItemId: string) => void
  // Present on the top card of a stack: whether the rest is shown and how to toggle it.
  stack?: { isOpen: boolean; onToggle: () => void }
}) {
  const { t } = useTranslation()
  const toast = useToast()
  const navigate = useNavigate()
  const cardRef = useRef<HTMLElement>(null)
  const [isBusy, setIsBusy] = useState(false)
  const [isInfoOpen, setIsInfoOpen] = useState(false)

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
      if (!openAgentConversation(answer.DiscussAgentFeedItem)) toast.failed(t('feed.agentUnavailable'))
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
      await graphql(DISMISS_ITEM, { feedItemId: item.feedItemId })
      setIsInfoOpen(false)
      onRemoved(item.feedItemId)
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
  const isChosen = item.chosenChoiceKey !== null && item.chosenChoiceKey !== ''

  return (
    <article ref={cardRef} className={isFresh ? 'card feed-card is-fresh' : 'card feed-card'}>
      {isFresh && <span className="feed-fresh-dot" role="img" aria-label={t('feed.unread')} />}
      <div className="feed-picture">
        <FeedPicture pictureSymbol={item.pictureSymbol} />
      </div>
      <div className="feed-content">
        <h3 className="feed-title">{item.title}</h3>
        {layout ? (
          <FeedLayoutContent layout={layout} />
        ) : (
          item.body && (
            <div className="feed-body">
              <Markdown text={item.body} />
            </div>
          )
        )}
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
            <span className="muted feed-date">
              <RelativeTime value={item.updatedAt} />
            </span>
            <Tooltip label={t('feed.info')}>
              <button
                type="button"
                className="icon-action"
                aria-label={`${item.title}: ${t('feed.info')}`}
                onClick={() => setIsInfoOpen(true)}
              >
                <InfoIcon size={16} />
              </button>
            </Tooltip>
          </span>
        </div>
      </div>
      {stack && item.stackItemCount > 1 && (
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
      )}
      {isInfoOpen && (
        <FeedInfoDialog
          item={item}
          isBusy={isBusy}
          onShowLess={() => void showLess()}
          onDismiss={() => void dismiss()}
          onClose={() => setIsInfoOpen(false)}
        />
      )}
    </article>
  )
}

// FeedInfoDialog says why a card is here: what posted it (linking to the goal,
// schedule or coding session when there is one), when, what it was based on,
// and the two ways to be rid of it.
function FeedInfoDialog({
  item,
  isBusy,
  onShowLess,
  onDismiss,
  onClose,
}: {
  item: FeedItem
  isBusy: boolean
  onShowLess: () => void
  onDismiss: () => void
  onClose: () => void
}) {
  const { t } = useTranslation()
  const sourceName = item.feedSourceTitle || t('feed.sourceUnnamed')
  const sourcePath =
    item.feedSourceKind === 'goal' && item.feedSourceId
      ? `/settings/agent/goals?goal=${encodeURIComponent(item.feedSourceId)}`
      : item.feedSourceKind === 'schedule'
        ? '/settings/agent/schedules'
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
  const knownKinds = ['goal', 'schedule', 'alert', 'speak_first', 'coding_session', 'agent']
  const sourceKey = `feed.source.${knownKinds.includes(item.feedSourceKind) ? item.feedSourceKind : 'agent'}` as Key
  const sourceLinks = item.sourceLinks
    .map((link) => ({ ...link, address: webAddressOf(link.sourceUrl) }))
    .filter((link) => link.address !== '')
  return (
    <FormDialog
      title={t('feed.infoTitle')}
      submitLabel={t('feed.showLess')}
      closeLabel={t('feed.close')}
      busy={isBusy}
      onSubmit={onShowLess}
      onClose={onClose}
      otherAction={
        <button type="button" disabled={isBusy} onClick={onDismiss}>
          {t('feed.dismiss')}
        </button>
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
  loadCount,
  readItemIds,
  onSeen,
}: {
  feedStackKey: string
  topFeedItemId: string
  loadCount: number
  readItemIds: Set<string>
  onSeen: (feedItemId: string) => void
}) {
  const { t } = useTranslation()
  const toast = useToast()
  const [feedItems, setFeedItems] = useState<FeedItem[] | null>(null)
  const [nextCursor, setNextCursor] = useState<string | null>(null)
  const [isLoadingMore, setIsLoadingMore] = useState(false)
  const countRef = useRef(0)
  const toastRef = useRef(toast)
  toastRef.current = toast

  useEffect(() => {
    let isStale = false
    const first = Math.min(Math.max(PAGE_SIZE, countRef.current), MAXIMUM_REFRESH_SIZE)
    graphql<{ ListAgentFeedStack: FeedPageAnswer }>(LIST_STACK, { feedStackKey, first })
      .then((answer) => {
        if (isStale) return
        countRef.current = answer.ListAgentFeedStack.feedItems.length
        setFeedItems(answer.ListAgentFeedStack.feedItems)
        setNextCursor(answer.ListAgentFeedStack.nextCursor)
      })
      .catch((caught) => {
        if (!isStale && countRef.current === 0) toastRef.current.failure(caught, t('feed.loadFailed'))
      })
    return () => {
      isStale = true
    }
    // The translation function changes with the language only.
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
      setFeedItems((previous) => {
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
    <div className="feed-stack-rest">
      <ul className="feed-list">
        {rest.map((item) => (
          <li key={item.feedItemId}>
            <FeedCard
              item={readItemIds.has(item.feedItemId) ? { ...item, isRead: true } : item}
              isFresh={!item.isRead}
              onSeen={onSeen}
              onChanged={(updated) =>
                setFeedItems((previous) =>
                  previous
                    ? previous.map((entry) => (entry.feedItemId === updated.feedItemId ? updated : entry))
                    : previous,
                )
              }
              onRemoved={(feedItemId) =>
                setFeedItems((previous) =>
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

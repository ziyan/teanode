import { useCallback, useEffect, useState } from 'react'
import { useNavigate, useParams, useSearchParams } from 'react-router-dom'

import { MailboxThreadItem, MailboxThreadView, graphql } from '../api'
import { ErrorMessage, Loading } from '../components/common'
import { ConfirmDialog } from '../components/dialog'
import { useToast } from '../components/toast'
import { EnvelopeTrail } from '../components/envelopeTrail'
import {
  ArchiveIcon,
  ArrowLeftIcon,
  BellOffIcon,
  InboxOffIcon,
  PictureIcon,
  JunkIcon,
  MailIcon,
  MailOpenIcon,
  TrashIcon,
} from '../components/icons'
import { RelativeTime } from '../components/relativeTime'
import { SenderLogo } from '../components/senderLogo'
import { Tooltip } from '../components/tooltip'
import { useQuery } from '../components/useQuery'
import { Key, useTranslation } from '../i18n/i18n'
import { folderOfKind, folderRows, useMailboxes } from '../mailboxes'
import { DELETE, IconAction, MOVE, MoveToMenu, REPORT_JUNK, SET_FLAGS, ThreadMessage } from './mailbox'

// A page at a time, like the mailbox's own list. The count beside the heading
// is the true total, so showing 200 of it and stopping without a word was the
// page saying two different things at once.
const PAGE_SIZE = 50

const SUBSCRIPTIONS = `
  query ($mailboxId: String!, $first: Int, $offset: Int, $left: Boolean) {
    ListMailboxSubscriptions(mailboxId: $mailboxId, first: $first, offset: $offset, left: $left) {
      total
      subscribed
      left
      subscriptions {
        id key name from count unread lastAt lastItemId oneClick unsubscribe logoDomain
        requestedAt method failed error stripped mutedAt imagesAt
      }
    }
  }`

const ONE = `
  query ($mailboxId: String!, $id: String, $key: String) {
    GetMailboxSubscription(mailboxId: $mailboxId, id: $id, key: $key) {
      id key name from count unread lastAt lastItemId oneClick unsubscribe logoDomain
      requestedAt method failed error stripped mutedAt imagesAt
    }
  }`

// A ULID as this server writes them: twenty-six characters of Crockford's
// base32, lowercased — no i, l, o or u, which is what keeps it from being
// misread aloud.
const IDENTITY = /^[0-9abcdefghjkmnpqrstvwxyz]{26}$/

const IMAGES = `
  mutation ($mailboxId: String!, $key: String!, $show: Boolean!) {
    ShowMailboxSubscriptionImages(mailboxId: $mailboxId, key: $key, show: $show) { key imagesAt }
  }`

const MUTE = `
  mutation ($mailboxId: String!, $key: String!, $muted: Boolean!) {
    MuteMailboxSubscription(mailboxId: $mailboxId, key: $key, muted: $muted) { key mutedAt }
  }`

const READ = `
  query ($mailboxId: String!, $key: String!) {
    ReadMailboxSubscription(mailboxId: $mailboxId, key: $key) {
      threadId subject truncated
      items {
        folderId folderName folderKind
        item {
          id folderId mailId uid seen flagged answered forwarded draft addedAt
          mail {
            id from fromName sender subject recipients receivedAt size kind status messageId
            listKey listName listOneClick logoDomain
            authenticationResults { spf { result } dkims { result } dmarc { result } spamFilter { score } }
          }
        }
      }
    }
  }`

const UNSUBSCRIBE = `
  mutation ($mailboxId: String!, $key: String!) {
    UnsubscribeMailboxSubscription(mailboxId: $mailboxId, key: $key) {
      key requestedAt method failed error
    }
  }`

type Page = { total: number; subscribed: number; left: number; subscriptions: Subscription[] }

export type Subscription = {
  // What a link to this list names. The key is the sender's own identifier,
  // often an address, and an address in a URL is an address on the screen of
  // anybody looking over a shoulder.
  id: string
  key: string
  name: string
  from: string
  count: number
  unread: number
  lastAt: string
  lastItemId: string
  oneClick: boolean
  unsubscribe: string[]
  logoDomain?: string
  requestedAt?: string | null
  method?: string
  failed?: boolean
  error?: string
  // The sender said how to leave and something on the way here removed it.
  stripped?: boolean
  // Set while the list is kept out of the Inbox.
  mutedAt?: string | null
  // Set while this list's pictures are loaded without asking.
  imagesAt?: string | null
}

// Which of the three ways of leaving this list offers, which decides what the
// button will do and therefore what to warn about before it does it.
export function unsubscribeKind(subscription: {
  oneClick: boolean
  unsubscribe: string[]
}): 'oneClick' | 'mail' | 'link' | 'none' {
  if (subscription.oneClick) {
    return 'oneClick'
  }
  if (subscription.unsubscribe.some((address) => address.toLowerCase().startsWith('mailto:'))) {
    return 'mail'
  }
  if (subscription.unsubscribe.some((address) => address.toLowerCase().startsWith('http'))) {
    return 'link'
  }
  return 'none'
}

// The mailing lists this mailbox receives, and the way out of each.
//
// Most of what arrives in a mailbox is not a letter, and the only way to stop
// one of them was to open it, find the word "unsubscribe" in the small print
// at the bottom, and hope. A newsletter says how to leave in its headers; this
// is that, as a page.
export function MailboxSubscriptionsPage() {
  const { t, plural } = useTranslation()
  const toast = useToast()
  const mailboxes = useMailboxes()
  const view = mailboxes.current
  const mailboxId = view?.mailbox.id ?? ''

  // A list is named in the address by its own identity, made when its first
  // message arrived. Not by its key: the key is the identifier the sender
  // chose for itself, usually an address, and an address in the address bar
  // is an address on the screen of anybody looking over a shoulder.
  const navigate = useNavigate()
  const [search] = useSearchParams()
  const asked = useParams().key ?? search.get('key')

  // Which it is, told apart by shape. A ULID is twenty-six characters of
  // Crockford's base32; a list key is a domain or an address, and looks
  // nothing like one. Anything that is not an identity is a key from a link
  // made before lists had identities, and is answered and then corrected.
  const readingId = asked && IDENTITY.test(asked) ? asked : null
  const legacyKey = asked && !IDENTITY.test(asked) ? asked : null
  const [rows, setRows] = useState<Subscription[]>([])
  const [total, setTotal] = useState(0)
  // How many on each side, so the switch can say what the other one holds.
  const [counts, setCounts] = useState({ subscribed: 0, left: 0 })
  const [paging, setPaging] = useState(false)

  // One side or the other. A list somebody has left is not one they are
  // subscribed to, so the page shows the lists writing to them or the ones
  // they have dealt with, and says how many are on the side they are not
  // looking at. Kept rather than gone: mail from a list often keeps arriving
  // for a while after the asking, and seeing that is the point of having
  // asked.
  //
  // In the address, like everything else that says what a list is showing, so
  // it survives a reload and the back button leads out of it.
  const showingLeft = search.get('left') === 'true'
  const showSide = (left: boolean) => {
    const written = new URLSearchParams(search)
    if (left) {
      written.set('left', 'true')
    } else {
      written.delete('left')
    }
    navigate({ pathname: '/mailbox/subscriptions', search: written.toString() })
  }
  const query = useQuery(
    () =>
      mailboxId
        ? graphql<{ ListMailboxSubscriptions: Page }>(SUBSCRIPTIONS, {
            mailboxId,
            first: PAGE_SIZE,
            left: showingLeft,
          })
        : Promise.resolve(null),
    [mailboxId, showingLeft],
    { refresh: false },
  )

  // The first page comes from the query above and replaces what is held; the
  // rest are appended. Reloading after an unsubscribe or a mute goes through
  // the same path, so the list never shows a stale first page.
  useEffect(() => {
    const page = query.data?.ListMailboxSubscriptions
    if (!page) {
      return
    }
    setRows(page.subscriptions)
    setTotal(page.total)
    setCounts({ subscribed: page.subscribed ?? 0, left: page.left ?? 0 })
  }, [query.data])

  const loadMore = useCallback(async () => {
    setPaging(true)
    try {
      const response = await graphql<{ ListMailboxSubscriptions: Page }>(SUBSCRIPTIONS, {
        mailboxId,
        first: PAGE_SIZE,
        offset: rows.length,
        left: showingLeft,
      })
      const page = response.ListMailboxSubscriptions
      setRows((previous) => {
        // Paged by offset, so a list that wrote since the last page shifts
        // the rest down one: the overlap is dropped rather than shown twice.
        const shown = new Set(previous.map((subscription) => subscription.key))
        return [...previous, ...page.subscriptions.filter((subscription) => !shown.has(subscription.key))]
      })
      setTotal(page.total)
    } catch (caught) {
      toast.failure(caught, t('domain.failed'))
    } finally {
      setPaging(false)
    }
  }, [mailboxId, rows.length, showingLeft, t])

  const subscriptions = rows

  // Which list is being read is in the address, not in a variable beside it.
  // It was state, and the row that set it cleared the address as it went, so
  // opening a list left no trace: the back button went to whatever came
  // before this page, and there was no way forward to the list just left.
  // Reading one is a place, and a place has a URL.
  const read = (subscription: Subscription | null) =>
    navigate({
      pathname: subscription ? `/mailbox/subscriptions/${subscription.id}` : '/mailbox/subscriptions',
      // The side travels with it: coming back from a list lands on the side
      // it was found on.
      search: showingLeft ? 'left=true' : '',
    })
  const [leaving, setLeaving] = useState<Subscription | null>(null)
  const [busy, setBusy] = useState(false)
  const [problem, setProblem] = useState<string | null>(null)
  // A list arrived at from one of its messages may be anywhere in the order,
  // including past the page that has been loaded. So the row is fetched on its
  // own rather than waiting for the reader to page down to it.
  const [fetched, setFetched] = useState<Subscription | null>(null)
  const reading =
    subscriptions.find((subscription) => subscription.id === readingId) ??
    (fetched?.id === readingId ? fetched : null)

  useEffect(() => {
    if (!mailboxId || (!readingId && !legacyKey)) {
      return
    }
    if (readingId && subscriptions.some((subscription) => subscription.id === readingId)) {
      return
    }
    let cancelled = false
    void graphql<{ GetMailboxSubscription: Subscription | null }>(ONE, {
      mailboxId,
      id: readingId,
      key: legacyKey,
    })
      .then((response) => {
        if (cancelled) {
          return
        }
        const found = response.GetMailboxSubscription
        setFetched(found)
        // A link made when the key was the identity: answered, and then the
        // address is put right, so what gets copied from here afterwards is
        // the identity rather than the address of a mailing list.
        if (found && legacyKey) {
          navigate(`/mailbox/subscriptions/${found.id}`, { replace: true })
        }
      })
      .catch(() => undefined)
    return () => {
      cancelled = true
    }
  }, [mailboxId, readingId, legacyKey, subscriptions, navigate])

  const mute = useCallback(
    async (key: string, muted: boolean) => {
      setProblem(null)
      try {
        await graphql(MUTE, { mailboxId, key, muted })
        await query.reload()
        toast.done(muted ? t('subscriptions.saidMuted') : t('subscriptions.saidUnmuted'))
      } catch (caught) {
        toast.failure(caught, t('domain.failed'))
      }
    },
    [mailboxId, query, t],
  )

  const images = useCallback(
    async (key: string, show: boolean) => {
      setProblem(null)
      try {
        await graphql(IMAGES, { mailboxId, key, show })
        await query.reload()
        toast.done(show ? t('subscriptions.saidImagesAlways') : t('subscriptions.saidImagesAsk'))
      } catch (caught) {
        toast.failure(caught, t('domain.failed'))
      }
    },
    [mailboxId, query, t],
  )

  const unsubscribe = useCallback(async () => {
    if (!leaving) {
      return
    }
    setBusy(true)
    setProblem(null)
    try {
      // A page that wants a human cannot be pressed by a server, so that one
      // is opened here — and the server is still told, so the row says it was
      // asked for.
      if (unsubscribeKind(leaving) === 'link') {
        const address = leaving.unsubscribe.find((candidate) => candidate.toLowerCase().startsWith('http'))
        if (address) {
          window.open(address, '_blank', 'noopener,noreferrer')
        }
      }
      await graphql(UNSUBSCRIBE, { mailboxId, key: leaving.key })
      setLeaving(null)
      await query.reload()
      toast.done(t('subscriptions.saidLeft', { name: leaving.name }))
    } catch (caught) {
      toast.failure(caught, t('domain.failed'))
    } finally {
      setBusy(false)
    }
  }, [leaving, mailboxId, query, t])

  return (
    <>
      {/* The query's failure stays on the page: it describes what is not
          there, and a message that takes itself away is no use for that. What
          an action did is said in a toast instead. */}
      {query.error ? <ErrorMessage error={query.error} /> : null}

      <div className={['mailbox', reading ? 'reading' : ''].filter(Boolean).join(' ')}>
        <div className="mailbox-list">
          <div className="mailbox-actions">
            {/* The name and the count, until the switch says both — "
                Subscriptions · 3" beside "Subscribed · 2 | Unsubscribed · 1"
                is the same fact twice, and the second telling is the one
                somebody can act on. */}
            {!showingLeft && counts.left === 0 && (
              <span className="muted">
                {t('subscriptions.title')}
                {query.data ? ` · ${total}` : ''}
              </span>
            )}
            {/* Shown once there is a second side to go to. Until somebody
                has left a list there is only one answer, and a switch with
                one side is a control that does nothing. */}
            {(showingLeft || counts.left > 0) && (
              <div className="segmented" role="group" aria-label={t('subscriptions.sides')}>
                <button
                  type="button"
                  className={showingLeft ? undefined : 'active'}
                  aria-pressed={!showingLeft}
                  onClick={() => showSide(false)}
                >
                  {t('subscriptions.sideSubscribed', { count: counts.subscribed })}
                </button>
                <button
                  type="button"
                  className={showingLeft ? 'active' : undefined}
                  aria-pressed={showingLeft}
                  onClick={() => showSide(true)}
                >
                  {t('subscriptions.sideLeft', { count: counts.left })}
                </button>
              </div>
            )}
          </div>

          {query.loading && !query.data && <Loading />}
          {query.data && subscriptions.length === 0 && (
            <p className="mailbox-placeholder">{t('subscriptions.empty')}</p>
          )}

          <ul className="mailbox-rows">
            {subscriptions.map((subscription) => (
              <li
                key={subscription.key}
                className={[
                  'subscription-row',
                  subscription.unread > 0 ? 'unread' : '',
                  subscription.requestedAt && !subscription.failed ? 'left' : '',
                  subscription.id === readingId ? 'active' : '',
                ]
                  .filter(Boolean)
                  .join(' ')}
                onClick={() => read(subscription)}
              >
                <SenderLogo name={subscription.name} logoDomain={subscription.logoDomain} size={28} />
                <Tooltip label={subscription.from}>
                  <button
                    type="button"
                    className="subscription-row-link"
                    onClick={(event) => {
                      event.stopPropagation()
                      read(subscription)
                    }}
                  >
                    <span className="subscription-row-name">{subscription.name}</span>
                    {/* Said plainly, beside the name, because the sentence
                        underneath says how it was left and this says that it
                        was. */}
                    {subscription.requestedAt && !subscription.failed ? (
                      <span className="subscription-row-status">{t('subscriptions.unsubscribed')}</span>
                    ) : null}
                    <span className="subscription-row-meta">
                      {plural(
                        subscription.count,
                        { one: 'subscriptions.messageCountOne', other: 'subscriptions.messageCountOther' },
                        { count: subscription.count },
                      )}
                      {subscription.unread > 0 ? ` · ${t('subscriptions.unread', { count: subscription.unread })}` : ''}
                      {subscription.mutedAt ? ` · ${t('subscriptions.muted')}` : ''}
                    </span>
                    {subscription.requestedAt ? (
                      <span className={subscription.failed ? 'subscription-row-left bad' : 'subscription-row-left'}>
                        {subscription.failed
                          ? t('subscriptions.leftFailed', { reason: subscription.error ?? '' })
                          : t(`subscriptions.left.${unsubscribeMethod(subscription.method)}`)}
                      </span>
                    ) : null}
                  </button>
                </Tooltip>
                <div className="subscription-row-when">
                  <RelativeTime value={subscription.lastAt} />
                </div>
              </li>
            ))}
          </ul>

          {/* What is shown, and the rest of it. The count beside the heading
              is the whole list; without this the page showed a fraction of it
              and said nothing. */}
          {query.data && subscriptions.length > 0 && (
            <div className="mailbox-foot">
              <span>
                {paging ? t('common.loading') : t('mailbox.count', { shown: subscriptions.length, total })}
              </span>
              {subscriptions.length < total && !paging && (
                <button type="button" className="link" onClick={() => void loadMore()}>
                  {t('mailbox.loadMore')}
                </button>
              )}
            </div>
          )}
        </div>

        <div className="mailbox-pane">
          {reading ? (
            <SubscriptionReader
              key={reading.key}
              mailboxId={mailboxId}
              subscription={reading}
              onBack={() => read(null)}
              onLeave={() => {
                setProblem(null)
                setLeaving(reading)
              }}
              onMute={(muted) => mute(reading.key, muted)}
              onImages={(show) => images(reading.key, show)}
              onChanged={() => void query.reload()}
            />
          ) : (
            <div className="mailbox-pane-placeholder">
              <EnvelopeTrail />
              <span>{t('subscriptions.choose')}</span>
            </div>
          )}
        </div>
      </div>

      {leaving &&
        (unsubscribeKind(leaving) === 'none' ? (
          // Nothing to confirm: this says why, and offers the one thing that
          // does work. The reason is not the same in both cases, and a reader
          // told "no way to leave" would otherwise blame the sender for a
          // relay's doing.
          <ConfirmDialog
            title={t('subscriptions.noWayOutTitle', { name: leaving.name })}
            body={leaving.stripped ? t('subscriptions.stripped') : t('subscriptions.noWayOut')}
            confirmLabel={leaving.mutedAt ? undefined : t('subscriptions.mute')}
            destructive={false}
            busy={busy}
            error={problem}
            onConfirm={
              leaving.mutedAt
                ? undefined
                : () => {
                    const key = leaving.key
                    setLeaving(null)
                    void mute(key, true)
                  }
            }
            onClose={() => setLeaving(null)}
          />
        ) : (
          <ConfirmDialog
            title={t('subscriptions.leaveTitle', { name: leaving.name })}
            body={t(`subscriptions.leaveBody.${unsubscribeKind(leaving)}`)}
            confirmLabel={t('subscriptions.leave')}
            busy={busy}
            error={problem}
            onConfirm={unsubscribe}
            onClose={() => setLeaving(null)}
          />
        ))}
    </>
  )
}

// The server's word for how it was left, guarded: a row written by a newer
// server than this page knows about should not read as a missing translation.
function unsubscribeMethod(method?: string): 'oneClick' | 'mail' | 'link' {
  return method === 'mail' || method === 'link' ? method : 'oneClick'
}

// One list's mail, read the way a conversation is: newest first, what has been
// read collapsed to a line.
function SubscriptionReader({
  mailboxId,
  subscription,
  onBack,
  onLeave,
  onMute,
  onImages,
  onChanged,
}: {
  mailboxId: string
  subscription: Subscription
  onBack: () => void
  onLeave: () => void
  onMute: (muted: boolean) => Promise<void>
  onImages: (show: boolean) => Promise<void>
  // Something was moved, deleted or marked: the list beside this shows counts
  // and has to be told.
  onChanged: () => void
}) {
  const { t, plural } = useTranslation()
  const toast = useToast()
  const mailboxes = useMailboxes()
  const folders = mailboxes.current?.folders ?? []
  const [busy, setBusy] = useState(false)
  const [problem, setProblem] = useState<string | null>(null)
  const [emptying, setEmptying] = useState(false)
  const query = useQuery(
    () => graphql<{ ReadMailboxSubscription: MailboxThreadView }>(READ, { mailboxId, key: subscription.key }),
    [mailboxId, subscription.key],
    { refresh: false },
  )
  const thread = query.data?.ReadMailboxSubscription
  // What the actions act on: every message of this list the mailbox holds,
  // which is what somebody means by "archive this newsletter". Drafts are
  // left out — a message being written is not part of what was sent to you.
  const acting = (thread?.items ?? []).filter((entry) => !entry.item.draft).map((entry) => entry.item.id)
  const anyUnread = (thread?.items ?? []).some((entry) => !entry.item.seen)
  // Where it can be moved to: every folder, since a list's mail is not read
  // from one in particular.
  const targets = folderRows(folders)
  const inJunk = (thread?.items ?? []).every((entry) => entry.folderKind === 'junk')
  const archive = folderOfKind(mailboxes.current, 'archive')

  const run = async (action: () => Promise<unknown>, said?: string) => {
    setBusy(true)
    setProblem(null)
    try {
      await action()
      await query.reload()
      onChanged()
      if (said) {
        toast.done(said)
      }
    } catch (caught) {
      toast.failure(caught, t('domain.failed'))
    } finally {
      setBusy(false)
    }
  }

  // How many messages an action is about, which is what the sentence needs:
  // acting on a whole list is not the same size of act as acting on one.
  const many = (one: Key, other: Key) =>
    plural(acting.length, { one, other }, { count: acting.length })
  // Which messages are open, and which have been read here: the same two
  // things a conversation tracks, for the same reasons. Decided once from what
  // arrived — the newest, and anything unread — and then it is the reader's,
  // so opening and closing sticks. Deriving "open" from "unread" on every
  // render instead meant an unread message could not be collapsed at all: the
  // click set the state and the state was ignored.
  const [open, setOpen] = useState<Set<string> | null>(null)
  const [marks, setMarks] = useState<Record<string, boolean>>({})
  const seenOf = (entry: MailboxThreadItem) => marks[entry.item.id] ?? entry.item.seen

  const markRead = useCallback(
    (itemIds: string[]) => {
      if (itemIds.length === 0) {
        return
      }
      setMarks((previous) => ({ ...previous, ...Object.fromEntries(itemIds.map((id) => [id, true])) }))
      // The list beside this one counts what is unread, so it is told too.
      void graphql(SET_FLAGS, { itemIds, seen: true }).then(onChanged)
    },
    [onChanged],
  )

  useEffect(() => {
    if (!thread || open !== null) {
      return
    }
    const wanted = new Set<string>()
    const readable = thread.items.filter((entry) => !entry.item.draft)
    if (readable.length > 0) {
      wanted.add(readable[0].item.id)
    }
    for (const entry of readable) {
      if (!entry.item.seen) {
        wanted.add(entry.item.id)
      }
    }
    setOpen(wanted)
    // Opening a list reads what it opened. Once — marking one unread again
    // afterwards leaves it that way.
    markRead(readable.filter((entry) => wanted.has(entry.item.id) && !entry.item.seen).map((entry) => entry.item.id))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [thread])

  const opened = open ?? new Set<string>()
  const toggle = (id: string) =>
    setOpen((previous) => {
      const next = new Set(previous ?? [])
      if (next.has(id)) {
        next.delete(id)
      } else {
        next.add(id)
      }
      return next
    })

  return (
    <>
      {/* The same actions a conversation has, over everything this list has
          sent: what somebody wants of a newsletter is usually all of it at
          once — archive the lot, move the lot, or throw the lot away. */}
      <div className="mailbox-pane-actions">
        {/* Back is for the width where the list is not beside this one. */}
        <IconAction label={t('subscriptions.back')} icon={<ArrowLeftIcon size={16} />} onClick={onBack} />
        <IconAction
          label={anyUnread ? t('mailbox.markRead') : t('mailbox.markUnread')}
          icon={anyUnread ? <MailOpenIcon size={16} /> : <MailIcon size={16} />}
          disabled={busy || acting.length === 0}
          onClick={() =>
            void run(
              () => graphql(SET_FLAGS, { itemIds: acting, seen: anyUnread }),
              anyUnread ? many('mailbox.saidReadOne', 'mailbox.saidReadOther') : many('mailbox.saidUnreadOne', 'mailbox.saidUnreadOther'),
            )
          }
        />
        {archive && (
          <IconAction
            label={t('mailbox.archive')}
            icon={<ArchiveIcon size={16} />}
            disabled={busy || acting.length === 0}
            onClick={() =>
              void run(
                () => graphql(MOVE, { itemIds: acting, folderId: archive.id }),
                many('mailbox.saidArchivedMessageOne', 'mailbox.saidArchivedMessageOther'),
              )
            }
          />
        )}
        <IconAction
          label={inJunk ? t('mailbox.notJunk') : t('mailbox.reportJunk')}
          icon={<JunkIcon size={16} />}
          disabled={busy || acting.length === 0}
          onClick={() =>
            void run(
              () => graphql(REPORT_JUNK, { itemIds: acting, notJunk: inJunk }),
              inJunk ? many('mailbox.saidNotJunkOne', 'mailbox.saidNotJunkOther') : many('mailbox.saidJunkOne', 'mailbox.saidJunkOther'),
            )
          }
        />
        <MoveToMenu
          targets={targets}
          disabled={busy || acting.length === 0}
          onMove={(folderId) =>
            void run(
              () => graphql(MOVE, { itemIds: acting, folderId }),
              many('mailbox.saidMovedMessageOne', 'mailbox.saidMovedMessageOther'),
            )
          }
        />
        <IconAction
          label={t('mailbox.delete')}
          icon={<TrashIcon size={16} />}
          className="danger"
          disabled={busy || acting.length === 0}
          onClick={() => setEmptying(true)}
        />
        <IconAction
          label={subscription.imagesAt ? t('subscriptions.imagesAsk') : t('subscriptions.imagesAlways')}
          icon={<PictureIcon size={16} />}
          className={subscription.imagesAt ? 'active' : undefined}
          disabled={busy}
          onClick={() => void onImages(!subscription.imagesAt)}
        />
        <IconAction
          label={subscription.mutedAt ? t('subscriptions.unmute') : t('subscriptions.mute')}
          icon={<InboxOffIcon size={16} />}
          className={subscription.mutedAt ? 'active' : undefined}
          disabled={busy}
          onClick={() => void onMute(!subscription.mutedAt)}
        />
        <IconAction
          label={t('subscriptions.leave')}
          icon={<BellOffIcon size={16} />}
          onClick={onLeave}
        />
      </div>


      <ErrorMessage error={problem} />

      {/* Asked first: this is every message of the list at once, and the
          count is the point of the question. */}
      {emptying && (
        <ConfirmDialog
          title={t('subscriptions.deleteTitle', { name: subscription.name })}
          body={t('subscriptions.deleteBody', { count: acting.length })}
          confirmLabel={t('mailbox.delete')}
          busy={busy}
          error={problem}
          onConfirm={async () => {
            await run(
              () => graphql(DELETE, { itemIds: acting }),
              many('mailbox.saidTrashedMessageOne', 'mailbox.saidTrashedMessageOther'),
            )
            setEmptying(false)
          }}
          onClose={() => setEmptying(false)}
        />
      )}

      <div className="mailbox-pane-head">
        <Tooltip label={subscription.from}>
          <h2 className="sender-row">
            <SenderLogo name={subscription.name} logoDomain={subscription.logoDomain} />
            {subscription.name}
          </h2>
        </Tooltip>
      </div>

      {query.loading && !query.data && <Loading />}
      {query.error ? <ErrorMessage error={query.error} /> : null}
      {thread?.truncated && <p className="muted">{t('mailbox.threadTruncated')}</p>}

      <ul className="mailbox-thread">
        {(thread?.items ?? []).map((entry) => (
          <ThreadMessage
            key={entry.item.id}
            entry={entry}
            folderId={entry.folderId}
            seen={seenOf(entry)}
            // The list's standing answer applies to a message that is
            // authenticated as from the list. A List-Id is a header anyone
            // can write, and the server applies the same rule.
            allowImages={!!subscription.imagesAt && entry.item.mail?.authenticationResults?.dmarc?.result === 'pass'}
            open={opened.has(entry.item.id)}
            onToggle={() => {
              const opening = !opened.has(entry.item.id)
              toggle(entry.item.id)
              // Reading one is reading it: what somebody has just looked at
              // should not still be counted as unread beside its name.
              if (opening && !seenOf(entry)) {
                markRead([entry.item.id])
              }
            }}
          />
        ))}
      </ul>
    </>
  )
}

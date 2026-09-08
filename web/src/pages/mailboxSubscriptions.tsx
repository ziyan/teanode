import { useCallback, useEffect, useState } from 'react'

import { MailboxThreadItem, MailboxThreadView, graphql } from '../api'
import { ErrorMessage, Loading } from '../components/common'
import { ConfirmDialog } from '../components/dialog'
import {
  ArchiveIcon,
  ArrowLeftIcon,
  BellOffIcon,
  JunkIcon,
  MailIcon,
  MailOpenIcon,
  TrashIcon,
} from '../components/icons'
import { RelativeTime } from '../components/relativeTime'
import { SenderLogo } from '../components/senderLogo'
import { Tooltip } from '../components/tooltip'
import { useQuery } from '../components/useQuery'
import { useTranslation } from '../i18n/i18n'
import { folderOfKind, folderRows, useMailboxes } from '../mailboxes'
import { DELETE, IconAction, MOVE, MoveToMenu, REPORT_JUNK, SET_FLAGS, ThreadMessage } from './mailbox'

const SUBSCRIPTIONS = `
  query ($mailboxId: String!) {
    ListMailboxSubscriptions(mailboxId: $mailboxId, first: 200) {
      total
      subscriptions {
        key name from count unread lastAt lastItemId oneClick unsubscribe logoDomain
        requestedAt method failed error
      }
    }
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

export type Subscription = {
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
  const mailboxes = useMailboxes()
  const view = mailboxes.current
  const mailboxId = view?.mailbox.id ?? ''

  const query = useQuery(
    () =>
      mailboxId
        ? graphql<{ ListMailboxSubscriptions: { total: number; subscriptions: Subscription[] } }>(SUBSCRIPTIONS, {
            mailboxId,
          })
        : Promise.resolve(null),
    [mailboxId],
    { refresh: false },
  )
  const subscriptions = query.data?.ListMailboxSubscriptions.subscriptions ?? []

  // Which list is being read, and which is being left. Both are one at a
  // time: reading is a page and leaving is a question.
  const [readingKey, setReadingKey] = useState<string | null>(null)
  const [leaving, setLeaving] = useState<Subscription | null>(null)
  const [busy, setBusy] = useState(false)
  const [problem, setProblem] = useState<string | null>(null)
  const reading = subscriptions.find((subscription) => subscription.key === readingKey) ?? null

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
    } catch (caught) {
      setProblem(caught instanceof Error ? caught.message : t('domain.failed'))
    } finally {
      setBusy(false)
    }
  }, [leaving, mailboxId, query, t])

  return (
    <>
      <ErrorMessage error={problem} />
      {query.error ? <ErrorMessage error={query.error} /> : null}

      <div className={['mailbox', reading ? 'reading' : ''].filter(Boolean).join(' ')}>
        <div className="mailbox-list">
          <div className="mailbox-actions">
            <span className="muted">
              {t('subscriptions.title')}
              {query.data ? ` · ${query.data.ListMailboxSubscriptions.total}` : ''}
            </span>
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
                  subscription.key === readingKey ? 'active' : '',
                ]
                  .filter(Boolean)
                  .join(' ')}
                onClick={() => setReadingKey(subscription.key)}
              >
                <SenderLogo name={subscription.name} logoDomain={subscription.logoDomain} size={28} />
                <Tooltip label={subscription.from}>
                  <button
                    type="button"
                    className="subscription-row-link"
                    onClick={(event) => {
                      event.stopPropagation()
                      setReadingKey(subscription.key)
                    }}
                  >
                    <span className="subscription-row-name">{subscription.name}</span>
                    <span className="subscription-row-meta">
                      {plural(
                        subscription.count,
                        { one: 'subscriptions.messageCountOne', other: 'subscriptions.messageCountOther' },
                        { count: subscription.count },
                      )}
                      {subscription.unread > 0 ? ` · ${t('subscriptions.unread', { count: subscription.unread })}` : ''}
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
        </div>

        <div className="mailbox-pane">
          {reading ? (
            <SubscriptionReader
              key={reading.key}
              mailboxId={mailboxId}
              subscription={reading}
              onBack={() => setReadingKey(null)}
              onLeave={() => {
                setProblem(null)
                setLeaving(reading)
              }}
              onChanged={() => void query.reload()}
            />
          ) : (
            <p className="mailbox-placeholder">{t('subscriptions.choose')}</p>
          )}
        </div>
      </div>

      {leaving && (
        <ConfirmDialog
          title={t('subscriptions.leaveTitle', { name: leaving.name })}
          body={t(`subscriptions.leaveBody.${unsubscribeKind(leaving)}`)}
          confirmLabel={t('subscriptions.leave')}
          busy={busy}
          error={problem}
          onConfirm={unsubscribe}
          onClose={() => setLeaving(null)}
        />
      )}
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
  onChanged,
}: {
  mailboxId: string
  subscription: Subscription
  onBack: () => void
  onLeave: () => void
  // Something was moved, deleted or marked: the list beside this shows counts
  // and has to be told.
  onChanged: () => void
}) {
  const { t } = useTranslation()
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

  const run = async (action: () => Promise<unknown>) => {
    setBusy(true)
    setProblem(null)
    try {
      await action()
      await query.reload()
      onChanged()
    } catch (caught) {
      setProblem(caught instanceof Error ? caught.message : t('domain.failed'))
    } finally {
      setBusy(false)
    }
  }
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
          onClick={() => void run(() => graphql(SET_FLAGS, { itemIds: acting, seen: anyUnread }))}
        />
        {archive && (
          <IconAction
            label={t('mailbox.archive')}
            icon={<ArchiveIcon size={16} />}
            disabled={busy || acting.length === 0}
            onClick={() => void run(() => graphql(MOVE, { itemIds: acting, folderId: archive.id }))}
          />
        )}
        <IconAction
          label={inJunk ? t('mailbox.notJunk') : t('mailbox.reportJunk')}
          icon={<JunkIcon size={16} />}
          disabled={busy || acting.length === 0}
          onClick={() => void run(() => graphql(REPORT_JUNK, { itemIds: acting, notJunk: inJunk }))}
        />
        <MoveToMenu
          targets={targets}
          disabled={busy || acting.length === 0}
          onMove={(folderId) => void run(() => graphql(MOVE, { itemIds: acting, folderId }))}
        />
        <IconAction
          label={t('mailbox.delete')}
          icon={<TrashIcon size={16} />}
          className="danger"
          disabled={busy || acting.length === 0}
          onClick={() => setEmptying(true)}
        />
        <IconAction
          label={t('subscriptions.leave')}
          icon={<BellOffIcon size={16} />}
          disabled={unsubscribeKind(subscription) === 'none'}
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
            await run(() => graphql(DELETE, { itemIds: acting }))
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

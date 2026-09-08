import { useCallback, useState } from 'react'

import { MailboxThreadView, graphql } from '../api'
import { ErrorMessage, Loading } from '../components/common'
import { ConfirmDialog } from '../components/dialog'
import { ArrowLeftIcon, ChevronRightIcon, CloseIcon } from '../components/icons'
import { RelativeTime } from '../components/relativeTime'
import { SettingsEmpty, SettingsRow, SettingsSection } from '../components/settingsList'
import { Tooltip } from '../components/tooltip'
import { useQuery } from '../components/useQuery'
import { useTranslation } from '../i18n/i18n'
import { useMailboxes } from '../mailboxes'
import { ThreadMessage } from './mailbox'

const SUBSCRIPTIONS = `
  query ($mailboxId: String!) {
    ListMailboxSubscriptions(mailboxId: $mailboxId, first: 200) {
      total
      subscriptions {
        key name from count unread lastAt lastItemId oneClick unsubscribe requestedAt method failed error
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
            listKey listName listOneClick
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
  const [reading, setReading] = useState<Subscription | null>(null)
  const [leaving, setLeaving] = useState<Subscription | null>(null)
  const [busy, setBusy] = useState(false)
  const [problem, setProblem] = useState<string | null>(null)

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

  if (reading) {
    return (
      <SubscriptionReader
        mailboxId={mailboxId}
        subscription={reading}
        onBack={() => {
          setReading(null)
          void query.reload()
        }}
      />
    )
  }

  return (
    <>
      <ErrorMessage error={problem} />
      {query.loading && !query.data && <Loading />}
      {query.error ? <ErrorMessage error={query.error} /> : null}

      <SettingsSection card title={t('subscriptions.title')} description={t('subscriptions.intro')}>
        {query.data && subscriptions.length === 0 && <SettingsEmpty>{t('subscriptions.empty')}</SettingsEmpty>}

        {subscriptions.map((subscription) => (
          <SettingsRow
            key={subscription.key}
            title={subscription.name}
            subtitle={
              <>
                <div>{subscription.from}</div>
                <div>
                  {plural(
                    subscription.count,
                    { one: 'subscriptions.messageCountOne', other: 'subscriptions.messageCountOther' },
                    { count: subscription.count },
                  )}
                  {subscription.unread > 0 ? ` · ${t('subscriptions.unread', { count: subscription.unread })}` : ''}
                  {' · '}
                  <RelativeTime value={subscription.lastAt} />
                </div>
                {subscription.requestedAt ? (
                  <div className={subscription.failed ? 'bad' : undefined}>
                    {subscription.failed
                      ? t('subscriptions.leftFailed', { reason: subscription.error ?? '' })
                      : t(`subscriptions.left.${unsubscribeMethod(subscription.method)}`)}{' '}
                    <RelativeTime value={subscription.requestedAt} />
                  </div>
                ) : null}
              </>
            }
            actions={
              <div className="row-actions">
                <Tooltip label={leaveLabel(t, subscription)}>
                  <button
                    className="icon-action danger"
                    type="button"
                    disabled={busy || unsubscribeKind(subscription) === 'none'}
                    aria-label={`${subscription.name}: ${leaveLabel(t, subscription)}`}
                    onClick={() => {
                      setProblem(null)
                      setLeaving(subscription)
                    }}
                  >
                    <CloseIcon size={16} />
                  </button>
                </Tooltip>
                <Tooltip label={t('subscriptions.read')}>
                  <button
                    className="icon-action"
                    type="button"
                    aria-label={`${subscription.name}: ${t('subscriptions.read')}`}
                    onClick={() => setReading(subscription)}
                  >
                    <ChevronRightIcon size={16} />
                  </button>
                </Tooltip>
              </div>
            }
          />
        ))}
      </SettingsSection>

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

// What the button will do, said before it does it.
function leaveLabel(
  t: (key: 'subscriptions.leave' | 'subscriptions.leaveNoWay') => string,
  subscription: Subscription,
) {
  return unsubscribeKind(subscription) === 'none' ? t('subscriptions.leaveNoWay') : t('subscriptions.leave')
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
}: {
  mailboxId: string
  subscription: Subscription
  onBack: () => void
}) {
  const { t } = useTranslation()
  const query = useQuery(
    () => graphql<{ ReadMailboxSubscription: MailboxThreadView }>(READ, { mailboxId, key: subscription.key }),
    [mailboxId, subscription.key],
    { refresh: false },
  )
  const thread = query.data?.ReadMailboxSubscription
  // Which messages are open, and which have been read since the page loaded:
  // the same two things a conversation tracks, for the same reasons.
  const [open, setOpen] = useState<Set<string>>(new Set())
  const toggle = (id: string) =>
    setOpen((previous) => {
      const next = new Set(previous)
      if (next.has(id)) {
        next.delete(id)
      } else {
        next.add(id)
      }
      return next
    })

  return (
    <>
      <div className="mailbox-pane-actions">
        <Tooltip label={t('subscriptions.back')}>
          <button className="icon-button" type="button" aria-label={t('subscriptions.back')} onClick={onBack}>
            <ArrowLeftIcon size={16} />
          </button>
        </Tooltip>
      </div>

      <div className="mailbox-pane-head">
        <h2>{subscription.name}</h2>
        <p className="muted">{subscription.from}</p>
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
            seen={entry.item.seen}
            open={open.has(entry.item.id) || !entry.item.seen}
            onToggle={() => toggle(entry.item.id)}
          />
        ))}
      </ul>
    </>
  )
}

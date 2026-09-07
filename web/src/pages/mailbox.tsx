import { useCallback, useEffect, useMemo, useState } from 'react'
import { Link, Navigate, useNavigate, useParams } from 'react-router-dom'

import { MailContent, MailboxFolder, MailboxItem, MailboxItemPage, MailboxView, graphql } from '../api'
import { ErrorMessage, Loading, formatTime } from '../components/common'
import { ConfirmDialog } from '../components/dialog'
import { RelativeTime } from '../components/relativeTime'
import { useQuery } from '../components/useQuery'
import { useBreadcrumbDetail } from '../components/breadcrumb'
import { Key, useTranslation } from '../i18n/i18n'
import { folderLabel, folderOfKind, folderRows, useMailboxes } from '../mailboxes'
import { hasAnywhere, useSession } from '../session'
import { MessageContent } from './mailDetail'

// The mailbox: one folder's messages beside the one being read.
//
// The folder tree is in the rail, so this page is the other two panes of the
// usual three. The list is a page of the folder, newest first, with the
// controls that act on a selection above it; the pane is the message, with
// the controls that act on that one message above it. Reading marks a
// message seen, as every mail program does, and the counts in the rail move
// with it.

const PAGE_SIZE = 50

// STARRED is the path segment of the view of every flagged message.
export const STARRED = 'starred'

function starredFolder(view: MailboxView): MailboxFolder {
  return { id: STARRED, mailboxId: view.mailbox.id, name: 'Starred', kind: STARRED, unread: 0, total: 0 }
}

const ITEMS = `
  query ($folderId: String, $mailboxId: String, $unread: Boolean, $flagged: Boolean, $search: String, $from: String, $to: String, $subject: String, $since: DateTime, $before: DateTime, $hasAttachment: Boolean, $first: Int, $after: String, $offset: Int) {
    ListMailboxItems(folderId: $folderId, mailboxId: $mailboxId, unread: $unread, flagged: $flagged, search: $search, from: $from, to: $to, subject: $subject, since: $since, before: $before, hasAttachment: $hasAttachment, first: $first, after: $after, offset: $offset) {
      total
      items {
        id folderId mailId uid seen flagged answered forwarded draft addedAt
        mail { id from fromName sender subject recipients receivedAt size kind status }
      }
    }
  }`

const ITEM = `
  query ($itemId: String!) {
    GetMailboxItem(itemId: $itemId) {
      id folderId mailId uid seen flagged answered forwarded draft addedAt
      mail { id from fromName sender subject recipients receivedAt size kind status messageId }
    }
  }`

const CONTENT = `
  query ($mailId: String!) {
    GetMailContent(mailId: $mailId) {
      mailId available text html hasRemoteContent size rawHeaders
      headers { key value }
      attachments { index filename contentType size inline }
    }
  }`

const SET_FLAGS = `
  mutation ($itemIds: [String!]!, $seen: Boolean, $flagged: Boolean) {
    SetMailboxItemFlags(itemIds: $itemIds, seen: $seen, flagged: $flagged)
  }`

const MOVE = `
  mutation ($itemIds: [String!]!, $folderId: String!) {
    MoveMailboxItems(itemIds: $itemIds, folderId: $folderId) { id folderId }
  }`

const DELETE = `
  mutation ($itemIds: [String!]!) {
    DeleteMailboxItems(itemIds: $itemIds)
  }`

const EMPTY_TRASH = `
  mutation ($mailboxId: String!) {
    EmptyMailboxTrash(mailboxId: $mailboxId)
  }`

type Filter = 'all' | 'unread' | 'flagged'

// What a search narrows by, beyond the words: where it looks and what it
// asks of a message. Empty means not asked.
type Narrowing = {
  everywhere: boolean
  from: string
  to: string
  subject: string
  since: string
  before: string
  attachment: 'any' | 'with' | 'without'
}

const NOTHING_NARROWED: Narrowing = { everywhere: false, from: '', to: '', subject: '', since: '', before: '', attachment: 'any' }

function narrowed(value: Narrowing): boolean {
  return value.from !== '' || value.to !== '' || value.subject !== '' || value.since !== '' || value.before !== '' || value.attachment !== 'any'
}

// A day typed into a date field, as the moment it starts (or the moment the
// next day starts, for an end that should include the day itself).
function dayStart(value: string, plusDays = 0): string | undefined {
  if (!value) {
    return undefined
  }
  const date = new Date(value + 'T00:00:00')
  if (Number.isNaN(date.getTime())) {
    return undefined
  }
  date.setDate(date.getDate() + plusDays)
  return date.toISOString()
}

export function MailboxPage() {
  const { folderId, itemId } = useParams()
  const mailboxes = useMailboxes()
  const { t } = useTranslation()

  if (!mailboxes.loaded) {
    return <Loading />
  }
  if (mailboxes.error) {
    return <ErrorMessage error={mailboxes.error} />
  }
  const view = folderId
    ? (mailboxes.views.find((candidate) => candidate.folders.some((folder) => folder.id === folderId)) ??
      mailboxes.current)
    : mailboxes.current
  if (!view) {
    return (
      <div className="card">
        <h3>{t('mailbox.none')}</h3>
        <p className="muted">{t('mailbox.noneHint')}</p>
      </div>
    )
  }

  // Starred is every flagged message wherever it sits: a view, not a
  // folder, drawn as one.
  if (folderId === STARRED) {
    return <FollowRail view={view} folder={starredFolder(view)} itemId={itemId} />
  }

  // /mailbox on its own is the inbox of the mailbox last looked at.
  const folder = folderId ? view.folders.find((candidate) => candidate.id === folderId) : undefined
  if (!folder) {
    const inbox = folderOfKind(view, 'inbox') ?? view.folders[0]
    return inbox ? <Navigate to={`/mailbox/${inbox.id}`} replace /> : <p className="muted">{t('common.notFound')}</p>
  }

  return <FollowRail view={view} folder={folder} itemId={itemId} />
}

// FollowRail keeps the tree in the rail on the mailbox being read: a link
// into a folder of the second mailbox should not leave the rail showing the
// first. An effect rather than a call during render, which React refuses.
function FollowRail({ view, folder, itemId }: { view: MailboxView; folder: MailboxFolder; itemId?: string }) {
  const mailboxes = useMailboxes()
  const mailboxId = view.mailbox.id
  useEffect(() => {
    if (mailboxes.current?.mailbox.id !== mailboxId) {
      mailboxes.setCurrentId(mailboxId)
    }
  }, [mailboxes, mailboxId])
  return <Folder key={folder.id} folder={folder} folders={view.folders} itemId={itemId} />
}

function Folder({
  folder,
  folders,
  itemId,
}: {
  folder: MailboxFolder
  folders: MailboxFolder[]
  itemId?: string
}) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const mailboxes = useMailboxes()
  const session = useSession()
  const addresses = mailboxes.current?.mailbox.addresses ?? []
  const managesDomains = hasAnywhere(session.permissions, 'domain:manage')
  useBreadcrumbDetail(folderLabel(t, folder))

  const [filter, setFilter] = useState<Filter>('all')
  const [search, setSearch] = useState('')
  const [applied, setApplied] = useState('')
  const [narrowing, setNarrowing] = useState<Narrowing>(NOTHING_NARROWED)
  const [appliedNarrowing, setAppliedNarrowing] = useState<Narrowing>(NOTHING_NARROWED)
  const [showNarrowing, setShowNarrowing] = useState(false)
  const starred = folder.kind === STARRED
  const everywhere = starred || (appliedNarrowing.everywhere && (applied !== '' || narrowed(appliedNarrowing) || filter !== 'all'))
  const [items, setItems] = useState<MailboxItem[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<unknown>(null)
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [emptying, setEmptying] = useState(false)
  const [busy, setBusy] = useState(false)

  const variables = useMemo(
    () => ({
      folderId: everywhere ? undefined : folder.id,
      mailboxId: everywhere ? folder.mailboxId : undefined,
      unread: filter === 'unread' ? true : undefined,
      flagged: starred || filter === 'flagged' ? true : undefined,
      search: applied || undefined,
      from: appliedNarrowing.from || undefined,
      to: appliedNarrowing.to || undefined,
      subject: appliedNarrowing.subject || undefined,
      since: dayStart(appliedNarrowing.since),
      before: dayStart(appliedNarrowing.before, 1),
      hasAttachment: appliedNarrowing.attachment === 'any' ? undefined : appliedNarrowing.attachment === 'with',
      first: PAGE_SIZE,
    }),
    [folder.id, folder.mailboxId, filter, applied, appliedNarrowing, everywhere, starred],
  )

  const load = useCallback(
    async (after?: string, offset?: number) => {
      setLoading(true)
      try {
        const response = await graphql<{ ListMailboxItems: MailboxItemPage }>(ITEMS, {
          ...variables,
          after: variables.mailboxId ? undefined : after,
          offset: variables.mailboxId ? offset : undefined,
        })
        const page = response.ListMailboxItems
        setItems((previous) => {
          if (!after) {
            return page.items
          }
          // Paged by offset across the mailbox, a message starred or
          // arrived since the last page shifts the rest down one: the
          // overlap is dropped rather than shown twice.
          const shown = new Set(previous.map((item) => item.id))
          return [...previous, ...page.items.filter((item) => !shown.has(item.id))]
        })
        setTotal(page.total)
        setError(null)
      } catch (failure) {
        setError(failure)
      } finally {
        setLoading(false)
      }
    },
    [variables],
  )

  useEffect(() => {
    setSelected(new Set())
    void load()
  }, [load])

  // A change to a message's flags is written into the list in place rather
  // than reloaded: the list should not jump under somebody who just marked
  // a row, and the rail's counts are refreshed separately.
  const patch = (itemIds: string[], change: Partial<MailboxItem>) =>
    setItems((previous) => previous.map((item) => (itemIds.includes(item.id) ? { ...item, ...change } : item)))
  const remove = (itemIds: string[]) => {
    setItems((previous) => previous.filter((item) => !itemIds.includes(item.id)))
    setTotal((previous) => Math.max(0, previous - itemIds.length))
    setSelected((previous) => {
      const next = new Set(previous)
      itemIds.forEach((id) => next.delete(id))
      return next
    })
  }

  const act = async (action: () => Promise<void>) => {
    setBusy(true)
    try {
      await action()
      setError(null)
    } catch (failure) {
      setError(failure)
    } finally {
      setBusy(false)
      void mailboxes.refresh()
    }
  }

  const setFlags = (itemIds: string[], flags: { seen?: boolean; flagged?: boolean }) =>
    act(async () => {
      await graphql(SET_FLAGS, { itemIds, ...flags })
      patch(itemIds, flags)
      if (starred && flags.flagged === false) {
        // Unstarred is gone from Starred.
        remove(itemIds)
        if (itemIds.includes(itemId ?? '')) {
          navigate(`/mailbox/${folder.id}`)
        }
      }
    })
  const moveTo = (itemIds: string[], target: string) =>
    act(async () => {
      await graphql(MOVE, { itemIds, folderId: target })
      remove(itemIds)
      if (itemIds.includes(itemId ?? '')) {
        navigate(`/mailbox/${folder.id}`)
      }
    })
  const deleteItems = (itemIds: string[]) =>
    act(async () => {
      await graphql(DELETE, { itemIds })
      remove(itemIds)
      if (itemIds.includes(itemId ?? '')) {
        navigate(`/mailbox/${folder.id}`)
      }
    })

  const chosen = items.filter((item) => selected.has(item.id))
  const chosenIds = chosen.map((item) => item.id)
  const archive = folderOfKind({ mailbox: undefined as never, folders, unread: 0 }, 'archive')
  const inTrash = folder.kind === 'trash'
  const targets = folderRows(folders).filter(({ folder: candidate }) => candidate.id !== folder.id)
  // In Starred, "delete" means what it means in the message's own folder;
  // the server decides by the item, so nothing to do here but not to call
  // it "delete for good".

  const toggleAll = () =>
    setSelected((previous) => (previous.size === items.length ? new Set() : new Set(items.map((item) => item.id))))

  return (
    <div className={['mailbox', itemId ? 'reading' : ''].filter(Boolean).join(' ')}>
      <div className="mailbox-list">
        <div className="mailbox-new">
          <button type="button" className="primary" onClick={() => navigate('/mailbox/compose')}>
            {t('mailbox.newMessage')}
          </button>
        </div>
        <form
          className="mailbox-toolbar"
          onSubmit={(event) => {
            event.preventDefault()
            setApplied(search.trim())
            setAppliedNarrowing(narrowing)
          }}
        >
          <input
            type="search"
            placeholder={narrowing.everywhere ? t('mailbox.searchEverywhere') : t('mailbox.search')}
            aria-label={t('mailbox.search')}
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            onBlur={() => setApplied(search.trim())}
          />
          <div className="segmented" role="group">
            <button type="button" className={filter === 'unread' ? 'active' : ''} onClick={() => setFilter(filter === 'unread' ? 'all' : 'unread')}>
              {t('mailbox.unreadOnly')}
            </button>
            {!starred && (
              <button type="button" className={filter === 'flagged' ? 'active' : ''} onClick={() => setFilter(filter === 'flagged' ? 'all' : 'flagged')}>
                {t('mailbox.flaggedOnly')}
              </button>
            )}
            <button
              type="button"
              className={showNarrowing || narrowed(appliedNarrowing) || appliedNarrowing.everywhere ? 'active' : ''}
              aria-expanded={showNarrowing}
              onClick={() => setShowNarrowing((previous) => !previous)}
            >
              {t('mailbox.narrow')}
            </button>
          </div>
          {showNarrowing && (
            <div className="mailbox-narrowing">
              <label className="check">
                <input
                  type="checkbox"
                  checked={narrowing.everywhere}
                  onChange={(event) => setNarrowing({ ...narrowing, everywhere: event.target.checked })}
                />
                {t('mailbox.narrowEverywhere')}
              </label>
              <label>
                <span>{t('mailbox.narrowFrom')}</span>
                <input value={narrowing.from} onChange={(event) => setNarrowing({ ...narrowing, from: event.target.value })} />
              </label>
              <label>
                <span>{t('mailbox.narrowTo')}</span>
                <input value={narrowing.to} onChange={(event) => setNarrowing({ ...narrowing, to: event.target.value })} />
              </label>
              <label>
                <span>{t('mailbox.narrowSubject')}</span>
                <input value={narrowing.subject} onChange={(event) => setNarrowing({ ...narrowing, subject: event.target.value })} />
              </label>
              <div className="mailbox-narrowing-row">
                <label>
                  <span>{t('mailbox.narrowSince')}</span>
                  <input type="date" value={narrowing.since} onChange={(event) => setNarrowing({ ...narrowing, since: event.target.value })} />
                </label>
                <label>
                  <span>{t('mailbox.narrowBefore')}</span>
                  <input type="date" value={narrowing.before} onChange={(event) => setNarrowing({ ...narrowing, before: event.target.value })} />
                </label>
              </div>
              <label>
                <span>{t('mailbox.narrowAttachment')}</span>
                <select
                  value={narrowing.attachment}
                  onChange={(event) => setNarrowing({ ...narrowing, attachment: event.target.value as Narrowing['attachment'] })}
                >
                  <option value="any">{t('mailbox.narrowAttachmentAny')}</option>
                  <option value="with">{t('mailbox.narrowAttachmentWith')}</option>
                  <option value="without">{t('mailbox.narrowAttachmentWithout')}</option>
                </select>
              </label>
              <div className="mailbox-narrowing-actions">
                <button type="submit" className="primary">
                  {t('mailbox.narrowApply')}
                </button>
                <button
                  type="button"
                  onClick={() => {
                    setNarrowing(NOTHING_NARROWED)
                    setAppliedNarrowing(NOTHING_NARROWED)
                    setShowNarrowing(false)
                  }}
                >
                  {t('mailbox.narrowClear')}
                </button>
              </div>
            </div>
          )}
        </form>

        {/* Acting on a selection. Shown always rather than only once
            something is selected, so the row does not appear and push the
            list down at the moment somebody clicks a checkbox. */}
        <div className="mailbox-actions">
          <input
            type="checkbox"
            aria-label={t('mailbox.selectAll')}
            checked={items.length > 0 && selected.size === items.length}
            onChange={toggleAll}
            disabled={items.length === 0}
          />
          {chosen.length > 0 ? (
            <>
              <span className="muted">{t('mailbox.selected', { count: chosen.length })}</span>
              {chosen.some((item) => !item.seen) ? (
                <button type="button" disabled={busy} onClick={() => setFlags(chosenIds, { seen: true })}>
                  {t('mailbox.markRead')}
                </button>
              ) : (
                <button type="button" disabled={busy} onClick={() => setFlags(chosenIds, { seen: false })}>
                  {t('mailbox.markUnread')}
                </button>
              )}
              {chosen.some((item) => !item.flagged) ? (
                <button type="button" disabled={busy} onClick={() => setFlags(chosenIds, { flagged: true })}>
                  {t('mailbox.flag')}
                </button>
              ) : (
                <button type="button" disabled={busy} onClick={() => setFlags(chosenIds, { flagged: false })}>
                  {t('mailbox.unflag')}
                </button>
              )}
              {archive && folder.id !== archive.id && (
                <button type="button" disabled={busy} onClick={() => moveTo(chosenIds, archive.id)}>
                  {t('mailbox.archive')}
                </button>
              )}
              <select
                aria-label={t('mailbox.moveTo')}
                value=""
                disabled={busy}
                onChange={(event) => event.target.value && moveTo(chosenIds, event.target.value)}
              >
                <option value="">{t('mailbox.moveTo')}</option>
                {targets.map(({ folder: candidate, depth }) => (
                  <option key={candidate.id} value={candidate.id}>
                    {'  '.repeat(depth) + folderLabel(t, candidate)}
                  </option>
                ))}
              </select>
              <button type="button" className="danger" disabled={busy} onClick={() => deleteItems(chosenIds)}>
                {inTrash ? t('mailbox.deleteForever') : t('mailbox.delete')}
              </button>
            </>
          ) : (
            <>
              {inTrash && total > 0 && (
                <button type="button" className="danger" disabled={busy} onClick={() => setEmptying(true)}>
                  {t('mailbox.emptyTrash')}
                </button>
              )}
            </>
          )}
        </div>

        {error ? <ErrorMessage error={error} /> : null}

        <ul className="mailbox-rows">
          {items.map((item) => (
            <Row
              key={item.id}
              item={item}
              folderName={everywhere && item.folderId !== folder.id ? folderLabelOf(folders, item.folderId, t) : undefined}
              active={item.id === itemId}
              selected={selected.has(item.id)}
              onSelect={(on) =>
                setSelected((previous) => {
                  const next = new Set(previous)
                  if (on) {
                    next.add(item.id)
                  } else {
                    next.delete(item.id)
                  }
                  return next
                })
              }
              href={item.draft ? `/mailbox/compose?draft=${item.id}` : `/mailbox/${folder.id}/${item.id}`}
              onOpen={() =>
                navigate(item.draft ? `/mailbox/compose?draft=${item.id}` : `/mailbox/${folder.id}/${item.id}`)
              }
              onFlag={(on) => setFlags([item.id], { flagged: on })}
            />
          ))}
          {!loading && items.length === 0 && (
            <li className="mailbox-placeholder">
              {applied || filter !== 'all' || narrowed(appliedNarrowing) ? (
                t('mailbox.nothingFound')
              ) : starred ? (
                t('mailbox.nothingStarred')
              ) : folder.kind === 'inbox' && addresses.length === 0 ? (
                // An Inbox with no address is the first thing a new account
                // sees, and "nothing here" would leave it wondering why.
                <>
                  {t('mailbox.noAddress')}
                  {managesDomains && (
                    <>
                      {' '}
                      <Link to="/domains">{t('mailbox.noAddressLink')}</Link>
                    </>
                  )}
                </>
              ) : (
                t('mailbox.nothing')
              )}
            </li>
          )}
        </ul>

        <div className="mailbox-foot">
          <span>{loading ? t('common.loading') : t('mailbox.count', { shown: items.length, total })}</span>
          {items.length < total && !loading && (
            <button type="button" className="link" onClick={() => load(items[items.length - 1]?.id, items.length)}>
              {t('mailbox.loadMore')}
            </button>
          )}
        </div>
      </div>

      <div className="mailbox-pane">
        {itemId ? (
          <Reader
            key={itemId}
            itemId={itemId}
            folder={folder}
            archive={archive}
            targets={targets}
            busy={busy}
            onSeen={(seen) => setFlags([itemId], { seen })}
            onFlag={(flagged) => setFlags([itemId], { flagged })}
            onMove={(target) => moveTo([itemId], target)}
            onDelete={() => deleteItems([itemId])}
            onBack={() => navigate(`/mailbox/${folder.id}`)}
          />
        ) : (
          <div className="mailbox-placeholder">{t('mailbox.chooseMessage')}</div>
        )}
      </div>

      {emptying && (
        <ConfirmDialog
          title={t('mailbox.emptyTrash')}
          body={t('mailbox.emptyTrashConfirm')}
          confirmLabel={t('mailbox.emptyTrash')}
          busy={busy}
          onConfirm={() =>
            act(async () => {
              await graphql(EMPTY_TRASH, { mailboxId: folder.mailboxId })
              setItems([])
              setTotal(0)
              setEmptying(false)
              if (itemId) {
                navigate(`/mailbox/${folder.id}`)
              }
            })
          }
          onClose={() => setEmptying(false)}
        />
      )}
    </div>
  )
}

// folderLabelOf is a folder's name by id, for a hit from another folder.
function folderLabelOf(folders: MailboxFolder[], folderId: string, t: (key: Key) => string): string {
  const found = folders.find((folder) => folder.id === folderId)
  return found ? folderLabel(t, found) : ''
}

function Row({
  item,
  folderName,
  href,
  active,
  selected,
  onSelect,
  onOpen,
  onFlag,
}: {
  item: MailboxItem
  folderName?: string
  href: string
  active: boolean
  selected: boolean
  onSelect: (on: boolean) => void
  onOpen: () => void
  onFlag: (on: boolean) => void
}) {
  const { t } = useTranslation()
  const mail = item.mail
  return (
    <li
      className={['mailbox-row', item.seen ? '' : 'unread', active ? 'active' : ''].filter(Boolean).join(' ')}
      onClick={onOpen}
    >
      <input
        type="checkbox"
        aria-label={t('mailbox.select')}
        checked={selected}
        onClick={(event) => event.stopPropagation()}
        onChange={(event) => onSelect(event.target.checked)}
      />
      <button
        type="button"
        className={['mailbox-star', item.flagged ? 'on' : ''].filter(Boolean).join(' ')}
        aria-label={item.flagged ? t('mailbox.unflag') : t('mailbox.flag')}
        aria-pressed={item.flagged}
        onClick={(event) => {
          event.stopPropagation()
          onFlag(!item.flagged)
        }}
      >
        {item.flagged ? '★' : '☆'}
      </button>
      {/* A real link, so the keyboard reaches it and a middle click opens
          it in a tab; the row's own click is for the mouse. */}
      <Link
        className="mailbox-row-link"
        to={href}
        onClick={(event) => {
          event.preventDefault()
          event.stopPropagation()
          onOpen()
        }}
      >
        <div className="mailbox-row-from" title={mail?.from || mail?.sender}>
          {mail?.fromName || mail?.from || mail?.sender || t('mailbox.unknownSender')}
        </div>
        <div className="mailbox-row-subject">
          {folderName && <span className="mailbox-row-folder">{folderName}</span>}
          {mail?.subject || t('mailbox.noSubject')}
        </div>
      </Link>
      <div className="mailbox-row-when">
        <RelativeTime value={mail?.receivedAt ?? item.addedAt} />
      </div>
    </li>
  )
}

function Reader({
  itemId,
  folder,
  archive,
  targets,
  busy,
  onSeen,
  onFlag,
  onMove,
  onDelete,
  onBack,
}: {
  itemId: string
  folder: MailboxFolder
  archive?: MailboxFolder
  targets: { folder: MailboxFolder; depth: number }[]
  busy: boolean
  onSeen: (seen: boolean) => void
  onFlag: (flagged: boolean) => void
  onMove: (folderId: string) => void
  onDelete: () => void
  onBack: () => void
}) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const item = useQuery(() => graphql<{ GetMailboxItem: MailboxItem }>(ITEM, { itemId }), [itemId], { refresh: false })
  const mailId = item.data?.GetMailboxItem?.mailId
  const content = useQuery(
    () => (mailId ? graphql<{ GetMailContent: MailContent }>(CONTENT, { mailId }) : Promise.resolve(null)),
    [mailId],
    { refresh: false },
  )

  // Opening a message reads it. Once, when it arrives unseen: the reader can
  // mark it unread again afterwards and it stays that way.
  const [seen, setSeen] = useState<boolean | null>(null)
  const [flagged, setFlagged] = useState<boolean | null>(null)
  useEffect(() => {
    const loaded = item.data?.GetMailboxItem
    if (loaded && seen === null) {
      setSeen(true)
      setFlagged(loaded.flagged)
      if (!loaded.seen) {
        onSeen(true)
      }
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [item.data])

  if (item.loading && !item.data) {
    return <Loading />
  }
  if (item.error) {
    return <ErrorMessage error={item.error} />
  }
  const loaded = item.data?.GetMailboxItem
  if (!loaded) {
    return <p className="muted">{t('common.notFound')}</p>
  }
  const mail = loaded.mail
  const inTrash = folder.kind === 'trash'

  return (
    <>
      <div className="mailbox-pane-actions">
        <button type="button" className="mailbox-back" onClick={onBack}>
          {t('mailbox.backToList')}
        </button>
        {loaded.draft ? (
          <button type="button" className="primary" onClick={() => navigate(`/mailbox/compose?draft=${itemId}`)}>
            {t('mailbox.editDraft')}
          </button>
        ) : (
          <>
            <button type="button" className="primary" onClick={() => navigate(`/mailbox/compose?reply=${itemId}`)}>
              {t('mailbox.reply')}
            </button>
            <button type="button" onClick={() => navigate(`/mailbox/compose?replyAll=${itemId}`)}>
              {t('mailbox.replyAll')}
            </button>
            <button type="button" onClick={() => navigate(`/mailbox/compose?forward=${itemId}`)}>
              {t('mailbox.forward')}
            </button>
          </>
        )}
        <button
          type="button"
          disabled={busy}
          onClick={() => {
            const next = !(seen ?? true)
            setSeen(next)
            onSeen(next)
          }}
        >
          {seen === false ? t('mailbox.markRead') : t('mailbox.markUnread')}
        </button>
        <button
          type="button"
          disabled={busy}
          onClick={() => {
            const next = !(flagged ?? loaded.flagged)
            setFlagged(next)
            onFlag(next)
          }}
        >
          {(flagged ?? loaded.flagged) ? t('mailbox.unflag') : t('mailbox.flag')}
        </button>
        {archive && folder.id !== archive.id && (
          <button type="button" disabled={busy} onClick={() => onMove(archive.id)}>
            {t('mailbox.archive')}
          </button>
        )}
        <select
          aria-label={t('mailbox.moveTo')}
          value=""
          disabled={busy}
          onChange={(event) => event.target.value && onMove(event.target.value)}
        >
          <option value="">{t('mailbox.moveTo')}</option>
          {targets.map(({ folder: candidate, depth }) => (
            <option key={candidate.id} value={candidate.id}>
              {'  '.repeat(depth) + folderLabel(t, candidate)}
            </option>
          ))}
        </select>
        <button type="button" className="danger" disabled={busy} onClick={onDelete}>
          {inTrash ? t('mailbox.deleteForever') : t('mailbox.delete')}
        </button>
      </div>

      <div className="mailbox-pane-head">
        <h2>{mail?.subject || t('mailbox.noSubject')}</h2>
      </div>
      {mail ? (
        <dl className="mailbox-pane-meta">
          <dt>{t('mailbox.from')}</dt>
          <dd>{mail.fromName ? `${mail.fromName} <${mail.from || mail.sender}>` : mail.from || mail.sender || t('mailbox.unknownSender')}</dd>
          <dt>{t('mailbox.to')}</dt>
          <dd>{(mail.recipients ?? []).join(', ')}</dd>
          <dt>{t('mail.received')}</dt>
          <dd>{formatTime(mail.receivedAt)}</dd>
        </dl>
      ) : (
        <p className="muted">{t('mailbox.messageGone')}</p>
      )}

      {mail ? (
        content.loading && !content.data ? (
          <Loading />
        ) : content.error ? (
          <ErrorMessage error={content.error} />
        ) : (
          <MessageContent mailId={mail.id} content={content.data?.GetMailContent} mode="mailbox" />
        )
      ) : null}
    </>
  )
}

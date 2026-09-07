import { useCallback, useEffect, useMemo, useState } from 'react'
import { Navigate, useNavigate, useParams } from 'react-router-dom'

import { MailContent, MailboxFolder, MailboxItem, MailboxItemPage, graphql } from '../api'
import { ErrorMessage, Loading, formatTime } from '../components/common'
import { ConfirmDialog } from '../components/dialog'
import { RelativeTime } from '../components/relativeTime'
import { useQuery } from '../components/useQuery'
import { useBreadcrumbDetail } from '../components/breadcrumb'
import { useTranslation } from '../i18n/i18n'
import { folderLabel, folderOfKind, folderRows, useMailboxes } from '../mailboxes'
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

const ITEMS = `
  query ($folderId: String!, $unread: Boolean, $flagged: Boolean, $search: String, $first: Int, $after: String) {
    ListMailboxItems(folderId: $folderId, unread: $unread, flagged: $flagged, search: $search, first: $first, after: $after) {
      total
      items {
        id folderId mailId uid seen flagged answered forwarded draft addedAt
        mail { id from sender subject recipients receivedAt size kind status }
      }
    }
  }`

const ITEM = `
  query ($itemId: String!) {
    GetMailboxItem(itemId: $itemId) {
      id folderId mailId uid seen flagged answered forwarded draft addedAt
      mail { id from sender subject recipients receivedAt size kind status messageId }
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

  // /mailbox on its own is the inbox of the mailbox last looked at.
  const folder = folderId ? view.folders.find((candidate) => candidate.id === folderId) : undefined
  if (!folder) {
    const inbox = folderOfKind(view, 'inbox') ?? view.folders[0]
    return inbox ? <Navigate to={`/mailbox/${inbox.id}`} replace /> : <p className="muted">{t('common.notFound')}</p>
  }

  // The tree in the rail follows the folder being read, when it belongs to
  // another mailbox — a link into a folder of the second mailbox should not
  // leave the rail showing the first.
  if (mailboxes.current?.mailbox.id !== view.mailbox.id) {
    mailboxes.setCurrentId(view.mailbox.id)
  }

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
  useBreadcrumbDetail(folderLabel(t, folder))

  const [filter, setFilter] = useState<Filter>('all')
  const [search, setSearch] = useState('')
  const [applied, setApplied] = useState('')
  const [items, setItems] = useState<MailboxItem[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<unknown>(null)
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [emptying, setEmptying] = useState(false)
  const [busy, setBusy] = useState(false)

  const variables = useMemo(
    () => ({
      folderId: folder.id,
      unread: filter === 'unread' ? true : undefined,
      flagged: filter === 'flagged' ? true : undefined,
      search: applied || undefined,
      first: PAGE_SIZE,
    }),
    [folder.id, filter, applied],
  )

  const load = useCallback(
    async (after?: string) => {
      setLoading(true)
      try {
        const response = await graphql<{ ListMailboxItems: MailboxItemPage }>(ITEMS, { ...variables, after })
        const page = response.ListMailboxItems
        setItems((previous) => (after ? [...previous, ...page.items] : page.items))
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
          }}
        >
          <input
            type="search"
            placeholder={t('mailbox.search')}
            aria-label={t('mailbox.search')}
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            onBlur={() => setApplied(search.trim())}
          />
          <div className="segmented" role="group">
            <button type="button" className={filter === 'unread' ? 'active' : ''} onClick={() => setFilter(filter === 'unread' ? 'all' : 'unread')}>
              {t('mailbox.unreadOnly')}
            </button>
            <button type="button" className={filter === 'flagged' ? 'active' : ''} onClick={() => setFilter(filter === 'flagged' ? 'all' : 'flagged')}>
              {t('mailbox.flaggedOnly')}
            </button>
          </div>
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
              <span className="muted">{t('mailbox.count', { shown: items.length, total })}</span>
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
              onOpen={() =>
                navigate(item.draft ? `/mailbox/compose?draft=${item.id}` : `/mailbox/${folder.id}/${item.id}`)
              }
              onFlag={(on) => setFlags([item.id], { flagged: on })}
            />
          ))}
          {!loading && items.length === 0 && (
            <li className="mailbox-placeholder">{applied || filter !== 'all' ? t('mailbox.nothingFound') : t('mailbox.nothing')}</li>
          )}
        </ul>

        <div className="mailbox-foot">
          <span>{loading ? t('common.loading') : t('mailbox.count', { shown: items.length, total })}</span>
          {items.length < total && !loading && (
            <button type="button" className="link" onClick={() => load(items[items.length - 1]?.id)}>
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

function Row({
  item,
  active,
  selected,
  onSelect,
  onOpen,
  onFlag,
}: {
  item: MailboxItem
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
      <div>
        <div className="mailbox-row-from">{mail?.from || mail?.sender || t('mailbox.unknownSender')}</div>
        <div className="mailbox-row-subject">{mail?.subject || t('mailbox.noSubject')}</div>
      </div>
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
          <dd>{mail.from || mail.sender || t('mailbox.unknownSender')}</dd>
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
          <MessageContent mailId={mail.id} content={content.data?.GetMailContent} />
        )
      ) : null}
    </>
  )
}

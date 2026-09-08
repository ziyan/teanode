import { useCallback, useEffect, useMemo, useState } from 'react'
import { Link, Navigate, useNavigate, useParams } from 'react-router-dom'

import {
  MailContent,
  MailboxFolder,
  MailboxItem,
  MailboxThread,
  MailboxThreadItem,
  MailboxThreadPage,
  MailboxThreadView,
  MailboxView,
  graphql,
} from '../api'
import { ErrorMessage, Loading, VerdictMark, formatTime, verdictOf } from '../components/common'
import {
  ArchiveIcon,
  ArrowLeftIcon,
  FlagIcon,
  ForwardIcon,
  JunkIcon,
  MailIcon,
  MailOpenIcon,
  MoveIcon,
  ReplyAllIcon,
  ReplyIcon,
  TrashIcon,
} from '../components/icons'
import { MenuButton } from '../components/menuButton'
import { Tooltip } from '../components/tooltip'
import { ConfirmDialog } from '../components/dialog'
import { RelativeTime } from '../components/relativeTime'
import { useQuery } from '../components/useQuery'
import { useBreadcrumbDetail } from '../components/breadcrumb'
import { Key, useTranslation } from '../i18n/i18n'
import { folderLabel, folderOfKind, folderRows, useMailboxes } from '../mailboxes'
import { hasAnywhere, useSession } from '../session'
import { MessageContent } from './mailDetail'
import { MailboxComposer } from './mailboxCompose'

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

const THREADS = `
  query ($folderId: String, $mailboxId: String, $unread: Boolean, $flagged: Boolean, $search: String, $from: String, $to: String, $subject: String, $since: DateTime, $before: DateTime, $hasAttachment: Boolean, $first: Int, $offset: Int) {
    ListMailboxThreads(folderId: $folderId, mailboxId: $mailboxId, unread: $unread, flagged: $flagged, search: $search, from: $from, to: $to, subject: $subject, since: $since, before: $before, hasAttachment: $hasAttachment, first: $first, offset: $offset) {
      total
      threads {
        threadId count unread flagged participants itemIds hasDraft
        item {
          id folderId mailId uid seen flagged answered forwarded draft addedAt
          mail {
            id from fromName sender subject recipients receivedAt size kind status
            authenticationResults { spf { result } dkims { result } dmarc { result } spamFilter { score } }
          }
        }
      }
    }
  }`

const THREAD = `
  query ($itemId: String!) {
    GetMailboxThread(itemId: $itemId) {
      threadId subject truncated
      items {
        folderId folderName folderKind
        item {
          id folderId mailId uid seen flagged answered forwarded draft addedAt
          mail {
            id from fromName sender subject recipients receivedAt size kind status messageId
            authenticationResults { spf { result } dkims { result } dmarc { result } spamFilter { score } }
          }
        }
      }
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

const REPORT_JUNK = `
  mutation ($itemIds: [String!]!, $notJunk: Boolean) {
    ReportMailboxJunk(itemIds: $itemIds, notJunk: $notJunk)
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

const NOTHING_NARROWED: Narrowing = {
  everywhere: false,
  from: '',
  to: '',
  subject: '',
  since: '',
  before: '',
  attachment: 'any',
}

function narrowed(value: Narrowing): boolean {
  return (
    value.from !== '' ||
    value.to !== '' ||
    value.subject !== '' ||
    value.since !== '' ||
    value.before !== '' ||
    value.attachment !== 'any'
  )
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

// A toolbar button: an icon, its name in a tooltip, and nothing else on the
// screen. A mail toolbar is nine verbs, and nine words of them wrapped onto a
// second line on anything narrower than a laptop.
function IconAction({
  label,
  icon,
  onClick,
  disabled,
  className,
  // The state the action would undo — a flagged conversation, so the flag is
  // drawn as set rather than as something to do.
  active,
}: {
  label: string
  icon: React.ReactNode
  onClick: () => void
  disabled?: boolean
  className?: string
  active?: boolean
}) {
  return (
    <Tooltip label={label}>
      <button
        type="button"
        className={['icon-button', className, active ? 'active' : ''].filter(Boolean).join(' ')}
        aria-label={label}
        aria-pressed={active}
        disabled={disabled}
        onClick={onClick}
      >
        {icon}
      </button>
    </Tooltip>
  )
}

// Where to move what is selected: the folders, in a menu the button opens.
// A native select in a row of icons is a box with a word and an arrow in it,
// which is the one control on the row that says what it is twice.
function MoveToMenu({
  targets,
  onMove,
  disabled,
}: {
  targets: { folder: MailboxFolder; depth: number }[]
  onMove: (folderId: string) => void
  disabled?: boolean
}) {
  const { t } = useTranslation()
  return (
    <MenuButton
      label={t('mailbox.moveTo')}
      className="icon-button"
      icon={<MoveIcon size={16} />}
      render={(close) =>
        targets.map(({ folder: candidate, depth }) => (
          <button
            key={candidate.id}
            type="button"
            role="menuitem"
            disabled={disabled}
            style={{ paddingLeft: `${12 + depth * 12}px` }}
            onClick={() => {
              close()
              onMove(candidate.id)
            }}
          >
            {folderLabel(t, candidate)}
          </button>
        ))
      }
    />
  )
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

function Folder({ folder, folders, itemId }: { folder: MailboxFolder; folders: MailboxFolder[]; itemId?: string }) {
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
  const everywhere =
    starred || (appliedNarrowing.everywhere && (applied !== '' || narrowed(appliedNarrowing) || filter !== 'all'))
  const [threads, setThreads] = useState<MailboxThread[]>([])
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
    async (offset?: number) => {
      setLoading(true)
      try {
        const response = await graphql<{ ListMailboxThreads: MailboxThreadPage }>(THREADS, {
          ...variables,
          offset,
        })
        const page = response.ListMailboxThreads
        setThreads((previous) => {
          if (!offset) {
            return page.threads
          }
          // Paged by offset, a conversation answered or arrived since the
          // last page shifts the rest down one: the overlap is dropped
          // rather than shown twice.
          const shown = new Set(previous.map((thread) => thread.threadId))
          return [...previous, ...page.threads.filter((thread) => !shown.has(thread.threadId))]
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
    setThreads((previous) =>
      previous.map((thread) => {
        if (!thread.itemIds.some((id) => itemIds.includes(id))) {
          return thread
        }
        const next = { ...thread }
        if (itemIds.includes(thread.item.id)) {
          next.item = { ...thread.item, ...change }
        }
        // The counts the row shows are of the whole conversation, so a
        // message marked read changes them whichever message it was.
        const touched = thread.itemIds.filter((id) => itemIds.includes(id)).length
        if (change.seen === true) {
          next.unread = Math.max(0, thread.unread - touched)
        } else if (change.seen === false) {
          next.unread = Math.min(thread.count, thread.unread + touched)
        }
        if (change.flagged !== undefined) {
          next.flagged = change.flagged
        }
        return next
      }),
    )
  const remove = (itemIds: string[]) => {
    let gone = 0
    setThreads((previous) =>
      previous
        .map((thread) => {
          const left = thread.itemIds.filter((id) => !itemIds.includes(id))
          if (left.length === thread.itemIds.length) {
            return thread
          }
          if (left.length === 0 || itemIds.includes(thread.item.id)) {
            // The message the row showed is gone. Rather than guess which of
            // the rest to show, the row goes and the next load brings it back
            // if the conversation still has messages here.
            gone += 1
            return null
          }
          return { ...thread, itemIds: left, count: Math.max(1, thread.count - (thread.itemIds.length - left.length)) }
        })
        .filter((thread): thread is MailboxThread => thread !== null),
    )
    setTotal((previous) => Math.max(0, previous - gone))
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
  // Junk is a move and a lesson at once. Moving without teaching leaves the
  // next one from the same sender in the Inbox; teaching without moving
  // leaves the reader looking at what they have just called junk.
  const reportJunk = (itemIds: string[], notJunk: boolean) =>
    act(async () => {
      await graphql(REPORT_JUNK, { itemIds, notJunk })
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

  // A chosen conversation is all of its messages in this folder: starring,
  // moving or deleting one means the conversation, which is what the row is.
  //
  // Except where the list is not a folder. Starred and a search across the
  // mailbox gather a conversation's messages from everywhere it has any —
  // Sent, Drafts, Trash — and acting on all of those from a starred row
  // would archive your own replies and delete an unsent draft. There the row
  // stands for the message it shows, which is what it stood for before
  // conversations existed.
  const chosen = threads.filter((thread) => selected.has(thread.threadId))
  const chosenIds = everywhere ? chosen.map((thread) => thread.item.id) : chosen.flatMap((thread) => thread.itemIds)
  const archive = folderOfKind({ mailbox: undefined as never, folders, unread: 0 }, 'archive')
  const inTrash = folder.kind === 'trash'
  const inJunk = folder.kind === 'junk'
  const targets = folderRows(folders).filter(({ folder: candidate }) => candidate.id !== folder.id)
  // In Starred, "delete" means what it means in the message's own folder;
  // the server decides by the item, so nothing to do here but not to call
  // it "delete for good".

  const toggleAll = () =>
    setSelected((previous) =>
      previous.size === threads.length ? new Set() : new Set(threads.map((thread) => thread.threadId)),
    )

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
            <button
              type="button"
              className={filter === 'unread' ? 'active' : ''}
              onClick={() => setFilter(filter === 'unread' ? 'all' : 'unread')}
            >
              {t('mailbox.unreadOnly')}
            </button>
            {!starred && (
              <button
                type="button"
                className={filter === 'flagged' ? 'active' : ''}
                onClick={() => setFilter(filter === 'flagged' ? 'all' : 'flagged')}
              >
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
                <input
                  value={narrowing.from}
                  onChange={(event) => setNarrowing({ ...narrowing, from: event.target.value })}
                />
              </label>
              <label>
                <span>{t('mailbox.narrowTo')}</span>
                <input
                  value={narrowing.to}
                  onChange={(event) => setNarrowing({ ...narrowing, to: event.target.value })}
                />
              </label>
              <label>
                <span>{t('mailbox.narrowSubject')}</span>
                <input
                  value={narrowing.subject}
                  onChange={(event) => setNarrowing({ ...narrowing, subject: event.target.value })}
                />
              </label>
              <div className="mailbox-narrowing-row">
                <label>
                  <span>{t('mailbox.narrowSince')}</span>
                  <input
                    type="date"
                    value={narrowing.since}
                    onChange={(event) => setNarrowing({ ...narrowing, since: event.target.value })}
                  />
                </label>
                <label>
                  <span>{t('mailbox.narrowBefore')}</span>
                  <input
                    type="date"
                    value={narrowing.before}
                    onChange={(event) => setNarrowing({ ...narrowing, before: event.target.value })}
                  />
                </label>
              </div>
              <label>
                <span>{t('mailbox.narrowAttachment')}</span>
                <select
                  value={narrowing.attachment}
                  onChange={(event) =>
                    setNarrowing({ ...narrowing, attachment: event.target.value as Narrowing['attachment'] })
                  }
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
            checked={threads.length > 0 && selected.size === threads.length}
            onChange={toggleAll}
            disabled={threads.length === 0}
          />
          {chosen.length > 0 ? (
            <>
              <span className="muted">{t('mailbox.selected', { count: chosen.length })}</span>
              {/* Icons, with their names in the tooltip. Eight words across
                  the top of a list wrapped onto two lines on anything
                  narrower than a laptop, and every one of them is a verb a
                  mail program already has a picture for. */}
              {chosen.some((thread) => thread.unread > 0) ? (
                <IconAction
                  label={t('mailbox.markRead')}
                  icon={<MailOpenIcon size={16} />}
                  disabled={busy}
                  onClick={() => setFlags(chosenIds, { seen: true })}
                />
              ) : (
                <IconAction
                  label={t('mailbox.markUnread')}
                  icon={<MailIcon size={16} />}
                  disabled={busy}
                  onClick={() => setFlags(chosenIds, { seen: false })}
                />
              )}
              {chosen.some((item) => !item.flagged) ? (
                <IconAction
                  label={t('mailbox.flag')}
                  icon={<FlagIcon size={16} />}
                  disabled={busy}
                  onClick={() => setFlags(chosenIds, { flagged: true })}
                />
              ) : (
                <IconAction
                  label={t('mailbox.unflag')}
                  icon={<FlagIcon size={16} />}
                  active
                  disabled={busy}
                  onClick={() => setFlags(chosenIds, { flagged: false })}
                />
              )}
              {archive && folder.id !== archive.id && (
                <IconAction
                  label={t('mailbox.archive')}
                  icon={<ArchiveIcon size={16} />}
                  disabled={busy}
                  onClick={() => moveTo(chosenIds, archive.id)}
                />
              )}
              <IconAction
                label={inJunk ? t('mailbox.notJunk') : t('mailbox.reportJunk')}
                icon={<JunkIcon size={16} />}
                disabled={busy}
                onClick={() => reportJunk(chosenIds, inJunk)}
              />
              <MoveToMenu targets={targets} disabled={busy} onMove={(folderId) => moveTo(chosenIds, folderId)} />
              <IconAction
                label={inTrash ? t('mailbox.deleteForever') : t('mailbox.delete')}
                icon={<TrashIcon size={16} />}
                className="danger"
                disabled={busy}
                onClick={() => deleteItems(chosenIds)}
              />
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
          {threads.map((thread) => (
            <Row
              key={thread.threadId}
              thread={thread}
              folderName={
                everywhere && thread.item.folderId !== folder.id
                  ? folderLabelOf(folders, thread.item.folderId, t)
                  : undefined
              }
              active={thread.itemIds.includes(itemId ?? '')}
              selected={selected.has(thread.threadId)}
              onSelect={(on) =>
                setSelected((previous) => {
                  const next = new Set(previous)
                  if (on) {
                    next.add(thread.threadId)
                  } else {
                    next.delete(thread.threadId)
                  }
                  return next
                })
              }
              href={
                thread.item.draft
                  ? `/mailbox/compose?draft=${thread.item.id}`
                  : `/mailbox/${folder.id}/${thread.item.id}`
              }
              onOpen={() =>
                navigate(
                  thread.item.draft
                    ? `/mailbox/compose?draft=${thread.item.id}`
                    : `/mailbox/${folder.id}/${thread.item.id}`,
                )
              }
              onFlag={(on) => setFlags(everywhere ? [thread.item.id] : thread.itemIds, { flagged: on })}
            />
          ))}
          {!loading && threads.length === 0 && (
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
          <span>{loading ? t('common.loading') : t('mailbox.count', { shown: threads.length, total })}</span>
          {threads.length < total && !loading && (
            <button type="button" className="link" onClick={() => load(threads.length)}>
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
            onSeen={(itemIds, seen) => setFlags(itemIds, { seen })}
            onFlag={(itemIds, flagged) => setFlags(itemIds, { flagged })}
            onMove={(itemIds, target) => moveTo(itemIds, target)}
            onJunk={(itemIds, notJunk) => reportJunk(itemIds, notJunk)}
            onDelete={(itemIds) => deleteItems(itemIds)}
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
              setThreads([])
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
  thread,
  folderName,
  href,
  active,
  selected,
  onSelect,
  onOpen,
  onFlag,
}: {
  thread: MailboxThread
  folderName?: string
  href: string
  active: boolean
  selected: boolean
  onSelect: (on: boolean) => void
  onOpen: () => void
  onFlag: (on: boolean) => void
}) {
  const { t } = useTranslation()
  const item = thread.item
  const mail = item.mail
  // Who the row is about: everybody who has written, when more than one has.
  // A conversation of one reads as it always did.
  const who =
    thread.participants.length > 1
      ? thread.participants.join(', ')
      : thread.participants[0] || mail?.fromName || mail?.from || mail?.sender || t('mailbox.unknownSender')
  return (
    <li
      className={['mailbox-row', thread.unread > 0 ? 'unread' : '', active ? 'active' : ''].filter(Boolean).join(' ')}
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
        className={['mailbox-star', thread.flagged ? 'on' : ''].filter(Boolean).join(' ')}
        aria-label={thread.flagged ? t('mailbox.unflag') : t('mailbox.flag')}
        aria-pressed={thread.flagged}
        onClick={(event) => {
          event.stopPropagation()
          onFlag(!thread.flagged)
        }}
      >
        {thread.flagged ? '★' : '☆'}
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
        <div className="mailbox-row-from">
          {who}
          {thread.count > 1 && <span className="mailbox-row-count">{thread.count}</span>}
          {/* An answer begun and left. Worth saying in the list, because the
              conversation looks finished otherwise and the half-written reply
              is two folders away. */}
          {thread.hasDraft && <span className="mailbox-row-draft">{t('mailbox.draft')}</span>}
        </div>
        <div className="mailbox-row-subject">
          {folderName && <span className="mailbox-row-folder">{folderName}</span>}
          {mail?.subject || t('mailbox.noSubject')}
        </div>
      </Link>
      <div className="mailbox-row-when">
        {/* What the checks said, in the width of a character: the answer to
            "is this really from who it says" belongs where the message is
            listed, not only on the audit page. */}
        <VerdictMark mail={mail} />
        <RelativeTime value={mail?.receivedAt ?? item.addedAt} />
      </div>
    </li>
  )
}

// Reader shows the conversation a message belongs to, not the message alone.
//
// Newest first, because the last thing said is what somebody opening a
// conversation wants; read messages collapsed to a line, because a
// conversation of twenty is unreadable otherwise; and every message of it in
// this mailbox whatever folder it is in, because the answers are in Sent
// while the conversation is being read from the Inbox.
function Reader({
  itemId,
  folder,
  archive,
  targets,
  busy,
  onSeen,
  onFlag,
  onMove,
  onJunk,
  onDelete,
  onBack,
}: {
  itemId: string
  folder: MailboxFolder
  archive?: MailboxFolder
  targets: { folder: MailboxFolder; depth: number }[]
  busy: boolean
  onSeen: (itemIds: string[], seen: boolean) => void
  onFlag: (itemIds: string[], flagged: boolean) => void
  onMove: (itemIds: string[], folderId: string) => void
  onJunk: (itemIds: string[], notJunk: boolean) => void
  onDelete: (itemIds: string[]) => void
  onBack: () => void
}) {
  const { t } = useTranslation()
  const thread = useQuery(() => graphql<{ GetMailboxThread: MailboxThreadView }>(THREAD, { itemId }), [itemId], {
    refresh: false,
  })

  // Which messages are open. Decided once from what arrives — the newest,
  // anything unread, and the one that was clicked — and then it is the
  // reader's, so opening and closing sticks while they are on the page.
  const [open, setOpen] = useState<Set<string> | null>(null)
  // What has been marked read or unread here, so that neither flickers back
  // to what the query returned. Absent means "as it arrived".
  const [marks, setMarks] = useState<Record<string, boolean>>({})
  const [flags, setFlagState] = useState<Record<string, boolean>>({})
  // What is being written, if anything: which message it answers and how.
  // Above the newest message rather than below the whole conversation,
  // because that is where the answer will be once it is sent.
  const [writing, setWriting] = useState<{ kind: 'reply' | 'replyAll' | 'forward' | 'draft'; itemId: string } | null>(
    null,
  )
  // The draft what is being written has been saved as. Closing the composer
  // saves what was typed, so reopening it — Reply, then Reply to all — has to
  // continue that draft rather than start a second one of the same reply.
  const [draftId, setDraftId] = useState<string | null>(null)

  const view = thread.data?.GetMailboxThread
  const entries = view?.items ?? []

  useEffect(() => {
    if (!view || open !== null) {
      return
    }
    const wanted = new Set<string>()
    const readable = view.items.filter((entry) => !entry.item.draft)
    if (readable.length > 0) {
      wanted.add(readable[0].item.id)
    }
    for (const entry of readable) {
      if (!entry.item.seen || entry.item.id === itemId) {
        wanted.add(entry.item.id)
      }
    }
    setOpen(wanted)
    // Opening a conversation reads what it opens. Once: the reader can mark
    // one unread again afterwards and it stays that way.
    //
    // Only what is in the folder being read, or the message that was asked
    // for. Opening a conversation from the Inbox should not clear the unread
    // mark on a message of it sitting in Junk.
    const opening = view.items
      .filter(
        (entry) =>
          wanted.has(entry.item.id) && !entry.item.seen && (entry.folderId === folder.id || entry.item.id === itemId),
      )
      .map((entry) => entry.item.id)
    if (opening.length > 0) {
      setMarks((previous) => ({ ...previous, ...Object.fromEntries(opening.map((id) => [id, true])) }))
      onSeen(opening, true)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [view])

  if (thread.loading && !thread.data) {
    return <Loading />
  }
  if (thread.error) {
    return <ErrorMessage error={thread.error} />
  }
  if (!view || entries.length === 0) {
    return <p className="muted">{t('common.notFound')}</p>
  }

  // What an answer answers is the newest message of the conversation, not
  // the unsent one at the top of it: a draft is what you are writing, and
  // replying to your own half-written reply is not a thing anybody means.
  const newest = entries.find((entry) => !entry.item.draft) ?? entries[0]
  const draft = entries.find((entry) => entry.item.draft)
  const opened = open ?? new Set([newest.item.id])
  // What the actions act on: the conversation's messages in the folder being
  // read, since that is the row the reader came from. A message of it that
  // lives in Sent is not archived by archiving the conversation.
  const here = entries.filter((entry) => entry.folderId === folder.id).map((entry) => entry.item.id)
  // Read from Starred or from a search, there is no folder to scope to, so
  // the actions act on the message that was opened — not on every message of
  // the conversation wherever it happens to be filed.
  const acting = here.length > 0 ? here : [itemId]
  const seenOf = (entry: MailboxThreadItem) => marks[entry.item.id] ?? entry.item.seen
  const anyUnread = entries.some((entry) => !seenOf(entry))
  const anyFlagged = entries.some((entry) => flags[entry.item.id] ?? entry.item.flagged)
  const inTrash = folder.kind === 'trash'
  const toggle = (id: string) =>
    setOpen((previous) => {
      const next = new Set(previous ?? opened)
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
        <IconAction label={t('mailbox.backToList')} icon={<ArrowLeftIcon size={16} />} onClick={onBack} />
        {draft && (
          <button
            type="button"
            className="primary"
            disabled={Boolean(writing)}
            onClick={() => setWriting({ kind: 'draft', itemId: draft.item.id })}
          >
            {t('mailbox.editDraft')}
          </button>
        )}
        {!newest.item.draft && (
          <>
            {/* One answer at a time. Swapping a half-written reply for a
                forward would either throw away what was typed or leave a
                second draft of it behind, and neither is what the click
                meant: close it, and the draft is kept. */}
            <IconAction
              label={t('mailbox.reply')}
              icon={<ReplyIcon size={16} />}
              disabled={Boolean(writing)}
              onClick={() => setWriting({ kind: 'reply', itemId: newest.item.id })}
            />
            <IconAction
              label={t('mailbox.replyAll')}
              icon={<ReplyAllIcon size={16} />}
              disabled={Boolean(writing)}
              onClick={() => setWriting({ kind: 'replyAll', itemId: newest.item.id })}
            />
            <IconAction
              label={t('mailbox.forward')}
              icon={<ForwardIcon size={16} />}
              disabled={Boolean(writing)}
              onClick={() => setWriting({ kind: 'forward', itemId: newest.item.id })}
            />
          </>
        )}
        <IconAction
          label={anyUnread ? t('mailbox.markRead') : t('mailbox.markUnread')}
          icon={anyUnread ? <MailOpenIcon size={16} /> : <MailIcon size={16} />}
          disabled={busy}
          onClick={() => {
            // Anything unread and the button says "Mark read", so that is
            // what it does; everything read and it marks unread.
            const seen = anyUnread
            setMarks((previous) => ({ ...previous, ...Object.fromEntries(acting.map((id) => [id, seen])) }))
            onSeen(acting, seen)
          }}
        />
        <IconAction
          label={anyFlagged ? t('mailbox.unflag') : t('mailbox.flag')}
          icon={<FlagIcon size={16} />}
          active={anyFlagged}
          disabled={busy}
          onClick={() => {
            const next = !anyFlagged
            // The same messages the server is told about, so the star does
            // not show the answer to a question that was never asked.
            setFlagState((previous) => ({ ...previous, ...Object.fromEntries(acting.map((id) => [id, next])) }))
            onFlag(acting, next)
          }}
        />
        {archive && folder.id !== archive.id && (
          <IconAction
            label={t('mailbox.archive')}
            icon={<ArchiveIcon size={16} />}
            disabled={busy}
            onClick={() => onMove(acting, archive.id)}
          />
        )}
        <IconAction
          label={folder.kind === 'junk' ? t('mailbox.notJunk') : t('mailbox.reportJunk')}
          icon={<JunkIcon size={16} />}
          disabled={busy}
          onClick={() => onJunk(acting, folder.kind === 'junk')}
        />
        <MoveToMenu targets={targets} disabled={busy} onMove={(folderId) => onMove(acting, folderId)} />
        <IconAction
          label={inTrash ? t('mailbox.deleteForever') : t('mailbox.delete')}
          icon={<TrashIcon size={16} />}
          className="danger"
          disabled={busy}
          onClick={() => onDelete(acting)}
        />
      </div>

      <div className="mailbox-pane-head">
        <h2>{view.subject || t('mailbox.noSubject')}</h2>
        {entries.length > 1 && (
          <p className="muted mailbox-thread-count">
            {t('mailbox.threadCount', { count: entries.length })}
            {/* A conversation longer than the server returns is shown from
                its newest end; saying so is the difference between a long
                conversation and a conversation that lost its beginning. */}
            {view.truncated ? ` · ${t('mailbox.threadTruncated', { count: entries.length })}` : ''}
          </p>
        )}
      </div>

      {writing && (
        <div className="mailbox-thread-compose">
          <div className="page-actions page-actions-end">
            {/* Closing is not discarding: what was typed is saved as a draft
                on the way out, and picking Reply again continues it. */}
            <button type="button" onClick={() => setWriting(null)}>
              {t('mailbox.closeReply')}
            </button>
          </div>
          <MailboxComposer
            key={`${writing.kind}-${writing.itemId}`}
            replyTo={writing.kind === 'forward' || writing.kind === 'draft' ? null : writing.itemId}
            replyAll={writing.kind === 'replyAll'}
            forwardOf={writing.kind === 'forward' ? writing.itemId : null}
            draftOf={writing.kind === 'draft' ? writing.itemId : draftId}
            onDraft={setDraftId}
            onSent={() => {
              setWriting(null)
              setDraftId(null)
              void thread.reload()
            }}
            onCancel={() => {
              setWriting(null)
              setDraftId(null)
            }}
          />
        </div>
      )}

      <ol className="mailbox-thread">
        {entries
          // The draft being written is the composer above, not a row as
          // well: the same half-written answer twice on one screen.
          .filter((entry) => !(writing?.kind === 'draft' && writing.itemId === entry.item.id))
          .map((entry) => (
            <ThreadMessage
              key={entry.item.id}
              entry={entry}
              folderId={folder.id}
              seen={seenOf(entry)}
              open={opened.has(entry.item.id)}
              onToggle={() => {
                // A draft is not a message to read, it is an answer to go back
                // to. Clicking it opens what was written where it was written.
                if (entry.item.draft) {
                  setWriting({ kind: 'draft', itemId: entry.item.id })
                  return
                }
                const opening = !opened.has(entry.item.id)
                toggle(entry.item.id)
                if (opening && !seenOf(entry)) {
                  setMarks((previous) => ({ ...previous, [entry.item.id]: true }))
                  onSeen([entry.item.id], true)
                }
              }}
            />
          ))}
      </ol>
    </>
  )
}

// ThreadMessage is one message of a conversation: a line when it is closed,
// the message itself when it is open. Its body is fetched only once it is
// opened, so a conversation of twenty costs one request rather than twenty.
function ThreadMessage({
  entry,
  folderId,
  seen,
  open,
  onToggle,
}: {
  entry: MailboxThreadItem
  folderId: string
  // Read as the page has it, which is what arrived plus what opening it has
  // marked since — so a message does not stay bold after it was just read.
  seen: boolean
  open: boolean
  onToggle: () => void
}) {
  const { t } = useTranslation()
  const item = entry.item
  const mail = item.mail
  // Where the message's own menu — download, headers, theme — goes: the end
  // of the line that names the message, not a row of its own above it. A
  // conversation of six would otherwise carry six rows holding one button.
  const [menuSlot, setMenuSlot] = useState<HTMLElement | null>(null)
  const content = useQuery(
    () =>
      open && mail ? graphql<{ GetMailContent: MailContent }>(CONTENT, { mailId: mail.id }) : Promise.resolve(null),
    [open, mail?.id],
    { refresh: false },
  )
  const who = mail?.fromName || mail?.from || mail?.sender || t('mailbox.unknownSender')
  const verdict = verdictOf(mail, t)
  // From, To, Received and what the checks said, when somebody asks for them.
  const [details, setDetails] = useState(false)

  return (
    <li className={['mailbox-message', open ? 'open' : '', seen ? '' : 'unread'].filter(Boolean).join(' ')}>
      <div className="mailbox-message-head">
        {/* The whole line opens and closes it, so there is nothing to aim
            at — except the menu at its end, which is a button of its own and
            so sits outside this one rather than inside it. */}
        <button type="button" className="mailbox-message-summary" aria-expanded={open} onClick={onToggle}>
          <Tooltip label={mail?.from || mail?.sender || ''}>
            <span className="mailbox-message-who">{who}</span>
          </Tooltip>
          {/* Where it is, when that is not where the conversation is being
              read: your own answer is in Sent, and saying so is the difference
              between a conversation and a list. */}
          {entry.folderId !== folderId && entry.folderName && (
            <span className="mailbox-row-folder">{entry.folderName}</span>
          )}
          {!open && <span className="mailbox-message-subject">{mail?.subject}</span>}
          <span className="mailbox-message-when">
            {/* Whether the message is really from who it says: the one thing
                about it worth seeing without asking. Everything else the
                checks found is behind "show details" in the menu. */}
            <VerdictMark mail={mail} />
            <RelativeTime value={mail?.receivedAt ?? item.addedAt} />
          </span>
        </button>
        {open && <span className="mailbox-message-menu" ref={setMenuSlot} />}
      </div>

      {open && (
        <div className="mailbox-message-body">
          {mail ? (
            <>
              {details && (
                <dl className="mailbox-pane-meta">
                  <dt>{t('mailbox.from')}</dt>
                  <dd>
                    {mail.fromName
                      ? `${mail.fromName} <${mail.from || mail.sender}>`
                      : mail.from || mail.sender || t('mailbox.unknownSender')}
                  </dd>
                  <dt>{t('mailbox.to')}</dt>
                  <dd>{(mail.recipients ?? []).join(', ')}</dd>
                  <dt>{t('mail.received')}</dt>
                  <dd>{formatTime(mail.receivedAt)}</dd>
                  {verdict && (
                    <>
                      <dt>{t('mailDetail.authentication')}</dt>
                      <dd className={verdict.tone ? `verdict-detail ${verdict.tone}` : 'verdict-detail'}>
                        {verdict.detail}
                      </dd>
                    </>
                  )}
                </dl>
              )}
              {content.loading && !content.data ? (
                <Loading />
              ) : content.error ? (
                <ErrorMessage error={content.error} />
              ) : (
                <MessageContent
                  mailId={mail.id}
                  content={content.data?.GetMailContent}
                  mode="mailbox"
                  menuContainer={menuSlot}
                  menuExtra={(close) => (
                    <button
                      type="button"
                      role="menuitem"
                      onClick={() => {
                        close()
                        setDetails((previous) => !previous)
                      }}
                    >
                      {t(details ? 'mailbox.hideDetails' : 'mailbox.showDetails')}
                    </button>
                  )}
                />
              )}
            </>
          ) : (
            <p className="muted">{t('mailbox.messageGone')}</p>
          )}
        </div>
      )}
    </li>
  )
}

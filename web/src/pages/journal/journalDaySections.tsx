import { useMemo } from 'react'
import { Link } from 'react-router-dom'

import { graphql } from '../../api'
import { CodeIcon, DocumentIcon, ExternalIcon, EyeOffIcon, TrashIcon } from '../../components/icons'
import { useToast } from '../../components/toast'
import { Tooltip } from '../../components/tooltip'
import { useTranslation } from '../../i18n/i18n'
import { photoSmallPreviewPath } from '../../photos/api'
import {
  JournalDay,
  JournalLink,
  JournalWorkProject,
  PROJECT_TOPIC_LIMIT,
  REMOVE_JOURNAL_LINK,
  amountText,
  clockText,
  dayOf,
  durationText,
  momentPaths,
  webAddressOf,
} from './journalApi'
import { Disclosure, ShowAllButton, useRowLimit } from './journalCollapse'
import { JournalCard } from './journalParts'
import { RecordKindIcon } from './journalNarrative'
import { RecordLink } from './journalOpen'
import { isDashboardPath } from './journalPaths'

// PROJECT_LIMIT is how many projects a day shows before Show all.
const PROJECT_LIMIT = 5

// The sections down the left of a day: what the person did and what came in,
// each shown only when the day has something for it, and each linking to the
// thing itself. Everything is read live from its source by the server, so a
// photo deleted since is simply not here.

type Section = { day: JournalDay; onChanged: () => Promise<void> }

// useDayWords gives the sections the clock of the zone the day was lived in
// and the paths the server gave for the day's records.
function useDayWords(day: JournalDay) {
  const { t, plural, language } = useTranslation()
  const paths = useMemo(() => momentPaths(day.moments), [day.moments])
  return {
    t,
    plural,
    language,
    clock: (value: string | null | undefined) => clockText(value, day.timeZone, language),
    // The page of a record the day's moments named, if they did.
    pathOf: (recordKind: string, recordId: string): string | undefined => paths.get(`${recordKind}:${recordId}`),
  }
}

// The agent's choices by the record they chose, and the ones the person hid:
// a hidden choice is not a choice any more as far as the page goes, and is
// kept only so the agent does not choose it again.
function useChoices(day: JournalDay) {
  return useMemo(() => {
    const found = new Map<string, JournalLink>()
    const hidden = new Set<string>()
    for (const choice of day.choices) {
      const key = `${choice.record.recordKind}:${choice.record.recordId}`
      if (choice.isHidden) hidden.add(key)
      else found.set(key, choice)
    }
    return { found, hidden }
  }, [day.choices])
}

function HideChoiceButton({
  link,
  name,
  onChanged,
}: {
  link: JournalLink
  name: string
  onChanged: () => Promise<void>
}) {
  const { t } = useTranslation()
  const toast = useToast()
  return (
    <Tooltip label={t('journal.hideChoice')}>
      <button
        type="button"
        className="icon-action"
        aria-label={`${t('journal.hideChoice')}: ${name}`}
        onClick={async () => {
          try {
            await graphql(REMOVE_JOURNAL_LINK, { linkId: link.linkId })
            toast.done(t('journal.choiceHidden'))
            await onChanged()
          } catch (caught) {
            toast.failure(caught, t('journal.choiceFailed'))
          }
        }}
      >
        <EyeOffIcon size={16} />
      </button>
    </Tooltip>
  )
}

// Row is one line of a section: a mark, what it is, where it came from and a
// word on why, the time, and what can be done to it.
function Row({
  to,
  icon,
  title,
  where,
  detail,
  reason,
  time,
  action,
}: {
  to?: string
  icon: React.ReactNode
  title: string
  where?: string
  detail?: string
  reason?: string
  time?: string
  action?: React.ReactNode
}) {
  return (
    <div className="journal-row">
      <span className="journal-row-icon">{icon}</span>
      <span className="journal-row-body">
        <span className="journal-row-title">
          {to ? <RecordLink to={to}>{title}</RecordLink> : <span>{title}</span>}
          {where && <span className="muted journal-row-where">{where}</span>}
        </span>
        {detail && <span className="muted journal-row-detail">{detail}</span>}
        {reason && <span className="journal-reason">{reason}</span>}
      </span>
      {time && <span className="muted journal-row-time">{time}</span>}
      {action}
    </div>
  )
}

// The coding assistants by their product names, which are the same in every
// language.
const ASSISTANTS: Record<string, string> = {
  'claude-code': 'Claude Code',
  codex: 'Codex',
}

// The folder a coding session ran in is shown by its last part: the whole
// path says more about the machine than about the work.
function folderName(directory: string): string {
  const parts = directory.split('/').filter(Boolean)
  return parts[parts.length - 1] ?? directory
}

// PinsSection is what the person put on the day by hand.
export function PinsSection({ day, onChanged }: Section) {
  const { t } = useDayWords(day)
  const toast = useToast()
  const limit = useRowLimit(day.pins.length)
  if (day.pins.length === 0) return null
  return (
    <JournalCard title={t('journal.pinned')} description={t('journal.pinnedHint')} collapseId="pins" isFlush>
      {day.pins.slice(0, limit.shownCount).map((pin) => (
        <Row
          key={pin.linkId}
          to={
            pin.record.isUnavailable || !isDashboardPath(pin.record.dashboardPath)
              ? undefined
              : pin.record.dashboardPath
          }
          icon={<RecordKindIcon recordKind={pin.record.recordKind} size={16} />}
          title={pin.record.isUnavailable ? t('journal.pinGone') : pin.record.title}
          detail={pin.linkReason}
          action={
            <Tooltip label={t('common.remove')}>
              <button
                type="button"
                className="icon-action danger"
                aria-label={`${t('common.remove')}: ${pin.record.title || t('journal.pinned')}`}
                onClick={async () => {
                  try {
                    await graphql(REMOVE_JOURNAL_LINK, { linkId: pin.linkId })
                    toast.done(t('journal.pinRemoved'))
                    await onChanged()
                  } catch (caught) {
                    toast.failure(caught, t('journal.choiceFailed'))
                  }
                }}
              >
                <TrashIcon size={16} />
              </button>
            </Tooltip>
          }
        />
      ))}
      <ShowAllButton limit={limit} />
    </JournalCard>
  )
}

// The documents and work messages of the day split into the ones the agent
// chose (and the person did not hide), which Work shows with why, and the
// rest, which are routine and counted.
function useWorkDocuments(day: JournalDay) {
  const choices = useChoices(day)
  const isChosenShown = (document: JournalDay['documents'][number]) =>
    document.isChosen && !choices.hidden.has(`agent_document:${document.documentId}`)
  const chosen = day.documents.filter(isChosenShown)
  const others = day.documents.filter((document) => !isChosenShown(document))
  return { choices, chosen, others, leftOutCount: Math.max(day.documentTotal - chosen.length, others.length) }
}

// How long a project took and when, as a line: "2 h 10 min, 9:20 AM to 5:20
// PM". The span of the clock says when it was done, the duration how much of
// that was work.
function projectTimeText(
  project: JournalWorkProject,
  clock: (value: string | null | undefined) => string,
  t: ReturnType<typeof useDayWords>['t'],
): string {
  const first = clock(project.firstAt)
  const last = clock(project.lastAt)
  const range = first && last && first !== last ? t('journal.timeRange', { start: first, end: last }) : first
  return [project.activeMinutes > 0 ? durationText(project.activeMinutes, t) : '', range].filter(Boolean).join(', ')
}

// ProjectCard is what one project was in a day: its name, how long, what the
// agent wrote was done, what it was about and what was sent for review.
function ProjectCard({ project, day }: { project: JournalWorkProject; day: JournalDay }) {
  const { t, clock } = useDayWords(day)
  const topics = project.topics.filter((topic) => topic.trim() !== '')
  const shownTopics = topics.slice(0, PROJECT_TOPIC_LIMIT)
  const shippedLimit = useRowLimit(project.shipped.length)
  const time = projectTimeText(project, clock, t)
  return (
    <article className="journal-project">
      <h4 className="journal-project-name">{project.name}</h4>
      {time && <div className="muted journal-project-time">{time}</div>}
      {project.summary && <p className="journal-project-summary">{project.summary}</p>}
      {shownTopics.length > 0 && (
        <ul className="journal-project-topics">
          {shownTopics.map((topic) => (
            <li key={topic}>{topic}</li>
          ))}
          {topics.length > shownTopics.length && (
            <li className="muted">{t('journal.topicsMore', { count: topics.length - shownTopics.length })}</li>
          )}
        </ul>
      )}
      {project.shipped.length > 0 && (
        <div className="journal-shipped">
          <div className="muted journal-shipped-label">{t('journal.shippedRequests')}</div>
          <ul>
            {project.shipped.slice(0, shippedLimit.shownCount).map((request) => {
              const address = webAddressOf(request.address)
              return (
                <li key={`${request.documentId}:${request.address}`}>
                  {address ? (
                    <a href={address} target="_blank" rel="noopener noreferrer">
                      <span>{request.title}</span>
                      <ExternalIcon size={12} />
                    </a>
                  ) : (
                    <span>{request.title}</span>
                  )}
                  {request.isDraft && <span className="tag">{t('journal.draftTag')}</span>}
                </li>
              )
            })}
          </ul>
          <ShowAllButton limit={shippedLimit} />
        </div>
      )}
    </article>
  )
}

// WorkSection is what the day's work was, one card for each project, and
// under them, folded, the coding sessions and the documents the agent chose
// with why. The documents it did not choose are the next section.
export function WorkSection({ day, onChanged }: Section) {
  const { t, plural, clock, pathOf } = useDayWords(day)
  const { choices, chosen } = useWorkDocuments(day)
  const detailRows: React.ReactNode[] = [
    ...day.codingSessions.map((session) => (
      <Row
        key={session.documentId}
        to={pathOf('agent_document', session.documentId)}
        icon={<CodeIcon size={16} />}
        title={session.title}
        where={[
          folderName(session.directory),
          ASSISTANTS[session.assistant] ?? session.assistant,
          session.stretchCount > 1 ? t('journal.stretches', { count: session.stretchCount }) : '',
        ]
          .filter(Boolean)
          .join(', ')}
        detail={session.linkReason}
        time={
          session.endedAt
            ? t('journal.timeRange', { start: clock(session.startedAt), end: clock(session.endedAt) })
            : clock(session.startedAt)
        }
      />
    )),
    ...chosen.map((document) => {
      const choice = choices.found.get(`agent_document:${document.documentId}`)
      return (
        <Row
          key={document.documentId}
          to={pathOf('agent_document', document.documentId)}
          icon={<DocumentIcon size={16} />}
          title={document.title}
          where={document.sourceName}
          reason={
            document.linkReason ? t('journal.chosenBecause', { reason: document.linkReason }) : t('journal.chosen')
          }
          time={clock(document.happenedAt)}
          action={choice && <HideChoiceButton link={choice} name={document.title} onChanged={onChanged} />}
        />
      )
    }),
  ]
  const detailLimit = useRowLimit(detailRows.length)
  // The projects come most worked on first, so the first few say what the
  // day's work was; the rest wait behind Show all.
  const projectLimit = useRowLimit(day.workProjects.length, PROJECT_LIMIT)
  if (day.workProjects.length === 0 && detailRows.length === 0) return null
  const summary = [
    day.facts.codingMinutes > 0 ? durationText(day.facts.codingMinutes, t) : '',
    day.workProjects.length > 0
      ? plural(day.workProjects.length, { one: 'journal.projectsOne', other: 'journal.projectsOther' })
      : '',
  ]
    .filter(Boolean)
    .join(', ')
  return (
    <JournalCard title={t('journal.work')} description={summary || undefined} collapseId="work">
      {day.workProjects.length > 0 && (
        <div className="journal-projects">
          {day.workProjects.slice(0, projectLimit.shownCount).map((project) => (
            <ProjectCard key={`${project.name}:${project.documentId}`} project={project} day={day} />
          ))}
          <ShowAllButton limit={projectLimit} />
        </div>
      )}
      {detailRows.length > 0 && (
        <Disclosure
          id="work-details"
          label={t('journal.workDetails')}
          isCollapsedByDefault={day.workProjects.length > 0}
        >
          <div className="journal-details">
            {detailRows.slice(0, detailLimit.shownCount)}
            <ShowAllButton limit={detailLimit} />
          </div>
        </Disclosure>
      )}
    </JournalCard>
  )
}

// WorkChatSection is the work chat and documents the agent left out as
// routine, counted and folded away, for the day that wants one looked at.
export function WorkChatSection({ day }: { day: JournalDay }) {
  const { t, plural, clock, pathOf } = useDayWords(day)
  const { others, leftOutCount } = useWorkDocuments(day)
  const limit = useRowLimit(others.length)
  if (leftOutCount === 0) return null
  return (
    <JournalCard
      title={t('journal.workChat')}
      description={plural(leftOutCount, { one: 'journal.workLeftOutOne', other: 'journal.workLeftOutOther' })}
      collapseId="work-chat"
      isCollapsedByDefault
      isFlush
    >
      {others.length > 0 ? (
        <>
          {others.slice(0, limit.shownCount).map((document) => (
            <Row
              key={document.documentId}
              to={pathOf('agent_document', document.documentId)}
              icon={<DocumentIcon size={16} />}
              title={document.title}
              where={document.sourceName}
              time={clock(document.happenedAt)}
            />
          ))}
          <ShowAllButton limit={limit} />
        </>
      ) : null}
    </JournalCard>
  )
}

// MailSection is the mail the agent chose and the mail that is flagged as
// important, each with a word on why, and the way to all of the day's mail.
export function MailSection({ day, onChanged }: Section) {
  const { t, plural, clock } = useDayWords(day)
  const choices = useChoices(day)
  const mattered = day.mail.filter(
    (message) => (message.isChosen && !choices.hidden.has(`mail:${message.itemId}`)) || message.isImportant,
  )
  const limit = useRowLimit(mattered.length)
  if (mattered.length === 0) return null
  const received = day.mail.filter((message) => !message.isSent).length
  const total = Math.max(day.mailTotal, received)
  const search = new URLSearchParams({ everywhere: 'true', since: dayOf(day.day), before: dayOf(day.day) })
  return (
    <JournalCard
      title={t('journal.mailMattered')}
      description={t('journal.mailMatteredHint', { shown: mattered.length, total })}
      action={
        <Link className="button-quiet" to={`/mailbox?${search.toString()}`}>
          {plural(total, { one: 'journal.mailAllOne', other: 'journal.mailAllOther' })}
        </Link>
      }
      collapseId="mail"
      isFlush
    >
      {mattered.slice(0, limit.shownCount).map((message) => {
        const choice = choices.found.get(`mail:${message.itemId}`)
        const sender = message.fromName || t('journal.unknownSender')
        return (
          <div className="journal-row" key={message.itemId}>
            <span className="journal-avatar" aria-hidden="true">
              {Array.from(sender)[0]?.toUpperCase()}
            </span>
            <span className="journal-row-body">
              <span className="journal-row-title">
                <strong>{sender}</strong>
                <Link to={`/mailbox/starred/${encodeURIComponent(message.itemId)}`}>{message.subject}</Link>
                {message.repeatCount > 1 && (
                  <span className="muted journal-row-where">
                    {t('journal.repeatCount', { count: message.repeatCount })}
                  </span>
                )}
              </span>
              <span className="journal-reason">
                {message.isChosen && message.linkReason ? message.linkReason : t('journal.mailImportant')}
              </span>
            </span>
            <span className="muted journal-row-time">{clock(message.receivedAt)}</span>
            {choice && <HideChoiceButton link={choice} name={message.subject} onChanged={onChanged} />}
          </div>
        )
      })}
      <ShowAllButton limit={limit} />
    </JournalCard>
  )
}

// PhotosSection is a grid of the day's photos, each opening in a lightbox
// over the day (see PhotoLightbox), which moves through them.
export function PhotosSection({ day }: { day: JournalDay }) {
  const { t, plural, clock } = useDayWords(day)
  if (day.photos.length === 0) return null
  const total = Math.max(day.photoTotal, day.photos.length)
  // The place most of the photos were taken at, said only when it is more
  // than a coincidence of two.
  const counts = new Map<string, number>()
  for (const photo of day.photos) {
    if (photo.placeName) counts.set(photo.placeName, (counts.get(photo.placeName) ?? 0) + 1)
  }
  const [mostPlace, mostCount] = [...counts.entries()].sort((first, second) => second[1] - first[1])[0] ?? ['', 0]
  const taken = plural(total, { one: 'journal.photosTakenOne', other: 'journal.photosTakenOther' })
  return (
    <JournalCard
      title={t('journal.photos')}
      description={
        mostCount >= 2 ? `${taken}, ${t('journal.photosAtPlace', { count: mostCount, place: mostPlace })}` : taken
      }
      action={
        <Link className="button-quiet" to="/photos">
          {t('journal.openInPhotos')}
        </Link>
      }
      collapseId="photos"
    >
      <div className="journal-photo-grid">
        {day.photos.map((photo) => (
          <RecordLink
            key={photo.photoId}
            className="journal-photo"
            to={`/photos/${encodeURIComponent(photo.photoId)}`}
            label={t(photo.isVideo ? 'journal.videoAt' : 'journal.photoAt', { time: clock(photo.takenAt) })}
          >
            <img src={photoSmallPreviewPath(photo.photoId)} alt="" loading="lazy" draggable={false} />
            <span className="journal-photo-time">{clock(photo.takenAt)}</span>
          </RecordLink>
        ))}
      </div>
      {total > day.photos.length && (
        <p className="muted journal-photo-more">{t('journal.photosMore', { shown: day.photos.length, total })}</p>
      )}
    </JournalCard>
  )
}

// RecordingsSection is the meetings and memos recorded that day.
export function RecordingsSection({ day }: { day: JournalDay }) {
  const { t, clock } = useDayWords(day)
  const limit = useRowLimit(day.recordings.length)
  if (day.recordings.length === 0) return null
  return (
    <JournalCard title={t('journal.recordings')} collapseId="recordings" isFlush>
      {day.recordings.slice(0, limit.shownCount).map((recording) => (
        <div className="journal-recording" key={recording.recordingId}>
          <div className="journal-row-title">
            <Link to={`/recordings/${encodeURIComponent(recording.recordingId)}`}>
              {recording.title || t('recordings.untitled')}
            </Link>
            <span className="muted journal-row-time">
              {[
                clock(recording.recordedAt),
                recording.durationMS > 0 ? durationText(recording.durationMS / 60000, t) : '',
              ]
                .filter(Boolean)
                .join(', ')}
            </span>
          </div>
          {recording.summary && <p className="muted journal-recording-summary">{recording.summary}</p>}
        </div>
      ))}
      <ShowAllButton limit={limit} />
    </JournalCard>
  )
}

// ConversationsSection is the conversations with TeaNode that day and the
// chats with people. A conversation opens in the agent's drawer, over the
// day, and the person's main one is named in their language.
export function ConversationsSection({ day }: { day: JournalDay }) {
  const { t, plural, clock } = useDayWords(day)
  const rows = [
    ...day.conversations.map((conversation) => (
      <Row
        key={conversation.conversationId}
        to={`/chat?conversation=${encodeURIComponent(conversation.conversationId)}`}
        icon={<RecordKindIcon recordKind="conversation_message" size={16} />}
        title={conversation.isMain ? t('journal.mainChat') : conversation.title || t('journal.untitledConversation')}
        detail={plural(conversation.messageCount, {
          one: 'journal.chatMessagesOne',
          other: 'journal.chatMessagesOther',
        })}
        time={clock(conversation.firstAt)}
      />
    )),
    ...day.chats.map((chat) => (
      <Row
        key={chat.chatId}
        icon={<RecordKindIcon recordKind="chat_message" size={16} />}
        title={chat.chatName}
        detail={plural(chat.messageCount, { one: 'journal.chatMessagesOne', other: 'journal.chatMessagesOther' })}
        time={clock(chat.firstAt)}
      />
    )),
  ]
  const limit = useRowLimit(rows.length)
  if (rows.length === 0) return null
  return (
    <JournalCard
      title={t('journal.conversations')}
      description={t('journal.conversationsHint')}
      collapseId="conversations"
      isFlush
    >
      {rows.slice(0, limit.shownCount)}
      <ShowAllButton limit={limit} />
    </JournalCard>
  )
}

// MoneySection is the day's transactions as a table; the rows line up and
// the alignment is the point, and on a phone it scrolls on its own.
export function MoneySection({ day }: { day: JournalDay }) {
  const { t, plural, language, clock } = useDayWords(day)
  const limit = useRowLimit(day.transactions.length)
  if (day.transactions.length === 0) return null
  const spent = day.facts.spentMinorUnits
  return (
    <JournalCard
      title={t('journal.money')}
      description={
        spent > 0
          ? t('journal.moneyHintSpent', {
              transactions: plural(day.transactions.length, {
                one: 'journal.transactionsOne',
                other: 'journal.transactionsOther',
              }),
              spent: amountText(spent, day.facts.spentCurrency, language),
            })
          : plural(day.transactions.length, { one: 'journal.transactionsOne', other: 'journal.transactionsOther' })
      }
      action={
        <Link className="button-quiet" to="/finance/transactions">
          {t('journal.openInFinance')}
        </Link>
      }
      collapseId="money"
    >
      <div className="journal-table-scroll">
        <table className="journal-table">
          <thead>
            <tr>
              <th>{t('journal.moneyTime')}</th>
              <th>{t('journal.moneyWhere')}</th>
              <th>{t('journal.moneyAccount')}</th>
              <th className="journal-number">{t('journal.moneyAmount')}</th>
            </tr>
          </thead>
          <tbody>
            {day.transactions.slice(0, limit.shownCount).map((transaction) => (
              <tr key={transaction.transactionId}>
                <td className="muted">{clock(transaction.transactedAt)}</td>
                <td>{transaction.description}</td>
                <td className="muted">{transaction.accountName}</td>
                <td className="journal-number">
                  {transaction.amountMinorUnits < 0
                    ? amountText(-transaction.amountMinorUnits, transaction.currency, language)
                    : `+${amountText(transaction.amountMinorUnits, transaction.currency, language)}`}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <ShowAllButton limit={limit} />
    </JournalCard>
  )
}

// ShoppingSection is the trips to the shops, with how much of the list was
// checked off.
export function ShoppingSection({ day }: { day: JournalDay }) {
  const { t, clock } = useDayWords(day)
  const limit = useRowLimit(day.shoppingTrips.length)
  if (day.shoppingTrips.length === 0) return null
  return (
    <JournalCard title={t('journal.shopping')} collapseId="shopping" isFlush>
      {day.shoppingTrips.slice(0, limit.shownCount).map((trip) => (
        <Row
          key={trip.tripId}
          icon={<RecordKindIcon recordKind="shopping_trip" size={16} />}
          title={trip.listNames.join(', ') || t('journal.shopping')}
          detail={t('journal.shoppingChecked', { bought: trip.boughtCount, count: trip.itemCount })}
          time={clock(trip.startedAt)}
        />
      ))}
      <ShowAllButton limit={limit} />
    </JournalCard>
  )
}

// KnowledgeSection is the pages of the agent's memory with a fact about
// that day: one line saying how many, and the list behind its chevron.
export function KnowledgeSection({ day }: { day: JournalDay }) {
  const { t, plural } = useDayWords(day)
  const limit = useRowLimit(day.memoryPages.length)
  if (day.memoryPages.length === 0) return null
  return (
    <JournalCard
      title={t('journal.knowledge')}
      description={plural(day.memoryPages.length, {
        one: 'journal.knowledgeSummaryOne',
        other: 'journal.knowledgeSummaryOther',
      })}
      collapseId="knowledge"
      isCollapsedByDefault
      isFlush
    >
      {day.memoryPages.slice(0, limit.shownCount).map((page) => (
        <Row
          key={page.nodeId}
          to={`/knowledge/${page.path}`}
          icon={<DocumentIcon size={16} />}
          title={page.title}
          detail={`${page.path}, ${plural(page.factCount, { one: 'journal.factsOne', other: 'journal.factsOther' })}`}
        />
      ))}
      <ShowAllButton limit={limit} />
    </JournalCard>
  )
}

export { Row as JournalRow }

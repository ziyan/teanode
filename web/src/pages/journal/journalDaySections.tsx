import { useMemo } from 'react'
import { Link } from 'react-router-dom'

import { graphql } from '../../api'
import { CodeIcon, DocumentIcon, EyeOffIcon, TrashIcon } from '../../components/icons'
import { useToast } from '../../components/toast'
import { Tooltip } from '../../components/tooltip'
import { useTranslation } from '../../i18n/i18n'
import { photoSmallPreviewPath } from '../../photos/api'
import {
  JournalDay,
  JournalLink,
  REMOVE_JOURNAL_LINK,
  amountText,
  clockText,
  dayOf,
  durationText,
  momentPaths,
} from './journalApi'
import { JournalCard, useAddressValue } from './journalParts'
import { RecordKindIcon } from './journalNarrative'
import { isDashboardPath } from './journalPaths'

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
          {to ? <Link to={to}>{title}</Link> : <span>{title}</span>}
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
  if (day.pins.length === 0) return null
  return (
    <JournalCard title={t('journal.pinned')} description={t('journal.pinnedHint')} isFlush>
      {day.pins.map((pin) => (
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
    </JournalCard>
  )
}

// WorkSection is the coding sessions, then the documents and work messages
// the agent chose with why. The rest are counted, and listed on request.
export function WorkSection({ day, onChanged }: Section) {
  const { t, plural, clock, pathOf } = useDayWords(day)
  const choices = useChoices(day)
  const [routine, setRoutine] = useAddressValue('routine')
  const isRoutineShown = routine === 'shown'
  // A document the agent chose and the person has not hidden.
  const isChosenShown = (document: (typeof day.documents)[number]) =>
    document.isChosen && !choices.hidden.has(`agent_document:${document.documentId}`)
  const shown = day.documents.filter(isChosenShown)
  const others = day.documents.filter((document) => !isChosenShown(document))
  const leftOutCount = Math.max(day.documentTotal - shown.length, others.length)
  if (day.codingSessions.length === 0 && shown.length === 0 && leftOutCount === 0) return null
  return (
    <JournalCard
      title={t('journal.work')}
      description={t('journal.workHint')}
      action={
        day.facts.codingMinutes > 0 ? (
          <span className="muted">{durationText(day.facts.codingMinutes, t)}</span>
        ) : undefined
      }
      isFlush
    >
      {day.codingSessions.map((session) => (
        <Row
          key={session.documentId}
          to={pathOf('agent_document', session.documentId)}
          icon={<CodeIcon size={16} />}
          title={session.title}
          where={[folderName(session.directory), ASSISTANTS[session.assistant] ?? session.assistant]
            .filter(Boolean)
            .join(', ')}
          detail={session.linkReason}
          time={
            session.endedAt
              ? t('journal.timeRange', { start: clock(session.startedAt), end: clock(session.endedAt) })
              : clock(session.startedAt)
          }
        />
      ))}
      {shown.map((document) => {
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
      })}
      {leftOutCount > 0 && (
        <div className="journal-row journal-row-note">
          <span className="muted">
            {plural(leftOutCount, { one: 'journal.workLeftOutOne', other: 'journal.workLeftOutOther' })}
          </span>
          {others.length > 0 && (
            <button type="button" className="button-quiet" onClick={() => setRoutine(isRoutineShown ? null : 'shown')}>
              {isRoutineShown ? t('journal.hideThem') : t('journal.showThem')}
            </button>
          )}
        </div>
      )}
      {isRoutineShown &&
        others.map((document) => (
          <Row
            key={document.documentId}
            to={pathOf('agent_document', document.documentId)}
            icon={<DocumentIcon size={16} />}
            title={document.title}
            where={document.sourceName}
            time={clock(document.happenedAt)}
          />
        ))}
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
      isFlush
    >
      {mattered.map((message) => {
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
    </JournalCard>
  )
}

// PhotosSection is a grid of the day's photos, each opening in the viewer.
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
    >
      <div className="journal-photo-grid">
        {day.photos.map((photo) => (
          <Link
            key={photo.photoId}
            className="journal-photo"
            to={`/photos/${encodeURIComponent(photo.photoId)}`}
            aria-label={t(photo.isVideo ? 'journal.videoAt' : 'journal.photoAt', { time: clock(photo.takenAt) })}
          >
            <img src={photoSmallPreviewPath(photo.photoId)} alt="" loading="lazy" draggable={false} />
            <span className="journal-photo-time">{clock(photo.takenAt)}</span>
          </Link>
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
  if (day.recordings.length === 0) return null
  return (
    <JournalCard title={t('journal.recordings')} isFlush>
      {day.recordings.map((recording) => (
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
    </JournalCard>
  )
}

// ConversationsSection is the conversations with TeaNode that day and the
// chats with people.
export function ConversationsSection({ day }: { day: JournalDay }) {
  const { t, plural, clock } = useDayWords(day)
  if (day.conversations.length === 0 && day.chats.length === 0) return null
  return (
    <JournalCard title={t('journal.conversations')} description={t('journal.conversationsHint')} isFlush>
      {day.conversations.map((conversation) => (
        <Row
          key={conversation.conversationId}
          to={`/chat?conversation=${encodeURIComponent(conversation.conversationId)}`}
          icon={<RecordKindIcon recordKind="conversation_message" size={16} />}
          title={conversation.title || t('journal.untitledConversation')}
          time={clock(conversation.firstAt)}
        />
      ))}
      {day.chats.map((chat) => (
        <Row
          key={chat.chatId}
          icon={<RecordKindIcon recordKind="chat_message" size={16} />}
          title={chat.chatName}
          detail={plural(chat.messageCount, { one: 'journal.chatMessagesOne', other: 'journal.chatMessagesOther' })}
          time={clock(chat.firstAt)}
        />
      ))}
    </JournalCard>
  )
}

// MoneySection is the day's transactions as a table; the rows line up and
// the alignment is the point, and on a phone it scrolls on its own.
export function MoneySection({ day }: { day: JournalDay }) {
  const { t, plural, language, clock } = useDayWords(day)
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
            {day.transactions.map((transaction) => (
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
    </JournalCard>
  )
}

// ShoppingSection is the trips to the shops, with how much of the list was
// checked off.
export function ShoppingSection({ day }: { day: JournalDay }) {
  const { t, clock } = useDayWords(day)
  if (day.shoppingTrips.length === 0) return null
  return (
    <JournalCard title={t('journal.shopping')} isFlush>
      {day.shoppingTrips.map((trip) => (
        <Row
          key={trip.tripId}
          icon={<RecordKindIcon recordKind="shopping_trip" size={16} />}
          title={trip.listNames.join(', ') || t('journal.shopping')}
          detail={t('journal.shoppingChecked', { bought: trip.boughtCount, count: trip.itemCount })}
          time={clock(trip.startedAt)}
        />
      ))}
    </JournalCard>
  )
}

// KnowledgeSection is the pages of the agent's memory with a fact about
// that day.
export function KnowledgeSection({ day }: { day: JournalDay }) {
  const { t, plural } = useDayWords(day)
  if (day.memoryPages.length === 0) return null
  return (
    <JournalCard title={t('journal.knowledge')} description={t('journal.knowledgeHint')} isFlush>
      {day.memoryPages.map((page) => (
        <Row
          key={page.nodeId}
          to={`/knowledge/${page.path}`}
          icon={<DocumentIcon size={16} />}
          title={page.title}
          detail={`${page.path}, ${plural(page.factCount, { one: 'journal.factsOne', other: 'journal.factsOther' })}`}
        />
      ))}
    </JournalCard>
  )
}

export { Row as JournalRow }

import { useState } from 'react'
import { graphql } from '../../api'
import { ConfirmDialog, FormDialog } from '../../components/dialog'
import {
  ActivityIcon,
  CalendarIcon,
  ChatIcon,
  CodeIcon,
  DocumentIcon,
  LocationIcon,
  MailIcon,
  PencilIcon,
  PictureIcon,
  ReceiptIcon,
  RecordIcon,
  RefreshIcon,
  SparkIcon,
  TrashIcon,
  UserIcon,
  WalletIcon,
  ListIcon,
  TargetIcon,
} from '../../components/icons'
import { useToast } from '../../components/toast'
import { Tooltip } from '../../components/tooltip'
import { useTranslation } from '../../i18n/i18n'
import {
  CORRECTION_MOST_CHARACTERS,
  CORRECTION_MOST_COUNT,
  CORRECT_JOURNAL_NARRATIVE,
  DELETE_JOURNAL_NARRATIVE,
  JournalNarrative,
  JournalRecord,
  JournalTarget,
  REMOVE_JOURNAL_CORRECTION,
  WRITE_JOURNAL_NARRATIVE,
  kindNameKey,
  narrativeParagraphs,
  targetVariables,
} from './journalApi'
import { RecordLink } from './journalOpen'
import { JournalCard } from './journalParts'
import { isDashboardPath } from './journalPaths'

// What TeaNode wrote about a day or a period. Each mention of a record in it
// is a chip that opens the record; one the reader can no longer open is
// plain words. Under it, the person can have it written again, and say what
// is wrong with it: a correction is kept and every later writing follows it.

// The mark of each kind of record, for a chip and a row.
export function RecordKindIcon({ recordKind, size = 14 }: { recordKind: string; size?: number }) {
  switch (recordKind) {
    case 'mail':
      return <MailIcon size={size} />
    case 'calendar_event':
    case 'calendar_occurrence':
      return <CalendarIcon size={size} />
    case 'photo':
      return <PictureIcon size={size} />
    case 'place_visit':
      return <LocationIcon size={size} />
    case 'agent_document':
      return <DocumentIcon size={size} />
    case 'chat_message':
    case 'conversation_message':
      return <ChatIcon size={size} />
    case 'recording':
    case 'recording_passage':
      return <RecordIcon size={size} />
    case 'finance_transaction':
      return <WalletIcon size={size} />
    case 'finance_receipt':
      return <ReceiptIcon size={size} />
    case 'health_day':
      return <ActivityIcon size={size} />
    case 'shopping_trip':
    case 'reminder':
      return <ListIcon size={size} />
    case 'note':
      return <PencilIcon size={size} />
    case 'goal':
      return <TargetIcon size={size} />
    case 'contact':
      return <UserIcon size={size} />
    case 'coding_session':
      return <CodeIcon size={size} />
    default:
      return <SparkIcon size={size} />
  }
}

// RecordMention is a mention in the narrative: the words the story wrote for
// the record, or its title when it wrote none, in bold inside the sentence and
// linking to the record, as on the iPhone. It wraps with the words around it:
// drawn as a box, a long title became a boxed paragraph of its own. A path
// that is not one of the dashboard's own is not followed.
export function RecordMention({ record, words }: { record: JournalRecord; words: string }) {
  return (
    <RecordLink className="journal-mention" to={record.dashboardPath}>
      {words || record.title}
    </RecordLink>
  )
}

export function NarrativeText({ narrative }: { narrative: JournalNarrative }) {
  const { t } = useTranslation()
  const references = new Map(narrative.references.map((reference) => [reference.reference, reference]))
  return (
    <div className="journal-prose journal-narrative">
      {narrativeParagraphs(narrative.narrative).map((paragraph, paragraphIndex) => (
        <p key={paragraphIndex}>
          {paragraph.map((piece, pieceIndex) => {
            if (!piece.isReference) return piece.text
            const reference = references.get(piece.reference)
            if (reference && !reference.isUnavailable && isDashboardPath(reference.dashboardPath)) {
              return <RecordMention key={pieceIndex} record={reference} words={piece.words} />
            }
            // A record that is gone, or no longer the reader's, is its kind
            // in plain words.
            return t(kindNameKey(reference?.recordKind ?? piece.reference.split(':')[0]))
          })}
        </p>
      ))}
    </div>
  )
}

export function NarrativeCard({
  narrative,
  target,
  title,
  description,
  isToday = false,
  writeLabel,
  onChanged,
}: {
  narrative: JournalNarrative
  target: JournalTarget
  title: string
  // The sentence under the title when there is a story to read.
  description?: string
  // Today's story is the day so far, written on request, and again after the
  // day ends.
  isToday?: boolean
  // What the button says when there is no story yet or it is out of reach.
  writeLabel: string
  onChanged: () => Promise<void>
}) {
  const { t, language } = useTranslation()
  const toast = useToast()
  const [isCorrecting, setIsCorrecting] = useState(false)
  const [isRemoving, setIsRemoving] = useState(false)
  const [isBusy, setIsBusy] = useState(false)
  const hasStory = narrative.narrative.trim() !== ''

  async function run(action: () => Promise<void>, failure: string) {
    setIsBusy(true)
    try {
      await action()
    } catch (caught) {
      toast.failure(caught, failure)
    } finally {
      setIsBusy(false)
    }
  }

  const writeNow = () =>
    run(async () => {
      await graphql(WRITE_JOURNAL_NARRATIVE, targetVariables(target))
      toast.done(t('journal.narrativeWriting'))
      await onChanged()
    }, t('journal.narrativeFailed'))

  const actions = narrative.narrativePending ? undefined : hasStory ? (
    <button type="button" className="button-quiet" disabled={isBusy} onClick={() => void writeNow()}>
      <RefreshIcon size={14} /> {t('journal.writeAgain')}
    </button>
  ) : undefined

  return (
    <JournalCard
      title={
        <span className="journal-title-with-icon">
          <SparkIcon size={16} />
          {title}
        </span>
      }
      description={hasStory ? description : undefined}
      action={actions}
    >
      {narrative.narrativePending && !hasStory ? (
        <p className="muted journal-status" role="status">
          {t('journal.narrativePending')}
        </p>
      ) : hasStory ? (
        <>
          <NarrativeText narrative={narrative} />
          <Corrections
            narrative={narrative}
            isBusy={isBusy}
            onRemove={(correctionIndex) =>
              run(async () => {
                await graphql(REMOVE_JOURNAL_CORRECTION, { ...targetVariables(target), correctionIndex })
                toast.done(t('journal.correctionRemoved'))
                await onChanged()
              }, t('journal.narrativeFailed'))
            }
          />
          <div className="journal-card-foot">
            <span className="muted">
              {narrative.narrativePending
                ? t('journal.narrativePending')
                : narrative.isStale
                  ? t('journal.narrativeStale')
                  : narrative.writtenAt
                    ? t('journal.narrativeWrittenAt', {
                        when: new Intl.DateTimeFormat(language, { dateStyle: 'medium', timeStyle: 'short' }).format(
                          new Date(narrative.writtenAt),
                        ),
                        count: narrative.sourceCount,
                      })
                    : ''}
            </span>
            <span className="journal-card-foot-actions">
              <button type="button" className="button-quiet" onClick={() => setIsCorrecting(true)}>
                {t('journal.somethingWrong')}
              </button>
              <button type="button" className="button-quiet" onClick={() => setIsRemoving(true)}>
                {t('journal.removeStory')}
              </button>
            </span>
          </div>
        </>
      ) : (
        <>
          <p className="muted">
            {isToday
              ? t('journal.narrativeToday')
              : narrative.isBeyondDepth
                ? t('journal.narrativeBeyondDepth')
                : t('journal.narrativeNone')}
          </p>
          <div className="page-actions">
            <button type="button" disabled={isBusy} onClick={() => void writeNow()}>
              {writeLabel}
            </button>
          </div>
        </>
      )}
      {narrative.failure && !narrative.narrativePending && (
        <p className="muted journal-status">{t('journal.narrativeRefused', { reason: narrative.failure })}</p>
      )}
      {isRemoving && (
        <ConfirmDialog
          title={t('journal.removeStoryTitle')}
          body={t('journal.removeStoryBody')}
          confirmLabel={t('journal.removeStory')}
          busy={isBusy}
          onConfirm={() =>
            void run(async () => {
              await graphql(DELETE_JOURNAL_NARRATIVE, targetVariables(target))
              setIsRemoving(false)
              toast.done(t('journal.storyRemoved'))
              await onChanged()
            }, t('journal.removeStoryFailed'))
          }
          onClose={() => setIsRemoving(false)}
        />
      )}
      {isCorrecting && (
        <CorrectionDialog
          isFull={narrative.corrections.length >= CORRECTION_MOST_COUNT}
          isBusy={isBusy}
          onClose={() => setIsCorrecting(false)}
          onSubmit={(correctionText) =>
            run(async () => {
              const answer = await graphql<{ CorrectJournalNarrative: { isCorrectionAlreadyKept: boolean } }>(CORRECT_JOURNAL_NARRATIVE, {
                ...targetVariables(target),
                correctionText,
              })
              setIsCorrecting(false)
              toast.done(t(answer.CorrectJournalNarrative.isCorrectionAlreadyKept ? 'journal.correctionAlreadyKept' : 'journal.correctionSaved'))
              await onChanged()
            }, t('journal.correctionFailed'))
          }
        />
      )}
    </JournalCard>
  )
}

function Corrections({
  narrative,
  isBusy,
  onRemove,
}: {
  narrative: JournalNarrative
  isBusy: boolean
  onRemove: (correctionIndex: number) => void
}) {
  const { t } = useTranslation()
  if (narrative.corrections.length === 0) return null
  return (
    <ul className="journal-corrections">
      {narrative.corrections.map((correction, correctionIndex) => (
        <li key={`${correctionIndex}-${correction.createdAt}`}>
          <span>
            <strong>{t('journal.yourCorrection')}</strong> {correction.correctionText}
          </span>
          <Tooltip label={t('common.remove')}>
            <button
              type="button"
              className="icon-action danger"
              aria-label={`${t('common.remove')}: ${correction.correctionText}`}
              disabled={isBusy}
              onClick={() => onRemove(correctionIndex)}
            >
              <TrashIcon size={16} />
            </button>
          </Tooltip>
        </li>
      ))}
    </ul>
  )
}

function CorrectionDialog({
  isFull,
  isBusy,
  onClose,
  onSubmit,
}: {
  isFull: boolean
  isBusy: boolean
  onClose: () => void
  onSubmit: (correctionText: string) => void
}) {
  const { t } = useTranslation()
  const [correctionText, setCorrectionText] = useState('')
  const trimmed = correctionText.trim()
  return (
    <FormDialog
      title={t('journal.correctionTitle')}
      submitLabel={t('journal.correctionSubmit')}
      busy={isBusy}
      canSubmit={trimmed !== '' && !isFull}
      onClose={onClose}
      onSubmit={() => onSubmit(trimmed)}
    >
      <p className="muted">
        {isFull ? t('journal.correctionFull', { count: CORRECTION_MOST_COUNT }) : t('journal.correctionExplained')}
      </p>
      <label>
        {t('journal.correctionLabel')}
        <textarea
          rows={4}
          maxLength={CORRECTION_MOST_CHARACTERS}
          value={correctionText}
          placeholder={t('journal.correctionPlaceholder')}
          onChange={(event) => setCorrectionText(event.target.value)}
        />
      </label>
    </FormDialog>
  )
}

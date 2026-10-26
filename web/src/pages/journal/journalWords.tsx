import { useState } from 'react'

import { graphql } from '../../api'
import { FormDialog } from '../../components/dialog'
import { PencilIcon, SparkIcon } from '../../components/icons'
import { useToast } from '../../components/toast'
import { useTranslation } from '../../i18n/i18n'
import {
  JournalTarget,
  JournalWords as JournalWordsView,
  SET_JOURNAL_LOOKING_BACK,
  SET_JOURNAL_WORDS,
  STOP_JOURNAL_TOUCH_UP,
  TOUCH_UP_JOURNAL_WORDS,
  isConflict,
  targetVariables,
  textParagraphs,
} from './journalApi'
import { JournalCard, useAddressValue } from './journalParts'

// The person's own words on a day, or their looking back on a week, month or
// year: the same component for both. When touching up is on the card shows
// the touched-up text with the original one switch away, where the original
// is edited and saving it makes the touched-up text again. When it is off the
// card shows the original, with a way to edit it and a way to ask for a
// touch up. TeaNode never changes the original.

export function JournalWordsCard({
  words,
  target,
  onChanged,
  title,
  isEditable = true,
  emptyWriteLabel,
}: {
  words: JournalWordsView
  target: JournalTarget
  // Reads the page again after a change.
  onChanged: () => Promise<void>
  title: string
  isEditable?: boolean
  // Given, a card with nothing yet shows this button to start writing instead
  // of an open text box: a week's looking back is not the first thing the
  // person came to the page to do.
  emptyWriteLabel?: string
}) {
  const { t, language } = useTranslation()
  const toast = useToast()
  // Which text is open is in the address: the original (being read and
  // edited) or, left out, the one the page is showing by default.
  const [openText, setOpenText] = useAddressValue('text')
  const isOriginalOpen = openText === 'original'
  const [isAsking, setIsAsking] = useState(false)
  const [isBusy, setIsBusy] = useState(false)
  // What the person was saving when somebody else saved first: kept, and put
  // back in the editor over the newer version, so a refused save loses no
  // words. The newer version is shown above it to compare.
  const [rescuedDraft, setRescuedDraft] = useState<string | null>(null)
  const isPeriod = target.periodKind !== null
  const hasWords = words.original.trim() !== ''
  const isTouchedUpShown = words.isTouchUpOn && !isOriginalOpen
  // With touching up off the original is shown, and edited in place when the
  // person opens it or when there is nothing yet to read.
  const isOpenWhenEmpty = emptyWriteLabel === undefined
  const isEditing =
    isEditable && (words.isTouchUpOn ? isOriginalOpen : isOriginalOpen || (!hasWords && isOpenWhenEmpty))

  async function run(action: () => Promise<void>, failure: string, draftBeingSaved?: string) {
    setIsBusy(true)
    try {
      await action()
    } catch (caught) {
      if (isConflict(caught)) {
        // Somebody (the phone, another tab) saved after this page read it.
        // Their text is read again, so nothing is overwritten by accident,
        // and the person's own draft stays in the editor to save again.
        toast.failed(t('journal.savedElsewhere'))
        if (draftBeingSaved !== undefined) {
          setRescuedDraft(draftBeingSaved)
          setOpenText('original')
        } else {
          setOpenText(null)
        }
        await onChanged()
      } else {
        toast.failure(caught, failure)
      }
    } finally {
      setIsBusy(false)
    }
  }

  async function saveOriginal(text: string) {
    await graphql(
      isPeriod ? SET_JOURNAL_LOOKING_BACK : SET_JOURNAL_WORDS,
      isPeriod
        ? { ...targetVariables(target), lookingBack: text, baseModifiedAt: words.modifiedAt }
        : { day: target.day, words: text, baseModifiedAt: words.modifiedAt },
    )
  }

  async function touchUp(how: string | null) {
    await graphql(TOUCH_UP_JOURNAL_WORDS, { ...targetVariables(target), how })
  }

  const switchControl = words.isTouchUpOn ? (
    <div className="segmented" role="group" aria-label={t('journal.wordsWhichText')}>
      <button
        type="button"
        className={isTouchedUpShown ? 'active' : undefined}
        aria-pressed={isTouchedUpShown}
        onClick={() => setOpenText(null)}
      >
        {t('journal.touchedUp')}
      </button>
      <button
        type="button"
        className={isTouchedUpShown ? undefined : 'active'}
        aria-pressed={!isTouchedUpShown}
        onClick={() => setOpenText('original')}
      >
        {t('journal.yourOriginal')}
      </button>
    </div>
  ) : undefined

  const description = isTouchedUpShown
    ? words.touchUpHow
      ? t('journal.wordsTouchedUpNote', { how: words.touchUpHow })
      : t('journal.wordsTouchedUpNoHow')
    : t('journal.wordsOwnNote')

  return (
    <JournalCard title={title} description={description} action={switchControl} isPaper id="journal-words">
      {isEditing ? (
        <OriginalEditor
          // A new copy from the server starts the text again: the draft is
          // only ever a draft of the version it was opened on.
          key={`${words.modifiedAt ?? 'none'}:${rescuedDraft === null ? 'own' : 'rescued'}`}
          words={words}
          rescuedDraft={rescuedDraft}
          isBusy={isBusy}
          isPeriod={isPeriod}
          onCancel={hasWords || words.isTouchUpOn || !isOpenWhenEmpty ? () => setOpenText(null) : undefined}
          onSave={(text, how) =>
            run(
              async () => {
                await saveOriginal(text)
                if (words.isTouchUpOn && how !== words.touchUpHow) await touchUp(how)
                toast.done(words.isTouchUpOn ? t('journal.touchUpOnItsWay') : t('journal.wordsSaved'))
                setRescuedDraft(null)
                setOpenText(null)
                await onChanged()
              },
              t('journal.wordsFailed'),
              text,
            )
          }
          onStopTouchUp={() =>
            run(async () => {
              await graphql(STOP_JOURNAL_TOUCH_UP, targetVariables(target))
              toast.done(t('journal.touchUpStopped'))
              setOpenText(null)
              await onChanged()
            }, t('journal.wordsFailed'))
          }
        />
      ) : (
        <>
          {isTouchedUpShown ? (
            <TouchedUpText words={words} />
          ) : hasWords ? (
            <Prose text={words.original} />
          ) : (
            <p className="muted">{isPeriod ? t('journal.lookingBackEmpty') : t('journal.wordsEmpty')}</p>
          )}
          {isEditable && !hasWords && !words.isTouchUpOn && emptyWriteLabel !== undefined && (
            <div className="page-actions">
              <button type="button" onClick={() => setOpenText('original')}>
                {emptyWriteLabel}
              </button>
            </div>
          )}
          {isEditable && (
            <div className="journal-card-foot">
              {words.modifiedAt && hasWords && (
                <span className="muted">
                  {t('journal.wordsChangedAt', {
                    when: new Intl.DateTimeFormat(language, { dateStyle: 'medium', timeStyle: 'short' }).format(
                      new Date(words.modifiedAt),
                    ),
                  })}
                </span>
              )}
              <span className="journal-card-foot-actions">
                {isTouchedUpShown ? (
                  <>
                    <button type="button" className="button-quiet" onClick={() => setOpenText('original')}>
                      {t('journal.editOriginal')}
                    </button>
                    <button
                      type="button"
                      className="button-quiet"
                      disabled={isBusy || words.touchUpPending}
                      onClick={() =>
                        void run(async () => {
                          await touchUp(null)
                          toast.done(t('journal.touchUpOnItsWay'))
                          await onChanged()
                        }, t('journal.wordsFailed'))
                      }
                    >
                      {t('journal.anotherVersion')}
                    </button>
                  </>
                ) : (
                  <>
                    {hasWords && (
                      <button type="button" className="button-quiet" onClick={() => setOpenText('original')}>
                        <PencilIcon size={14} /> {t('common.edit')}
                      </button>
                    )}
                    {hasWords && !words.isTouchUpOn && (
                      <button
                        type="button"
                        className="button-quiet"
                        disabled={isBusy}
                        onClick={() => setIsAsking(true)}
                      >
                        <SparkIcon size={14} /> {t('journal.touchUp')}
                      </button>
                    )}
                  </>
                )}
              </span>
            </div>
          )}
        </>
      )}
      <JournalNotes words={words} />
      {isAsking && (
        <TouchUpDialog
          isBusy={isBusy}
          onClose={() => setIsAsking(false)}
          onSubmit={(how) =>
            run(async () => {
              await touchUp(how === '' ? null : how)
              setIsAsking(false)
              toast.done(t('journal.touchUpOnItsWay'))
              await onChanged()
            }, t('journal.wordsFailed'))
          }
        />
      )}
    </JournalCard>
  )
}

// Prose is the person's words as paragraphs in the writing face. A single line
// break inside one is kept.
export function Prose({ text }: { text: string }) {
  return (
    <div className="journal-prose">
      {textParagraphs(text).map((paragraph, index) => (
        <p key={index}>{paragraph}</p>
      ))}
    </div>
  )
}

function TouchedUpText({ words }: { words: JournalWordsView }) {
  const { t } = useTranslation()
  return (
    <>
      {words.touchedUp.trim() !== '' && <Prose text={words.touchedUp} />}
      {words.touchUpPending && (
        <p className="muted journal-status" role="status">
          {t('journal.touchUpPending')}
        </p>
      )}
      {!words.touchUpPending && words.isTouchedUpStale && (
        <p className="muted journal-status">{t('journal.touchUpStale')}</p>
      )}
    </>
  )
}

// The notes the person keeps in their notes folder for the day, which are
// theirs and are only read here.
function JournalNotes({ words }: { words: JournalWordsView }) {
  const { t } = useTranslation()
  if (words.journalNotes.length === 0) return null
  return (
    <div className="journal-notes">
      <div className="journal-notes-label muted">{t('journal.fromYourNotes')}</div>
      {words.journalNotes.map((note) => (
        <div key={note.documentId} className="journal-note">
          {note.title && <div className="journal-note-title">{note.title}</div>}
          <Prose text={note.text} />
        </div>
      ))}
    </div>
  )
}

function OriginalEditor({
  words,
  rescuedDraft,
  isBusy,
  isPeriod,
  onCancel,
  onSave,
  onStopTouchUp,
}: {
  words: JournalWordsView
  rescuedDraft: string | null
  isBusy: boolean
  isPeriod: boolean
  onCancel?: () => void
  onSave: (text: string, how: string) => void
  onStopTouchUp: () => void
}) {
  const { t } = useTranslation()
  const [draft, setDraft] = useState(rescuedDraft ?? words.original)
  const [how, setHow] = useState(words.touchUpHow)
  const hasChanged = draft !== words.original || (words.isTouchUpOn && how !== words.touchUpHow)
  return (
    <form
      className="journal-editor"
      onSubmit={(event) => {
        event.preventDefault()
        onSave(draft, how.trim())
      }}
    >
      {rescuedDraft !== null && (
        <div className="journal-rescued">
          <p className="muted">{t('journal.savedElsewhereAbove')}</p>
          <Prose text={words.original} />
        </div>
      )}
      <label htmlFor="journal-original">
        {words.isTouchUpOn ? t('journal.originalLabelTouchedUp') : t('journal.originalLabel')}
      </label>
      <textarea
        id="journal-original"
        rows={7}
        value={draft}
        placeholder={isPeriod ? t('journal.lookingBackPlaceholder') : t('journal.wordsPlaceholder')}
        onChange={(event) => setDraft(event.target.value)}
      />
      {words.isTouchUpOn && (
        <label className="journal-how">
          <span>{t('journal.how')}</span>
          <input
            value={how}
            placeholder={t('journal.howPlaceholder')}
            onChange={(event) => setHow(event.target.value)}
          />
        </label>
      )}
      <div className="journal-card-foot">
        <span className="journal-card-foot-actions">
          <button type="submit" className="primary" disabled={isBusy || !hasChanged}>
            {words.isTouchUpOn ? t('journal.saveAndTouchUp') : t('common.save')}
          </button>
          {onCancel && (
            <button type="button" disabled={isBusy} onClick={onCancel}>
              {t('common.cancel')}
            </button>
          )}
        </span>
        {words.isTouchUpOn && (
          <button type="button" className="button-quiet" disabled={isBusy} onClick={onStopTouchUp}>
            {isPeriod ? t('journal.stopTouchUpPeriod') : t('journal.stopTouchUp')}
          </button>
        )}
      </div>
    </form>
  )
}

function TouchUpDialog({
  isBusy,
  onClose,
  onSubmit,
}: {
  isBusy: boolean
  onClose: () => void
  onSubmit: (how: string) => void
}) {
  const { t } = useTranslation()
  const [how, setHow] = useState('')
  return (
    <FormDialog
      title={t('journal.touchUpTitle')}
      submitLabel={t('journal.touchUp')}
      busy={isBusy}
      onClose={onClose}
      onSubmit={() => onSubmit(how.trim())}
    >
      <p className="muted">{t('journal.touchUpExplained')}</p>
      <label>
        {t('journal.how')}
        <input value={how} placeholder={t('journal.howPlaceholder')} onChange={(event) => setHow(event.target.value)} />
      </label>
    </FormDialog>
  )
}

import { useState } from 'react'
import { FormDialog } from '../components/dialog'
import { useToast } from '../components/toast'
import { useTranslation } from '../i18n/i18n'
import { recordingOperation, type SpeakersView, type TranscriptSegment } from './processing'

const caughtText = (caught: unknown) => (caught instanceof Error ? caught.message : String(caught))

// BubbleTextDialog corrects the words of one bubble: a box for each passage it
// is made of, since a correction belongs to one passage, and only the passages
// whose words changed are saved. Each saved correction advances the
// recording's generation, so the next one reads it again first.
export function BubbleTextDialog({
  recordingId,
  segments,
  passageText,
  onSaved,
  onClose,
}: {
  recordingId: string
  segments: TranscriptSegment[]
  passageText: (segment: TranscriptSegment) => string
  onSaved: () => void
  onClose: () => void
}) {
  const { t } = useTranslation()
  const toast = useToast()
  const [passageTexts, setPassageTexts] = useState(() => segments.map(passageText))
  const [isSaving, setIsSaving] = useState(false),
    [saveError, setSaveError] = useState<string | null>(null)
  const changedIndexes = segments
    .map((_segment, segmentIndex) => segmentIndex)
    .filter((segmentIndex) => passageTexts[segmentIndex].trim() !== passageText(segments[segmentIndex]).trim())
  const save = async () => {
    setIsSaving(true)
    setSaveError(null)
    try {
      for (const segmentIndex of changedIndexes) {
        const speakers = await recordingOperation<SpeakersView>('ReadRecordingSpeakers', { recordingId })
        await recordingOperation('CorrectRecordingTranscriptText', {
          recordingId,
          recordingTranscriptSegmentId: segments[segmentIndex].id,
          expectedSourceGeneration: speakers.sourceGeneration,
          clientCorrectionId: crypto.randomUUID(),
          correctedTranscriptText: passageTexts[segmentIndex].trim(),
        })
      }
      toast.done(t('recordingProcessing.saved'))
      onClose()
      onSaved()
    } catch (caught) {
      setSaveError(caughtText(caught))
      onSaved()
    } finally {
      setIsSaving(false)
    }
  }
  return (
    <FormDialog
      title={t('recordingProcessing.editText')}
      submitLabel={t('common.save')}
      busy={isSaving}
      error={saveError}
      canSubmit={changedIndexes.length > 0 && passageTexts.every((text) => text.trim() !== '')}
      onClose={() => {
        if (!isSaving) onClose()
      }}
      onSubmit={() => void save()}
    >
      {segments.map((segment, segmentIndex) => (
        <label key={segment.id}>
          {segments.length > 1 ? (
            <span>{t('recordings.passageNumber', { passageNumber: segmentIndex + 1 })}</span>
          ) : null}
          <textarea
            rows={segments.length > 1 ? 3 : 6}
            value={passageTexts[segmentIndex]}
            aria-label={segments.length > 1 ? undefined : t('recordingProcessing.correctedText')}
            onChange={(event) =>
              setPassageTexts((previous) =>
                previous.map((text, index) => (index === segmentIndex ? event.target.value : text)),
              )
            }
          />
        </label>
      ))}
    </FormDialog>
  )
}

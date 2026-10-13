import { useState } from 'react'
import { FormDialog } from '../components/dialog'
import { CalendarIcon } from '../components/icons'
import { Tooltip } from '../components/tooltip'
import { useTranslation } from '../i18n/i18n'
import { recordingOperation } from './processing'

export function RecordingSourceSchedule({
  sourceId,
  sourceCron,
  sourceTitle,
  isIconOnly = false,
  onSaved,
}: {
  sourceId: string
  sourceCron: string
  sourceTitle: string
  isIconOnly?: boolean
  onSaved: () => Promise<unknown>
}) {
  const { t } = useTranslation()
  const [isOpen, setIsOpen] = useState(false)
  const [isSaving, setIsSaving] = useState(false)
  const [editedSourceCron, setEditedSourceCron] = useState('')
  const [errorMessage, setErrorMessage] = useState<string | null>(null)
  const open = () => {
    setEditedSourceCron(sourceCron)
    setErrorMessage(null)
    setIsOpen(true)
  }
  const save = async () => {
    setIsSaving(true)
    setErrorMessage(null)
    try {
      await recordingOperation('SaveRecordingSourceSchedule', { sourceId, cron: editedSourceCron.trim() })
      await onSaved()
      setIsOpen(false)
    } catch (caught) {
      setErrorMessage(caught instanceof Error ? caught.message : String(caught))
    } finally {
      setIsSaving(false)
    }
  }
  return (
    <>
      {isIconOnly ? (
        <Tooltip label={t('recordingProcessing.sourceSchedule')}>
          <button
            type="button"
            className="icon-action"
            aria-label={`${sourceTitle}: ${t('recordingProcessing.sourceSchedule')}`}
            onClick={open}
          >
            <CalendarIcon size={16} />
          </button>
        </Tooltip>
      ) : (
        <button type="button" onClick={open}>
          {t('recordingProcessing.sourceSchedule')}
        </button>
      )}
      {isOpen ? (
        <FormDialog
          title={t('recordingProcessing.sourceSchedule')}
          submitLabel={t('common.save')}
          busy={isSaving}
          error={errorMessage}
          onClose={() => {
            if (!isSaving) setIsOpen(false)
          }}
          onSubmit={() => void save()}
        >
          <label>
            <span>{t('agent.knowledgeCron')}</span>
            <input value={editedSourceCron} onChange={(event) => setEditedSourceCron(event.target.value)} />
          </label>
          <p className="muted">{t('recordingProcessing.sourceScheduleHint')}</p>
        </FormDialog>
      ) : null}
    </>
  )
}

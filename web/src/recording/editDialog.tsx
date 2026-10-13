import { useEffect, useId, useState } from 'react'
import { FormDialog } from '../components/dialog'
import { useToast } from '../components/toast'
import { useTranslation } from '../i18n/i18n'
import { recordingOperation } from './processing'
import { type RECORDING_PROCESSING_DOCUMENTS } from './processingDocuments'

export type DialogField = {
  fieldName: string
  labelKey: string
  inputType?: 'number' | 'text' | 'textarea'
  fieldValue: string
}
export type ProcessingDialog = {
  operationName: keyof typeof RECORDING_PROCESSING_DOCUMENTS
  titleKey: string
  // A title of the caller's own, for a form whose heading is not one of the
  // processing strings.
  titleText?: string
  variables: Record<string, unknown>
  fields: DialogField[]
  isPaid?: boolean
  isDestructive?: boolean
}

const caughtText = (caught: unknown) => (caught instanceof Error ? caught.message : String(caught))

// RecordingEditDialog is the one form behind every change to a recording's
// evidence: the page's conversation and the details drawer open the same
// dialog, so a correction behaves the same wherever it was started.
export function RecordingEditDialog({
  dialog,
  speakerNames,
  onSaved,
  onClose,
}: {
  dialog: ProcessingDialog
  speakerNames: string[]
  onSaved: (operationName: ProcessingDialog['operationName']) => Promise<unknown>
  onClose: () => void
}) {
  const { t } = useTranslation()
  const toast = useToast()
  const speakerNamesListId = useId()
  const [fields, setFields] = useState(dialog.fields)
  const [isSaving, setIsSaving] = useState(false),
    [editError, setEditError] = useState<string | null>(null)
  const save = async () => {
    setIsSaving(true)
    setEditError(null)
    const variables = { ...dialog.variables }
    for (const field of fields)
      variables[field.fieldName] = field.inputType === 'number' ? Number(field.fieldValue) : field.fieldValue
    if (dialog.operationName === 'UpdateRecordingMetadata') variables.recordedAt = variables.recordedAt || null
    try {
      await recordingOperation(dialog.operationName, variables)
      toast.done(t('recordingProcessing.saved'))
      onClose()
      await onSaved(dialog.operationName)
    } catch (caught) {
      setEditError(caughtText(caught))
    } finally {
      setIsSaving(false)
    }
  }
  // A speaker's name is offered from the voices already named, the people in
  // the agent's memory and the person's contacts, narrowed as it is typed.
  const typedName = fields.find((field) => field.fieldName === 'confirmedName')?.fieldValue
  const [suggestedNames, setSuggestedNames] = useState<string[]>(speakerNames)
  useEffect(() => {
    if (typedName === undefined) return
    let isCurrent = true
    const timer = window.setTimeout(() => {
      recordingOperation<{ confirmedName: string }[]>('SuggestRecordingSpeakerNames', { searchText: typedName.trim() })
        .then((suggestions) => {
          if (isCurrent) setSuggestedNames(suggestions.map((suggestion) => suggestion.confirmedName))
        })
        .catch(() => {})
    }, 200)
    return () => {
      isCurrent = false
      window.clearTimeout(timer)
    }
  }, [typedName])
  const changeField = (fieldIndex: number, fieldValue: string) =>
    setFields((previous) => previous.map((field, index) => (index === fieldIndex ? { ...field, fieldValue } : field)))
  return (
    <FormDialog
      title={dialog.titleText ?? t(`recordingProcessing.${dialog.titleKey}` as Parameters<typeof t>[0])}
      submitLabel={t(dialog.isPaid ? 'recordingProcessing.confirmPaid' : 'common.save')}
      busy={isSaving}
      error={editError}
      canSubmit={fields.every((field) => field.fieldName === 'recordedAt' || field.fieldValue.trim() !== '')}
      onClose={() => {
        if (!isSaving) onClose()
      }}
      onSubmit={() => void save()}
    >
      {dialog.isPaid ? <p>{t('recordingProcessing.paidHint')}</p> : null}
      {dialog.isDestructive ? <p>{t('recordingProcessing.referenceDeleteHint')}</p> : null}
      {dialog.operationName === 'RetryRecordingTranscription' ? <p>{t('recordingProcessing.retryHint')}</p> : null}
      {dialog.operationName === 'CancelRecordingTranscription' ? <p>{t('recordingProcessing.cancelHint')}</p> : null}
      {fields.map((field, fieldIndex) => (
        <label key={field.fieldName}>
          <span>{t(`recordingProcessing.${field.labelKey}` as Parameters<typeof t>[0])}</span>
          {field.inputType === 'textarea' ? (
            <textarea
              rows={6}
              value={field.fieldValue}
              onChange={(event) => changeField(fieldIndex, event.target.value)}
            />
          ) : (
            <input
              type={field.inputType ?? 'text'}
              list={field.fieldName === 'confirmedName' ? speakerNamesListId : undefined}
              value={field.fieldValue}
              onChange={(event) => changeField(fieldIndex, event.target.value)}
            />
          )}
        </label>
      ))}
      <datalist id={speakerNamesListId}>
        {suggestedNames.map((speakerName) => (
          <option key={speakerName} value={speakerName} />
        ))}
      </datalist>
    </FormDialog>
  )
}

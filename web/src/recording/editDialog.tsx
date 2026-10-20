import { useEffect, useId, useState } from 'react'
import { FormDialog } from '../components/dialog'
import { useToast } from '../components/toast'
import { useTranslation } from '../i18n/i18n'
import { recordingOperation, type SpeakerNameSuggestion } from './processing'
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
    // Naming a speaker with a name that is a contact's ties the speaker to that
    // contact too.
    if (tiedContact) {
      variables.contactAddressBookId = tiedContact.addressBookId
      variables.contactId = tiedContact.contactId
    }
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
  const [suggestions, setSuggestions] = useState<SpeakerNameSuggestion[]>(() =>
    speakerNames.map((confirmedName) => ({ confirmedName, speakerNameSource: 'speaker' })),
  )
  useEffect(() => {
    if (typedName === undefined) return
    let isCurrent = true
    const timer = window.setTimeout(() => {
      recordingOperation<SpeakerNameSuggestion[]>('SuggestRecordingSpeakerNames', { searchText: typedName.trim() })
        .then((offered) => {
          if (isCurrent) setSuggestions(offered)
        })
        .catch(() => {})
    }, 200)
    return () => {
      isCurrent = false
      window.clearTimeout(timer)
    }
  }, [typedName])
  // The contact the typed name is, when it is a name offered from one. Only a
  // new name for a voice ties it: a rename keeps the contact it has.
  const isNaming =
    dialog.operationName === 'CorrectRecordingSpeaker' || dialog.operationName === 'NameRecordingTemporarySpeaker'
  const tiedSuggestion = isNaming
    ? suggestions.find(
        (suggestion) =>
          suggestion.addressBookId &&
          suggestion.contactId &&
          suggestion.confirmedName.toLowerCase() === (typedName ?? '').trim().toLowerCase(),
      )
    : undefined
  const tiedContact = tiedSuggestion
    ? { addressBookId: tiedSuggestion.addressBookId!, contactId: tiedSuggestion.contactId! }
    : null
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
      {tiedSuggestion ? (
        <p className="muted">{t('recordingProcessing.nameTiesContact', { name: tiedSuggestion.confirmedName })}</p>
      ) : null}
      <datalist id={speakerNamesListId}>
        {suggestions.map((suggestion) => (
          <option key={suggestion.confirmedName} value={suggestion.confirmedName} />
        ))}
      </datalist>
    </FormDialog>
  )
}

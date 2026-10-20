import { useState } from 'react'

import { ContactPicker, type ContactChoice } from '../components/contactPicker'
import { FormDialog } from '../components/dialog'
import { useTranslation } from '../i18n/i18n'

// SpeakerContactDialog chooses the contact a speaker's name is: from every
// contact the person has, so that the recordings the speaker is in show on that
// contact's card. Only the person sees the tie. The caller saves it and shows
// what went wrong in error, so the dialog stays open with the choice in it.
export function SpeakerContactDialog({
  speakerName,
  currentContactId,
  busy,
  error,
  onChoose,
  onClose,
}: {
  speakerName: string
  currentContactId: string
  busy: boolean
  error: string | null
  onChoose: (choice: ContactChoice) => void
  onClose: () => void
}) {
  const { t } = useTranslation()
  const [chosen, setChosen] = useState<ContactChoice | null>(null)
  return (
    <FormDialog
      title={t('recordingProcessing.speakerContactTitle', { name: speakerName })}
      submitLabel={t('common.save')}
      busy={busy}
      error={error}
      canSubmit={chosen !== null && chosen.id !== currentContactId}
      onSubmit={() => chosen && onChoose(chosen)}
      onClose={onClose}
    >
      <p className="muted">{t('recordingProcessing.speakerContactHint')}</p>
      <ContactPicker chosen={chosen} onChoose={setChosen} />
    </FormDialog>
  )
}

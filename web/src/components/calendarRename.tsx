import { useState } from 'react'

import { failureText, graphql } from '../api'
import { FormDialog } from './dialog'
import { useToast } from './toast'
import { useTranslation } from '../i18n/i18n'

// Renaming the person's own calendar, or a circle's they are an admin of.
//
// The server replaces every field it is sent, so the ones this dialog does not
// show go back as the calendar holds them; leaving them out would clear the
// description, the color, the zone a new event is written in and the day the
// week starts on. The command line's `calendar set` and the agent's `set`
// action do the same.
const SAVE_CALENDAR = `mutation ($id: String!, $name: String, $description: String, $colour: String,
  $timezone: String, $weekStart: String) {
  SaveCalendar(id: $id, name: $name, description: $description, colour: $colour, timezone: $timezone,
    weekStart: $weekStart) { id name }
}`

export type RenameableCalendar = {
  id: string
  name: string
  description?: string
  colour?: string
  timezone?: string
  weekStart?: string
}

export function CalendarRenameDialog({
  calendar,
  title,
  onClose,
  onSaved,
}: {
  calendar: RenameableCalendar
  // What the dialog is called; renaming the person's own calendar by default.
  title?: string
  onClose: () => void
  // Called once the new name is kept, so the page can read the calendars again.
  onSaved: () => void | Promise<unknown>
}) {
  const { t } = useTranslation()
  const toast = useToast()
  const [name, setName] = useState(calendar.name)
  const [isBusy, setIsBusy] = useState(false)
  const [problem, setProblem] = useState<string | null>(null)

  async function save() {
    setIsBusy(true)
    setProblem(null)
    try {
      await graphql(SAVE_CALENDAR, {
        id: calendar.id,
        name: name.trim(),
        description: calendar.description ?? '',
        colour: calendar.colour ?? '',
        timezone: calendar.timezone ?? '',
        weekStart: calendar.weekStart ?? '',
      })
      toast.done(t('calendar.renamed'))
      onClose()
      try {
        await onSaved()
      } catch (failure) {
        toast.failure(failure, t('calendar.refreshFailed'))
      }
    } catch (failure) {
      // The dialog is open, so the failure is said in it, with what was typed
      // still in the box.
      setProblem(failureText(failure, t('calendar.failed')))
    } finally {
      setIsBusy(false)
    }
  }

  return (
    <FormDialog
      title={title ?? t('calendar.renameTitle')}
      submitLabel={t('common.save')}
      busy={isBusy}
      error={problem}
      canSubmit={name.trim() !== '' && name.trim() !== calendar.name}
      onClose={onClose}
      onSubmit={() => void save()}
    >
      <label>
        {t('calendar.name')}
        <input autoFocus value={name} maxLength={200} onChange={(event) => setName(event.target.value)} />
      </label>
    </FormDialog>
  )
}

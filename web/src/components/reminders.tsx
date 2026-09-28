import { useState } from 'react'

import { graphql } from '../api'
import { useTranslation } from '../i18n/i18n'
import { ConfirmDialog, FormDialog } from './dialog'
import { SettingsEmpty } from './settingsList'
import { useToast } from './toast'
import { useQuery } from './useQuery'

// The reminders list beside the calendar: what a phone's Reminders app syncs,
// kept here too. The same operations as teanode reminder and the agent's
// reminder tool.

export interface Reminder {
  id: string
  title: string
  notes: string
  dueAt: string | null
  isDueDate: boolean
  isDone: boolean
  doneAt: string | null
  priority: number
  isRepeating: boolean
}

const FIELDS = `{ id title notes dueAt isDueDate isDone doneAt priority isRepeating }`

const LIST = `query ($isDone: Boolean) { ListReminders(isDone: $isDone) ${FIELDS} }`

const SAVE = `
  mutation ($reminderId: String, $title: String, $notes: String, $dueAt: String, $dueOn: String, $isDueCleared: Boolean) {
    SaveReminder(reminderId: $reminderId, title: $title, notes: $notes, dueAt: $dueAt, dueOn: $dueOn, isDueCleared: $isDueCleared) ${FIELDS}
  }`

const SET_DONE = `mutation ($reminderId: String!, $isDone: Boolean!) { SetReminderDone(reminderId: $reminderId, isDone: $isDone) { id } }`

const DELETE = `mutation ($reminderId: String!) { DeleteReminder(reminderId: $reminderId) }`

// dueOf is how a reminder's due time reads, and whether it has passed. A
// reminder due on a day is due that day wherever the reader is, so the day
// is read as written rather than moved by a zone.
function dueOf(reminder: Reminder, language: string): { text: string; isOverdue: boolean } | null {
  if (!reminder.dueAt) return null
  const due = new Date(reminder.dueAt)
  if (reminder.isDueDate) {
    const day = new Date(due.getUTCFullYear(), due.getUTCMonth(), due.getUTCDate())
    const today = new Date()
    today.setHours(0, 0, 0, 0)
    return {
      text: day.toLocaleDateString(language, { weekday: 'short', month: 'short', day: 'numeric' }),
      isOverdue: !reminder.isDone && day < today,
    }
  }
  return {
    text: due.toLocaleString(language, {
      weekday: 'short',
      month: 'short',
      day: 'numeric',
      hour: 'numeric',
      minute: '2-digit',
    }),
    isOverdue: !reminder.isDone && due < new Date(),
  }
}

// dayOf and timeOf are a due time as the date and time fields hold it.
function dayOf(reminder: Reminder): string {
  if (!reminder.dueAt) return ''
  const due = new Date(reminder.dueAt)
  if (reminder.isDueDate) return reminder.dueAt.slice(0, 10)
  const pad = (value: number) => String(value).padStart(2, '0')
  return `${due.getFullYear()}-${pad(due.getMonth() + 1)}-${pad(due.getDate())}`
}

function timeOf(reminder: Reminder): string {
  if (!reminder.dueAt || reminder.isDueDate) return ''
  const due = new Date(reminder.dueAt)
  return `${String(due.getHours()).padStart(2, '0')}:${String(due.getMinutes()).padStart(2, '0')}`
}

// dueVariables is a day and maybe a time as the operation takes them: a day
// alone is due that day, a day and a time a moment where the reader is.
function dueVariables(day: string, time: string): Record<string, unknown> {
  if (!day) return { isDueCleared: true }
  if (!time) return { dueOn: day }
  return { dueAt: new Date(`${day}T${time}`).toISOString() }
}

export function RemindersView() {
  const { t, language } = useTranslation()
  const toast = useToast()
  const [busy, setBusy] = useState(false)
  const [title, setTitle] = useState('')
  const [day, setDay] = useState('')
  const [isShowingDone, setShowingDone] = useState(false)
  const [editing, setEditing] = useState<Reminder | null>(null)
  const open = useQuery(() => graphql<{ ListReminders: Reminder[] }>(LIST, { isDone: false }), [], { refresh: true })
  const done = useQuery(
    () =>
      isShowingDone
        ? graphql<{ ListReminders: Reminder[] }>(LIST, { isDone: true })
        : Promise.resolve({ ListReminders: [] as Reminder[] }),
    [isShowingDone],
    { refresh: true },
  )

  async function act(work: () => Promise<unknown>, said?: string) {
    setBusy(true)
    try {
      await work()
      if (said) toast.done(said)
      await Promise.all([open.reload(), done.reload()])
      return true
    } catch (caught) {
      toast.failure(caught, t('reminders.failed'))
      return false
    } finally {
      setBusy(false)
    }
  }

  const add = () => {
    const words = title.trim()
    if (!words) return
    void act(() => graphql(SAVE, { title: words, ...(day ? { dueOn: day } : {}) })).then((isKept) => {
      if (isKept) {
        setTitle('')
        setDay('')
      }
    })
  }
  const setDone = (reminder: Reminder, isDone: boolean) =>
    void act(
      () => graphql(SET_DONE, { reminderId: reminder.id, isDone }),
      isDone ? t(reminder.isRepeating ? 'reminders.movedOn' : 'reminders.markedDone') : undefined,
    )

  const openReminders = open.data?.ListReminders ?? []
  const doneReminders = done.data?.ListReminders ?? []

  return (
    <div className="reminders">
      <form
        className="reminders-add"
        onSubmit={(event) => {
          event.preventDefault()
          add()
        }}
      >
        <input
          type="text"
          value={title}
          placeholder={t('reminders.addPlaceholder')}
          aria-label={t('reminders.addPlaceholder')}
          onChange={(event) => setTitle(event.target.value)}
        />
        <input
          type="date"
          value={day}
          aria-label={t('reminders.dueDay')}
          title={t('reminders.dueDay')}
          onChange={(event) => setDay(event.target.value)}
        />
        <button type="submit" className="primary" disabled={busy || !title.trim()}>
          {t('reminders.add')}
        </button>
      </form>

      {open.loading && !open.data ? (
        <p className="muted">{t('common.loading')}</p>
      ) : openReminders.length === 0 ? (
        <SettingsEmpty>{t('reminders.none')}</SettingsEmpty>
      ) : (
        <ul className="reminder-list">
          {openReminders.map((reminder) => (
            <ReminderRow
              key={reminder.id}
              reminder={reminder}
              language={language}
              busy={busy}
              onDone={setDone}
              onOpen={setEditing}
            />
          ))}
        </ul>
      )}

      <button type="button" className="link reminders-done-toggle" onClick={() => setShowingDone(!isShowingDone)}>
        {isShowingDone ? t('reminders.hideDone') : t('reminders.showDone')}
      </button>
      {isShowingDone &&
        (doneReminders.length === 0 ? (
          <SettingsEmpty>{t('reminders.noneDone')}</SettingsEmpty>
        ) : (
          <ul className="reminder-list done">
            {doneReminders.map((reminder) => (
              <ReminderRow
                key={reminder.id}
                reminder={reminder}
                language={language}
                busy={busy}
                onDone={setDone}
                onOpen={setEditing}
              />
            ))}
          </ul>
        ))}

      {editing ? (
        <ReminderDialog
          reminder={editing}
          busy={busy}
          onClose={() => setEditing(null)}
          onSave={(variables) =>
            void act(() => graphql(SAVE, { reminderId: editing.id, ...variables }), t('reminders.saved')).then(
              (isKept) => isKept && setEditing(null),
            )
          }
          onDelete={() =>
            void act(() => graphql(DELETE, { reminderId: editing.id }), t('reminders.removed')).then(
              (isRemoved) => isRemoved && setEditing(null),
            )
          }
        />
      ) : null}
    </div>
  )
}

function ReminderRow({
  reminder,
  language,
  busy,
  onDone,
  onOpen,
}: {
  reminder: Reminder
  language: string
  busy: boolean
  onDone: (reminder: Reminder, isDone: boolean) => void
  onOpen: (reminder: Reminder) => void
}) {
  const { t } = useTranslation()
  const due = dueOf(reminder, language)
  return (
    <li className={['reminder-row', reminder.isDone ? 'done' : ''].filter(Boolean).join(' ')}>
      <input
        type="checkbox"
        checked={reminder.isDone}
        disabled={busy}
        aria-label={`${reminder.title}: ${reminder.isDone ? t('reminders.reopen') : t('reminders.markDone')}`}
        onChange={(event) => onDone(reminder, event.target.checked)}
      />
      <button type="button" className="reminder-text" onClick={() => onOpen(reminder)}>
        <span className="reminder-title">{reminder.title}</span>
        {due ? (
          <span className={['reminder-due', due.isOverdue ? 'overdue' : ''].filter(Boolean).join(' ')}>
            {reminder.isRepeating ? `${due.text} · ${t('reminders.repeats')}` : due.text}
          </span>
        ) : null}
        {reminder.notes ? <span className="reminder-notes muted">{reminder.notes}</span> : null}
      </button>
    </li>
  )
}

function ReminderDialog({
  reminder,
  busy,
  onClose,
  onSave,
  onDelete,
}: {
  reminder: Reminder
  busy: boolean
  onClose: () => void
  onSave: (variables: Record<string, unknown>) => void
  onDelete: () => void
}) {
  const { t } = useTranslation()
  const [title, setTitle] = useState(reminder.title)
  const [notes, setNotes] = useState(reminder.notes)
  const [day, setDay] = useState(dayOf(reminder))
  const [time, setTime] = useState(timeOf(reminder))
  const [isDeleting, setDeleting] = useState(false)
  if (isDeleting) {
    return (
      <ConfirmDialog
        title={t('reminders.removeTitle')}
        body={t('reminders.removeBody', { title: reminder.title })}
        confirmLabel={t('reminders.remove')}
        busy={busy}
        onConfirm={onDelete}
        onClose={() => setDeleting(false)}
      />
    )
  }
  return (
    <FormDialog
      title={t('reminders.edit')}
      submitLabel={t('common.save')}
      busy={busy}
      canSubmit={title.trim() !== ''}
      otherAction={
        <button type="button" className="danger" disabled={busy} onClick={() => setDeleting(true)}>
          {t('reminders.remove')}
        </button>
      }
      onClose={onClose}
      onSubmit={() => {
        // Only what changed here: a phone may have changed the rest while the
        // dialog was open, and sending it back would undo that.
        const variables: Record<string, unknown> = {}
        if (title.trim() !== reminder.title) variables.title = title.trim()
        if (notes !== reminder.notes) variables.notes = notes
        if (day !== dayOf(reminder) || time !== timeOf(reminder))
          Object.assign(variables, dueVariables(day, day ? time : ''))
        onSave(variables)
      }}
    >
      <label>
        {t('reminders.title')}
        <input type="text" value={title} onChange={(event) => setTitle(event.target.value)} />
      </label>
      <label>
        {t('reminders.notes')}
        <textarea rows={3} value={notes} onChange={(event) => setNotes(event.target.value)} />
      </label>
      <div className="row">
        <label>
          {t('reminders.dueDay')}
          <input type="date" value={day} onChange={(event) => setDay(event.target.value)} />
        </label>
        <label>
          {t('reminders.dueTime')}
          <input type="time" value={time} disabled={!day} onChange={(event) => setTime(event.target.value)} />
        </label>
      </div>
    </FormDialog>
  )
}

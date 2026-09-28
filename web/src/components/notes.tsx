import { useEffect, useRef, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'

import { MailboxFolder, graphql } from '../api'
import { useTranslation } from '../i18n/i18n'
import { folderLabel } from '../mailboxes'
import { useBreadcrumbDetail } from './breadcrumb'
import { ErrorMessage, Loading } from './common'
import { ConfirmDialog } from './dialog'
import { ArrowLeftIcon, TrashIcon } from './icons'
import { RelativeTime } from './relativeTime'
import { RichTextEditor } from './richText'
import { SettingsEmpty } from './settingsList'
import { useToast } from './toast'
import { useQuery } from './useQuery'

// The notes folder, shown as notes rather than as mail: what a phone's Notes
// app keeps in the mailbox, listed by title and edited here. The same
// operations as teanode note and the agent's note tool.

export interface Note {
  id: string
  mailboxId: string
  folderId: string
  title: string
  preview: string
  createdAt: string
  modifiedAt: string
  html?: string
  text?: string
}

// NEW_NOTE is the path segment of a note not yet saved. A note's own
// identifier is a UUID, so it cannot be this word.
export const NEW_NOTE = 'new'

const FIELDS = `id mailboxId folderId title preview createdAt modifiedAt`

export const LIST_NOTES = `query ($mailboxId: String!) { ListNotes(mailboxId: $mailboxId) { ${FIELDS} } }`

const GET = `query ($mailboxId: String!, $noteId: String!) { GetNote(mailboxId: $mailboxId, noteId: $noteId) { ${FIELDS} html text } }`

const SAVE = `
  mutation ($mailboxId: String!, $noteId: String, $html: String) {
    SaveNote(mailboxId: $mailboxId, noteId: $noteId, html: $html) { ${FIELDS} html }
  }`

const DELETE = `mutation ($mailboxId: String!, $noteId: String!) { DeleteNote(mailboxId: $mailboxId, noteId: $noteId) }`

// isNotesFolder says whether a folder is where the phone keeps notes: it
// holds one, or it is the owner's own folder called Notes, which is what the
// phone makes and so what an empty one is called.
export function isNotesFolder(folder: MailboxFolder, notes: Note[]): boolean {
  if (notes.some((note) => note.folderId === folder.id)) return true
  return !folder.kind && folder.name.trim().toLowerCase() === 'notes'
}

function newestFirst(notes: Note[]): Note[] {
  return [...notes].sort((left, right) => Date.parse(right.modifiedAt) - Date.parse(left.modifiedAt))
}

export function NotesView({ folder, noteId }: { folder: MailboxFolder; noteId?: string }) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const mailboxId = folder.mailboxId
  const notes = useQuery(() => graphql<{ ListNotes: Note[] }>(LIST_NOTES, { mailboxId }), [mailboxId], {
    refresh: true,
  })
  const listed = newestFirst((notes.data?.ListNotes ?? []).filter((note) => note.folderId === folder.id))
  const open = noteId ? listed.find((note) => note.id === noteId) : undefined
  useBreadcrumbDetail(
    folderLabel(t, folder),
    noteId ? (noteId === NEW_NOTE ? t('notes.new') : open?.title || t('notes.untitled')) : undefined,
  )

  const folderPath = `/mailbox/${folder.id}`

  return (
    <div className="mailbox-frame">
      <div className={['notes', noteId ? 'reading' : ''].filter(Boolean).join(' ')}>
        <div className="notes-list">
          <div className="notes-list-head">
            <button type="button" className="primary" onClick={() => navigate(`${folderPath}/${NEW_NOTE}`)}>
              {t('notes.new')}
            </button>
          </div>
          {notes.loading && !notes.data ? (
            <Loading />
          ) : notes.error && !notes.data ? (
            <ErrorMessage error={notes.error} />
          ) : listed.length === 0 ? (
            <SettingsEmpty>{t('notes.none')}</SettingsEmpty>
          ) : (
            <ul className="notes-rows">
              {listed.map((note) => (
                <li key={note.id}>
                  <Link
                    to={`${folderPath}/${note.id}`}
                    className={['notes-row', note.id === noteId ? 'active' : ''].filter(Boolean).join(' ')}
                    aria-current={note.id === noteId ? 'page' : undefined}
                  >
                    <span className="notes-row-title">{note.title || t('notes.untitled')}</span>
                    <span className="notes-row-when">
                      <RelativeTime value={note.modifiedAt} />
                    </span>
                    <span className="notes-row-preview">{note.preview || t('notes.noPreview')}</span>
                  </Link>
                </li>
              ))}
            </ul>
          )}
        </div>

        <div className="notes-pane">
          {noteId ? (
            <NoteEditor
              key={noteId}
              mailboxId={mailboxId}
              noteId={noteId}
              onBack={() => navigate(folderPath)}
              onSaved={(saved) => {
                void notes.reload(true)
                if (noteId === NEW_NOTE) {
                  navigate(`/mailbox/${saved.folderId || folder.id}/${saved.id}`, { replace: true })
                }
              }}
              onDeleted={() => {
                void notes.reload(true)
                navigate(folderPath, { replace: true })
              }}
            />
          ) : (
            <div className="notes-pane-placeholder muted">{t('notes.choose')}</div>
          )}
        </div>
      </div>
    </div>
  )
}

function NoteEditor({
  mailboxId,
  noteId,
  onBack,
  onSaved,
  onDeleted,
}: {
  mailboxId: string
  noteId: string
  onBack: () => void
  onSaved: (saved: Note) => void
  onDeleted: () => void
}) {
  const { t } = useTranslation()
  const toast = useToast()
  const isNew = noteId === NEW_NOTE
  const note = useQuery(
    () =>
      isNew
        ? Promise.resolve({ GetNote: null as Note | null })
        : graphql<{ GetNote: Note | null }>(GET, { mailboxId, noteId }),
    [mailboxId, noteId, isNew],
    { refresh: !isNew },
  )
  // What the editor holds, and what was last loaded or saved: the difference
  // is what Save would write.
  const [html, setHtml] = useState('')
  const [savedHtml, setSavedHtml] = useState('')
  const [modifiedAt, setModifiedAt] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [isDeleting, setDeleting] = useState(false)
  const isDirty = html !== savedHtml

  // The newest version the editor has taken in. A refresh brings an edit made
  // on the phone into the editor only while nothing typed here is unsaved,
  // and only when it is newer than what the editor has: the answer to a
  // refresh that left before a save must not put the older text back.
  const shownModifiedAt = useRef(0)
  const loaded = note.data?.GetNote
  useEffect(() => {
    if (!loaded || isDirty) return
    const loadedAt = Date.parse(loaded.modifiedAt)
    if (loadedAt <= shownModifiedAt.current) return
    shownModifiedAt.current = loadedAt
    setHtml(loaded.html ?? '')
    setSavedHtml(loaded.html ?? '')
    setModifiedAt(loaded.modifiedAt)
  }, [loaded, isDirty])

  async function save() {
    setBusy(true)
    try {
      const response = await graphql<{ SaveNote: Note }>(SAVE, {
        mailboxId,
        ...(isNew ? {} : { noteId }),
        html,
      })
      const saved = response.SaveNote
      // What was typed stays in the editor rather than the server's copy of
      // it: putting that back would move the cursor to the start.
      shownModifiedAt.current = Math.max(shownModifiedAt.current, Date.parse(saved.modifiedAt))
      setSavedHtml(html)
      setModifiedAt(saved.modifiedAt)
      toast.done(t('notes.saved'))
      onSaved(saved)
    } catch (caught) {
      toast.failure(caught, t('notes.failed'))
    } finally {
      setBusy(false)
    }
  }

  async function remove() {
    setBusy(true)
    try {
      await graphql(DELETE, { mailboxId, noteId })
      toast.done(t('notes.deleted'))
      setDeleting(false)
      onDeleted()
    } catch (caught) {
      toast.failure(caught, t('notes.failed'))
    } finally {
      setBusy(false)
    }
  }

  if (!isNew && note.error && !note.data) {
    return <ErrorMessage error={note.error} />
  }
  if (!isNew && !loaded) {
    return <Loading />
  }

  const title = loaded?.title || t('notes.untitled')

  return (
    <div className="notes-editor">
      <div className="notes-editor-actions">
        <button
          type="button"
          className="icon-action notes-back"
          title={t('mailbox.backToList')}
          aria-label={t('mailbox.backToList')}
          onClick={onBack}
        >
          <ArrowLeftIcon size={16} />
        </button>
        <span className="notes-editor-when muted">
          {modifiedAt ? (
            <>
              {t('notes.edited')} <RelativeTime value={modifiedAt} />
            </>
          ) : (
            t('notes.notSaved')
          )}
        </span>
        {!isNew && (
          <button
            type="button"
            className="icon-action danger"
            title={t('common.delete')}
            aria-label={`${title}: ${t('common.delete')}`}
            disabled={busy}
            onClick={() => setDeleting(true)}
          >
            <TrashIcon size={16} />
          </button>
        )}
        <button type="button" className="primary" disabled={busy || !isDirty} onClick={() => void save()}>
          {t('common.save')}
        </button>
      </div>
      <RichTextEditor value={html} readOnly={busy} placeholder={t('notes.placeholder')} onChange={setHtml} />
      {isDeleting && (
        <ConfirmDialog
          title={t('notes.deleteTitle')}
          body={t('notes.deleteBody', { title })}
          confirmLabel={t('common.delete')}
          busy={busy}
          onConfirm={() => void remove()}
          onClose={() => setDeleting(false)}
        />
      )}
    </div>
  )
}

import { useCallback, useEffect, useRef, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'

import { MailboxFolder, graphql } from '../api'
import { useTranslation } from '../i18n/i18n'
import { folderLabel } from '../mailboxes'
import { useBreadcrumbDetail } from './breadcrumb'
import { ErrorMessage, Loading } from './common'
import { ConfirmDialog } from './dialog'
import { ArrowLeftIcon, TrashIcon } from './icons'
import { RelativeTime } from './relativeTime'
import { RichTextEditor, quotableHtml } from './richText'
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
  // Whether it can be changed here: a note with pictures or attachments
  // cannot, since a new version is written from its HTML alone.
  isEditable: boolean
  html?: string
  text?: string
}

// NEW_NOTE is the path segment of a note not yet saved. A note's own
// identifier is a UUID, so it cannot be this word.
export const NEW_NOTE = 'new'

const FIELDS = `id mailboxId folderId title preview isEditable createdAt modifiedAt`

export const LIST_NOTES = `query ($mailboxId: String!) { ListNotes(mailboxId: $mailboxId) { ${FIELDS} } }`

const GET = `query ($mailboxId: String!, $noteId: String!) { GetNote(mailboxId: $mailboxId, noteId: $noteId) { ${FIELDS} html text } }`

const SAVE = `
  mutation ($mailboxId: String!, $noteId: String, $html: String, $folderId: String, $expectedModifiedAt: String) {
    SaveNote(
      mailboxId: $mailboxId
      noteId: $noteId
      html: $html
      folderId: $folderId
      expectedModifiedAt: $expectedModifiedAt
    ) { ${FIELDS} html }
  }`

const DELETE = `mutation ($mailboxId: String!, $noteId: String!) { DeleteNote(mailboxId: $mailboxId, noteId: $noteId) }`

// isNotesFolder says whether a folder is shown as notes: one of the owner's
// own folders holding notes and nothing else, or an empty one called Notes,
// which is what the phone makes. A folder where somebody filed notes among
// mail stays mail, so none of the mail is hidden behind the notes view.
export function isNotesFolder(folder: MailboxFolder): boolean {
  if (folder.kind) return false
  const noteCount = folder.noteCount ?? 0
  if (noteCount > 0) return noteCount === folder.total
  return folder.total === 0 && folder.name.trim().toLowerCase() === 'notes'
}

// isConflict says whether a save was refused because the note changed
// elsewhere after it was loaded.
function isConflict(caught: unknown): boolean {
  return caught instanceof Error && caught.message.includes('api: conflict')
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
              folderId={folder.id}
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
  folderId,
  noteId,
  onBack,
  onSaved,
  onDeleted,
}: {
  mailboxId: string
  folderId: string
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
  const [isBusy, setIsBusy] = useState(false)
  const [isDeleting, setIsDeleting] = useState(false)
  const isDirty = html !== savedHtml

  // The newest version the editor has taken in. A refresh brings an edit made
  // on the phone into the editor only while nothing typed here is unsaved,
  // and only when it is newer than what the editor has: the answer to a
  // refresh that left before a save must not put the older text back.
  const shownModifiedAt = useRef(0)
  const loaded = note.data?.GetNote
  const isEditable = isNew || (loaded?.isEditable ?? true)

  // show puts a version of the note into the editor. The server has already
  // made its HTML safe for this page; quotableHtml is the same rule the
  // compose page applies to a quoted message, kept here too so the editor
  // never takes in a style block or an id whatever the server sent.
  const show = useCallback((version: Note) => {
    const shownHtml = quotableHtml(version.html ?? '')
    shownModifiedAt.current = Date.parse(version.modifiedAt)
    setHtml(shownHtml)
    setSavedHtml(shownHtml)
    setModifiedAt(version.modifiedAt)
  }, [])

  useEffect(() => {
    if (!loaded || isDirty) return
    if (Date.parse(loaded.modifiedAt) <= shownModifiedAt.current) return
    show(loaded)
  }, [loaded, isDirty, show])

  // After a save was refused because the note changed elsewhere, the editor
  // shows that change, and the toast says why what was typed is gone rather
  // than leaving it to be saved over the other change.
  async function showChangedElsewhere() {
    try {
      const response = await graphql<{ GetNote: Note | null }>(GET, { mailboxId, noteId })
      if (response.GetNote) show(response.GetNote)
    } catch (caught) {
      toast.failure(caught, t('notes.failed'))
    }
  }

  async function save() {
    setIsBusy(true)
    try {
      const response = await graphql<{ SaveNote: Note }>(SAVE, {
        mailboxId,
        ...(isNew ? { folderId } : { noteId, ...(modifiedAt ? { expectedModifiedAt: modifiedAt } : {}) }),
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
      if (isConflict(caught)) {
        toast.failed(t('notes.changedElsewhere'))
        await showChangedElsewhere()
      } else {
        toast.failure(caught, t('notes.failed'))
      }
    } finally {
      setIsBusy(false)
    }
  }

  async function remove() {
    setIsBusy(true)
    try {
      await graphql(DELETE, { mailboxId, noteId })
      toast.done(t('notes.deleted'))
      setIsDeleting(false)
      onDeleted()
    } catch (caught) {
      toast.failure(caught, t('notes.failed'))
    } finally {
      setIsBusy(false)
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
            disabled={isBusy}
            onClick={() => setIsDeleting(true)}
          >
            <TrashIcon size={16} />
          </button>
        )}
        {isEditable && (
          <button type="button" className="primary" disabled={isBusy || !isDirty} onClick={() => void save()}>
            {t('common.save')}
          </button>
        )}
      </div>
      {!isEditable && <p className="notes-editor-uneditable muted">{t('notes.notEditable')}</p>}
      <RichTextEditor
        value={html}
        readOnly={isBusy || !isEditable}
        placeholder={t('notes.placeholder')}
        onChange={setHtml}
      />
      {isDeleting && (
        <ConfirmDialog
          title={t('notes.deleteTitle')}
          body={t('notes.deleteBody', { title })}
          confirmLabel={t('common.delete')}
          busy={isBusy}
          onConfirm={() => void remove()}
          onClose={() => setIsDeleting(false)}
        />
      )}
    </div>
  )
}

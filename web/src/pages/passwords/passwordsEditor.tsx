import { useState } from 'react'

import { FormDialog } from '../../components/dialog'
import { useToast } from '../../components/toast'
import { Key, useTranslation } from '../../i18n/i18n'
import {
  CardFields,
  emptyCardFields,
  emptyLoginFields,
  emptyNoteFields,
  ItemKind,
  LoginFields,
  NoteFields,
} from '../../passwords/fields'
import { parseOneTimeCodeUri } from '../../passwords/oneTimeCode'
import { STALE_REVISION_PHRASE } from './passwordsApi'
import { NO_PASSWORD_MANAGER, PasswordGenerator, secretNumberInputProps } from './passwordsParts'
import { PasswordItem } from './passwordsProtocol'
import { useFailureToast } from './passwordsErrors'
import { PasswordItemDraft, passwordsSession } from './passwordsSession'

// The editor, one form per kind, for a new item and for a changed one. A
// change is sent with the revision it was based on; if another device changed
// the item first the server refuses it, the editor stays open with what was
// typed, the person is told, and Reload reads the other device's change in its
// place, so one device never overwrites another's change without anybody
// seeing it.

// siteFromText is a site as it is kept: an address with its scheme, https
// when none was typed.
export function siteFromText(text: string): string {
  const site = text.trim()
  return site.includes('://') ? site : `https://${site}`
}

// sitesFromText reads one site per line.
export function sitesFromText(text: string): string[] {
  const sites: string[] = []
  for (const line of text.split('\n')) {
    if (line.trim() === '') continue
    const site = siteFromText(line)
    if (!sites.includes(site)) sites.push(site)
  }
  return sites
}

// oneTimeCodeUriFromText takes what was pasted in the one-time code field: an
// otpauth:// address, kept as it is, or a secret, which gets an address made
// around it. It returns null when the text is not a code the page can make.
export function oneTimeCodeUriFromText(text: string, name: string): string | null {
  const cleaned = text.trim()
  if (cleaned === '') return ''
  const uri = cleaned.toLowerCase().startsWith('otpauth://')
    ? cleaned
    : `otpauth://totp/${encodeURIComponent(name || 'TeaNode')}?secret=${encodeURIComponent(cleaned.replace(/\s/g, ''))}`
  try {
    parseOneTimeCodeUri(uri)
  } catch {
    return null
  }
  return uri
}

const TITLES: Record<ItemKind, { create: Key; change: Key }> = {
  login: { create: 'passwords.newLogin', change: 'passwords.editLogin' },
  card: { create: 'passwords.newCard', change: 'passwords.editCard' },
  note: { create: 'passwords.newNote', change: 'passwords.editNote' },
}

export function PasswordsEditor({
  itemKind,
  existing: openedItem,
  folders,
  onSaved,
  onClose,
}: {
  itemKind: ItemKind
  // existing is the item being changed; null for a new one.
  existing: PasswordItem | null
  // folders are the ones already in use, offered while typing a folder.
  folders: string[]
  onSaved: (item: PasswordItem) => void
  onClose: () => void
}) {
  const { t } = useTranslation()
  const toast = useToast()
  const reportFailure = useFailureToast()
  const [isBusy, setIsBusy] = useState(false)
  // The item as it was when the editor opened, and so the revision a save is
  // based on: a newer one read in the background must not be saved over
  // without the person having seen it. Only Reload replaces it.
  const [existing, setExisting] = useState(openedItem)
  const [isStale, setIsStale] = useState(false)

  const [login, setLogin] = useState<LoginFields>(() =>
    existing?.itemKind === 'login' ? existing.fields : emptyLoginFields(),
  )
  const [card, setCard] = useState<CardFields>(() =>
    existing?.itemKind === 'card' ? existing.fields : emptyCardFields(),
  )
  const [note, setNote] = useState<NoteFields>(() =>
    existing?.itemKind === 'note' ? existing.fields : emptyNoteFields(),
  )
  // The sites are typed one to a line, and the one-time code as the person
  // pasted it; both are read when the form is sent.
  const [sitesText, setSitesText] = useState(() =>
    existing?.itemKind === 'login' ? existing.fields.sites.join('\n') : '',
  )
  const [oneTimeCodeText, setOneTimeCodeText] = useState(() =>
    existing?.itemKind === 'login' ? existing.fields.oneTimeCodeUri : '',
  )
  const [isPasswordShown, setIsPasswordShown] = useState(false)
  const [isGeneratorOpen, setIsGeneratorOpen] = useState(false)

  const name = itemKind === 'login' ? login.name : itemKind === 'card' ? card.name : note.name

  async function save() {
    let draft: PasswordItemDraft
    if (itemKind === 'login') {
      const oneTimeCodeUri = oneTimeCodeUriFromText(oneTimeCodeText, login.name.trim())
      if (oneTimeCodeUri === null) {
        toast.failed(t('passwords.oneTimeCodeInvalid'))
        return
      }
      draft = {
        itemKind: 'login',
        fields: { ...login, name: login.name.trim(), sites: sitesFromText(sitesText), oneTimeCodeUri },
      }
    } else if (itemKind === 'card') {
      draft = { itemKind: 'card', fields: { ...card, name: card.name.trim() } }
    } else {
      draft = { itemKind: 'note', fields: { ...note, name: note.name.trim() } }
    }
    setIsBusy(true)
    try {
      const saved = await passwordsSession.saveItem(draft, existing)
      toast.done(t('passwords.itemSaved', { name: saved.fields.name }))
      onSaved(saved)
    } catch (failure) {
      if (failure instanceof Error && failure.message.includes(STALE_REVISION_PHRASE)) {
        toast.failed(t('passwords.itemChangedElsewhere'))
        setIsStale(true)
      } else {
        reportFailure(failure, t('passwords.saveFailed'))
      }
    } finally {
      setIsBusy(false)
    }
  }

  // reload reads what the other device saved and puts it in the form in place
  // of what was typed.
  async function reload() {
    if (existing === null) return
    setIsBusy(true)
    try {
      await passwordsSession.refresh()
      const latest = passwordsSession.getSnapshot().items.find((candidate) => candidate.id === existing.id)
      if (!latest || latest.isUnreadable || latest.deletedAt !== null) {
        toast.failed(t('passwords.itemNotFound'))
        onClose()
        return
      }
      setExisting(latest)
      if (latest.itemKind === 'login') {
        setLogin(latest.fields)
        setSitesText(latest.fields.sites.join('\n'))
        setOneTimeCodeText(latest.fields.oneTimeCodeUri)
      } else if (latest.itemKind === 'card') {
        setCard(latest.fields)
      } else {
        setNote(latest.fields)
      }
      setIsStale(false)
    } catch (failure) {
      reportFailure(failure, t('passwords.reloadFailed'))
    } finally {
      setIsBusy(false)
    }
  }

  const folderListId = 'passwords-folders'

  return (
    <FormDialog
      wide
      title={t(existing ? TITLES[itemKind].change : TITLES[itemKind].create)}
      submitLabel={t('common.save')}
      busy={isBusy}
      canSubmit={name.trim() !== ''}
      otherAction={
        isStale ? (
          <button type="button" disabled={isBusy} onClick={() => void reload()}>
            {t('passwords.reload')}
          </button>
        ) : undefined
      }
      onSubmit={() => void save()}
      onClose={onClose}
    >
      <label>
        <span>{t('passwords.fieldName')}</span>
        <input
          type="text"
          value={name}
          autoComplete="off"
          {...NO_PASSWORD_MANAGER}
          onChange={(event) => {
            if (itemKind === 'login') setLogin({ ...login, name: event.target.value })
            else if (itemKind === 'card') setCard({ ...card, name: event.target.value })
            else setNote({ ...note, name: event.target.value })
          }}
        />
      </label>

      {itemKind === 'login' && (
        <>
          <label>
            <span>{t('passwords.fieldUserName')}</span>
            <input
              type="text"
              autoComplete="off"
              {...NO_PASSWORD_MANAGER}
              autoCapitalize="none"
              spellCheck={false}
              value={login.userName}
              onChange={(event) => setLogin({ ...login, userName: event.target.value })}
            />
          </label>
          <label>
            <span>{t('passwords.fieldPassword')}</span>
            <input
              type={isPasswordShown ? 'text' : 'password'}
              autoComplete="new-password"
              {...NO_PASSWORD_MANAGER}
              spellCheck={false}
              value={login.password}
              onChange={(event) => setLogin({ ...login, password: event.target.value })}
            />
          </label>
          <div className="page-actions">
            <button type="button" onClick={() => setIsPasswordShown(!isPasswordShown)} aria-pressed={isPasswordShown}>
              {isPasswordShown ? t('passwords.hidePassword') : t('passwords.showPassword')}
            </button>
            <button type="button" onClick={() => setIsGeneratorOpen(!isGeneratorOpen)} aria-expanded={isGeneratorOpen}>
              {t('passwords.generate')}
            </button>
          </div>
          {isGeneratorOpen && (
            <PasswordGenerator
              onGenerated={(password) => setLogin((previous) => ({ ...previous, password }))}
              onDone={() => {
                setIsPasswordShown(true)
                setIsGeneratorOpen(false)
              }}
            />
          )}
          <label>
            <span>{t('passwords.fieldSitesHint')}</span>
            <textarea
              rows={2}
              autoComplete="off"
              {...NO_PASSWORD_MANAGER}
              spellCheck={false}
              value={sitesText}
              onChange={(event) => setSitesText(event.target.value)}
            />
          </label>
          <label>
            <span>{t('passwords.fieldOneTimeCodeHint')}</span>
            <input
              type="text"
              autoComplete="off"
              {...NO_PASSWORD_MANAGER}
              spellCheck={false}
              value={oneTimeCodeText}
              onChange={(event) => setOneTimeCodeText(event.target.value)}
            />
          </label>
          <label>
            <span>{t('passwords.fieldNotes')}</span>
            <textarea
              rows={3}
              spellCheck={false}
              value={login.notes}
              onChange={(event) => setLogin({ ...login, notes: event.target.value })}
            />
          </label>
        </>
      )}

      {itemKind === 'card' && (
        <>
          <label>
            <span>{t('passwords.fieldCardholderName')}</span>
            <input
              type="text"
              autoComplete="off"
              {...NO_PASSWORD_MANAGER}
              value={card.cardholderName}
              onChange={(event) => setCard({ ...card, cardholderName: event.target.value })}
            />
          </label>
          <label>
            <span>{t('passwords.fieldCardNumber')}</span>
            <input
              {...secretNumberInputProps()}
              inputMode="numeric"
              autoComplete="off"
              {...NO_PASSWORD_MANAGER}
              {...NO_PASSWORD_MANAGER}
              value={card.number}
              onChange={(event) => setCard({ ...card, number: event.target.value })}
            />
          </label>
          <div className="row">
            <label>
              <span>{t('passwords.fieldExpiryMonth')}</span>
              <input
                type="text"
                inputMode="numeric"
                autoComplete="off"
                {...NO_PASSWORD_MANAGER}
                value={card.expiryMonth}
                onChange={(event) => setCard({ ...card, expiryMonth: event.target.value })}
              />
            </label>
            <label>
              <span>{t('passwords.fieldExpiryYear')}</span>
              <input
                type="text"
                inputMode="numeric"
                autoComplete="off"
                {...NO_PASSWORD_MANAGER}
                value={card.expiryYear}
                onChange={(event) => setCard({ ...card, expiryYear: event.target.value })}
              />
            </label>
          </div>
          <label>
            <span>{t('passwords.fieldSecurityCode')}</span>
            <input
              {...secretNumberInputProps()}
              inputMode="numeric"
              autoComplete="off"
              {...NO_PASSWORD_MANAGER}
              {...NO_PASSWORD_MANAGER}
              value={card.securityCode}
              onChange={(event) => setCard({ ...card, securityCode: event.target.value })}
            />
          </label>
          <label>
            <span>{t('passwords.fieldBrand')}</span>
            <input
              type="text"
              autoComplete="off"
              {...NO_PASSWORD_MANAGER}
              value={card.brand}
              onChange={(event) => setCard({ ...card, brand: event.target.value })}
            />
          </label>
          <label>
            <span>{t('passwords.fieldNotes')}</span>
            <textarea
              rows={3}
              spellCheck={false}
              value={card.notes}
              onChange={(event) => setCard({ ...card, notes: event.target.value })}
            />
          </label>
        </>
      )}

      {itemKind === 'note' && (
        <label>
          <span>{t('passwords.fieldNoteText')}</span>
          <textarea
            rows={8}
            spellCheck={false}
            value={note.text}
            onChange={(event) => setNote({ ...note, text: event.target.value })}
          />
        </label>
      )}

      <label>
        <span>{t('passwords.fieldFolder')}</span>
        <input
          type="text"
          list={folderListId}
          autoComplete="off"
          {...NO_PASSWORD_MANAGER}
          value={itemKind === 'login' ? login.folder : itemKind === 'card' ? card.folder : note.folder}
          onChange={(event) => {
            if (itemKind === 'login') setLogin({ ...login, folder: event.target.value })
            else if (itemKind === 'card') setCard({ ...card, folder: event.target.value })
            else setNote({ ...note, folder: event.target.value })
          }}
        />
        <datalist id={folderListId}>
          {folders.map((folder) => (
            <option key={folder} value={folder} />
          ))}
        </datalist>
      </label>
    </FormDialog>
  )
}

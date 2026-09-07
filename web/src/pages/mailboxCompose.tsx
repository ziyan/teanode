import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'

import { Attachment, MailContent, MailboxItem, graphql } from '../api'
import { ErrorMessage, Loading, formatBytes, formatTime } from '../components/common'
import { ConfirmDialog } from '../components/dialog'
import { PaperclipIcon } from '../components/icons'
import { RichTextEditor, htmlToText, textToHtml } from '../components/richText'
import { useBreadcrumbDetail } from '../components/breadcrumb'
import { useTranslation } from '../i18n/i18n'
import { folderOfKind, useMailboxes } from '../mailboxes'

// Writing from a mailbox: a new message, a reply, a forward, or a draft
// picked up again. One page for the four, told apart by the address bar:
// ?reply=<item>, ?replyAll=<item>, ?forward=<item>, ?draft=<item>.
//
// The draft is saved when asked, when the page is left, and every half
// minute while something has changed. Each save is a new stored message
// that replaces the previous one, which is what a mail program does over
// IMAP, so a draft saved here can be picked up from Thunderbird and back.

const ITEM = `
  query ($itemId: String!) {
    GetMailboxItem(itemId: $itemId) {
      id folderId mailId
      mail { id from sender subject recipients receivedAt messageId }
    }
  }`

const CONTENT = `
  query ($mailId: String!) {
    GetMailContent(mailId: $mailId) {
      mailId available text html
      headers { key value }
      attachments { index filename contentType size inline }
    }
  }`

const DRAFT = `
  query ($itemId: String!) {
    GetMailboxDraft(itemId: $itemId) {
      itemId mailId from fromName to cc bcc subject html text replyToItemId forwardItemId
      attachments { index filename contentType size inline }
    }
  }`

const SEND = `
  mutation ($mailboxId: String!, $message: MailboxMessageParametersInput!) {
    SendMailboxMessage(mailboxId: $mailboxId, message: $message) { mail { id } }
  }`

const SAVE = `
  mutation ($mailboxId: String!, $message: MailboxMessageParametersInput!) {
    SaveMailboxDraft(mailboxId: $mailboxId, message: $message) { id }
  }`

const CONTACTS = `
  query ($mailboxId: String!, $prefix: String, $first: Int) {
    ListMailboxContacts(mailboxId: $mailboxId, prefix: $prefix, first: $first) { address name }
  }`

const AUTOSAVE_INTERVAL = 30_000

type Editor = 'rich' | 'plain'

type Draft = {
  itemId: string
  mailId: string
  from: string
  fromName?: string
  to: string[]
  cc: string[]
  bcc: string[]
  subject: string
  html?: string
  text?: string
  replyToItemId?: string
  forwardItemId?: string
  attachments: Attachment[]
}

function splitAddresses(value: string): string[] {
  return value
    .split(/[,;\n]/)
    .map((entry) => entry.trim())
    .filter((entry) => entry !== '')
}

function readAsBase64(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onerror = () => reject(reader.error)
    reader.onload = () => {
      const result = String(reader.result)
      resolve(result.slice(result.indexOf(',') + 1))
    }
    reader.readAsDataURL(file)
  })
}

// The text of an original message, quoted the way mail has always quoted.
function quoteText(text: string): string {
  return text
    .split('\n')
    .map((line) => (line.startsWith('>') ? `>${line}` : `> ${line}`))
    .join('\n')
}

function escapeHtml(value: string): string {
  return value.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;')
}

function replySubject(subject: string): string {
  return /^\s*re:/i.test(subject) ? subject : `Re: ${subject}`
}

function forwardSubject(subject: string): string {
  return /^\s*fwd?:/i.test(subject) ? subject : `Fwd: ${subject}`
}

export function MailboxComposePage() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const [search] = useSearchParams()
  const mailboxes = useMailboxes()
  // The mailbox this message belongs to: the one holding the item replied
  // to, forwarded or continued, when there is one, else the one the rail
  // shows. A link into another mailbox's message must not be answered from
  // this one.
  const [ownerFolderId, setOwnerFolderId] = useState<string | null>(null)
  const view =
    (ownerFolderId && mailboxes.views.find((candidate) => candidate.folders.some((folder) => folder.id === ownerFolderId))) ||
    mailboxes.current

  const replyTo = search.get('reply') ?? search.get('replyAll')
  const replyAll = search.has('replyAll')
  const forwardOf = search.get('forward')
  const draftOf = search.get('draft')
  const initialTo = search.get('to') ?? ''

  useBreadcrumbDetail(
    draftOf
      ? t('compose.mailbox.draftTitle')
      : replyTo
        ? t('compose.mailbox.replyTitle')
        : forwardOf
          ? t('compose.mailbox.forwardTitle')
          : t('compose.mailbox.title'),
  )

  const [from, setFrom] = useState('')
  const [to, setTo] = useState(initialTo)
  const [cc, setCc] = useState('')
  const [bcc, setBcc] = useState('')
  const [showCc, setShowCc] = useState(false)
  const [subject, setSubject] = useState('')
  const [editor, setEditor] = useState<Editor>('rich')
  const [html, setHtml] = useState('')
  const [text, setText] = useState('')
  const [files, setFiles] = useState<File[]>([])
  const [kept, setKept] = useState<Attachment[]>([])
  const [carried, setCarried] = useState<Attachment[]>([])
  const [draftItemId, setDraftItemId] = useState<string | null>(draftOf)
  const [replyItemId, setReplyItemId] = useState<string | null>(replyTo)
  const [forwardItemId, setForwardItemId] = useState<string | null>(forwardOf)
  const [loading, setLoading] = useState(Boolean(replyTo || forwardOf || draftOf))
  const [loadError, setLoadError] = useState<unknown>(null)
  const [problem, setProblem] = useState<unknown>(null)
  const [sending, setSending] = useState(false)
  const [saving, setSaving] = useState(false)
  const [savedAt, setSavedAt] = useState<Date | null>(null)
  const [sent, setSent] = useState(false)
  const [discarding, setDiscarding] = useState(false)
  const dirty = useRef(false)
  const fileInput = useRef<HTMLInputElement>(null)

  const addresses = useMemo(() => view?.mailbox.addresses ?? [], [view])

  // Whoever has written to this mailbox, offered as the address is typed.
  // The last entry of the field is what is being typed; the ones before
  // the comma are done.
  const [contacts, setContacts] = useState<{ address: string; name?: string }[]>([])
  const [typing, setTyping] = useState('')
  useEffect(() => {
    if (!view) {
      return
    }
    const prefix = typing.split(/[,;]/).pop()?.trim() ?? ''
    const timer = window.setTimeout(() => {
      graphql<{ ListMailboxContacts: { address: string; name?: string }[] }>(CONTACTS, {
        mailboxId: view.mailbox.id,
        prefix,
        first: 10,
      })
        .then((response) => setContacts(response.ListMailboxContacts))
        .catch(() => setContacts([]))
    }, 150)
    return () => window.clearTimeout(timer)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [view?.mailbox.id, typing])
  const completions = (value: string) => {
    const done = value.split(/[,;]/).slice(0, -1).map((entry) => entry.trim()).filter(Boolean)
    const before = done.length > 0 ? done.join(', ') + ', ' : ''
    return contacts.map((contact) => before + (contact.name ? `${contact.name} <${contact.address}>` : contact.address))
  }
  useEffect(() => {
    if (!from && addresses.length > 0) {
      setFrom(addresses[0].address)
    }
  }, [addresses, from])

  // The message being answered or forwarded, or the draft being continued,
  // read once into the fields.
  useEffect(() => {
    if (!view) {
      return
    }
    let cancelled = false
    const load = async () => {
      try {
        if (draftOf) {
          const response = await graphql<{ GetMailboxDraft: Draft }>(DRAFT, { itemId: draftOf })
          const draft = response.GetMailboxDraft
          if (cancelled) {
            return
          }
          const draftItem = (await graphql<{ GetMailboxItem: MailboxItem }>(ITEM, { itemId: draftOf })).GetMailboxItem
          setOwnerFolderId(draftItem.folderId)
          const { cc: draftCopies, bcc: draftBlindCopies } = draft
          setFrom(draft.from || addresses[0]?.address || '')
          setTo(draft.to.join(', '))
          setCc(draftCopies.join(', '))
          setBcc(draftBlindCopies.join(', '))
          setShowCc(draftCopies.length > 0 || draftBlindCopies.length > 0)
          setSubject(draft.subject)
          if (draft.html) {
            setEditor('rich')
            setHtml(draft.html)
          } else {
            setEditor('plain')
            setText(draft.text ?? '')
          }
          setKept(draft.attachments ?? [])
          setReplyItemId(draft.replyToItemId || null)
          setForwardItemId(draft.forwardItemId || null)
        } else if (replyTo || forwardOf) {
          const itemId = (replyTo ?? forwardOf) as string
          const item = (await graphql<{ GetMailboxItem: MailboxItem }>(ITEM, { itemId })).GetMailboxItem
          setOwnerFolderId(item.folderId)
          const original = item.mail
          const content = original
            ? (await graphql<{ GetMailContent: MailContent }>(CONTENT, { mailId: original.id })).GetMailContent
            : null
          if (cancelled) {
            return
          }
          const originalFrom = original?.from || original?.sender || ''
          const when = original?.receivedAt ? formatTime(original.receivedAt) : ''
          const mine = new Set(addresses.map((address) => address.address.toLowerCase()))
          // Answer to the address the message was sent to, when it is one
          // of ours, so a reply leaves from the address that was written to.
          const wroteTo = (original?.recipients ?? []).find((recipient) => mine.has(recipient.toLowerCase()))
          if (wroteTo) {
            setFrom(wroteTo)
          }
          const originalHtml = content?.html || (content?.text ? textToHtml(content.text) : '')
          const originalText = content?.text || (content?.html ? htmlToText(content.html) : '')
          if (replyTo) {
            const replyToHeader = content?.headers?.find((header) => header.key.toLowerCase() === 'reply-to')?.value
            setTo(replyToHeader || originalFrom)
            if (replyAll) {
              const others = (original?.recipients ?? []).filter((recipient) => !mine.has(recipient.toLowerCase()))
              setCc(others.join(', '))
              setShowCc(others.length > 0)
            }
            setSubject(replySubject(original?.subject ?? ''))
            const attribution = t('compose.mailbox.quotedOn', { date: when, from: originalFrom })
            setHtml(
              `<p><br></p><p>${escapeHtml(attribution)}</p><blockquote>${originalHtml}</blockquote>`,
            )
            setText(`\n\n${attribution}\n${quoteText(originalText)}`)
          } else {
            setSubject(forwardSubject(original?.subject ?? ''))
            const header = [
              t('compose.mailbox.forwardedHeader'),
              `${t('mailbox.from')}: ${originalFrom}`,
              `${t('mail.received')}: ${when}`,
              `${t('mailbox.to')}: ${(original?.recipients ?? []).join(', ')}`,
              `${t('compose.mailbox.subject')}: ${original?.subject ?? ''}`,
            ]
            setHtml(
              `<p><br></p><p>---------- ${escapeHtml(header[0])} ----------<br>${header
                .slice(1)
                .map(escapeHtml)
                .join('<br>')}</p>${originalHtml}`,
            )
            setText(`\n\n---------- ${header[0]} ----------\n${header.slice(1).join('\n')}\n\n${originalText}`)
            setCarried((content?.attachments ?? []).filter((attachment) => !attachment.inline))
          }
          if (!original) {
            setProblem(new Error(t('compose.mailbox.originalGone')))
          }
        }
      } catch (failure) {
        if (!cancelled) {
          setLoadError(failure)
        }
      } finally {
        if (!cancelled) {
          setLoading(false)
        }
      }
    }
    void load()
    return () => {
      cancelled = true
    }
    // Once, for the message named in the address bar.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [view?.mailbox.id, draftOf, replyTo, forwardOf])

  // The mailbox's signature, once, under a new message. Not under a draft
  // picked up again, which already has whatever it has.
  const signed = useRef(false)
  useEffect(() => {
    if (signed.current || loading || draftOf || !view) {
      return
    }
    signed.current = true
    const signatureHtml = view.mailbox.signatureHtml || (view.mailbox.signatureText ? textToHtml(view.mailbox.signatureText) : '')
    const signatureText = view.mailbox.signatureText || (view.mailbox.signatureHtml ? htmlToText(view.mailbox.signatureHtml) : '')
    if (signatureHtml) {
      setHtml((previous) => `<p><br></p><p>-- <br>${signatureHtml}</p>${previous}`)
    }
    if (signatureText) {
      setText((previous) => `\n\n-- \n${signatureText}${previous}`)
    }
  }, [loading, draftOf, view])

  const touch = () => {
    dirty.current = true
  }

  const buildMessage = useCallback(
    async () => ({
      from,
      to: splitAddresses(to),
      cc: splitAddresses(cc),
      bcc: splitAddresses(bcc),
      subject,
      htmlContent: editor === 'rich' ? html : '',
      textContent: editor === 'rich' ? htmlToText(html) : text,
      attachments: await Promise.all(
        files.map(async (file) => ({
          filename: file.name,
          contentType: file.type || null,
          content: await readAsBase64(file),
        })),
      ),
      replyToItemId: replyItemId,
      forwardItemId: forwardItemId,
      forwardAttachments: forwardItemId ? carried.map((attachment) => attachment.index) : [],
      draftItemId: draftItemId,
      keepAttachments: draftItemId ? kept.map((attachment) => attachment.index) : [],
    }),
    [from, to, cc, bcc, subject, editor, html, text, files, replyItemId, forwardItemId, carried, draftItemId, kept],
  )

  // The save in flight, so a send can wait for it rather than race it: a
  // send that overtook an autosave left the message sent and a copy of it
  // in Drafts.
  const pendingSave = useRef<Promise<void> | null>(null)

  const save = useCallback(async () => {
    if (!view || saving || sending || sent || !from) {
      return
    }
    setSaving(true)
    let finish: () => void = () => {}
    pendingSave.current = new Promise<void>((resolve) => {
      finish = resolve
    })
    try {
      const message = await buildMessage()
      const response = await graphql<{ SaveMailboxDraft: { id: string } }>(SAVE, {
        mailboxId: view.mailbox.id,
        message,
      })
      // The draft holds every part now — kept, carried and just uploaded —
      // and the server numbers them, so it is asked rather than guessed:
      // the text parts come first and the order is its own.
      const draftId = response.SaveMailboxDraft.id
      setDraftItemId(draftId)
      const stored = (await graphql<{ GetMailboxDraft: Draft }>(DRAFT, { itemId: draftId })).GetMailboxDraft
      setKept((stored.attachments ?? []).filter((attachment) => !attachment.inline))
      setFiles([])
      setCarried([])
      dirty.current = false
      setSavedAt(new Date())
      setProblem(null)
    } catch (failure) {
      setProblem(failure)
    } finally {
      setSaving(false)
      pendingSave.current = null
      finish()
    }
  }, [view, saving, sending, sent, from, buildMessage])

  // Save on a clock while something has changed, and when the page is
  // hidden — a tab closed or switched away from.
  const latestSave = useRef(save)
  latestSave.current = save
  useEffect(() => {
    // Leaving the page — a folder in the rail, the back button — is the
    // most common way to stop writing, and it must not cost what was written.
    return () => {
      if (dirty.current) {
        void latestSave.current()
      }
    }
  }, [])

  useEffect(() => {
    const timer = window.setInterval(() => {
      if (dirty.current) {
        void save()
      }
    }, AUTOSAVE_INTERVAL)
    const onHide = () => {
      if (document.visibilityState === 'hidden' && dirty.current) {
        void save()
      }
    }
    document.addEventListener('visibilitychange', onHide)
    return () => {
      window.clearInterval(timer)
      document.removeEventListener('visibilitychange', onHide)
    }
  }, [save])

  const send = async () => {
    if (!view) {
      return
    }
    setSending(true)
    setProblem(null)
    try {
      if (pendingSave.current) {
        await pendingSave.current
      }
      const message = await buildMessage()
      await graphql(SEND, { mailboxId: view.mailbox.id, message })
      dirty.current = false
      setSent(true)
      void mailboxes.refresh()
    } catch (failure) {
      setProblem(failure)
    } finally {
      setSending(false)
    }
  }

  const discard = async () => {
    if (draftItemId) {
      try {
        await graphql(`mutation ($itemIds: [String!]!) { DeleteMailboxItems(itemIds: $itemIds) }`, {
          itemIds: [draftItemId],
        })
      } catch (failure) {
        setProblem(failure)
        setDiscarding(false)
        return
      }
    }
    dirty.current = false
    void mailboxes.refresh()
    navigate('/mailbox')
  }

  if (!mailboxes.loaded || loading) {
    return <Loading />
  }
  if (!view) {
    return <p className="muted">{t('mailbox.none')}</p>
  }
  if (loadError) {
    return <ErrorMessage error={loadError} />
  }
  if (addresses.length === 0) {
    return (
      <div className="card">
        <p className="muted" style={{ margin: 0 }}>
          {t('compose.mailbox.noAddress')}
        </p>
      </div>
    )
  }

  const sentFolder = folderOfKind(view, 'sent')
  if (sent) {
    return (
      <div className="card">
        <h3>{t('compose.mailbox.sent')}</h3>
        <p className="muted">
          {sentFolder ? <Link to={`/mailbox/${sentFolder.id}`}>{t('compose.mailbox.sentHint')}</Link> : t('compose.mailbox.sentHint')}
        </p>
        <div className="page-actions">
          <Link className="button" to="/mailbox">
            {t('nav.backToMailbox')}
          </Link>
        </div>
      </div>
    )
  }

  const ready =
    from !== '' &&
    !sending &&
    !saving &&
    splitAddresses(to).length + splitAddresses(cc).length + splitAddresses(bcc).length > 0 &&
    (html.trim() !== '' || text.trim() !== '' || files.length + kept.length + carried.length > 0)

  return (
    <form
      className="card compose"
      onSubmit={(event) => {
        event.preventDefault()
        void send()
      }}
    >
      <label>
        {t('compose.mailbox.from')}
        {addresses.length > 1 ? (
          <select
            value={from}
            onChange={(event) => {
              setFrom(event.target.value)
              touch()
            }}
          >
            {addresses.map((address) => (
              <option key={address.aliasId} value={address.address}>
                {address.address}
              </option>
            ))}
          </select>
        ) : (
          <input value={from} readOnly />
        )}
      </label>
      <label>
        {t('compose.mailbox.to')}
        <input
          value={to}
          list="compose-contacts-to"
          onChange={(event) => {
            setTo(event.target.value)
            setTyping(event.target.value)
            touch()
          }}
          placeholder="ada@example.com, Bob <bob@example.org>"
          autoFocus={!replyTo && !forwardOf && !draftOf}
        />
        <datalist id="compose-contacts-to">
          {completions(to).map((option) => (
            <option key={option} value={option} />
          ))}
        </datalist>
      </label>
      {!showCc && (
        <button type="button" className="link" onClick={() => setShowCc(true)}>
          {t('compose.mailbox.showCc')}
        </button>
      )}
      {showCc && (
        <>
          <label>
            {t('compose.mailbox.copy')}
            <input
              value={cc}
              list="compose-contacts-cc"
              onChange={(event) => {
                setCc(event.target.value)
                setTyping(event.target.value)
                touch()
              }}
            />
            <datalist id="compose-contacts-cc">
              {completions(cc).map((option) => (
                <option key={option} value={option} />
              ))}
            </datalist>
          </label>
          <label>
            {t('compose.mailbox.blindCopy')}
            <input
              value={bcc}
              list="compose-contacts-bcc"
              onChange={(event) => {
                setBcc(event.target.value)
                setTyping(event.target.value)
                touch()
              }}
            />
            <datalist id="compose-contacts-bcc">
              {completions(bcc).map((option) => (
                <option key={option} value={option} />
              ))}
            </datalist>
          </label>
        </>
      )}
      <p className="muted field-hint">{t('compose.mailbox.addressesHint')}</p>
      <label>
        {t('compose.mailbox.subject')}
        <input
          value={subject}
          onChange={(event) => {
            setSubject(event.target.value)
            touch()
          }}
        />
      </label>

      <div className="segmented compose-editor-switch" role="group">
        <button
          type="button"
          className={editor === 'rich' ? 'active' : ''}
          onClick={() => {
            if (editor === 'plain') {
              setHtml(textToHtml(text))
            }
            setEditor('rich')
          }}
        >
          {t('compose.mailbox.richText')}
        </button>
        <button
          type="button"
          className={editor === 'plain' ? 'active' : ''}
          onClick={() => {
            if (editor === 'rich') {
              setText(htmlToText(html))
            }
            setEditor('plain')
          }}
        >
          {t('compose.mailbox.plainText')}
        </button>
      </div>
      {editor === 'rich' ? (
        <RichTextEditor
          value={html}
          onChange={(next) => {
            setHtml(next)
            touch()
          }}
        />
      ) : (
        <textarea
          rows={14}
          value={text}
          onChange={(event) => {
            setText(event.target.value)
            touch()
          }}
        />
      )}

      <div className="attachments">
        {[...carried, ...kept].length > 0 && (
          <div className="attachments-kept">
            <span className="muted">{t('compose.mailbox.keptAttachments')}</span>
            <ul>
              {carried.map((attachment) => (
                <li key={`carried-${attachment.index}`}>
                  {attachment.filename} <span className="muted">{formatBytes(attachment.size)}</span>{' '}
                  <button
                    type="button"
                    className="link"
                    onClick={() => {
                      setCarried((previous) => previous.filter((each) => each.index !== attachment.index))
                      touch()
                    }}
                  >
                    {t('compose.mailbox.remove')}
                  </button>
                </li>
              ))}
              {kept.map((attachment) => (
                <li key={`kept-${attachment.index}`}>
                  {attachment.filename} <span className="muted">{formatBytes(attachment.size)}</span>{' '}
                  <button
                    type="button"
                    className="link"
                    onClick={() => {
                      setKept((previous) => previous.filter((each) => each.index !== attachment.index))
                      touch()
                    }}
                  >
                    {t('compose.mailbox.remove')}
                  </button>
                </li>
              ))}
            </ul>
          </div>
        )}
        {files.length > 0 && (
          <ul>
            {files.map((file, index) => (
              <li key={index}>
                {file.name} <span className="muted">{formatBytes(file.size)}</span>{' '}
                <button
                  type="button"
                  className="link"
                  onClick={() => {
                    setFiles((previous) => previous.filter((_, at) => at !== index))
                    touch()
                  }}
                >
                  {t('compose.mailbox.remove')}
                </button>
              </li>
            ))}
          </ul>
        )}
        <input
          ref={fileInput}
          type="file"
          multiple
          hidden
          onChange={(event) => {
            setFiles((previous) => [...previous, ...Array.from(event.target.files ?? [])])
            touch()
            event.target.value = ''
          }}
        />
        <button type="button" className="link" onClick={() => fileInput.current?.click()}>
          <PaperclipIcon /> {t('compose.mailbox.attach')}
        </button>
      </div>

      {problem ? <ErrorMessage error={problem} /> : null}

      <div className="page-actions">
        <button className="primary" type="submit" disabled={!ready}>
          {t('compose.mailbox.send')}
        </button>
        <button type="button" disabled={saving || sending || !from} onClick={() => save()}>
          {t('compose.mailbox.saveDraft')}
        </button>
        <button type="button" className="danger" onClick={() => setDiscarding(true)}>
          {t('compose.mailbox.discard')}
        </button>
        <span className="muted">
          {saving ? t('compose.mailbox.saving') : savedAt ? t('compose.mailbox.draftSaved', { time: formatTime(savedAt.toISOString()) }) : ''}
        </span>
      </div>

      {discarding && (
        <ConfirmDialog
          title={t('compose.mailbox.discard')}
          body={t('compose.mailbox.discardConfirm')}
          confirmLabel={t('compose.mailbox.discard')}
          onConfirm={discard}
          onClose={() => setDiscarding(false)}
        />
      )}
    </form>
  )
}

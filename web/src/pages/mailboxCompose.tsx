import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'

import { Attachment, MailContent, MailboxItem, graphql } from '../api'
import { ErrorMessage, Loading, formatBytes, formatTime } from '../components/common'
import { SettingsEmpty } from '../components/settingsList'
import { ConfirmDialog } from '../components/dialog'
import { PaperclipIcon, SparkIcon } from '../components/icons'
import { RichTextEditor, htmlToText, quotableHtml, textToHtml } from '../components/richText'
import { useBreadcrumbDetail } from '../components/breadcrumb'
import { useToast } from '../components/toast'
import { useTranslation } from '../i18n/i18n'
import { UploadHandle, isCancelled, uploadFiles } from '../upload'
import { folderOfKind, useMailboxes } from '../mailboxes'
import { Combobox, Select } from '../components/select'

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
    SendMailboxMessage(mailboxId: $mailboxId, message: $message) {
      mail { id }
      item { id folderId }
    }
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

// MailboxComposePage is the composer as a page of its own, at
// /mailbox/compose, with what it is answering named in the address bar.
export function MailboxComposePage() {
  const { t } = useTranslation()
  const [search] = useSearchParams()

  const replyTo = search.get('reply') ?? search.get('replyAll')
  const forwardOf = search.get('forward')
  const draftOf = search.get('draft')

  useBreadcrumbDetail(
    draftOf
      ? t('compose.mailbox.draftTitle')
      : replyTo
        ? t('compose.mailbox.replyTitle')
        : forwardOf
          ? t('compose.mailbox.forwardTitle')
          : t('compose.mailbox.title'),
  )

  return (
    <MailboxComposer
      replyTo={replyTo}
      replyAll={search.has('replyAll')}
      forwardOf={forwardOf}
      draftOf={draftOf}
      initialTo={search.get('to') ?? ''}
    />
  )
}

// MailboxComposer is writing a message: a new one, a reply, a forward, or a
// draft picked up again. It is a component rather than a page because it is
// used twice — on its own page, and at the top of a conversation, where a
// reply is written where the conversation is being read.
const DRAFT_REPLY = `
  mutation ($itemId: String!, $instructions: String) {
    DraftReply(itemId: $itemId, instructions: $instructions) { text }
  }`

export function MailboxComposer({
  replyTo,
  replyAll,
  forwardOf,
  draftOf,
  initialTo = '',
  onSent,
  onDiscarded,
  onDraft,
}: {
  replyTo?: string | null
  replyAll?: boolean
  forwardOf?: string | null
  draftOf?: string | null
  initialTo?: string
  // Told when the message has gone, so a conversation can show it. Without
  // one the composer says so itself, which is what the page does.
  onSent?: () => void
  // Told that the draft has been thrown away, and which one it was, so a
  // conversation showing it can stop showing it. This fires only for
  // discarding: closing the composer keeps what was typed, and is the page's
  // own business.
  onDiscarded?: (itemId: string | null) => void
  // Told the id of the draft this has been saved as, each time it is saved.
  //
  // A conversation that closes and reopens the composer — Reply, then Reply
  // to all — unmounts it, and the unmount saves what was typed. Without this
  // the new composer would know nothing of that draft and its first save
  // would write a second one, leaving two drafts of the same reply.
  onDraft?: (itemId: string) => void
}) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const toast = useToast()
  const mailboxes = useMailboxes()
  // The mailbox this message belongs to: the one holding the item replied
  // to, forwarded or continued, when there is one, else the one the rail
  // shows. A link into another mailbox's message must not be answered from
  // this one.
  const [ownerFolderId, setOwnerFolderId] = useState<string | null>(null)
  const view =
    (ownerFolderId &&
      mailboxes.views.find((candidate) => candidate.folders.some((folder) => folder.id === ownerFolderId))) ||
    mailboxes.current

  const [from, setFrom] = useState('')
  const [to, setTo] = useState(initialTo)
  const [cc, setCc] = useState('')
  const [bcc, setBcc] = useState('')
  // Whether this message carries a quoted original, and whether it is being
  // shown. Folded by default: what is being written is the answer, and the
  // thing it answers is above it in the conversation anyway.
  const [quoted, setQuoted] = useState(false)
  const [showQuoted, setShowQuoted] = useState(false)
  const [subject, setSubject] = useState('')
  const [editor, setEditor] = useState<Editor>('rich')
  const [html, setHtml] = useState('')
  const [text, setText] = useState('')
  // The agent's help with a reply: a line saying what it should do, and
  // whether it is being written. The result lands in the editor above
  // whatever is there, and sending stays with the person.
  const [say, setSay] = useState('')
  const [drafting, setDrafting] = useState(false)
  // Files on their way up: one entry per file of the selection in flight,
  // with how far along it is. A selection is one request; another one
  // chosen meanwhile waits its turn, since each rewrites the draft.
  type Uploading = { file: File; progress: number; error?: string }
  const [uploading, setUploading] = useState<Uploading[]>([])
  // Selections waiting their turn behind the upload in flight.
  const [queued, setQueued] = useState(0)
  const queue = useRef<File[][]>([])
  const inFlight = useRef<UploadHandle | null>(null)
  const [kept, setKept] = useState<Attachment[]>([])
  const [carried, setCarried] = useState<Attachment[]>([])
  const [draftItemId, setDraftItemId] = useState<string | null>(draftOf ?? null)
  const [replyItemId, setReplyItemId] = useState<string | null>(replyTo ?? null)
  const [forwardItemId, setForwardItemId] = useState<string | null>(forwardOf ?? null)
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

  const draftWithAgent = async () => {
    if (!replyItemId || drafting) {
      return
    }
    setDrafting(true)
    setProblem(null)
    try {
      const response = await graphql<{ DraftReply: { text: string } }>(DRAFT_REPLY, {
        itemId: replyItemId,
        instructions: say.trim() || undefined,
      })
      const written = response.DraftReply.text
      if (editor === 'rich') {
        setHtml((previous) => textToHtml(written) + previous)
      } else {
        setText((previous) => (previous.trim() ? `${written}\n\n${previous}` : written))
      }
      touch()
    } catch (error) {
      setProblem(error)
    } finally {
      setDrafting(false)
    }
  }

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
    const done = value
      .split(/[,;]/)
      .slice(0, -1)
      .map((entry) => entry.trim())
      .filter(Boolean)
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
    let canceled = false
    const load = async () => {
      try {
        if (draftOf) {
          const response = await graphql<{ GetMailboxDraft: Draft }>(DRAFT, { itemId: draftOf })
          const draft = response.GetMailboxDraft
          if (canceled) {
            return
          }
          const draftItem = (await graphql<{ GetMailboxItem: MailboxItem }>(ITEM, { itemId: draftOf })).GetMailboxItem
          setOwnerFolderId(draftItem.folderId)
          const { cc: draftCopies, bcc: draftBlindCopies } = draft
          setFrom(draft.from || addresses[0]?.address || '')
          setTo(draft.to.join(', '))
          setCc(draftCopies.join(', '))
          setBcc(draftBlindCopies.join(', '))
          setSubject(draft.subject)
          if (draft.html) {
            setEditor('rich')
            setHtml(draft.html)
            // A draft picked up again carries whatever quote it was written
            // with, so it folds away the same as a fresh reply's.
            setQuoted(draft.html.includes('teanode-quote'))
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
          if (canceled) {
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
          const originalHtml = content?.html
            ? quotableHtml(content.html)
            : content?.text
              ? textToHtml(content.text)
              : ''
          const originalText = content?.text || (content?.html ? htmlToText(content.html) : '')
          if (replyTo) {
            const replyToHeader = content?.headers?.find((header) => header.key.toLowerCase() === 'reply-to')?.value
            setTo(replyToHeader || originalFrom)
            if (replyAll) {
              const others = (original?.recipients ?? []).filter((recipient) => !mine.has(recipient.toLowerCase()))
              setCc(others.join(', '))
            }
            setSubject(replySubject(original?.subject ?? ''))
            const attribution = t('compose.mailbox.quotedOn', { date: when, from: originalFrom })
            setHtml(
              `<p><br></p><div class="teanode-quote"><p>${escapeHtml(attribution)}</p>` +
                `<blockquote>${originalHtml}</blockquote></div>`,
            )
            setQuoted(true)
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
              `<p><br></p><div class="teanode-quote">` +
                `<p>---------- ${escapeHtml(header[0])} ----------<br>${header
                  .slice(1)
                  .map(escapeHtml)
                  .join('<br>')}</p>${originalHtml}</div>`,
            )
            setQuoted(true)
            setText(`\n\n---------- ${header[0]} ----------\n${header.slice(1).join('\n')}\n\n${originalText}`)
            setCarried((content?.attachments ?? []).filter((attachment) => !attachment.inline))
          }
          if (!original) {
            setProblem(new Error(t('compose.mailbox.originalGone')))
          }
        }
      } catch (failure) {
        if (!canceled) {
          setLoadError(failure)
        }
      } finally {
        if (!canceled) {
          setLoading(false)
        }
      }
    }
    void load()
    return () => {
      canceled = true
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
    const signatureHtml =
      view.mailbox.signatureHtml || (view.mailbox.signatureText ? textToHtml(view.mailbox.signatureText) : '')
    const signatureText =
      view.mailbox.signatureText || (view.mailbox.signatureHtml ? htmlToText(view.mailbox.signatureHtml) : '')
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
      replyToItemId: replyItemId,
      forwardItemId: forwardItemId,
      forwardAttachments: forwardItemId ? carried.map((attachment) => attachment.index) : [],
      draftItemId: draftItemId,
      keepAttachments: draftItemId ? kept.map((attachment) => attachment.index) : [],
    }),
    [from, to, cc, bcc, subject, editor, html, text, replyItemId, forwardItemId, carried, draftItemId, kept],
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
      // An upload rewrites the draft too, and each replaces the draft the
      // other would write to; the two take turns.
      while (inFlight.current) {
        await inFlight.current.promise.catch(() => {})
      }
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
      latestDraftId.current = draftId
      if (onDraft) {
        onDraft(draftId)
      }
      const stored = (await graphql<{ GetMailboxDraft: Draft }>(DRAFT, { itemId: draftId })).GetMailboxDraft
      setKept((stored.attachments ?? []).filter((attachment) => !attachment.inline))
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

  // The draft in hand, for an upload that arrives while a save is running.
  const latestDraftId = useRef<string | null>(draftItemId)
  latestDraftId.current = draftItemId

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

  // Sending a selection up: to the draft in hand, or making the first draft
  // around the files when there is none yet. The reply names the draft that
  // now holds every part, and the kept list is taken from it.
  const startNextUpload = useCallback(() => {
    if (inFlight.current || !view) {
      return
    }
    if (pendingSave.current) {
      // A save rewrites the draft too; the upload goes after it.
      void pendingSave.current.then(() => startNextUpload())
      return
    }
    const batch = queue.current.shift()
    setQueued(queue.current.length)
    if (!batch) {
      return
    }
    setUploading(batch.map((file) => ({ file, progress: 0 })))
    const draftId = latestDraftId.current
    const handle = uploadFiles(
      draftId ? 'PUT' : 'POST',
      draftId
        ? `/api/v1/mailbox/drafts/${encodeURIComponent(draftId)}/attachments`
        : `/api/v1/mailbox/${encodeURIComponent(view.mailbox.id)}/drafts/attachments`,
      batch,
      (progress) => setUploading(batch.map((file, index) => ({ file, progress: progress.files[index] ?? 0 }))),
    )
    inFlight.current = handle
    handle.promise
      .then((result) => {
        const reply = result as { itemId: string; attachments: Attachment[] }
        setDraftItemId(reply.itemId)
        latestDraftId.current = reply.itemId
        setKept((reply.attachments ?? []).filter((attachment) => !attachment.inline))
        setCarried([])
        setSavedAt(new Date())
        setUploading([])
      })
      .catch((failure) => {
        if (isCancelled(failure)) {
          setUploading([])
          queue.current = []
          setQueued(0)
          return
        }
        setUploading(
          batch.map((file) => ({
            file,
            progress: 0,
            error: failure instanceof Error ? failure.message : String(failure),
          })),
        )
      })
      .finally(() => {
        inFlight.current = null
        startNextUpload()
      })
  }, [view])

  const attach = (chosen: File[]) => {
    if (chosen.length === 0) {
      return
    }
    // A selection larger than a message may be is refused here, with the
    // reason, rather than sent up to be refused at the end.
    const limit = view?.maxMessageSize ?? 0
    const size = chosen.reduce((sum, file) => sum + file.size, 0)
    if (limit > 0 && size > limit) {
      setProblem(new Error(t('compose.mailbox.tooLarge', { size: formatBytes(size), limit: formatBytes(limit) })))
      return
    }
    // Whatever has been typed goes into the draft the upload rewrites, so
    // it is saved first when it has changed; the upload waits for it.
    queue.current.push(chosen)
    setQueued(queue.current.length)
    if (dirty.current && !saving) {
      void save().then(() => startNextUpload())
    } else {
      startNextUpload()
    }
  }

  const send = async () => {
    if (!view) {
      return
    }
    setSending(true)
    setProblem(null)
    try {
      // What is still being saved or uploaded belongs to the message.
      while (pendingSave.current || inFlight.current) {
        await pendingSave.current
        await inFlight.current?.promise.catch(() => {})
      }
      const message = await buildMessage()
      const answer = await graphql<{ SendMailboxMessage: { item: { id: string; folderId: string } | null } }>(SEND, {
        mailboxId: view.mailbox.id,
        message,
      })
      dirty.current = false
      setSent(true)
      void mailboxes.refresh()
      if (onSent) {
        onSent()
        return
      }
      // On a page of its own, sending is the end of the page. Rather than
      // leave a card saying it worked and a link to go and find the message,
      // this opens the message — which is where somebody who just sent one
      // wants to be, and is the same thing the link offered a click later.
      toast.done(t('compose.mailbox.sent'))
      const landed = answer.SendMailboxMessage?.item
      const sentFolderId = landed?.folderId ?? folderOfKind(view, 'sent')?.id
      navigate(
        landed ? `/mailbox/${landed.folderId}/${landed.id}` : sentFolderId ? `/mailbox/${sentFolderId}` : '/mailbox',
      )
    } catch (failure) {
      setProblem(failure)
    } finally {
      setSending(false)
    }
  }

  const discard = async () => {
    inFlight.current?.cancel()
    queue.current = []
    setQueued(0)
    if (draftItemId) {
      try {
        await graphql(
          `
            mutation ($itemIds: [String!]!) {
              DeleteMailboxItems(itemIds: $itemIds)
            }
          `,
          {
            itemIds: [draftItemId],
          },
        )
      } catch (failure) {
        setProblem(failure)
        setDiscarding(false)
        return
      }
    }
    dirty.current = false
    void mailboxes.refresh()
    if (onDiscarded) {
      onDiscarded(draftItemId)
      return
    }
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
        <SettingsEmpty>{t('compose.mailbox.noAddress')}</SettingsEmpty>
      </div>
    )
  }

  if (sent) {
    // Gone, and the page has gone with it: inline, whoever asked for it is
    // showing the conversation; on its own page, the message it sent is
    // already opening.
    return null
  }

  const ready =
    from !== '' &&
    !sending &&
    !saving &&
    splitAddresses(to).length + splitAddresses(cc).length + splitAddresses(bcc).length > 0 &&
    uploading.length === 0 &&
    queued === 0 &&
    (html.trim() !== '' || text.trim() !== '' || kept.length + carried.length > 0)

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
          <Select
            block
            value={from}
            label={t('compose.mailbox.from')}
            options={addresses.map((address) => ({ value: address.address, label: address.address }))}
            onChange={(value) => {
              setFrom(value)
              touch()
            }}
          />
        ) : (
          <input value={from} readOnly />
        )}
      </label>
      <label>
        {t('compose.mailbox.to')}
        <Combobox
          value={to}
          label={t('compose.mailbox.to')}
          suggestions={completions(to)}
          onChange={(value) => {
            setTo(value)
            setTyping(value)
            touch()
          }}
          placeholder="ada@example.com, Bob <bob@example.org>"
          autoFocus={!replyTo && !forwardOf && !draftOf}
        />
      </label>
      {/* Copy and blind copy are fields like any other. They were behind a
          link, which made two ordinary boxes into something to go looking
          for, and put a link where the form's rhythm wanted a label. */}
      <label>
        {t('compose.mailbox.copy')}
        <Combobox
          value={cc}
          label={t('compose.mailbox.copy')}
          suggestions={completions(cc)}
          onChange={(value) => {
            setCc(value)
            setTyping(value)
            touch()
          }}
        />
      </label>
      <label>
        {t('compose.mailbox.blindCopy')}
        <Combobox
          value={bcc}
          label={t('compose.mailbox.blindCopy')}
          suggestions={completions(bcc)}
          onChange={(value) => {
            setBcc(value)
            setTyping(value)
            touch()
          }}
        />
      </label>
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
      {replyItemId && view?.mailbox.agent?.granted && view.mailbox.agent.draftReplies && (
        <div className="compose-agent">
          <input
            value={say}
            placeholder={t('compose.mailbox.agentSay')}
            aria-label={t('compose.mailbox.agentSay')}
            disabled={drafting}
            onChange={(event) => setSay(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === 'Enter') {
                event.preventDefault()
                void draftWithAgent()
              }
            }}
          />
          <button type="button" disabled={drafting} onClick={() => void draftWithAgent()}>
            <SparkIcon size={14} /> {drafting ? t('compose.mailbox.agentDrafting') : t('compose.mailbox.agentDraft')}
          </button>
        </div>
      )}
      {editor === 'rich' ? (
        <RichTextEditor
          value={html}
          hideQuoted={quoted && !showQuoted}
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

      {/* The quote is folded away while the answer is being written, and this
          unfolds it. Only in the rich text editor: the plain one is a
          textarea, where "> " lines are the message and there is nothing to
          fold them into. */}
      {quoted && editor === 'rich' && (
        <div className="compose-quoted">
          <button type="button" className="link" onClick={() => setShowQuoted((previous) => !previous)}>
            {t(showQuoted ? 'compose.mailbox.hideQuoted' : 'compose.mailbox.showQuoted')}
          </button>
        </div>
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
        {uploading.length > 0 && (
          <ul className="uploads">
            {uploading.map((entry, index) => (
              <li key={index} className={entry.error ? 'failed' : ''}>
                <span className="upload-name">{entry.file.name}</span>{' '}
                <span className="muted">{formatBytes(entry.file.size)}</span>
                {entry.error ? (
                  <span className="muted"> {entry.error}</span>
                ) : (
                  <progress
                    max={1}
                    value={entry.progress}
                    aria-label={t('compose.mailbox.uploading', { name: entry.file.name })}
                  />
                )}
              </li>
            ))}
            {/* Canceling is for while the bytes are still going up. Once
                they have all arrived the server is writing the draft, and
                an abort then would leave a draft the page knows nothing
                about. */}
            {uploading.some((entry) => !entry.error) ? (
              uploading.some((entry) => entry.progress < 1) && (
                <li>
                  <button type="button" className="link" onClick={() => inFlight.current?.cancel()}>
                    {t('compose.mailbox.cancelUpload')}
                  </button>
                </li>
              )
            ) : (
              <li>
                <button type="button" className="link" onClick={() => setUploading([])}>
                  {t('compose.mailbox.dismissUpload')}
                </button>
              </li>
            )}
          </ul>
        )}
        <input
          ref={fileInput}
          type="file"
          multiple
          hidden
          onChange={(event) => {
            attach(Array.from(event.target.files ?? []))
            event.target.value = ''
          }}
        />
        <button type="button" className="link" disabled={!from} onClick={() => fileInput.current?.click()}>
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
          {saving
            ? t('compose.mailbox.saving')
            : savedAt
              ? t('compose.mailbox.draftSaved', { time: formatTime(savedAt.toISOString()) })
              : ''}
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

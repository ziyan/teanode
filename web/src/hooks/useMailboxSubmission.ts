import { useRef, useState } from 'react'

import { Attachment, graphql } from '../api'

export class AnotherMailboxSubmissionError extends Error {}

export interface MailboxSendMessage {
  from: string
  to: string[]
  cc: string[]
  bcc: string[]
  subject: string
  htmlContent: string
  textContent: string
  replyToItemId: string | null
  forwardItemId: string | null
  forwardAttachments: number[]
  draftItemId: string | null
  keepAttachments: number[]
}

export interface PendingMailboxSubmission {
  submissionId: string
  mailboxId: string
  message: MailboxSendMessage
  kept: Attachment[]
  carried: Attachment[]
}

export interface AcceptedMailboxSubmission {
  mailId?: string
  sentItemId?: string
  folderId?: string
}

const SEND = `mutation ($mailboxId: String!, $submissionId: String!, $message: MailboxMessageParametersInput!) {
  SendMailboxMessage(mailboxId: $mailboxId, submissionId: $submissionId, message: $message) {
    mail { id } item { id folderId }
  }
}`
const LOOKUP = `query ($mailboxId: String!, $submissionId: String!) {
  GetMailboxSubmission(mailboxId: $mailboxId, submissionId: $submissionId) { mailId sentItemId }
}`
const CANCEL = `mutation ($mailboxId: String!, $submissionId: String!) {
  CancelMailboxSubmission(mailboxId: $mailboxId, submissionId: $submissionId) { mailId sentItemId }
}`

// One unresolved send per account in this tab. Keeping the exact request before
// contacting the server makes a remounted composer recoverable after draft
// reconciliation. Other accounts never load these message bodies.
export function useMailboxSubmission(ownerId: string) {
  const storageKey = `mailbox-pending-send:${ownerId}`
  const readPending = () => {
    const encoded = window.sessionStorage.getItem(storageKey)
    return encoded ? (JSON.parse(encoded) as PendingMailboxSubmission) : null
  }
  const [initial] = useState(() => {
    try {
      return { pending: readPending(), failure: null as unknown }
    } catch (failure) {
      return { pending: null, failure }
    }
  })
  const [pending, setPending] = useState(initial.pending)
  const pendingRequest = useRef(pending)
  const [isWorking, setIsWorking] = useState(false)
  const working = useRef(false)

  const perform = async <Result>(operation: () => Promise<Result>): Promise<Result> => {
    if (working.current) throw new Error('a send request is already in progress')
    working.current = true
    setIsWorking(true)
    try {
      return await operation()
    } finally {
      working.current = false
      setIsWorking(false)
    }
  }

  const send = (request?: Omit<PendingMailboxSubmission, 'submissionId'>) =>
    perform(async (): Promise<AcceptedMailboxSubmission> => {
      if (!ownerId) throw new Error('sending requires an account')
      // Another composer may have saved an uncertain request in this tab.
      let saved = pendingRequest.current
      if (!saved && readPending()) throw new AnotherMailboxSubmissionError()
      const isRetry = Boolean(saved)
      if (!saved) {
        if (!request) throw new Error('no message to send')
        saved = JSON.parse(
          JSON.stringify({
            ...request,
            submissionId: Array.from(crypto.getRandomValues(new Uint8Array(16)), (byte) =>
              byte.toString(16).padStart(2, '0'),
            ).join(''),
          }),
        ) as PendingMailboxSubmission
        // A storage failure must prevent sending, not silently lose its identity.
        window.sessionStorage.setItem(storageKey, JSON.stringify(saved))
      }
      pendingRequest.current = saved
      setPending(saved)
      const identity = { mailboxId: saved.mailboxId, submissionId: saved.submissionId }
      if (isRetry) {
        const response = await graphql<{ GetMailboxSubmission: AcceptedMailboxSubmission | null }>(LOOKUP, identity)
        if (response.GetMailboxSubmission) return response.GetMailboxSubmission
      }
      const response = await graphql<{
        SendMailboxMessage: { mail: { id: string } | null; item: { id: string; folderId: string } | null }
      }>(SEND, { ...identity, message: saved.message })
      return {
        mailId: response.SendMailboxMessage.mail?.id,
        sentItemId: response.SendMailboxMessage.item?.id,
        folderId: response.SendMailboxMessage.item?.folderId,
      }
    })

  const cancel = () =>
    perform(async (): Promise<AcceptedMailboxSubmission | null> => {
      const saved = pendingRequest.current
      if (!saved) throw new Error('no pending send')
      const response = await graphql<{ CancelMailboxSubmission: AcceptedMailboxSubmission | null }>(CANCEL, {
        mailboxId: saved.mailboxId,
        submissionId: saved.submissionId,
      })
      return response.CancelMailboxSubmission
    })

  // Called only after acceptance or committed cancellation, never after a
  // transport error or an empty lookup. A stale retained entry remains safe to
  // recover if storage becomes unavailable while clearing it.
  const clear = () => {
    try {
      if (readPending()?.submissionId === pendingRequest.current?.submissionId) {
        window.sessionStorage.removeItem(storageKey)
      }
    } catch {
      // A later composer will resolve the same accepted or cancelled identity.
    }
    pendingRequest.current = null
    setPending(null)
  }

  return { pending, pendingRequest, isWorking, send, cancel, clear, initialFailure: initial.failure }
}

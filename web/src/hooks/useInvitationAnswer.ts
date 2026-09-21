import { useRef, useState } from 'react'

import { graphql } from '../api'

export type InvitationAnswer = 'ACCEPTED' | 'DECLINED' | 'TENTATIVE'
interface PendingAnswer {
  requestId: string
  itemId: string
  answer: InvitationAnswer
}

const ANSWER = `mutation ($requestId: String!, $itemId: String!, $answer: String!) {
  AnswerMailInvitation(requestId: $requestId, itemId: $itemId, answer: $answer) { id participation }
}`
const LOOKUP = `query ($requestId: String!) {
  GetCalendarRequest(requestId: $requestId) { requestId isMissing }
}`

// The owning card remounts when account or item changes. Persist before sending,
// so reloading after a lost response retries the same answer, not another reply.
export function useInvitationAnswer(ownerId: string, itemId: string) {
  const storageKey = `calendar-pending-answer:${ownerId}:${itemId}`
  const readPending = (): PendingAnswer | null => {
    const encoded = sessionStorage.getItem(storageKey)
    if (!encoded) return null
    const saved = JSON.parse(encoded) as PendingAnswer
    if (!saved.requestId || saved.itemId !== itemId || !['ACCEPTED', 'DECLINED', 'TENTATIVE'].includes(saved.answer)) {
      throw new Error('The saved answer could not be read')
    }
    return saved
  }
  const [initial] = useState(() => {
    try {
      return { pending: readPending(), failure: null as unknown }
    } catch (failure) {
      return { pending: null, failure }
    }
  })
  const [pending, setPending] = useState(initial.pending)
  const [isWorking, setIsWorking] = useState(false)
  const working = useRef(false)

  const send = async (answer: InvitationAnswer) => {
    if (working.current) throw new Error('An answer is already being sent')
    working.current = true
    setIsWorking(true)
    try {
      if (!ownerId) throw new Error('Answering requires an account')
      if (initial.failure) throw initial.failure
      let saved = readPending()
      const isRetry = saved !== null
      if (saved) setPending(saved)
      if (saved && saved.answer !== answer) throw new Error('Confirm the pending answer before choosing another')
      if (!saved) {
        saved = {
          itemId,
          answer,
          requestId: Array.from(crypto.getRandomValues(new Uint8Array(16)), (byte) =>
            byte.toString(16).padStart(2, '0'),
          ).join(''),
        }
        sessionStorage.setItem(storageKey, JSON.stringify(saved))
      }
      setPending(saved)
      let isCompleted = false
      if (isRetry) {
        const response = await graphql<{ GetCalendarRequest: { requestId: string; isMissing: boolean } | null }>(
          LOOKUP,
          { requestId: saved.requestId },
        )
        if (response.GetCalendarRequest && response.GetCalendarRequest.requestId !== saved.requestId) {
          throw new Error('The completed answer did not match the pending request')
        }
        isCompleted = Boolean(response.GetCalendarRequest)
      }
      if (!isCompleted) await graphql(ANSWER, { ...saved })
      // A second mounted reader may already have started a later answer.
      if (readPending()?.requestId === saved.requestId) sessionStorage.removeItem(storageKey)
      setPending(readPending())
    } finally {
      working.current = false
      setIsWorking(false)
    }
  }
  return { pending, isWorking, send }
}

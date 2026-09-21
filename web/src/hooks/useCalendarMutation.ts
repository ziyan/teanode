import { useRef, useState } from 'react'

import { graphql } from '../api'

export interface CalendarMutation {
  operation: 'save' | 'delete'
  summary: string
  variables: Record<string, unknown> & { calendarId: string; id: string | null }
}
interface PendingCalendarMutation extends CalendarMutation {
  requestId: string
}
const SAVE = `mutation ($requestId: String!, $calendarId: String!, $id: String, $summary: String, $location: String,
  $description: String, $startsAt: String, $endsAt: String, $allDay: Boolean,
  $timezone: String, $recurrence: String, $status: String, $attendees: [String!],
  $proposalItemId: String, $proposalIndex: Int, $expectedProposal: String) {
  SaveCalendarEvent(requestId: $requestId, calendarId: $calendarId, id: $id, summary: $summary, location: $location,
    description: $description, startsAt: $startsAt, endsAt: $endsAt, allDay: $allDay,
    timezone: $timezone, recurrence: $recurrence, status: $status, attendees: $attendees, proposalItemId: $proposalItemId,
    proposalIndex: $proposalIndex, expectedProposal: $expectedProposal) { id }
}`
const DELETE = `mutation ($requestId: String!, $calendarId: String!, $id: String!) {
  DeleteCalendarEvent(requestId: $requestId, calendarId: $calendarId, id: $id)
}`
const LOOKUP = `query ($requestId: String!) {
  GetCalendarRequest(requestId: $requestId) { requestId isMissing }
}`

const CANCEL = `mutation ($requestId: String!) {
  CancelCalendarRequest(requestId: $requestId) { requestId isMissing }
}`

// The calendar page remounts on account changes. Keep the exact prepared times
// and fields, rather than recomputing them in a different timezone on retry.
export function useCalendarMutation(ownerId: string) {
  const storageKey = `calendar-pending-change:${ownerId}`
  const readPending = (): PendingCalendarMutation | null => {
    const encoded = sessionStorage.getItem(storageKey)
    if (!encoded) return null
    const saved = JSON.parse(encoded) as PendingCalendarMutation
    if (!saved.requestId || !['save', 'delete'].includes(saved.operation) || !saved.variables?.calendarId) {
      throw new Error('The saved calendar change could not be read')
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
  const execute = async (request?: CalendarMutation) => {
    if (working.current) throw new Error('A calendar change is already in progress')
    working.current = true
    setIsWorking(true)
    try {
      if (!ownerId) throw new Error('Changing a calendar requires an account')
      if (initial.failure) throw initial.failure
      let saved = readPending()
      if (saved && request) {
        setPending(saved)
        throw new Error('Resolve the pending calendar change first')
      }
      const isRetry = saved !== null
      if (!saved) {
        if (!request) throw new Error('There is no pending calendar change')
        saved = JSON.parse(
          JSON.stringify({
            ...request,
            requestId: Array.from(crypto.getRandomValues(new Uint8Array(16)), (byte) =>
              byte.toString(16).padStart(2, '0'),
            ).join(''),
          }),
        ) as PendingCalendarMutation
        sessionStorage.setItem(storageKey, JSON.stringify(saved))
      }
      setPending(saved)
      let isCompleted = false
      if (isRetry) {
        const response = await graphql<{ GetCalendarRequest: { requestId: string; isMissing: boolean } | null }>(
          LOOKUP,
          { requestId: saved.requestId },
        )
        if (response.GetCalendarRequest && response.GetCalendarRequest.requestId !== saved.requestId)
          throw new Error('The completed change did not match the pending request')
        isCompleted = Boolean(response.GetCalendarRequest)
      }
      if (!isCompleted)
        await graphql(saved.operation === 'save' ? SAVE : DELETE, { ...saved.variables, requestId: saved.requestId })
      if (readPending()?.requestId === saved.requestId) sessionStorage.removeItem(storageKey)
      setPending(readPending())
      return saved
    } finally {
      working.current = false
      setIsWorking(false)
    }
  }
  const cancel = async (): Promise<boolean> => {
    if (working.current) throw new Error('A request is already in progress')
    working.current = true
    setIsWorking(true)
    try {
      if (!ownerId) throw new Error('Resolving a request requires an account')
      if (initial.failure) throw initial.failure
      const saved = readPending()
      if (!saved) throw new Error('There is no pending request')
      const response = await graphql<{ CancelCalendarRequest: { requestId: string } | null }>(CANCEL, {
        requestId: saved.requestId,
      })
      if (!('CancelCalendarRequest' in response)) throw new Error('The cancellation result could not be read')
      if (response.CancelCalendarRequest && response.CancelCalendarRequest.requestId !== saved.requestId)
        throw new Error('The completed change did not match the pending request')
      if (readPending()?.requestId === saved.requestId) sessionStorage.removeItem(storageKey)
      setPending(readPending())
      return Boolean(response.CancelCalendarRequest)
    } finally {
      working.current = false
      setIsWorking(false)
    }
  }
  return { pending, isWorking, execute, cancel }
}

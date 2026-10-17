import { useEffect, useSyncExternalStore } from 'react'

import { graphql } from '../api'
import { useTranslation } from '../i18n/i18n'
import { useSession, hasPermission } from '../session'

// The Circles page answered an invitation, so the number on the account
// menu's Circles row has changed. The menu does not wait for its next read.
export const CIRCLES_CHANGED_EVENT = 'teanode:circles-changed'

export function announceCirclesChanged() {
  window.dispatchEvent(new Event(CIRCLES_CHANGED_EVENT))
}

// How often the number is read again, as a list refreshes itself.
const INVITATION_COUNT_EVERY = 10_000

// The number is read once for the whole page, whoever shows it: the account
// button, its menu and the phone's menu button all draw it, and each asking
// the server for itself was three questions every ten seconds, with answers
// that could disagree for a moment. The reading starts with the first one
// that shows it and stops with the last.
let openInvitationCountNow = 0
let readersOfInvitationCount = 0
let invitationCountTimer: number | undefined
let invitationCountReadNumber = 0
const invitationCountListeners = new Set<() => void>()

async function readOpenInvitationCount() {
  const thisReadNumber = ++invitationCountReadNumber
  try {
    const answer = await graphql<{ ListCircleInvitations: { id: string }[] }>('query { ListCircleInvitations { id } }')
    // A newer read has the last word; a failed one keeps what is on screen.
    if (thisReadNumber !== invitationCountReadNumber || answer.ListCircleInvitations.length === openInvitationCountNow) {
      return
    }
    openInvitationCountNow = answer.ListCircleInvitations.length
    invitationCountListeners.forEach((listener) => listener())
  } catch {
    // The next read tries again.
  }
}

function readOpenInvitationCountUnlessHidden() {
  if (!document.hidden) {
    void readOpenInvitationCount()
  }
}

function startReadingInvitationCount() {
  readersOfInvitationCount++
  if (readersOfInvitationCount > 1) {
    return stopReadingInvitationCount
  }
  void readOpenInvitationCount()
  invitationCountTimer = window.setInterval(readOpenInvitationCountUnlessHidden, INVITATION_COUNT_EVERY)
  window.addEventListener(CIRCLES_CHANGED_EVENT, readOpenInvitationCountUnlessHidden)
  // A tab coming back reads at once, as the queries the pages use do.
  document.addEventListener('visibilitychange', readOpenInvitationCountUnlessHidden)
  return stopReadingInvitationCount
}

function stopReadingInvitationCount() {
  readersOfInvitationCount--
  if (readersOfInvitationCount > 0) {
    return
  }
  window.clearInterval(invitationCountTimer)
  window.removeEventListener(CIRCLES_CHANGED_EVENT, readOpenInvitationCountUnlessHidden)
  document.removeEventListener('visibilitychange', readOpenInvitationCountUnlessHidden)
  // Whoever shows it next starts from a new reading, not this person's number.
  invitationCountReadNumber++
  openInvitationCountNow = 0
}

function subscribeToInvitationCount(listener: () => void) {
  invitationCountListeners.add(listener)
  return () => {
    invitationCountListeners.delete(listener)
  }
}

function currentInvitationCount() {
  return openInvitationCountNow
}

// useOpenCircleInvitationCount is how many invitations to join a circle are
// waiting for this person's answer. Zero for somebody who may not use circles,
// without asking the server a question it would refuse.
//
// Here and not in the page because the account menu, which is in the first
// download, shows the number, while the page is fetched only when somebody
// opens it.
export function useOpenCircleInvitationCount(): number {
  const session = useSession()
  const canUseCircles = Boolean(session.userId) && hasPermission(session.permissions, 'circle:use')
  const shared = useSyncExternalStore(subscribeToInvitationCount, currentInvitationCount)
  useEffect(() => (canUseCircles ? startReadingInvitationCount() : undefined), [canUseCircles])
  return canUseCircles ? shared : 0
}

// useLabelWithInvitationCount adds the number of invitations waiting to the
// name of a button that leads to them, for the people who hear the button
// rather than see it: the mark on it is hidden from them.
export function useLabelWithInvitationCount(label: string): string {
  const { plural } = useTranslation()
  const openInvitationCount = useOpenCircleInvitationCount()
  if (openInvitationCount === 0) {
    return label
  }
  return `${label}: ${plural(
    openInvitationCount,
    { one: 'circles.waitingOne', other: 'circles.waiting' },
    { count: openInvitationCount },
  )}`
}

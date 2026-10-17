import { useEffect } from 'react'

import { graphql } from '../api'
import { useSession, hasPermission } from '../session'
import { useQuery } from './useQuery'

// The Circles page answered an invitation, so the number on the account
// menu's Circles row has changed. The menu does not wait for its next read.
export const CIRCLES_CHANGED_EVENT = 'teanode:circles-changed'

export function announceCirclesChanged() {
  window.dispatchEvent(new Event(CIRCLES_CHANGED_EVENT))
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
  const { data, reload } = useQuery(async () => {
    if (!canUseCircles) {
      return 0
    }
    const answer = await graphql<{ ListCircleInvitations: { id: string }[] }>('query { ListCircleInvitations { id } }')
    return answer.ListCircleInvitations.length
  }, [canUseCircles])

  useEffect(() => {
    const onChanged = () => void reload(true)
    window.addEventListener(CIRCLES_CHANGED_EVENT, onChanged)
    return () => window.removeEventListener(CIRCLES_CHANGED_EVENT, onChanged)
  }, [reload])

  return data ?? 0
}

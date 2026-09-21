import { createContext, useContext } from 'react'

import type { Permissions, Session } from './api'

// The session, for every component that has to know who is here and what
// they may do. Provided once by the shell and read wherever a page or the
// rail decides what to show.
//
// What the web UI hides is a courtesy: every mutation is checked again on the
// server, and a page reached by URL for something the caller may not see is
// answered not found there.
const SessionContext = createContext<Session | null>(null)

export const SessionProvider = SessionContext.Provider

export function useSession(): Session {
  const session = useContext(SessionContext)
  if (!session) {
    throw new Error('useSession outside of the shell')
  }
  return session
}

// hasPermission says whether a server or all-domains permission is held.
export function hasPermission(permissions: Permissions | undefined | null, key: string): boolean {
  return Boolean(permissions?.everywhere.includes(key))
}

// widens is the domain permission an all-domains permission stands in for.
function widens(key: string): string | undefined {
  if (key.endsWith('-all')) {
    return key.slice(0, -'-all'.length)
  }
  return undefined
}

// hasAnywhere says whether a domain permission is held over at least one
// domain, or everywhere through the all-domains permission that widens it.
export function hasAnywhere(permissions: Permissions | undefined | null, key: string): boolean {
  if (!permissions) {
    return false
  }
  if (permissions.everywhere.some((held) => held === key || widens(held) === key)) {
    return true
  }
  return permissions.byDomain.some((entry) => entry.permissions.includes(key))
}

// hasOverDomain says whether a domain permission is held over this domain.
export function hasOverDomain(permissions: Permissions | undefined | null, key: string, domainId: string): boolean {
  if (!permissions) {
    return false
  }
  if (permissions.everywhere.some((held) => held === key || widens(held) === key)) {
    return true
  }
  return permissions.byDomain.some((entry) => entry.domainId === domainId && entry.permissions.includes(key))
}

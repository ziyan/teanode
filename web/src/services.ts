import type { ServiceCapabilities, Session } from './api'
import { hasPermission } from './session'

export type HostedServiceKey =
  'isMailRoutingEnabled' | 'isMailboxEnabled' | 'isCalendarEnabled' | 'isRemindersEnabled' | 'isContactsEnabled'

export function serviceForPath(pathname: string): HostedServiceKey | undefined {
  if (pathname.startsWith('/mailbox/contacts') || pathname.startsWith('/contacts')) return 'isContactsEnabled'
  if (pathname.startsWith('/mailbox/calendar') || pathname.startsWith('/calendar')) return 'isCalendarEnabled'
  if (pathname.startsWith('/reminders')) return 'isRemindersEnabled'
  if (
    pathname.startsWith('/mailbox') ||
    pathname.startsWith('/settings/mailbox') ||
    pathname.startsWith('/settings/agent/mail')
  )
    return 'isMailboxEnabled'
  if (['mail', 'queue', 'domains', 'reports'].some((part) => pathname.startsWith(`/manage/${part}`)))
    return 'isMailRoutingEnabled'
  return undefined
}

export function isPathAvailable(pathname: string, services?: ServiceCapabilities): boolean {
  const serviceKey = serviceForPath(pathname)
  if (serviceKey === 'isCalendarEnabled' && services?.isRemindersEnabled === true) return true
  return !serviceKey || services?.[serviceKey] !== false
}

export function homePath(session: Session): string {
  // A new server's first operator: straight to the one page that starts the
  // assistant, unless mail was chosen, whose next step is a domain.
  if (!session.services?.hasCompletedSetup && hasPermission(session.permissions, 'server:manage'))
    return !session.services?.hasModelProvider && !session.services?.isMailRoutingEnabled
      ? '/welcome'
      : '/manage/server/setup'
  if (session.services?.isMailboxEnabled !== false && session.userId && hasPermission(session.permissions, 'mail:read'))
    return '/mailbox'
  if (session.services?.isAssistantEnabled && hasPermission(session.permissions, 'agent:use')) return '/agent'
  if (session.services?.isCalendarEnabled && hasPermission(session.permissions, 'calendar:use')) return '/calendar'
  if (session.services?.isRemindersEnabled && hasPermission(session.permissions, 'calendar:use')) return '/reminders'
  if (session.services?.isContactsEnabled && hasPermission(session.permissions, 'contacts:use')) return '/contacts'
  if (hasPermission(session.permissions, 'server:manage')) return '/manage/server'
  return '/settings/preference'
}

import type { ServiceCapabilities, Session } from './api'
import { hasPermission } from './session'

export type HostedServiceKey =
  | 'isMailRoutingEnabled'
  | 'isMailboxEnabled'
  | 'isCalendarEnabled'
  | 'isRemindersEnabled'
  | 'isContactsEnabled'
  | 'isPhotosEnabled'

export function serviceForPath(pathname: string): HostedServiceKey | undefined {
  if (pathname.startsWith('/mailbox/contacts') || pathname.startsWith('/contacts')) return 'isContactsEnabled'
  if (pathname.startsWith('/mailbox/calendar') || pathname.startsWith('/calendar')) return 'isCalendarEnabled'
  if (pathname.startsWith('/reminders')) return 'isRemindersEnabled'
  if (pathname === '/photos' || pathname.startsWith('/photos/')) return 'isPhotosEnabled'
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
  // A server that does not say is taken to be without Photos, which came
  // after the others; they are on until said otherwise.
  if (serviceKey === 'isPhotosEnabled') return services?.isPhotosEnabled === true
  return !serviceKey || services?.[serviceKey] !== false
}

export function homePath(session: Session): string {
  // A new server's first operator: straight to the one page that starts the
  // assistant, unless mail was chosen, whose next step is a domain. A server
  // an operator set up for Photos, with Photos now running and no mail to set
  // up, is already a working service, so it does not keep sending its
  // operator back here on every sign-in until somebody presses "Finish
  // setup"; the welcome and setup pages stay reachable under Manage, Server,
  // Setup. Photos running is not enough: it is on by default, so a brand-new
  // server has it too and still needs the welcome page for its model key.
  const isSetUpForPhotosAndRunning =
    session.services?.isPhotosChosen === true &&
    session.services.isPhotosEnabled &&
    !session.services.hasPendingServiceRestart &&
    !session.services.isMailRoutingEnabled &&
    hasPermission(session.permissions, 'photos:use')
  if (
    !session.services?.hasCompletedSetup &&
    !isSetUpForPhotosAndRunning &&
    hasPermission(session.permissions, 'server:manage')
  )
    return !session.services?.hasModelProvider && !session.services?.isMailRoutingEnabled
      ? '/welcome'
      : '/manage/server/setup'
  if (session.services?.isMailboxEnabled !== false && session.userId && hasPermission(session.permissions, 'mail:read'))
    return '/mailbox'
  if (session.services?.isAssistantEnabled && hasPermission(session.permissions, 'agent:use')) return '/chat'
  if (session.services?.isCalendarEnabled && hasPermission(session.permissions, 'calendar:use')) return '/calendar'
  if (session.services?.isRemindersEnabled && hasPermission(session.permissions, 'calendar:use')) return '/reminders'
  if (session.services?.isContactsEnabled && hasPermission(session.permissions, 'contacts:use')) return '/contacts'
  if (session.services?.isPhotosEnabled && hasPermission(session.permissions, 'photos:use')) return '/photos'
  if (hasPermission(session.permissions, 'server:manage')) return '/manage/server'
  return '/settings/preference'
}

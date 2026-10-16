import { describe, expect, it } from 'vitest'
import type { ServiceCapabilities, Session } from './api'
import { homePath } from './services'

const operator = (services: Partial<ServiceCapabilities>) =>
  ({
    authenticated: true,
    username: 'owner',
    userId: 'user-1',
    permissions: { everywhere: ['server:manage', 'agent:use'], byDomain: [] },
    services: {
      isMailRoutingEnabled: false,
      isMailboxEnabled: false,
      isCalendarEnabled: false,
      isRemindersEnabled: false,
      isContactsEnabled: false,
      isAssistantEnabled: false,
      hasPendingServiceRestart: false,
      hasCompletedSetup: false,
      ...services,
    },
  }) as unknown as Session

// A new server's first operator lands on the one page that starts the
// assistant; a mail server goes on to its setup, and a finished one home.
describe('homePath', () => {
  it('sends a new operator without a model to the welcome page', () => {
    expect(homePath(operator({}))).toBe('/welcome')
  })
  it('sends a new mail server to its setup', () => {
    expect(homePath(operator({ isMailRoutingEnabled: true, isMailboxEnabled: true }))).toBe('/manage/server/setup')
  })
  it('sends an operator with a model but setup unfinished to setup', () => {
    expect(homePath(operator({ hasModelProvider: true }))).toBe('/manage/server/setup')
  })
  it('sends a finished assistant server to the assistant', () => {
    expect(homePath(operator({ hasCompletedSetup: true, hasModelProvider: true, isAssistantEnabled: true }))).toBe(
      '/agent',
    )
  })
})

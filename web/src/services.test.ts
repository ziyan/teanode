import { describe, expect, it } from 'vitest'
import type { ServiceCapabilities, Session } from './api'
import { homePath, isPathAvailable } from './services'

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
      isPhotosEnabled: false,
      isPhotosChosen: false,
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
  // Photos is on by default, so a brand-new server has it running too, and
  // its first operator still has to be asked for a model key.
  it('sends a brand-new server whose Photos is on only by default to the welcome page', () => {
    const session = operator({ isPhotosEnabled: true })
    session.permissions = { everywhere: ['server:manage', 'photos:use'], byDomain: [] }
    expect(homePath(session)).toBe('/welcome')
  })
  it('sends an operator who chose Photos, now running, home and not to setup', () => {
    const session = operator({ isPhotosEnabled: true, isPhotosChosen: true })
    session.permissions = { everywhere: ['server:manage', 'photos:use'], byDomain: [] }
    expect(homePath(session)).toBe('/photos')
  })
  it('keeps an operator on setup while a chosen Photos is waiting for a restart', () => {
    const session = operator({ isPhotosEnabled: true, isPhotosChosen: true, hasPendingServiceRestart: true })
    session.permissions = { everywhere: ['server:manage', 'photos:use'], byDomain: [] }
    expect(homePath(session)).toBe('/welcome')
  })
  it('keeps an operator on setup when Photos was chosen with mail, whose next step is a domain', () => {
    const session = operator({ isPhotosEnabled: true, isPhotosChosen: true, isMailRoutingEnabled: true })
    session.permissions = { everywhere: ['server:manage', 'photos:use'], byDomain: [] }
    expect(homePath(session)).toBe('/manage/server/setup')
  })
  it('does not skip setup for an operator who chose Photos but may not use it', () => {
    expect(homePath(operator({ isPhotosEnabled: true, isPhotosChosen: true }))).toBe('/welcome')
  })
})

describe('photos', () => {
  const withPhotos = (isPhotosEnabled: boolean) => operator({ isPhotosEnabled }).services
  it('is a page only when the service is on', () => {
    expect(isPathAvailable('/photos', withPhotos(true))).toBe(true)
    expect(isPathAvailable('/photos/photo-1', withPhotos(true))).toBe(true)
    expect(isPathAvailable('/photos', withPhotos(false))).toBe(false)
    expect(isPathAvailable('/photos', undefined)).toBe(false)
  })
  it('is not mistaken for another page whose path starts the same', () => {
    expect(isPathAvailable('/photosynthesis', withPhotos(false))).toBe(true)
  })
  it('is home for a person who has only photos', () => {
    const session = operator({ hasCompletedSetup: true, isPhotosEnabled: true })
    session.permissions = { everywhere: ['photos:use'], byDomain: [] }
    expect(homePath(session)).toBe('/photos')
  })
})

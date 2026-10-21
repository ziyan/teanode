import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

// The Passwords window runs when it is loaded, so each test loads it afresh
// into a page of its own, with the service worker played by the test.
const settle = () => new Promise((resolve) => setTimeout(resolve, 10))

const OWN_LOGIN = {
  id: 'own',
  name: 'Mine',
  userName: 'ada',
  siteAddress: 'https://example.test',
  hasPassword: true,
  hasOneTimeCode: false,
  passkeyCount: 0,
  isOwnedByMe: true,
  ownerName: 'Ada',
  sharedByText: '',
}
const SHARED_LOGIN = {
  ...OWN_LOGIN,
  id: 'shared',
  name: 'Team account',
  userName: 'team',
  isOwnedByMe: false,
  ownerName: 'Grace',
  sharedByText: 'Shared by Grace',
}
const OTHER_LOGIN = { ...OWN_LOGIN, id: 'other', name: 'Elsewhere', userName: 'ada', siteAddress: 'https://other.test' }

let sent: Record<string, any>[]
let answers: Record<string, (message: Record<string, any>) => unknown>

beforeEach(() => {
  vi.resetModules()
  window.history.replaceState({}, '', '/passwords.html?tab=5&frame=0')
  document.body.innerHTML = '<div id="app"></div>'
  sent = []
  answers = {
    'passwords:state': () => ({
      isSignedIn: true,
      isSetUp: true,
      isUnlocked: true,
      serverOrigin: 'https://tea.example.test',
      frameHost: 'example.com',
      topHost: 'example.com',
      isFrameCrossOrigin: false,
      hasPendingSave: false,
      pendingSave: null,
    }),
    'passwords:search': () => ({
      forThisSite: [OWN_LOGIN, SHARED_LOGIN],
      others: [OTHER_LOGIN],
      ownerKeyChangedCount: 0,
      ownerKeyChanges: [],
    }),
  }
  vi.stubGlobal('chrome', {
    runtime: {
      sendMessage: vi.fn(async (message: Record<string, any>) => {
        sent.push(message)
        return answers[message.type]?.(message) ?? {}
      }),
      openOptionsPage: vi.fn(),
    },
  })
  vi.spyOn(window, 'close').mockImplementation(() => {})
})
afterEach(() => {
  vi.unstubAllGlobals()
})

async function load() {
  await import('./passwords')
  await settle()
}

function buttonIn(row: Element, text: string): HTMLButtonElement {
  const found = Array.from(row.querySelectorAll('button')).find((button) => button.textContent === text)
  if (!found) throw new Error(`no button ${text}`)
  return found
}

function rowFor(name: string): HTMLElement {
  const found = Array.from(document.querySelectorAll<HTMLElement>('.row')).find(
    (row) => row.querySelector('.name')?.textContent === name,
  )
  if (!found) throw new Error(`no row ${name}`)
  return found
}

describe('who owns a login in the list', () => {
  it('says "Shared by" under a login somebody else owns, and nothing under one the person owns', async () => {
    await load()
    expect(rowFor('Team account').textContent).toContain('Shared by Grace')
    expect(rowFor('Mine').textContent).not.toContain('Shared by')
  })
})

describe('filling a login that is not saved for the page', () => {
  const question = {
    loginName: 'Elsewhere',
    origin: 'https://example.com',
    savedSites: ['https://other.test:8443'],
    isPageEncrypted: true,
  }

  it('asks the question the iPhone asks and fills nothing until the person says yes', async () => {
    answers['passwords:fill-in-tab'] = (message) =>
      message.confirmedOtherSiteOrigin
        ? { filledCount: 1, foreignActionHost: '' }
        : { filledCount: 0, foreignActionHost: '', otherSite: question }
    await load()
    buttonIn(rowFor('Elsewhere'), 'Fill').click()
    await settle()
    const card = document.querySelector('.card[role="alert"]') as HTMLElement
    expect(card.textContent).toContain('Fill Elsewhere into https://example.com?')
    expect(card.textContent).toContain('It is saved for https://other.test:8443.')
    expect(document.querySelector('.status')?.textContent).not.toContain('Filled')
    expect(sent.filter((message) => message.type === 'passwords:fill-in-tab')).toHaveLength(1)
    expect(sent[sent.length - 1]?.confirmedOtherSiteOrigin).toBeUndefined()
    // The yes names the very origin that was asked about.
    buttonIn(card, 'Fill').click()
    await settle()
    const fills = sent.filter((message) => message.type === 'passwords:fill-in-tab')
    expect(fills).toHaveLength(2)
    expect(fills[1]).toMatchObject({ itemId: 'other', confirmedOtherSiteOrigin: 'https://example.com' })
    expect(document.querySelector('.card[role="alert"]')).toBeNull()
    expect(document.querySelector('.status')?.textContent).toBe('Filled.')
  })

  it('fills nothing on Cancel, says when the login has no site, and warns about a page that is not encrypted', async () => {
    answers['passwords:fill-in-tab'] = () => ({
      filledCount: 0,
      foreignActionHost: '',
      otherSite: { ...question, savedSites: [], isPageEncrypted: false },
    })
    await load()
    buttonIn(rowFor('Elsewhere'), 'Fill').click()
    await settle()
    const card = document.querySelector('.card[role="alert"]') as HTMLElement
    expect(card.textContent).toContain('It is not saved for any site.')
    expect(card.textContent).toContain('This page is not encrypted.')
    buttonIn(card, 'Cancel').click()
    await settle()
    expect(document.querySelector('.card[role="alert"]')).toBeNull()
    expect(sent.filter((message) => message.type === 'passwords:fill-in-tab')).toHaveLength(1)
  })

  it('names the frame when the form is in a frame from another site', async () => {
    answers['passwords:state'] = () => ({
      isSignedIn: true,
      isSetUp: true,
      isUnlocked: true,
      serverOrigin: 'https://tea.example.test',
      frameHost: 'pay.example.test',
      topHost: 'shop.example.test',
      isFrameCrossOrigin: true,
      hasPendingSave: false,
      pendingSave: null,
    })
    answers['passwords:fill-in-tab'] = () => ({
      filledCount: 0,
      foreignActionHost: '',
      otherSite: { ...question, origin: 'https://pay.example.test' },
    })
    await load()
    buttonIn(rowFor('Elsewhere'), 'Fill').click()
    await settle()
    const card = document.querySelector('.card[role="alert"]') as HTMLElement
    expect(card.textContent).toContain('Fill Elsewhere into https://pay.example.test?')
    expect(card.textContent).toContain('The sign-in form is in a frame from pay.example.test.')
    expect(card.textContent).toContain('The page around it is shop.example.test.')
  })

  it('carries the yes through the question about a form that sends elsewhere', async () => {
    answers['passwords:fill-in-tab'] = (message) =>
      message.confirmedOtherSiteOrigin && message.isForeignActionConfirmed
        ? { filledCount: 1, foreignActionHost: '' }
        : message.confirmedOtherSiteOrigin
          ? { filledCount: 0, foreignActionHost: 'collect.example.test' }
          : { filledCount: 0, foreignActionHost: '', otherSite: question }
    await load()
    buttonIn(rowFor('Elsewhere'), 'Fill').click()
    await settle()
    buttonIn(document.querySelector('.card[role="alert"]') as HTMLElement, 'Fill').click()
    await settle()
    buttonIn(document.querySelector('.status') as HTMLElement, 'Fill anyway').click()
    await settle()
    const last = sent.filter((message) => message.type === 'passwords:fill-in-tab').slice(-1)[0]
    expect(last).toMatchObject({ isForeignActionConfirmed: true, confirmedOtherSiteOrigin: 'https://example.com' })
  })
})

describe("a shared login's owner whose key changed", () => {
  const change = {
    ownerUserId: 'owner-b',
    ownerName: 'Grace',
    oldSafetyCode: '0123 4567 8901 2345 6789 0123 4567 8901',
    newSafetyCode: '1111 2222 3333 4444 5555 6666 7777 8888',
    newPublicKey: 'new-key',
  }

  beforeEach(() => {
    answers['passwords:search'] = () => ({
      forThisSite: [],
      others: [],
      ownerKeyChangedCount: 2,
      ownerKeyChanges: [change],
    })
  })

  it('shows both safety codes, in the words of the dashboard, and confirms nothing until the button', async () => {
    await load()
    const card = document.querySelector('.card[role="alert"]') as HTMLElement
    expect(card.textContent).toContain("Grace's key changed")
    expect(card.textContent).toContain('Safety code before 0123 4567 8901 2345 6789 0123 4567 8901')
    expect(card.textContent).toContain('Safety code now 1111 2222 3333 4444 5555 6666 7777 8888')
    expect(card.textContent).toContain('Ask Grace to open Passwords settings on their own device')
    expect(card.textContent).toContain('Confirm only if it matches')
    expect(card.textContent).not.toMatch(/trust/i)
    expect(sent.some((message) => message.type === 'passwords:confirm-owner-key')).toBe(false)
  })

  it('confirms the very key it showed and reads the list again', async () => {
    answers['passwords:confirm-owner-key'] = () => ({ isDone: true })
    await load()
    const searchesBefore = sent.filter((message) => message.type === 'passwords:search').length
    buttonIn(document.querySelector('.card[role="alert"]') as HTMLElement, 'The new safety code matches').click()
    await settle()
    expect(sent.find((message) => message.type === 'passwords:confirm-owner-key')).toEqual({
      type: 'passwords:confirm-owner-key',
      ownerUserId: 'owner-b',
      newPublicKey: 'new-key',
    })
    expect(sent.filter((message) => message.type === 'passwords:search').length).toBe(searchesBefore + 1)
  })

  it('says so when the key changed again, and does not confirm it', async () => {
    answers['passwords:confirm-owner-key'] = () => ({ error: 'Their key changed again.' })
    await load()
    buttonIn(document.querySelector('.card[role="alert"]') as HTMLElement, 'The new safety code matches').click()
    await settle()
    expect(document.querySelector('.status')?.textContent).toBe('Their key changed again.')
    const button = buttonIn(document.querySelector('.card[role="alert"]') as HTMLElement, 'The new safety code matches')
    expect(button.disabled).toBe(false)
  })

  it('says "could not be read" for a code that cannot be worked out', async () => {
    answers['passwords:search'] = () => ({
      forThisSite: [],
      others: [],
      ownerKeyChangedCount: 1,
      ownerKeyChanges: [{ ...change, oldSafetyCode: '' }],
    })
    await load()
    expect(document.querySelector('.card[role="alert"]')?.textContent).toContain('Safety code before could not be read')
  })
})

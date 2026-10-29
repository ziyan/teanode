import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

// The toolbar button's dropdown runs when it is loaded, so each test loads it
// afresh into a page of its own, with the service worker played by the test.
// It is in no tab: it asks Chrome for the tab in front, here tab 5 in window 1.
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
  window.history.replaceState({}, '', '/passwords.html')
  document.body.innerHTML = '<div id="app"></div>'
  sent = []
  answers = {
    'passwords:state': () => ({
      isSignedIn: true,
      isSetUp: true,
      isUnlocked: true,
      serverOrigin: 'https://tea.example.test',
      frameId: 2,
      frameHost: 'example.com',
      topHost: 'example.com',
      isFrameCrossOrigin: false,
      hasPendingSave: false,
      pendingSave: null,
    }),
    'passwords:search': () => ({
      forThisSite: [OWN_LOGIN, SHARED_LOGIN],
      others: [OTHER_LOGIN],
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
    tabs: { query: vi.fn(async () => [{ id: 5 }]), create: vi.fn() },
    windows: { getCurrent: vi.fn(async () => ({ id: 1 })) },
    sidePanel: { open: vi.fn(async () => undefined) },
    commands: { getAll: vi.fn(async () => [{ name: 'fill-login', shortcut: 'Ctrl+Shift+L' }]) },
  })
  vi.spyOn(window, 'close').mockImplementation(() => {})
  // jsdom lays nothing out, and has no scrolling into view.
  Element.prototype.scrollIntoView = vi.fn()
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

const settleList = async () => {
  await settle()
  await settle()
}

function search(): HTMLInputElement {
  return document.querySelector('input[type="search"]') as HTMLInputElement
}

function press(key: string) {
  search().dispatchEvent(new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true }))
}

describe('opening', () => {
  it("starts with the page's host in the search, selected, and asks for the logins saved for the page", async () => {
    await load()
    expect(sent[0]).toEqual({ type: 'passwords:state', tabId: 5 })
    expect(search().value).toBe('example.com')
    expect(document.activeElement).toBe(search())
    expect(search().selectionStart).toBe(0)
    expect(search().selectionEnd).toBe('example.com'.length)
    expect(sent.find((message) => message.type === 'passwords:search')).toEqual({
      type: 'passwords:search',
      query: 'example.com',
      tabId: 5,
      frameId: 2,
    })
  })

  it('says when no login is saved for the page, and how to see the rest', async () => {
    answers['passwords:search'] = () => ({ forThisSite: [], others: [], ownerKeyChanges: [] })
    await load()
    expect(document.querySelector('.list-area')?.textContent).toBe(
      'No login is saved for example.com. Clear the search to see every login.',
    )
  })

  it('lists every login once the search is cleared', async () => {
    await load()
    search().value = ''
    search().dispatchEvent(new Event('input'))
    await settleList()
    expect(sent.filter((message) => message.type === 'passwords:search').slice(-1)[0].query).toBe('')
  })
})

describe('the keyboard', () => {
  it('fills the first login with Enter, into the frame the worker named, and closes', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout'] })
    answers['passwords:fill-in-tab'] = () => ({ filledCount: 2, foreignActionHost: '' })
    try {
      await import('./passwords')
      await vi.advanceTimersByTimeAsync(20)
      expect(rowFor('Mine').classList.contains('is-chosen')).toBe(true)
      press('Enter')
      await vi.advanceTimersByTimeAsync(20)
      expect(sent.filter((message) => message.type === 'passwords:fill-in-tab')).toEqual([
        {
          type: 'passwords:fill-in-tab',
          itemId: 'own',
          fillPart: 'login',
          isForeignActionConfirmed: false,
          confirmedOtherSiteOrigin: undefined,
          tabId: 5,
          frameId: 2,
        },
      ])
      await vi.advanceTimersByTimeAsync(400)
      expect(window.close).toHaveBeenCalled()
    } finally {
      vi.useRealTimers()
    }
  })

  it('chooses another login with the arrows, never past the ends', async () => {
    answers['passwords:fill-in-tab'] = () => ({ filledCount: 2, foreignActionHost: '' })
    await load()
    press('ArrowUp')
    expect(rowFor('Mine').classList.contains('is-chosen')).toBe(true)
    press('ArrowDown')
    press('ArrowDown')
    press('ArrowDown')
    expect(rowFor('Elsewhere').classList.contains('is-chosen')).toBe(true)
    expect(rowFor('Elsewhere').getAttribute('aria-selected')).toBe('true')
    expect(rowFor('Mine').classList.contains('is-chosen')).toBe(false)
    press('Enter')
    await settle()
    expect(sent.filter((message) => message.type === 'passwords:fill-in-tab')[0].itemId).toBe('other')
  })

  it('fills nothing with Enter where there is no page to fill', async () => {
    const state = answers['passwords:state']
    answers['passwords:state'] = () => ({ ...(state as any)(), frameHost: '' })
    await load()
    expect(search().value).toBe('')
    press('Enter')
    await settle()
    expect(sent.some((message) => message.type === 'passwords:fill-in-tab')).toBe(false)
  })
})

describe('the chat', () => {
  it('opens the agent in the side panel of this window and closes the dropdown', async () => {
    await load()
    const chat = Array.from(document.querySelectorAll('button')).find(
      (button) => button.textContent === 'Chat with TeaNode',
    )
    chat!.click()
    await settle()
    expect((globalThis as any).chrome.sidePanel.open).toHaveBeenCalledWith({ windowId: 1 })
    expect(window.close).toHaveBeenCalled()
  })
})

describe('a login waiting to be saved', () => {
  it('offers Save or Not now, and Not now forgets it for the tab', async () => {
    const state = answers['passwords:state']
    answers['passwords:state'] = () => ({
      ...(state as any)(),
      hasPendingSave: true,
      pendingSave: { host: 'example.com', userName: 'ada', offerAction: 'update' },
    })
    answers['passwords:dismiss-pending'] = () => {
      answers['passwords:state'] = state
      return { isDone: true }
    }
    await load()
    const card = document.querySelector('.card') as HTMLElement
    expect(card.textContent).toContain('Update the saved password?')
    expect(card.textContent).toContain('ada on example.com')
    buttonIn(card, 'Not now').click()
    await settleList()
    expect(sent.find((message) => message.type === 'passwords:dismiss-pending')).toEqual({
      type: 'passwords:dismiss-pending',
      tabId: 5,
    })
    expect(document.querySelector('.card')).toBeNull()
  })
})

describe('the generator', () => {
  it('fills the password it made into the page, and copies it', async () => {
    answers['passwords:fill-generated'] = () => ({ filledCount: 2, foreignActionHost: '' })
    await load()
    const section = document.querySelector('details') as HTMLElement
    buttonIn(section, 'Generate').click()
    const made = (section.querySelector('input[type="text"]') as HTMLInputElement).value
    expect(made.length).toBeGreaterThan(8)
    buttonIn(section, 'Fill').click()
    await settle()
    expect(sent.find((message) => message.type === 'passwords:fill-generated')).toEqual({
      type: 'passwords:fill-generated',
      password: made,
      tabId: 5,
      frameId: 2,
    })
    expect(sent.find((message) => message.type === 'passwords:copy-text')).toEqual({
      type: 'passwords:copy-text',
      text: made,
    })
  })
})

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
      frameId: 0,
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
    ownSafetyCode: '9999 8888 7777 6666 5555 4444 3333 2222',
  }

  beforeEach(() => {
    answers['passwords:search'] = () => ({
      forThisSite: [],
      others: [OTHER_LOGIN],
      ownerKeyChanges: [change],
    })
  })

  it('is a notice next to the logins, which are listed, and shows no codes until asked', async () => {
    await load()
    const card = document.querySelector('.card[role="alert"]') as HTMLElement
    expect(card.textContent).toContain("Grace's key changed")
    expect(card.textContent).toContain('This happens when they set Passwords up again')
    expect(card.textContent).not.toMatch(/new device/i)
    expect(card.textContent).not.toContain('0123 4567')
    expect(document.querySelector('.list')?.textContent).toContain(OTHER_LOGIN.name)
    // Nothing is a button that gates use, and nothing is sent until one is pressed.
    expect(card.textContent).not.toMatch(/confirm/i)
    expect(sent.some((message) => message.type === 'passwords:dismiss-owner-key')).toBe(false)
  })

  it('comes before the logins, so a long list does not push it out of sight', async () => {
    answers['passwords:search'] = () => ({
      forThisSite: [OWN_LOGIN, SHARED_LOGIN],
      others: [OTHER_LOGIN],
      ownerKeyChanges: [change],
    })
    await load()
    const area = document.querySelector('.list-area') as HTMLElement
    expect(area.firstElementChild?.getAttribute('role')).toBe('alert')
    expect(buttonIn(area.firstElementChild as HTMLElement, 'Compare codes')).toBeTruthy()
    expect(area.querySelectorAll('.list').length).toBe(2)
  })

  it('shows both codes of the owner and the own one with Compare codes', async () => {
    await load()
    const card = document.querySelector('.card[role="alert"]') as HTMLElement
    buttonIn(card, 'Compare codes').click()
    expect(card.textContent).toContain('Safety code before 0123 4567 8901 2345 6789 0123 4567 8901')
    expect(card.textContent).toContain('Safety code now 1111 2222 3333 4444 5555 6666 7777 8888')
    expect(card.textContent).toContain('Your safety code 9999 8888 7777 6666 5555 4444 3333 2222')
    expect(card.textContent).toContain('Ask Grace to open Passwords settings on their own device')
    expect(sent.some((message) => message.type === 'passwords:dismiss-owner-key')).toBe(false)
  })

  it('is dismissed with Dismiss, and the list is read again', async () => {
    answers['passwords:dismiss-owner-key'] = () => ({ isDone: true })
    await load()
    const searchesBefore = sent.filter((message) => message.type === 'passwords:search').length
    buttonIn(document.querySelector('.card[role="alert"]') as HTMLElement, 'Dismiss').click()
    await settle()
    expect(sent.find((message) => message.type === 'passwords:dismiss-owner-key')).toEqual({
      type: 'passwords:dismiss-owner-key',
      ownerUserId: 'owner-b',
    })
    expect(sent.filter((message) => message.type === 'passwords:search').length).toBe(searchesBefore + 1)
  })

  it('says "could not be read" for a code that cannot be worked out', async () => {
    answers['passwords:search'] = () => ({
      forThisSite: [],
      others: [],
      ownerKeyChanges: [{ ...change, oldSafetyCode: '' }],
    })
    await load()
    buttonIn(document.querySelector('.card[role="alert"]') as HTMLElement, 'Compare codes').click()
    expect(document.querySelector('.card[role="alert"]')?.textContent).toContain('Safety code before could not be read')
  })
})

// @vitest-environment-options {"url": "https://shop.example.test/sign-in"}
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { inPage } from './inPage'
import { giveLayout } from './passwords/testLayout'

// The two snapshots the agent reads a page by: the headless browser's script
// (internal/browser/snapshot.js, evaluated here as the browser evaluates it)
// and the person's tab (inPage). Neither shows a password, a code or anything
// the agent filled one into, and neither cuts a long value: it is not shown.

const headlessScript = readFileSync(resolve(__dirname, '../../../internal/browser/snapshot.js'), 'utf8')

function headlessSnapshot(): string {
  const run = (0, eval)(headlessScript) as (mode: string, maximum: number) => { text: string }
  return run('interactive', 20000).text
}

async function tabSnapshot(): Promise<string> {
  const answer = await inPage('snapshot', {})
  expect(answer.ok).toBe(true)
  return answer.data.text as string
}

const snapshots: [string, () => Promise<string>][] = [
  ['the headless browser', async () => headlessSnapshot()],
  ['the person’s tab', tabSnapshot],
]

function field(id: string): HTMLInputElement {
  return document.getElementById(id) as HTMLInputElement
}

beforeEach(() => {
  giveLayout()
  document.body.innerHTML = `<form>
    <input id="user" type="text" name="user" value="ada">
    <input id="pass" type="password" name="secret" value="hunter2-secret">
    <input id="code" type="text" name="code" autocomplete="one-time-code" value="654321">
    <input id="plain" type="text" name="note" value="a value that is shown">
    <input id="long" type="text" name="long">
  </form>`
  field('long').value = 'q'.repeat(100)
})
afterEach(() => {
  delete (window as any).__teanodeRefs
  delete (window as any).__teanodeNextRef
  delete (window as any).__teanodeFilled
})

describe.each(snapshots)('the snapshot of %s', (_name, snapshot) => {
  it('does not show a password or a code, and shows what it may', async () => {
    const text = await snapshot()
    expect(text).not.toContain('hunter2-secret')
    expect(text).not.toContain('654321')
    expect(text).toContain('(filled, value not shown)')
    expect(text).toContain('value="ada"')
    expect(text).toContain('value="a value that is shown"')
  })

  it('does not show a field the agent filled, whatever its type has become', async () => {
    const filled = new WeakSet<Node>([field('user'), field('pass'), field('plain')])
    ;(window as any).__teanodeFilled = filled
    // A show-password button turns a password field into a text one.
    field('pass').type = 'text'
    const text = await snapshot()
    expect(text).not.toContain('hunter2-secret')
    expect(text).not.toContain('value="ada"')
    expect(text).not.toContain('a value that is shown')
    expect(text.match(/\(filled, value not shown\)/g)?.length).toBeGreaterThanOrEqual(4)
  })

  it('does not name a field by the value the agent filled into it', async () => {
    const filled = new WeakSet<Node>([field('plain')])
    ;(window as any).__teanodeFilled = filled
    field('plain').removeAttribute('name')
    field('plain').setAttribute('aria-label', '')
    const text = await snapshot()
    expect(text).not.toContain('a value that is shown')
  })

  it('shows a long value in no part: it is not cut', async () => {
    const text = await snapshot()
    expect(text).not.toContain('qqqqq')
    expect(text).toContain('(long value, not shown)')
  })

  it('takes a filled-node set it cannot read for a mark that everything is filled', async () => {
    ;(window as any).__teanodeFilled = {
      has() {
        throw new Error('not a set')
      },
    }
    const text = await snapshot()
    expect(text).not.toContain('value="ada"')
    expect(text).not.toContain('a value that is shown')
  })
})

describe('the snapshot of the person’s tab', () => {
  it('says a field named for a password holds a secret, whatever its type', async () => {
    document.body.innerHTML = `<input id="a" type="text" name="passcode" value="1234-secret">
      <input id="b" type="text" aria-label="Enter your password" value="second-secret">
      <input id="c" type="text" name="card-number" value="4000000000001234">`
    const text = await tabSnapshot()
    expect(text).not.toContain('1234-secret')
    expect(text).not.toContain('second-secret')
    expect(text).not.toContain('4000000000001234')
    expect(text.match(/\(holds a secret\)/g)).toHaveLength(3)
  })
})

describe('the snapshot of the person’s tab and a masked text input', () => {
  // A card field drawn as dots with -webkit-text-security is a text input
  // with no hint in its name. jsdom draws no such style, so the computed
  // style is what the browser would answer.
  function maskWith(maskedIds: string[]): void {
    const real = window.getComputedStyle.bind(window)
    vi.spyOn(window, 'getComputedStyle').mockImplementation((node: Element, pseudo?: string | null) => {
      const style = real(node, pseudo)
      const mask = maskedIds.includes(node.id) ? 'disc' : 'none'
      return new Proxy(style, {
        get(target, property) {
          if (property === 'getPropertyValue') {
            return (name: string) => (name === '-webkit-text-security' ? mask : target.getPropertyValue(name))
          }
          const value = Reflect.get(target, property)
          return typeof value === 'function' ? value.bind(target) : value
        },
      })
    })
  }

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('does not show the value of a masked input, and shows an unmasked one', async () => {
    document.body.innerHTML = `<input id="masked" type="text" name="field-one" value="4000-masked-value">
      <input id="shown" type="text" name="field-two" value="visible-value">`
    maskWith(['masked'])
    const text = await tabSnapshot()
    expect(text).not.toContain('4000-masked-value')
    expect(text).toContain('visible-value')
    expect(text.match(/\(filled, value not shown\)/g)).toHaveLength(1)
  })

  it('does not read a masked input into the name of a field', async () => {
    document.body.innerHTML = '<input id="masked" type="text" value="4000-masked-value">'
    maskWith(['masked'])
    const text = await tabSnapshot()
    expect(text).not.toContain('4000-masked-value')
  })
})

describe('the person’s tab when it shows a document the agent filled', () => {
  // A filled value copied into an ordinary field is not known to be a secret.
  afterEach(() => {
    delete (Element.prototype as any).scrollIntoView
  })

  beforeEach(() => {
    Element.prototype.scrollIntoView = () => {}
    document.body.insertAdjacentHTML(
      'beforeend',
      '<input id="pasted" type="text" name="pasted"><textarea id="notes" name="notes"></textarea><button id="go">Go</button>',
    )
    field('pasted').value = '654321'
    ;(document.getElementById('notes') as HTMLTextAreaElement).value = 'pasted-secret-text'
  })

  it('shows the value of no input or textarea in a snapshot', async () => {
    const answer = await inPage('snapshot', { isFilledDocument: true })
    const text = answer.data.text as string
    expect(text).not.toContain('654321')
    expect(text).not.toContain('pasted-secret-text')
    expect(text).not.toContain('value="ada"')
    expect(text).not.toContain('a value that is shown')
    expect(text).toContain('(value not shown)')
    // Buttons and links are still named.
    expect(text).toContain('"Go"')
  })

  it('shows them as before when the document was not filled', async () => {
    const text = (await inPage('snapshot', {})).data.text as string
    expect(text).toContain('value="654321"')
  })

  it('names no input or textarea by its value when it is clicked or hovered', async () => {
    document.body.insertAdjacentHTML('beforeend', '<input id="bare" type="text">')
    field('bare').value = '135790'
    await inPage('snapshot', { isFilledDocument: true })
    const refs = (window as any).__teanodeRefs as Map<number, Element>
    const bareRef = [...refs].find(([, node]) => node === field('bare'))?.[0] as number
    expect(bareRef).toBeGreaterThan(0)
    for (const action of ['click', 'hover']) {
      const answer = await inPage(action, { ref: bareRef, isFilledDocument: true })
      expect(answer.ok, action).toBe(true)
      expect(JSON.stringify(answer), action).not.toContain('135790')
    }
    // Not told the document was filled, it names the field by what it holds.
    expect(JSON.stringify(await inPage('hover', { ref: bareRef }))).toContain('135790')
  })

  it('names an unlabeled input by nothing rather than its value', async () => {
    document.body.insertAdjacentHTML('beforeend', '<input id="bare" type="text">')
    field('bare').value = '135790'
    const text = (await inPage('snapshot', { isFilledDocument: true })).data.text as string
    expect(text).not.toContain('135790')
    const plain = (await inPage('snapshot', {})).data.text as string
    expect(plain).toContain('135790')
  })

  it('answers no selector, and takes refs', async () => {
    const snapshot = (await inPage('snapshot', { isFilledDocument: true })).data.text as string
    const goRef = Number(/\[ref=(\d+)\] button "Go"/.exec(snapshot)?.[1])
    expect((await inPage('click', { selector: '#go', isFilledDocument: true })).ok).toBe(false)
    expect((await inPage('hover', { selector: '#go', isFilledDocument: true })).ok).toBe(false)
    expect((await inPage('click', { ref: goRef, isFilledDocument: true })).ok).toBe(true)
    expect((await inPage('click', { selector: '#go' })).ok).toBe(true)
    const waited = await inPage('wait', { for: 'selector', selector: '#go', timeout_ms: 300, isFilledDocument: true })
    expect(waited.data.ended_by).toBe('timeout')
    expect((await inPage('wait', { for: 'selector', selector: '#go', timeout_ms: 300 })).data.ended_by).toBe('selector')
  })
})

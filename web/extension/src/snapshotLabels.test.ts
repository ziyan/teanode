// @vitest-environment-options {"url": "https://shop.example.test/contact"}
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { inPage } from './inPage'
import { giveLayout, setLayout } from './passwords/testLayout'

// A form whose fields sit inside their labels, as many sites write them: the
// field, not its label, is what a snapshot gives a ref, named by the label's
// words; typing and choosing reach the field even through the label's ref;
// and a long list of options is cut short. Both snapshots: the headless
// browser's script and the person's tab.

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

const callingCodes = Array.from(
  { length: 60 },
  (_, index) => `<option value="${index + 1}">${index + 1}</option>`,
).join('')

beforeEach(() => {
  giveLayout()
  document.body.innerHTML = `<form>
    <label>App name: <input id="app" type="text" name="Q101"></label>
    <label>Platform: <select id="platform" name="Q102"><option>Choose</option><option>Phone</option><option>Tablet</option></select></label>
    <label>Country code <select id="code" name="Q103">${callingCodes}</select></label>
    <label><input id="agree" type="checkbox"> I agree</label>
    <label id="drawn">Remember me <input id="remember" type="checkbox"></label>
    <label>Send me news <input id="news" type="checkbox"></label>
  </form>`
  setLayout(document.getElementById('remember')!, { width: 0, height: 0 })
  // Drawn, but as a pixel: how a page hides a checkbox it draws itself.
  setLayout(document.getElementById('news')!, { width: 1, height: 1 })
})
afterEach(() => {
  delete (window as any).__teanodeRefs
  delete (window as any).__teanodeNextRef
})

describe.each(snapshots)('%s', (_, snapshot) => {
  it('gives the field the ref, named by its label and not by its name', async () => {
    const text = await snapshot()
    expect(text).toMatch(/\[ref=\d+\] input\(text\) "App name:"/)
    expect(text).toMatch(/\[ref=\d+\] select "Platform:"/)
    expect(text).not.toContain('Q101')
    expect(text).not.toMatch(/\[ref=\d+\] label "App name/)
  })

  it('keeps the ref on a label whose field is not drawn, or is a checkbox', async () => {
    const text = await snapshot()
    expect(text).toMatch(/\[ref=\d+\] label "Remember me/)
    expect(text).toMatch(/\[ref=\d+\] label "Send me news/)
  })

  it('lists the first options of a long select and says how many more there are', async () => {
    const text = await snapshot()
    expect(text).toContain('- option "25"')
    expect(text).not.toContain('- option "26"')
    expect(text).toContain('- and 35 more options')
  })
})

describe('the person’s tab', () => {
  const refOf = (text: string, pattern: RegExp) => Number(pattern.exec(text)![1])

  it('types into a field and chooses in a select through their refs', async () => {
    const text = await tabSnapshot()
    expect(
      (await inPage('type', { ref: refOf(text, /\[ref=(\d+)\] input\(text\) "App name:"/), text: 'Lantern' })).ok,
    ).toBe(true)
    expect((document.getElementById('app') as HTMLInputElement).value).toBe('Lantern')
    expect((await inPage('select', { ref: refOf(text, /\[ref=(\d+)\] select "Platform:"/), value: 'Tablet' })).ok).toBe(
      true,
    )
    expect((document.getElementById('platform') as HTMLSelectElement).value).toBe('Tablet')
  })

  it('types into the field a label stands for, and refuses what is not a field', async () => {
    expect((await inPage('type', { selector: 'label', text: 'Lantern' })).ok).toBe(true)
    expect((document.getElementById('app') as HTMLInputElement).value).toBe('Lantern')
    for (const selector of ['form', '#agree']) {
      const refused = await inPage('type', { selector, text: 'nowhere' })
      expect(refused.ok, selector).toBe(false)
      expect(refused.error, selector).toContain('not a field')
    }
  })

  it('takes the drawn element a selector matches before a hidden one', async () => {
    document.body.innerHTML = `<button id="hidden" hidden>Clear</button><button id="shown">Send</button>`
    // jsdom draws nothing, so it has no scrolling either.
    Element.prototype.scrollIntoView = () => {}
    const answer = await inPage('click', { selector: 'button' })
    expect(answer.data.clicked).toBe('Send')
  })
})

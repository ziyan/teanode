// @vitest-environment-options {"url": "https://shop.example.test/sign-in"}
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
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

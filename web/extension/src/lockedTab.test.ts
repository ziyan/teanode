import { describe, expect, it } from 'vitest'
import { lockedActionRefusal, lockedMethodRefusal } from './lockedTab'

describe('actions on a tab showing a filled document', () => {
  it('refuses the ones that read the page back', () => {
    for (const action of ['evaluate', 'screenshot', 'storage']) {
      expect(lockedActionRefusal(action, {}), action).not.toBe('')
    }
    for (const action of ['snapshot', 'click', 'type', 'press', 'navigate', 'back', 'cdp', 'cdp_events', 'fetch']) {
      expect(lockedActionRefusal(action, { ref: 3 }), action).toBe('')
    }
  })

  it('refuses a selector, and waiting for one, and takes a ref', () => {
    for (const action of ['click', 'hover', 'type', 'select', 'scroll']) {
      expect(lockedActionRefusal(action, { selector: 'input[value^="a"]' }), action).toContain('refs')
      expect(lockedActionRefusal(action, { ref: '4' }), action).toBe('')
      expect(lockedActionRefusal(action, { ref: 4, selector: 'input' }), action).toBe('')
    }
    expect(lockedActionRefusal('wait', { for: 'selector', selector: 'input[value^="a"]' })).toContain('refs')
    expect(lockedActionRefusal('wait', { for: 'navigation' })).toBe('')
    expect(lockedActionRefusal('wait', { for: 'timeout', timeout_ms: 10 })).toBe('')
    expect(lockedActionRefusal('scroll', {})).toBe('')
  })
})

describe('DevTools methods on a tab showing a filled document', () => {
  it('answers real input and moving the page, and nothing else', () => {
    for (const method of ['Input.dispatchMouseEvent', 'Input.insertText', 'Page.getLayoutMetrics', 'Page.enable']) {
      expect(lockedMethodRefusal(method, {}), method).toBe('')
    }
    for (const method of [
      'Runtime.evaluate',
      'DOM.getDocument',
      'DOMSnapshot.captureSnapshot',
      'Page.captureSnapshot',
      'Network.enable',
      'Network.getResponseBody',
      'Accessibility.getFullAXTree',
      'CSS.getComputedStyleForNode',
      'Input.dispatchDragEvent',
      'Input.setInterceptDrags',
      'Emulation.setEmulatedMedia',
    ]) {
      expect(lockedMethodRefusal(method, {}), method).toContain('is not answered')
    }
  })

  it('takes plain typing but not a key event that carries commands or a control, alt or meta modifier', () => {
    expect(lockedMethodRefusal('Input.dispatchKeyEvent', { type: 'keyDown', key: 'a', text: 'a' })).toBe('')
    expect(lockedMethodRefusal('Input.dispatchKeyEvent', { type: 'keyDown', key: 'A', modifiers: 8 })).toBe('')
    for (const params of [
      { type: 'keyDown', key: 'a', commands: ['selectAll'] },
      { type: 'keyDown', key: 'c', commands: ['copy', 'paste'] },
      { type: 'rawKeyDown', key: 'c', modifiers: 2 },
      { type: 'rawKeyDown', key: 'c', modifiers: 4 },
      { type: 'rawKeyDown', key: 'c', modifiers: 1 },
      { type: 'rawKeyDown', key: 'c', modifiers: 2 | 8 },
      { type: 'keyDown', key: 'Insert' },
      { type: 'keyDown', code: 'Insert' },
      { type: 'keyDown', key: 'Paste' },
      { type: 'keyDown', windowsVirtualKeyCode: 45 },
      { type: 'keyDown', key: 'Delete', modifiers: 8 },
      { type: 'keyDown', key: 'a', modifiers: 2.5 },
      { type: 'keyDown', key: 'a', modifiers: '2' },
    ]) {
      expect(lockedMethodRefusal('Input.dispatchKeyEvent', params), JSON.stringify(params)).toContain('Copying')
    }
  })

  it('takes a click and a plain move, but not a move with a button held or the middle button', () => {
    expect(lockedMethodRefusal('Input.dispatchMouseEvent', { type: 'mousePressed', button: 'left', x: 1, y: 1 })).toBe(
      '',
    )
    expect(lockedMethodRefusal('Input.dispatchMouseEvent', { type: 'mouseMoved', x: 1, y: 1 })).toBe('')
    expect(
      lockedMethodRefusal('Input.dispatchMouseEvent', { type: 'mouseMoved', button: 'none', buttons: 0, x: 1, y: 1 }),
    ).toBe('')
    for (const params of [
      { type: 'mouseMoved', button: 'left', x: 1, y: 1 },
      { type: 'mouseMoved', buttons: 1, x: 1, y: 1 },
      { type: 'mouseMoved', buttons: '1', x: 1, y: 1 },
      { type: 'mousePressed', button: 'middle', x: 1, y: 1 },
    ]) {
      expect(lockedMethodRefusal('Input.dispatchMouseEvent', params), JSON.stringify(params)).toContain('Copying')
    }
  })

  it('moves the page with an address and nothing else', () => {
    expect(lockedMethodRefusal('Page.navigate', { url: 'https://shop.example.test/' })).toBe('')
    expect(lockedMethodRefusal('Page.navigate', { url: 'https://shop.example.test/', referrer: 'x' })).not.toBe('')
  })

  it('reads a method with spaces around it the way the worker does, and a missing parameters as none', () => {
    expect(lockedMethodRefusal(' Runtime.evaluate ', undefined)).toContain('is not answered')
    expect(lockedMethodRefusal(' Input.dispatchMouseEvent ', undefined)).toBe('')
  })
})

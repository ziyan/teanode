import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { checkFrameShown } from './frameVisibility'
import { giveLayout, setLayout } from './testLayout'

let host: HTMLElement

// coveredBy: what elementsFromPoint answers for every point, top first.
function stack(...elements: Element[]): void {
  document.elementsFromPoint = () => elements
}

beforeEach(() => {
  giveLayout()
  document.body.innerHTML = '<div id="wrapper"><div id="host"></div></div><div id="decoy"></div>'
  host = document.getElementById('host') as HTMLElement
  Object.defineProperty(window, 'innerWidth', { value: 1024, configurable: true })
  Object.defineProperty(window, 'innerHeight', { value: 768, configurable: true })
  stack(host, document.body)
})
afterEach(() => {
  document.body.innerHTML = ''
  delete (document as any).elementsFromPoint
})

describe('checkFrameShown', () => {
  it('passes a frame that is displayed, opaque and on top', () => {
    expect(checkFrameShown(host)).toEqual({ isShown: true, failureReason: '' })
  })

  it('fails when the frame is made see-through, by itself or by an element around it', () => {
    host.style.opacity = '0'
    expect(checkFrameShown(host).isShown).toBe(false)
    host.style.opacity = '1'
    ;(document.getElementById('wrapper') as HTMLElement).style.opacity = '0.2'
    expect(checkFrameShown(host).isShown).toBe(false)
  })

  it('fails when the frame or an element around it is hidden', () => {
    host.style.visibility = 'hidden'
    expect(checkFrameShown(host).isShown).toBe(false)
    host.style.visibility = 'visible'
    ;(document.getElementById('wrapper') as HTMLElement).style.display = 'none'
    expect(checkFrameShown(host).isShown).toBe(false)
  })

  it('fails when a filter, a clip or a blend mode is put on it or around it', () => {
    const wrapper = document.getElementById('wrapper') as HTMLElement
    wrapper.style.filter = 'opacity(0)'
    expect(checkFrameShown(host)).toMatchObject({ isShown: false, failureReason: expect.stringContaining('filter') })
    wrapper.style.filter = ''
    host.style.clipPath = 'inset(100%)'
    expect(checkFrameShown(host).isShown).toBe(false)
    host.style.clipPath = ''
    host.style.mixBlendMode = 'multiply'
    expect(checkFrameShown(host).isShown).toBe(false)
  })

  it('fails when another element covers it', () => {
    stack(document.getElementById('decoy') as HTMLElement, host)
    expect(checkFrameShown(host)).toMatchObject({ isShown: false, failureReason: 'another element covers the frame' })
  })

  it('fails when only a corner is covered', () => {
    let calls = 0
    document.elementsFromPoint = () => (calls++ === 3 ? [document.getElementById('decoy') as Element] : [host])
    expect(checkFrameShown(host).isShown).toBe(false)
  })

  it('fails when the frame is too small or off the screen', () => {
    setLayout(host, { width: 5, height: 5 })
    expect(checkFrameShown(host).isShown).toBe(false)
    setLayout(host, { left: 5000, top: 5000, width: 200, height: 100 })
    expect(checkFrameShown(host)).toMatchObject({ isShown: false, failureReason: 'the frame is off the screen' })
  })

  it('fails when the frame has been taken off the page', () => {
    host.remove()
    expect(checkFrameShown(host).isShown).toBe(false)
  })

  it('fails when another popover or a modal dialog is open, but not for its own', () => {
    const other = document.createElement('div')
    const original = document.querySelectorAll.bind(document)
    vi.spyOn(document, 'querySelectorAll').mockImplementation(((selector: string) => {
      if (selector === ':popover-open') return [host, other] as unknown as NodeListOf<Element>
      return original(selector)
    }) as typeof document.querySelectorAll)
    expect(checkFrameShown(host)).toMatchObject({ isShown: false, failureReason: 'a popover is open' })
  })

  it('passes when the only popover open is the frame itself', () => {
    const original = document.querySelectorAll.bind(document)
    vi.spyOn(document, 'querySelectorAll').mockImplementation(((selector: string) => {
      if (selector === ':popover-open') return [host] as unknown as NodeListOf<Element>
      return original(selector)
    }) as typeof document.querySelectorAll)
    expect(checkFrameShown(host).isShown).toBe(true)
  })

  it('fails when an element is full screen', () => {
    Object.defineProperty(document, 'fullscreenElement', { value: document.body, configurable: true })
    expect(checkFrameShown(host)).toMatchObject({ isShown: false, failureReason: 'an element is full screen' })
    delete (document as any).fullscreenElement
  })
})

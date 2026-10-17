import { act, cleanup, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { TranslationProvider } from '../i18n/i18n'
import { ToastProvider, toastRoom, useToast } from './toast'

afterEach(cleanup)

it('ends the strip before the drawer and the tray, and keeps to the window when nothing stands there', () => {
  // The rail open at 232 pixels, a window 1280 wide, nothing at the right.
  expect(toastRoom(1280, 232, [])).toEqual({ edge: 1264, isOverRail: false })
  // The drawer, 440 wide and 20 from the edge, leaves plenty of room at 1280.
  expect(toastRoom(1280, 232, [{ left: 820, right: 1260 }])).toEqual({ edge: 808, isOverRail: false })
  // At 1000 the room is 540 - 12 - 248 = 280: a message still fits beside the rail.
  expect(toastRoom(1000, 232, [{ left: 540, right: 980 }])).toEqual({ edge: 528, isOverRail: false })
  // At 800 there are 92 pixels beside an open rail, so the strip goes over the rail instead.
  expect(toastRoom(800, 232, [{ left: 340, right: 780 }])).toEqual({ edge: 328, isOverRail: true })
  // A collapsed rail (60 pixels) leaves room where an open one did not.
  expect(toastRoom(800, 60, [{ left: 340, right: 780 }])).toEqual({ edge: 328, isOverRail: false })
  // The tray counts as well, and the nearer of the two wins.
  expect(
    toastRoom(1000, 232, [
      { left: 540, right: 980 },
      { left: 640, right: 980 },
    ]).edge,
  ).toBe(528)
  // A rail that is a drawer takes no room from the strip.
  expect(toastRoom(740, 0, [{ left: 300, right: 720 }]).isOverRail).toBe(false)
})

function Speaker() {
  const toast = useToast()
  return <button onClick={() => toast.done('Saved.')}>Say</button>
}

beforeEach(() => {
  document.documentElement.style.setProperty('--rail-width', '232px')
  vi.stubGlobal('innerWidth', 800)
})
afterEach(() => {
  document.documentElement.style.removeProperty('--rail-width')
  vi.unstubAllGlobals()
  document.querySelector('.agent-drawer')?.remove()
})

it('keeps clear of an open agent drawer while a toast shows', async () => {
  const drawer = document.createElement('aside')
  drawer.className = 'agent-drawer'
  drawer.getBoundingClientRect = () =>
    ({ left: 340, right: 780, top: 100, bottom: 880, width: 440, height: 780 }) as DOMRect
  document.body.appendChild(drawer)
  render(
    <TranslationProvider>
      <ToastProvider>
        <Speaker />
      </ToastProvider>
    </TranslationProvider>,
  )
  const strip = document.querySelector('.toasts') as HTMLElement
  expect(strip.style.getPropertyValue('--toast-edge')).toBe('')
  await act(async () => {
    screen.getByText('Say').click()
  })
  expect(strip.style.getPropertyValue('--toast-edge')).toBe('328px')
  expect(strip.classList.contains('over-rail')).toBe(true)
})

it('leaves out what is not beside the strip: a tray above a single toast, a drawer dragged to the left', () => {
  const band = { top: 840, bottom: 884 }
  expect(toastRoom(1280, 232, [{ left: 900, right: 1260, top: 500, bottom: 824 }], band).edge).toBe(1264)
  expect(toastRoom(1280, 232, [{ left: 900, right: 1260, top: 500, bottom: 860 }], band).edge).toBe(888)
  expect(toastRoom(1280, 232, [{ left: 100, right: 540, top: 0, bottom: 900 }], band)).toEqual({ edge: 1264, isOverRail: false })
})

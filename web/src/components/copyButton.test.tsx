import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { CopyButton } from './settingsList'
import { ToastProvider } from './toast'
import { TranslationProvider } from '../i18n/i18n'

function renderButton(label?: string) {
  render(
    <TranslationProvider>
      <ToastProvider>
        <CopyButton value="teanode photo upload ~/Pictures" label={label} />
      </ToastProvider>
    </TranslationProvider>,
  )
}

function useClipboard(writeText: ((text: string) => Promise<void>) | undefined) {
  Object.defineProperty(navigator, 'clipboard', {
    configurable: true,
    value: writeText ? { writeText } : undefined,
  })
}

beforeEach(() => useClipboard(undefined))
afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

it('copies the value and says so in a toast, leaving the button as it was', async () => {
  const writeText = vi.fn().mockResolvedValue(undefined)
  useClipboard(writeText)
  renderButton()
  fireEvent.click(screen.getByRole('button', { name: 'Copy' }))
  expect(writeText).toHaveBeenCalledWith('teanode photo upload ~/Pictures')
  expect((await screen.findByRole('status')).textContent).toContain('Copied')
  expect(screen.getByRole('button', { name: 'Copy' }).textContent).toBe('Copy')
})

it('says it could not copy in a toast when the browser refuses', async () => {
  useClipboard(vi.fn().mockRejectedValue(new Error('denied')))
  renderButton()
  fireEvent.click(screen.getByRole('button', { name: 'Copy' }))
  await waitFor(() => expect(screen.getByRole('status').textContent).toContain('Copy it by hand'))
  expect(screen.getByRole('button', { name: 'Copy' }).textContent).toBe('Copy')
})

it('says it could not copy when the page has no clipboard at all', () => {
  renderButton()
  fireEvent.click(screen.getByRole('button', { name: 'Copy' }))
  expect(screen.getByRole('status').textContent).toContain('Copy it by hand')
})

it('takes its accessible name from the label while still reading Copy', () => {
  renderButton('Copy the upload command')
  const button = screen.getByRole('button', { name: 'Copy the upload command' })
  expect(button.textContent).toBe('Copy')
})

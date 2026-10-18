import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { graphql } from '../api'
import { ToastProvider } from '../components/toast'
import { TranslationProvider } from '../i18n/i18n'
import { ConnectPage } from './connect'

vi.mock('../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../api')>()),
  graphql: vi.fn(),
}))
afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  window.history.pushState({}, '', '/')
})

function show(search: string) {
  vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} }))
  window.history.pushState({}, '', `/connect${search}`)
  vi.mocked(graphql).mockResolvedValue({
    CreateOAuthConnectCode: {
      connectLink: 'https://photos.example.com/connect',
      qrCodeSvg: '<svg xmlns="http://www.w3.org/2000/svg"></svg>',
      expiresAt: new Date(Date.now() + 600_000).toISOString(),
    },
  } as never)
  render(
    <TranslationProvider>
      <ToastProvider>
        <ConnectPage />
      </ToastProvider>
    </TranslationProvider>,
  )
}

// The page is outside the dashboard's frame, so its way back is the only exit.
it('leads back to Devices unless it was opened from Photos', async () => {
  show('')
  expect(screen.getByRole('link', { name: 'Back to Devices' }).getAttribute('href')).toBe('/settings/devices')
})

it('leads back to Photos when Photos opened it', async () => {
  show('?from=photos')
  expect(screen.getByRole('link', { name: 'Back to Photos' }).getAttribute('href')).toBe('/photos')
})

it('ignores a place it does not know, so an address cannot choose where the link goes', async () => {
  show('?from=https://example.com/elsewhere')
  expect(screen.getByRole('link', { name: 'Back to Devices' }).getAttribute('href')).toBe('/settings/devices')
})

import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { TranslationProvider } from '../i18n/i18n'
import { PhotoBackupGuide, canPhoneUseAddress } from './photoBackupGuide'

const address = (url: string) => canPhoneUseAddress(new URL(url))

// The TeaNode app only signs in over https, and a phone away from the
// computer cannot reach what only that computer or its network can.
it('accepts an https address on the internet', () => {
  expect(address('https://photos.example.com')).toBe(true)
  expect(address('https://photos.example.com:8443/')).toBe(true)
  expect(address('https://172.32.0.1')).toBe(true)
})

it('refuses plain http, whatever the host', () => {
  expect(address('http://photos.example.com')).toBe(false)
  expect(address('http://localhost:8080')).toBe(false)
})

it('refuses addresses that only a local network reaches, even over https', () => {
  expect(address('https://localhost')).toBe(false)
  expect(address('https://127.0.0.1:8080')).toBe(false)
  expect(address('https://10.1.2.3')).toBe(false)
  expect(address('https://172.16.5.5')).toBe(false)
  expect(address('https://172.31.5.5')).toBe(false)
  expect(address('https://192.168.1.20')).toBe(false)
  expect(address('https://169.254.1.1')).toBe(false)
  expect(address('https://[::1]')).toBe(false)
  expect(address('https://[fd12:3456::1]')).toBe(false)
  expect(address('https://[fe80::1]')).toBe(false)
  expect(address('https://nas.local')).toBe(false)
  expect(address('https://nas')).toBe(false)
})

it('shows the page address and warns when a phone cannot use it', () => {
  // jsdom's address is http://localhost:3000.
  render(
    <TranslationProvider>
      <PhotoBackupGuide />
    </TranslationProvider>,
  )
  expect(screen.getByText('Back up an iPhone')).toBeTruthy()
  expect(screen.getAllByText(window.location.origin).length).toBeGreaterThan(0)
  expect(screen.getByRole('note').textContent).toContain('needs this server on an https address')
  expect(screen.getByRole('link', { name: 'How to put the server on https' }).getAttribute('href')).toContain(
    'deployment.md#photos-and-the-iphone-app',
  )
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

// The code the iPhone's camera scans is only worth offering when the phone can
// use the address it leads to.
it('offers the code for a phone only where a phone can use the address', () => {
  vi.stubGlobal('location', new URL('https://photos.example.com/photos'))
  render(
    <TranslationProvider>
      <PhotoBackupGuide canConnect />
    </TranslationProvider>,
  )
  expect(screen.getByRole('link', { name: 'Connect a phone' }).getAttribute('href')).toBe('/connect?from=photos')
  expect(screen.queryByRole('note')).toBeNull()
  cleanup()
  vi.stubGlobal('location', new URL('http://localhost:3000/photos'))
  render(
    <TranslationProvider>
      <PhotoBackupGuide canConnect />
    </TranslationProvider>,
  )
  expect(screen.queryByRole('link', { name: 'Connect a phone' })).toBeNull()
  expect(screen.getByRole('note')).toBeTruthy()
})

it('leaves the button out where the page has one of its own', () => {
  vi.stubGlobal('location', new URL('https://photos.example.com/settings/devices'))
  render(
    <TranslationProvider>
      <PhotoBackupGuide />
    </TranslationProvider>,
  )
  expect(screen.queryByRole('link', { name: 'Connect a phone' })).toBeNull()
})

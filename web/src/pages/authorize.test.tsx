import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { graphql } from '../api'
import { ToastProvider } from '../components/toast'
import { TranslationProvider } from '../i18n/i18n'
import { AuthorizePage } from './authorize'

vi.mock('../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../api')>()),
  graphql: vi.fn(),
}))
afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  window.history.pushState({}, '', '/')
})

function show(isTeaNodeApp: boolean) {
  vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} }))
  window.history.pushState(
    {},
    '',
    '/authorize?client_id=client-1&redirect_uri=com.teanode.app%3A%2Foauth&response_type=code&code_challenge=abc&code_challenge_method=S256',
  )
  vi.mocked(graphql).mockResolvedValue({
    ReadOAuthAuthorizationRequest: {
      clientName: 'TeaNode',
      redirectHost: 'com.teanode.app',
      username: 'ada',
      registered: new Date().toISOString(),
      isWholeAccount: true,
      isTeaNodeApp,
    },
  } as never)
  render(
    <TranslationProvider>
      <ToastProvider>
        <AuthorizePage />
      </ToastProvider>
    </TranslationProvider>,
  )
}

// The app's return address is a scheme another app on the phone can claim, so
// the page for the TeaNode app says what the reader must have just done.
it('tells the reader to approve the TeaNode app only after starting the sign-in there', async () => {
  show(true)
  expect(
    await screen.findByText('Approve only if you just started signing in to the TeaNode app on your iPhone.'),
  ).toBeTruthy()
})

it('does not say it for another app', async () => {
  show(false)
  await screen.findByText('Allow')
  expect(screen.queryByText(/Approve only if you just started/)).toBeNull()
})

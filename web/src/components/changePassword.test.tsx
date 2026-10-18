import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'

import { ChangePassword } from './changePassword'
import { ToastProvider } from './toast'
import { TranslationProvider } from '../i18n/i18n'

afterEach(cleanup)

it('says whose password this is, and gives a password manager the username', () => {
  const { container } = render(
    <TranslationProvider>
      <ToastProvider>
        <ChangePassword username="ada" />
      </ToastProvider>
    </TranslationProvider>,
  )
  expect(screen.getByText('Signed in as ada.')).toBeTruthy()
  const hidden = container.querySelector('input[autocomplete="username"]') as HTMLInputElement
  expect(hidden.value).toBe('ada')
  expect(hidden.getAttribute('aria-hidden')).toBe('true')
})

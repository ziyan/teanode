import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { TranslationProvider } from '../../i18n/i18n'
import { DateFormatHint, dateFieldOrder } from './dateFormatHint'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

it('knows the order a browser writes a date in', () => {
  expect(dateFieldOrder('en-US')).toEqual(['month', 'day', 'year'])
  expect(dateFieldOrder('ja-JP')).toEqual(['year', 'month', 'day'])
  expect(dateFieldOrder('en-GB')).toEqual(['day', 'month', 'year'])
})

it('says nothing when the browser and the page are in one language', () => {
  vi.stubGlobal('navigator', { language: 'en-US' })
  render(
    <TranslationProvider>
      <DateFormatHint />
    </TranslationProvider>,
  )
  expect(screen.queryByText(/date fields/)).toBeNull()
})

it('says how the fields write dates when the browser is in another language than the page', () => {
  vi.stubGlobal('navigator', { language: 'de-DE' })
  render(
    <TranslationProvider>
      <DateFormatHint />
    </TranslationProvider>,
  )
  expect(screen.getByText('The date fields write dates the way your browser does: day/month/year.')).toBeTruthy()
})

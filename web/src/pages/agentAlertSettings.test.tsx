import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, expect, it, vi } from 'vitest'
import { TranslationProvider } from '../i18n/i18n'
import { Agent, AlertSettings, READ_AGENT } from './agent'

afterEach(cleanup)

const removed = ['alertQuietStart', 'alertQuietEnd', 'alertDailyMost', 'lookAroundWeekday', 'lookAroundTime']

it('does not ask for the removed alert fields', () => {
  for (const name of removed) expect(READ_AGENT).not.toContain(name)
})

it('shows only the switch and saves only it', () => {
  const onSave = vi.fn().mockResolvedValue(undefined)
  const { container } = render(
    <MemoryRouter>
      <TranslationProvider>
        <AlertSettings agent={{ isAlertsEnabled: true } as Agent} busy={false} onSave={onSave} />
      </TranslationProvider>
    </MemoryRouter>,
  )
  expect(container.querySelectorAll('input[type="time"], input[type="number"], select')).toHaveLength(0)
  fireEvent.click(screen.getByRole('checkbox'))
  expect(onSave).toHaveBeenCalledTimes(1)
  expect(Object.keys(onSave.mock.calls[0][0])).toEqual(['isAlertsEnabled'])
})

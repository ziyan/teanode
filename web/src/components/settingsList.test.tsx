import { cleanup, render } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'

import { SettingsRow } from './settingsList'
import { Tooltip } from './tooltip'
import { TranslationProvider } from '../i18n/i18n'

afterEach(cleanup)

function iconButton(label: string) {
  return (
    <Tooltip label={label}>
      <button type="button" className="icon-action" aria-label={label} />
    </Tooltip>
  )
}

function rowOf(actions: React.ReactNode) {
  const { container } = render(
    <TranslationProvider>
      <SettingsRow title="Example" actions={actions} />
    </TranslationProvider>,
  )
  return container.querySelector('.settings-row') as HTMLElement
}

it('marks a row whose actions are one to three icons, so a phone keeps them beside the title', () => {
  expect(
    rowOf(
      <div className="row-actions">
        {iconButton('Edit')}
        {false}
        {iconButton('Remove')}
      </div>,
    ).classList.contains('has-icon-actions'),
  ).toBe(true)
})

it('does not mark a row with a word button, four icons or no actions', () => {
  expect(
    rowOf(
      <>
        {iconButton('Edit')}
        <button type="button">Rename</button>
      </>,
    ).classList.contains('has-icon-actions'),
  ).toBe(false)
  expect(
    rowOf(
      <div className="row-actions">
        {iconButton('One')}
        {iconButton('Two')}
        {iconButton('Three')}
        {iconButton('Four')}
      </div>,
    ).classList.contains('has-icon-actions'),
  ).toBe(false)
  cleanup()
  expect(rowOf(undefined).classList.contains('has-icon-actions')).toBe(false)
})

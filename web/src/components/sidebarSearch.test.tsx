import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'

import { TranslationProvider } from '../i18n/i18n'
import { focusSearch } from '../searchEverything'
import { SidebarSearch } from './sidebarSearch'

afterEach(cleanup)

function Where() {
  const location = useLocation()
  return <p data-testid="where">{location.pathname + location.search}</p>
}

function renderBox(
  path = '/page',
  props: { isHidden?: boolean; onReveal?: () => void; onSubmitted?: () => void } = {},
) {
  const onReveal = props.onReveal ?? vi.fn()
  const onSubmitted = props.onSubmitted ?? vi.fn()
  const tree = (isHidden: boolean) => (
    <TranslationProvider>
      <MemoryRouter initialEntries={[path]}>
        <SidebarSearch pages={[]} isHidden={isHidden} onReveal={onReveal} onSubmitted={onSubmitted} />
        <Routes>
          <Route path="*" element={<Where />} />
        </Routes>
      </MemoryRouter>
    </TranslationProvider>
  )
  const view = render(tree(props.isHidden ?? false))
  return { onReveal, onSubmitted, show: () => view.rerender(tree(false)) }
}

it('takes focus, with its words selected, when the search key is pressed', () => {
  renderBox()
  const box = screen.getByRole('combobox', { name: 'Search' }) as HTMLInputElement
  fireEvent.change(box, { target: { value: 'seed tray' } })
  focusSearch()
  expect(document.activeElement).toBe(box)
  expect(box.selectionStart).toBe(0)
  expect(box.selectionEnd).toBe('seed tray'.length)
})

it('opens the results with the words in the address on Enter, and closes an overlay over them', () => {
  const { onSubmitted } = renderBox()
  const box = screen.getByRole('combobox', { name: 'Search' })
  fireEvent.change(box, { target: { value: '  tea garden ' } })
  fireEvent.submit(box.closest('form')!)
  expect(screen.getByTestId('where').textContent).toBe('/search?q=tea+garden')
  expect(onSubmitted).toHaveBeenCalledTimes(1)
})

it('does nothing on Enter with no words', () => {
  const { onSubmitted } = renderBox()
  const box = screen.getByRole('combobox', { name: 'Search' })
  fireEvent.change(box, { target: { value: '   ' } })
  fireEvent.submit(box.closest('form')!)
  expect(screen.getByTestId('where').textContent).toBe('/page')
  expect(onSubmitted).not.toHaveBeenCalled()
})

it('shows the words of the results it is on, and none elsewhere', () => {
  renderBox('/search?q=seed+tray')
  expect((screen.getByRole('combobox') as HTMLInputElement).value).toBe('seed tray')
  cleanup()
  renderBox('/page?q=seed+tray')
  expect((screen.getByRole('combobox') as HTMLInputElement).value).toBe('')
})

it('lets go of focus on Escape', () => {
  renderBox()
  const box = screen.getByRole('combobox')
  box.focus()
  fireEvent.keyDown(box, { key: 'Escape' })
  expect(document.activeElement).not.toBe(box)
})

it('opens a narrowed rail or a closed overlay first, then takes focus once it is there', () => {
  const { onReveal, show } = renderBox('/page', { isHidden: true })
  expect(screen.queryByRole('combobox')).toBeNull()
  focusSearch()
  expect(onReveal).toHaveBeenCalledTimes(1)
  show()
  expect(document.activeElement).toBe(screen.getByRole('combobox'))
})

it('is one button when the rail is narrowed, which opens it and asks for the box', () => {
  const { onReveal, show } = renderBox('/page', { isHidden: true })
  fireEvent.click(screen.getByRole('button', { name: 'Search' }))
  expect(onReveal).toHaveBeenCalledTimes(1)
  show()
  expect(document.activeElement).toBe(screen.getByRole('combobox'))
})

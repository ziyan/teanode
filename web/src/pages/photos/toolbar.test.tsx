import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { TranslationProvider } from '../../i18n/i18n'
import { PhotoIconAction, PhotoToolbar, PhotoToolbarButton } from './toolbar'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const stubPhone = (isPhone: boolean) =>
  vi.stubGlobal('matchMedia', () => ({ matches: isPhone, addEventListener() {}, removeEventListener() {} }))

const show = (hasTools = true) => {
  const onUpload = vi.fn()
  const onDelete = vi.fn()
  render(
    <TranslationProvider>
      <PhotoToolbar
        tools={hasTools ? <input type="search" aria-label="Search" /> : null}
        essentials={<button type="button">Years</button>}
      >
        <PhotoToolbarButton isPrimary label="Upload photos" onClick={onUpload} />
        <PhotoIconAction label="Delete album" icon={<span />} isDangerous onClick={onDelete} />
      </PhotoToolbar>
    </TranslationProvider>,
  )
  return { onUpload, onDelete }
}

it('draws every action in the row at desktop width', () => {
  stubPhone(false)
  show()
  expect(screen.getByRole('button', { name: 'Upload photos' })).toBeTruthy()
  expect(screen.getByRole('button', { name: 'Delete album' })).toBeTruthy()
  expect(screen.queryByRole('button', { name: 'More' })).toBeNull()
})

it('keeps the search and the essentials in the row on a phone and puts the actions behind More', () => {
  stubPhone(true)
  const { onUpload, onDelete } = show()
  expect(screen.getByRole('searchbox')).toBeTruthy()
  expect(screen.getByRole('button', { name: 'Years' })).toBeTruthy()
  expect(screen.queryByRole('button', { name: 'Upload photos' })).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: 'More' }))
  // Each action is a row of the menu with its name written out.
  expect(screen.getByRole('menuitem', { name: 'Delete album' }).classList.contains('is-dangerous')).toBe(true)
  fireEvent.click(screen.getByRole('menuitem', { name: 'Upload photos' }))
  expect(onUpload).toHaveBeenCalledTimes(1)
  expect(onDelete).not.toHaveBeenCalled()
  // Choosing one closes the menu.
  expect(screen.queryByRole('menu')).toBeNull()
})

it('keeps the action in the row on a phone when the page has nothing to search', () => {
  stubPhone(true)
  show(false)
  expect(screen.getByRole('button', { name: 'Upload photos' })).toBeTruthy()
  expect(screen.queryByRole('button', { name: 'More' })).toBeNull()
})

it('draws no row at all when there is nothing to search and nothing to do', () => {
  stubPhone(false)
  const { container } = render(
    <TranslationProvider>
      <PhotoToolbar tools={null} essentials={false}>
        {false}
        {null}
      </PhotoToolbar>
    </TranslationProvider>,
  )
  expect(container.querySelector('.photo-toolbar')).toBeNull()
})

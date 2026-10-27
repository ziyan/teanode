import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useNavigate } from 'react-router-dom'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { useKeptScroll } from './journalParts'

// Back from a record opened on another page lands where the person was
// reading: the place is kept for the history entry, and a link to the page
// afresh starts at the top.

function Page({ name, day }: { name: string; day: string }) {
  useKeptScroll(true)
  const navigate = useNavigate()
  return (
    <>
      <p>{name}</p>
      <button type="button" onClick={() => navigate('/record')}>
        open the record
      </button>
      <button type="button" onClick={() => navigate(-1)}>
        go back
      </button>
      <button type="button" onClick={() => navigate(day)}>
        open the day again
      </button>
    </>
  )
}

let content: HTMLElement
let scrolledTo: number
const scrollTo = vi.fn()

beforeEach(() => {
  content = document.createElement('div')
  content.className = 'content'
  // jsdom has no layout, so the scroller keeps its own place.
  scrolledTo = 0
  Object.defineProperty(content, 'scrollTop', {
    get: () => scrolledTo,
    set: (value: number) => {
      scrolledTo = value
    },
  })
  content.scrollTo = scrollTo as unknown as typeof content.scrollTo
  document.body.appendChild(content)
})

afterEach(() => {
  cleanup()
  content.remove()
  scrollTo.mockReset()
})

// The places are kept by address, for as long as the page is open, so each
// test reads a day of its own.
function renderPages(day: string) {
  return render(
    <MemoryRouter initialEntries={[day]}>
      <Routes>
        <Route path={day} element={<Page key="day" name="the day" day={day} />} />
        <Route path="/record" element={<Page key="record" name="the record" day={day} />} />
      </Routes>
    </MemoryRouter>,
  )
}

it('returns to where the day was scrolled to when Back comes from a record', () => {
  renderPages('/day-one')
  act(() => {
    scrolledTo = 420
    content.dispatchEvent(new Event('scroll'))
  })
  fireEvent.click(screen.getByRole('button', { name: 'open the record' }))
  expect(screen.getByText('the record')).toBeTruthy()
  // The record is read from the top.
  expect(scrollTo).not.toHaveBeenCalled()

  fireEvent.click(screen.getByRole('button', { name: 'go back' }))
  expect(screen.getByText('the day')).toBeTruthy()
  expect(scrollTo).toHaveBeenCalledWith({ top: 420 })
})

it('starts a day opened afresh at the top', () => {
  renderPages('/day-two')
  act(() => {
    scrolledTo = 420
    content.dispatchEvent(new Event('scroll'))
  })
  fireEvent.click(screen.getByRole('button', { name: 'open the record' }))
  fireEvent.click(screen.getByRole('button', { name: 'open the day again' }))
  expect(screen.getByText('the day')).toBeTruthy()
  expect(scrollTo).not.toHaveBeenCalled()
})

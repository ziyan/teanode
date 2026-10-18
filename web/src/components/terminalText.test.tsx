import { cleanup, render } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'
import { TerminalText, applyCodes } from './terminalText'

afterEach(cleanup)

it('colors each run of text as the codes before it say', () => {
  const { container } = render(
    <TerminalText text={'\x1b[38;2;220;40;40mred\x1b[0m plain \x1b[1;32mbold green\x1b[22m green\x1b[0m'} />,
  )
  const spans = [...container.querySelectorAll('span')]
  expect(spans.map((span) => span.textContent)).toEqual(['red', ' plain ', 'bold green', ' green'])
  expect(spans[0].style.color).toBe('rgb(220, 40, 40)')
  expect(spans[1].getAttribute('style')).toBeNull()
  expect(spans[2].style.fontWeight).toBe('700')
  expect(spans[3].style.fontWeight).toBe('')
  expect(spans[3].style.color).toBe(spans[2].style.color)
})

it('reads the 256 colors and resets', () => {
  expect(applyCodes({}, [38, 5, 196]).foreground).toBe('rgb(255, 0, 0)')
  expect(applyCodes({}, [48, 5, 232]).background).toBe('rgb(8, 8, 8)')
  expect(applyCodes({ foreground: 'red', isBold: true }, [0])).toEqual({})
  expect(applyCodes({ foreground: 'red' }, [39])).toEqual({})
})

it('reads a full color written with colons, with or without its color space', () => {
  const { container } = render(<TerminalText text={'\x1b[38:2::10:20:30mone\x1b[38:2:40:50:60mtwo'} />)
  const spans = [...container.querySelectorAll('span')]
  expect(spans[0].style.color).toBe('rgb(10, 20, 30)')
  expect(spans[1].style.color).toBe('rgb(40, 50, 60)')
})

it('writes the text as text, never as markup', () => {
  const { container } = render(<TerminalText text={'<b>not bold</b>'} />)
  expect(container.querySelector('b')).toBeNull()
  expect(container.textContent).toBe('<b>not bold</b>')
})

import { cleanup, render } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, expect, it, vi } from 'vitest'

vi.mock('../api', () => ({ framedDrawer: false }))
vi.mock('./codeBlock', () => ({ CodeBlock: ({ text }: { text: string }) => <pre>{text}</pre> }))

import { Markdown } from './markdown'

afterEach(cleanup)

const through = (address: string) => `/picture?url=${encodeURIComponent(address)}`

it('draws a picture through the source it is given, linked where the text links it', () => {
  const { container } = render(
    <MemoryRouter>
      <Markdown
        text={'A box: [![Twenty caramels](https://shop.example.org/twenty.png)](https://shop.example.org/caramels) and ![Seven](https://shop.example.org/seven.png)'}
        pictureSource={through}
      />
    </MemoryRouter>,
  )
  const pictures = container.querySelectorAll('img')
  expect(pictures).toHaveLength(2)
  expect(pictures[0].getAttribute('src')).toBe(through('https://shop.example.org/twenty.png'))
  expect(pictures[0].getAttribute('alt')).toBe('Twenty caramels')
  expect(pictures[0].closest('a')?.getAttribute('href')).toBe('https://shop.example.org/caramels')
  expect(pictures[1].closest('a')?.getAttribute('href')).toBe('https://shop.example.org/seven.png')
  expect(container.textContent).not.toContain('![')
})

it('draws a picture as a link where there is no source, and never fetches it directly', () => {
  const { container } = render(
    <MemoryRouter>
      <Markdown text={'![Seven caramels](https://shop.example.org/seven.png)'} />
    </MemoryRouter>,
  )
  expect(container.querySelector('img')).toBeNull()
  const link = container.querySelector('a')
  expect(link?.getAttribute('href')).toBe('https://shop.example.org/seven.png')
  expect(link?.textContent).toBe('Seven caramels')
})

it('draws a picture whose address is not http as its words', () => {
  const { container } = render(
    <MemoryRouter>
      <Markdown text={'![a note](javascript:alert(1))'} pictureSource={through} />
    </MemoryRouter>,
  )
  expect(container.querySelector('img')).toBeNull()
  expect(container.querySelector('a')).toBeNull()
  expect(container.textContent).toContain('a note')
})

it('reads a link inside bold as a link', () => {
  const { container } = render(
    <MemoryRouter>
      <Markdown text={'- **[A Thai kitchen](https://kitchen.example.org/)** is open until ten.'} />
    </MemoryRouter>,
  )
  const link = container.querySelector('strong a')
  expect(link?.getAttribute('href')).toBe('https://kitchen.example.org/')
  expect(link?.textContent).toBe('A Thai kitchen')
  expect(container.textContent).not.toContain('](')
})

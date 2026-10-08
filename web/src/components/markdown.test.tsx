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
        text={
          'A box: [![Twenty caramels](https://shop.example.org/twenty.png)](https://shop.example.org/caramels) and ![Seven](https://shop.example.org/seven.png)'
        }
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

it("links a page of the agent's memory to the Knowledge page, and the drawer is told it is leaving", () => {
  const leaving = vi.fn()
  const { container } = render(
    <MemoryRouter>
      <Markdown
        text={'That is [Some Person](memory:people/some-person), fact [four](memory:projects/example-app#4).'}
        onLeaving={leaving}
      />
    </MemoryRouter>,
  )
  const links = container.querySelectorAll('a')
  expect(links).toHaveLength(2)
  expect(links[0].getAttribute('href')).toBe('/knowledge/people/some-person')
  expect(links[0].textContent).toBe('Some Person')
  expect(links[0].getAttribute('target')).toBeNull()
  expect(links[1].getAttribute('href')).toBe('/knowledge/projects/example-app')
  links[0].click()
  expect(leaving).toHaveBeenCalledTimes(1)
})

it('draws a memory link whose path is not a page as its words', () => {
  for (const address of [
    'memory:../server/about',
    'memory:people//some-person',
    'memory:People/Some-Person',
    'memory:people/some-person?tab=tokens',
    'memory:people/some-person#first',
    'memory://example.net/people',
    'memory:',
    'mail:item1/../../settings',
  ]) {
    const { container, unmount } = render(
      <MemoryRouter>
        <Markdown text={`See [the page](${address}).`} />
      </MemoryRouter>,
    )
    expect(container.querySelector('a'), address).toBeNull()
    expect(container.textContent).toBe('See the page.')
    unmount()
  }
})

const drawn = (text: string) =>
  render(
    <MemoryRouter>
      <Markdown text={text} />
    </MemoryRouter>,
  ).container

it('draws a displayed formula as MathML, on one line or several', () => {
  const container = drawn(
    'The rate:\n\n\\[\n\\text{error rate} = \\frac{\\text{failed picks}}{\\text{all picks}}\n\\]\n\nand \\[ x^2 \\] too.',
  )
  const formulas = container.querySelectorAll('math')
  expect(formulas).toHaveLength(2)
  expect(formulas[0].getAttribute('display')).toBe('block')
  const fraction = formulas[0].querySelector('mfrac')!
  expect(fraction.children[0].textContent).toBe('failed picks')
  expect(fraction.children[1].textContent).toBe('all picks')
  expect(formulas[0].querySelector('annotation')?.textContent).toContain('\\frac')
  expect(formulas[1].querySelector('msup')?.textContent).toBe('x2')
  expect(container.textContent).not.toContain('\\[')
})

it('reads a formula whose backslashes were eaten from a bracket on a line of its own', () => {
  const container = drawn('[\n\\text{rate} = \\frac{a}{b}\n]\n\n[a link](https://example.org)')
  expect(container.querySelector('mfrac')).not.toBeNull()
  expect(container.querySelector('a')?.getAttribute('href')).toBe('https://example.org/')
})

it('draws inline formulas but leaves prices as words', () => {
  const container = drawn('With \\(\\alpha \\le 1\\) and $x_i$, it costs $5 or $10, between $3-$4.')
  const formulas = container.querySelectorAll('math')
  expect(formulas).toHaveLength(2)
  expect(formulas[0].textContent).toContain('α≤1')
  expect(formulas[1].querySelector('msub')).not.toBeNull()
  expect(container.textContent).toContain('it costs $5 or $10, between $3-$4.')
})

it('lays out matrices, aligned rows and cases as tables', () => {
  const container = drawn(
    '$$\\begin{pmatrix} 1 & 2 \\\\ 3 & 4 \\end{pmatrix}$$\n\n\\[\n\\begin{aligned} a &= b + c \\\\ &= d \\end{aligned}\n\\]',
  )
  const tables = container.querySelectorAll('mtable')
  expect(tables).toHaveLength(2)
  expect(tables[0].querySelectorAll('mtr')).toHaveLength(2)
  expect(tables[0].querySelectorAll('mtd')).toHaveLength(4)
  expect(tables[1].querySelectorAll('mtr')).toHaveLength(2)
  expect(tables[1].querySelector('mtd')?.getAttribute('class')).toContain('formula-right')
})

it('draws a command it does not know as it was written, and survives broken TeX', () => {
  const container = drawn('\\[ \\unknowncommand{x} + \\frac{1}{ \\left( 2 \\]')
  expect(container.querySelector('math')?.textContent).toContain('\\unknowncommand')
})

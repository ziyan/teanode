import type { CSSProperties, ReactNode } from 'react'

// A terminal's screen as the program in it colored it. The text comes with
// the codes that select a color or a style (SGR) and no other escape
// sequence: the computer program keeps only those. Each run of text between
// codes becomes a span with the style the codes before it set, so nothing
// is ever written into the page as markup.

interface TerminalStyle {
  foreground?: string
  background?: string
  isBold?: boolean
  isDim?: boolean
  isItalic?: boolean
  isUnderlined?: boolean
  isInverse?: boolean
}

// The sixteen colors most terminals start with.
const BASIC_COLORS = [
  '#000000',
  '#cd3131',
  '#0dbc79',
  '#e5e510',
  '#2472c8',
  '#bc3fbc',
  '#11a8cd',
  '#e5e5e5',
  '#666666',
  '#f14c4c',
  '#23d18b',
  '#f5f543',
  '#3b8eea',
  '#d670d6',
  '#29b8db',
  '#ffffff',
]

// colorOfIndex is one of the 256 colors a code may name by number.
function colorOfIndex(index: number): string | undefined {
  if (index < 0 || index > 255) return undefined
  if (index < 16) return BASIC_COLORS[index]
  if (index < 232) {
    const cube = index - 16
    const level = (value: number) => (value === 0 ? 0 : 55 + value * 40)
    return `rgb(${level(Math.floor(cube / 36))}, ${level(Math.floor(cube / 6) % 6)}, ${level(cube % 6)})`
  }
  const gray = 8 + (index - 232) * 10
  return `rgb(${gray}, ${gray}, ${gray})`
}

// applyCodes changes a style as one code's numbers say.
export function applyCodes(style: TerminalStyle, numbers: number[]): TerminalStyle {
  const next = { ...style }
  for (let at = 0; at < numbers.length; at++) {
    const number = numbers[at]
    if (number === 0) {
      for (const key of Object.keys(next)) delete next[key as keyof TerminalStyle]
    } else if (number === 1) next.isBold = true
    else if (number === 2) next.isDim = true
    else if (number === 3) next.isItalic = true
    else if (number === 4) next.isUnderlined = true
    else if (number === 7) next.isInverse = true
    else if (number === 22) next.isBold = next.isDim = false
    else if (number === 23) next.isItalic = false
    else if (number === 24) next.isUnderlined = false
    else if (number === 27) next.isInverse = false
    else if (number >= 30 && number <= 37) next.foreground = BASIC_COLORS[number - 30]
    else if (number >= 90 && number <= 97) next.foreground = BASIC_COLORS[number - 90 + 8]
    else if (number >= 40 && number <= 47) next.background = BASIC_COLORS[number - 40]
    else if (number >= 100 && number <= 107) next.background = BASIC_COLORS[number - 100 + 8]
    else if (number === 39) delete next.foreground
    else if (number === 49) delete next.background
    else if (number === 38 || number === 48) {
      const key = number === 38 ? 'foreground' : 'background'
      if (numbers[at + 1] === 5) {
        next[key] = colorOfIndex(numbers[at + 2])
        at += 2
      } else if (numbers[at + 1] === 2) {
        const [red, green, blue] = numbers.slice(at + 2, at + 5)
        if ([red, green, blue].every((value) => value >= 0 && value <= 255))
          next[key] = `rgb(${red}, ${green}, ${blue})`
        at += 4
      }
    }
  }
  return next
}

function cssOf(style: TerminalStyle): CSSProperties | undefined {
  let { foreground, background } = style
  if (style.isInverse)
    [foreground, background] = [background ?? 'var(--terminal-background)', foreground ?? 'var(--terminal-text)']
  const css: CSSProperties = {}
  if (foreground) css.color = foreground
  if (background) css.backgroundColor = background
  if (style.isBold) css.fontWeight = 700
  if (style.isDim) css.opacity = 0.6
  if (style.isItalic) css.fontStyle = 'italic'
  if (style.isUnderlined) css.textDecoration = 'underline'
  return Object.keys(css).length > 0 ? css : undefined
}

const CODE = /\x1b\[([0-9;:]*)m/g

// TerminalText draws colored terminal text in a pre, on the terminal's own
// dark ground whatever the page's theme, since the colors were chosen
// against one.
export function TerminalText({ text, className }: { text: string; className?: string }) {
  const parts: ReactNode[] = []
  let style: TerminalStyle = {}
  let last = 0
  for (const match of text.matchAll(CODE)) {
    if (match.index > last) {
      parts.push(
        <span key={parts.length} style={cssOf(style)}>
          {text.slice(last, match.index)}
        </span>,
      )
    }
    const numbers = match[1] === '' ? [0] : match[1].split(/[;:]/).map((each) => Number(each || 0))
    style = applyCodes(style, numbers)
    last = match.index + match[0].length
  }
  if (last < text.length) {
    parts.push(
      <span key={parts.length} style={cssOf(style)}>
        {text.slice(last)}
      </span>,
    )
  }
  return <pre className={['terminal-text', className].filter(Boolean).join(' ')}>{parts}</pre>
}

import { createElement, useLayoutEffect, useMemo, useRef } from 'react'

// A formula an answer wrote in TeX, \frac{a}{b} and the rest, drawn as
// MathML, which every browser the dashboard runs in lays out itself.
//
// No library, for the reason the Markdown beside it has none: the text is
// a model's. This reads the part of TeX an answer is written in, fractions,
// roots, scripts, Greek, operators, \text, accents, \left and \right,
// matrices and aligned rows, and draws a command it does not know as the
// words it was written as. Like the Markdown, everything ends up as text
// nodes and a fixed set of MathML elements and attributes, never HTML.

type MathNode = { tag: string; attributes?: Record<string, string>; children: (MathNode | string)[] }

type Token = { kind: 'command'; name: string } | { kind: 'character'; text: string }

function node(tag: string, children: (MathNode | string)[], attributes?: Record<string, string>): MathNode {
  return { tag, children, attributes }
}

const identifier = (text: string, attributes?: Record<string, string>) => node('mi', [text], attributes)
const operator = (text: string, attributes?: Record<string, string>) => node('mo', [text], attributes)
const space = (width: string) => node('mspace', [], { width })
const row = (children: MathNode[]): MathNode => (children.length === 1 ? children[0] : node('mrow', children))

// The function application mark after sin or log, which gives the thin
// space TeX puts between a function's name and what it is applied to.
const APPLY_FUNCTION = '⁡'

// prettier-ignore
const GREEK: Record<string, string> = {
  alpha: 'α', beta: 'β', gamma: 'γ', delta: 'δ', epsilon: 'ϵ', varepsilon: 'ε', zeta: 'ζ', eta: 'η',
  theta: 'θ', vartheta: 'ϑ', iota: 'ι', kappa: 'κ', lambda: 'λ', mu: 'μ', nu: 'ν', xi: 'ξ', pi: 'π',
  varpi: 'ϖ', rho: 'ρ', varrho: 'ϱ', sigma: 'σ', varsigma: 'ς', tau: 'τ', upsilon: 'υ', phi: 'ϕ',
  varphi: 'φ', chi: 'χ', psi: 'ψ', omega: 'ω',
}

// Capital Greek letters stand upright, as TeX sets them.
// prettier-ignore
const UPRIGHT: Record<string, string> = {
  Gamma: 'Γ', Delta: 'Δ', Theta: 'Θ', Lambda: 'Λ', Xi: 'Ξ', Pi: 'Π', Sigma: 'Σ', Upsilon: 'Υ',
  Phi: 'Φ', Psi: 'Ψ', Omega: 'Ω', infty: '∞', emptyset: '∅', varnothing: '∅', aleph: 'ℵ',
  ell: 'ℓ', hbar: 'ℏ', Re: 'ℜ', Im: 'ℑ', nabla: '∇', partial: '∂', wp: '℘',
}

// prettier-ignore
const OPERATORS: Record<string, string> = {
  times: '×', cdot: '⋅', div: '÷', pm: '±', mp: '∓', le: '≤', leq: '≤', ge: '≥', geq: '≥',
  ne: '≠', neq: '≠', approx: '≈', equiv: '≡', sim: '∼', simeq: '≃', cong: '≅', propto: '∝',
  ll: '≪', gg: '≫', in: '∈', notin: '∉', ni: '∋', subset: '⊂', subseteq: '⊆', supset: '⊃',
  supseteq: '⊇', cup: '∪', cap: '∩', setminus: '∖', wedge: '∧', land: '∧', vee: '∨', lor: '∨',
  neg: '¬', lnot: '¬', forall: '∀', exists: '∃', to: '→', rightarrow: '→', leftarrow: '←',
  gets: '←', Rightarrow: '⇒', Leftarrow: '⇐', Leftrightarrow: '⇔', leftrightarrow: '↔',
  iff: '⟺', implies: '⟹', longrightarrow: '⟶', longleftarrow: '⟵', mapsto: '↦',
  uparrow: '↑', downarrow: '↓', mid: '∣', parallel: '∥', perp: '⊥', circ: '∘', bullet: '∙',
  star: '⋆', ast: '∗', oplus: '⊕', otimes: '⊗', cdots: '⋯', ldots: '…', dots: '…', vdots: '⋮',
  ddots: '⋱', langle: '⟨', rangle: '⟩', lfloor: '⌊', rfloor: '⌋', lceil: '⌈', rceil: '⌉',
  vert: '|', Vert: '‖', '|': '‖', lbrace: '{', rbrace: '}', '{': '{', '}': '}', colon: ':',
  prime: '′', angle: '∠', triangle: '△', therefore: '∴', because: '∵', degree: '°',
  lt: '<', gt: '>', cdotp: '⋅', ldotp: '.', '%': '%', '#': '#', '&': '&', $: '$', _: '_',
}

// Operators whose scripts go above and below them in a displayed formula.
// prettier-ignore
const LARGE_OPERATORS: Record<string, string> = {
  sum: '∑', prod: '∏', coprod: '∐', bigcup: '⋃', bigcap: '⋂', bigoplus: '⨁', bigotimes: '⨂',
}

const INTEGRALS: Record<string, string> = { int: '∫', iint: '∬', iiint: '∭', oint: '∮' }

// prettier-ignore
const FUNCTIONS = new Set([
  'sin', 'cos', 'tan', 'cot', 'sec', 'csc', 'arcsin', 'arccos', 'arctan', 'sinh', 'cosh', 'tanh',
  'log', 'ln', 'lg', 'exp', 'det', 'dim', 'ker', 'deg', 'gcd', 'arg', 'hom',
])

// Functions whose scripts go below them in a displayed formula, as lim does.
const LIMIT_FUNCTIONS = new Set(['lim', 'liminf', 'limsup', 'max', 'min', 'sup', 'inf', 'Pr', 'argmax', 'argmin'])

// prettier-ignore
const SPACES: Record<string, string> = {
  ',': '0.1667em', ':': '0.2222em', '>': '0.2222em', ';': '0.2778em', '!': '-0.1667em',
  quad: '1em', qquad: '2em', enspace: '0.5em', thinspace: '0.1667em',
}

// prettier-ignore
const ACCENTS: Record<string, string> = {
  hat: '^', widehat: '^', bar: '¯', overline: '¯', vec: '→', overrightarrow: '→', tilde: '~',
  widetilde: '~', dot: '˙', ddot: '¨', check: 'ˇ', breve: '˘', acute: '´', grave: '`',
}

// The sizes of \big, \Big, \bigg and \Bigg, as TeX makes them.
const BIG_SIZES: Record<string, string> = { big: '1.2em', Big: '1.623em', bigg: '2.047em', Bigg: '2.470em' }

const ENVIRONMENT_FENCES: Record<string, [string, string]> = {
  pmatrix: ['(', ')'],
  bmatrix: ['[', ']'],
  Bmatrix: ['{', '}'],
  vmatrix: ['|', '|'],
  Vmatrix: ['‖', '‖'],
  cases: ['{', ''],
}

// Letters in the styles \mathbb, \mathbf and the rest pick: MathML Core
// draws only mathvariant="normal", so the others are the Unicode letters
// made for them, where they start, and the few that live elsewhere.
// prettier-ignore
const ALPHABETS: Record<string, { capital: number; small: number; digit?: number; elsewhere?: Record<string, string> }> = {
  bold: { capital: 0x1d400, small: 0x1d41a, digit: 0x1d7ce },
  'bold-italic': { capital: 0x1d468, small: 0x1d482, digit: 0x1d7ce },
  script: {
    capital: 0x1d49c, small: 0x1d4b6,
    elsewhere: { B: 'ℬ', E: 'ℰ', F: 'ℱ', H: 'ℋ', I: 'ℐ', L: 'ℒ', M: 'ℳ', R: 'ℛ', e: 'ℯ', g: 'ℊ', o: 'ℴ' },
  },
  fraktur: { capital: 0x1d504, small: 0x1d51e, elsewhere: { C: 'ℭ', H: 'ℌ', I: 'ℑ', R: 'ℜ', Z: 'ℨ' } },
  'double-struck': {
    capital: 0x1d538, small: 0x1d552, digit: 0x1d7d8,
    elsewhere: { C: 'ℂ', H: 'ℍ', N: 'ℕ', P: 'ℙ', Q: 'ℚ', R: 'ℝ', Z: 'ℤ' },
  },
  'sans-serif': { capital: 0x1d5a0, small: 0x1d5ba, digit: 0x1d7e2 },
  monospace: { capital: 0x1d670, small: 0x1d68a, digit: 0x1d7f6 },
}

// prettier-ignore
const STYLE_COMMANDS: Record<string, string> = {
  mathrm: 'normal', mathbf: 'bold', boldsymbol: 'bold-italic', bm: 'bold-italic', mathcal: 'script',
  mathscr: 'script', mathfrak: 'fraktur', mathbb: 'double-struck', mathsf: 'sans-serif', mathtt: 'monospace',
  mathit: 'italic',
}

const TEXT_COMMANDS = new Set(['text', 'textrm', 'textnormal', 'mbox', 'textit', 'textbf', 'textsf', 'texttt'])

// Commands that change nothing a reader sees here, and what they take.
// prettier-ignore
const IGNORED_COMMANDS: Record<string, number> = {
  displaystyle: 0, textstyle: 0, limits: 0, nolimits: 0, nonumber: 0, notag: 0, label: 1, tag: 1,
  color: 1,
}

function restyled(text: string, style: string): string {
  const alphabet = ALPHABETS[style]
  if (!alphabet) return text
  return Array.from(text)
    .map((character) => {
      if (alphabet.elsewhere?.[character]) return alphabet.elsewhere[character]
      const code = character.charCodeAt(0)
      if (code >= 65 && code <= 90) return String.fromCodePoint(alphabet.capital + code - 65)
      if (code >= 97 && code <= 122) return String.fromCodePoint(alphabet.small + code - 97)
      if (code >= 48 && code <= 57 && alphabet.digit) return String.fromCodePoint(alphabet.digit + code - 48)
      return character
    })
    .join('')
}

// restyle sets every letter and number under a node in a style.
function restyle(styled: MathNode, style: string): MathNode {
  if (styled.tag === 'mi' || styled.tag === 'mn') {
    const text = styled.children.join('')
    if (style === 'normal') return identifier(text, { mathvariant: 'normal' })
    if (style === 'italic') return identifier(text)
    // A letter in a style of its own is not slanted again.
    return node(styled.tag, [restyled(text, style)], styled.tag === 'mi' ? { mathvariant: 'normal' } : undefined)
  }
  return {
    ...styled,
    children: styled.children.map((child) => (typeof child === 'string' ? child : restyle(child, style))),
  }
}

function tokenize(tex: string): Token[] {
  const tokens: Token[] = []
  for (let at = 0; at < tex.length;) {
    if (tex[at] === '\\') {
      const word = /^\\([a-zA-Z]+)/.exec(tex.slice(at))
      if (word) {
        tokens.push({ kind: 'command', name: word[1] })
        at += word[0].length
      } else {
        tokens.push({ kind: 'command', name: tex[at + 1] ?? '' })
        at += 2
      }
      continue
    }
    const character = String.fromCodePoint(tex.codePointAt(at)!)
    tokens.push({ kind: 'character', text: character })
    at += character.length
  }
  return tokens
}

const isCharacter = (token: Token | undefined, text: string) => token?.kind === 'character' && token.text === text
const isCommand = (token: Token | undefined, name: string) => token?.kind === 'command' && token.name === name

class FormulaParser {
  private at = 0
  private groupDepth = 0
  private fenceDepth = 0
  private environmentDepth = 0
  // The nodes whose scripts go above and below them rather than beside.
  private takesLimits = new WeakSet<MathNode>()

  constructor(
    private tokens: Token[],
    private isDisplay: boolean,
  ) {}

  private skipSpace() {
    while (this.at < this.tokens.length) {
      const token = this.tokens[this.at]
      if (token.kind !== 'character' || !/\s/.test(token.text)) return
      this.at++
    }
  }

  private peek(): Token | undefined {
    this.skipSpace()
    return this.tokens[this.at]
  }

  private next(): Token | undefined {
    this.skipSpace()
    return this.tokens[this.at++]
  }

  // rows reads a formula, or the body of an environment, as rows of cells:
  // & between cells, \\ between rows.
  rows(): MathNode[][][] {
    const rows: MathNode[][][] = []
    for (;;) {
      const cells: MathNode[][] = []
      for (;;) {
        cells.push(this.row())
        if (!isCharacter(this.peek(), '&')) break
        this.next()
      }
      rows.push(cells)
      if (!isCommand(this.peek(), '\\')) break
      this.next()
    }
    // A \\ at the very end opens no row.
    const last = rows[rows.length - 1]
    if (rows.length > 1 && last.length === 1 && last[0].length === 0) rows.pop()
    return rows
  }

  // row reads what stands side by side, up to whatever closes it.
  row(closing?: string): MathNode[] {
    const nodes: MathNode[] = []
    for (;;) {
      const token = this.peek()
      if (!token) break
      if (closing && isCharacter(token, closing)) break
      if (isCharacter(token, '&') || isCommand(token, '\\')) break
      if (isCharacter(token, '}') && this.groupDepth > 0) break
      if (isCommand(token, 'right') && this.fenceDepth > 0) break
      if (isCommand(token, 'end') && this.environmentDepth > 0) break
      if (isCommand(token, 'middle') && this.fenceDepth > 0) {
        this.next()
        nodes.push(this.delimiter(true))
        continue
      }
      const scripted = this.scripted()
      if (scripted) nodes.push(scripted)
    }
    return nodes
  }

  private scripted(): MathNode | null {
    const base = this.atom()
    if (!base) return null
    let subscript: MathNode | undefined
    let superscript: MathNode | undefined
    for (;;) {
      const token = this.peek()
      if (isCharacter(token, '_') && !subscript) {
        this.next()
        subscript = this.argument()
      } else if (isCharacter(token, '^') && !superscript) {
        this.next()
        superscript = this.argument()
      } else if (isCharacter(token, "'")) {
        this.next()
        const prime = operator('′')
        superscript = superscript ? row([prime, superscript]) : prime
      } else {
        break
      }
    }
    if (!subscript && !superscript) return base
    const isLimits = this.takesLimits.has(base)
    if (subscript && superscript) return node(isLimits ? 'munderover' : 'msubsup', [base, subscript, superscript])
    if (subscript) return node(isLimits ? 'munder' : 'msub', [base, subscript])
    return node(isLimits ? 'mover' : 'msup', [base, superscript!])
  }

  // argument reads what a command or a script applies to: a {group}, or
  // the one thing after it.
  private argument(): MathNode {
    const token = this.peek()
    if (token?.kind === 'character' && /[0-9]/.test(token.text)) {
      this.next()
      return node('mn', [token.text])
    }
    return this.atom() ?? row([])
  }

  private group(): MathNode {
    this.groupDepth++
    const nodes = this.row()
    this.groupDepth--
    if (isCharacter(this.peek(), '}')) this.next()
    return row(nodes)
  }

  // rawGroup reads a {group} as the words written in it, for \text.
  private rawGroup(): string {
    this.skipSpace()
    if (!isCharacter(this.tokens[this.at], '{')) {
      const token = this.next()
      return token?.kind === 'character' ? token.text : ''
    }
    this.at++
    let depth = 1
    let text = ''
    while (this.at < this.tokens.length) {
      const token = this.tokens[this.at++]
      if (token.kind === 'command') {
        text += token.name.length === 1 ? (token.name === '\\' ? ' ' : token.name) : `\\${token.name}`
        continue
      }
      if (token.text === '{') depth++
      if (token.text === '}' && --depth === 0) break
      text += token.text
    }
    return text
  }

  // delimiter reads what \left, \right or \big stretch.
  private delimiter(isStretchy: boolean, size?: string): MathNode {
    const token = this.next()
    let text = ''
    if (token?.kind === 'character') text = token.text === '.' ? '' : token.text
    if (token?.kind === 'command') text = OPERATORS[token.name] ?? ''
    if (!text) return space('0em')
    const attributes: Record<string, string> = { stretchy: isStretchy ? 'true' : 'false', symmetric: 'true' }
    if (size) Object.assign(attributes, { minsize: size, maxsize: size })
    return operator(text, attributes)
  }

  private atom(): MathNode | null {
    const token = this.next()
    if (!token) return null
    if (token.kind === 'character') return this.characterAtom(token.text)
    return this.commandAtom(token.name)
  }

  private characterAtom(text: string): MathNode | null {
    if (text === '{') return this.group()
    if (/[0-9.]/.test(text)) {
      let number = text
      while (this.at < this.tokens.length) {
        const following = this.tokens[this.at]
        if (following.kind !== 'character' || !/[0-9.]/.test(following.text)) break
        number += following.text
        this.at++
      }
      return node('mn', [number])
    }
    if (/\p{L}/u.test(text)) return identifier(text)
    if (text === '-') return operator('−')
    if (text === '*') return operator('∗')
    if (text === '~') return node('mtext', [' '])
    if ('()[]|'.includes(text)) return operator(text, { stretchy: 'false' })
    // A brace that closes nothing, or a script with nothing before it.
    return operator(text)
  }

  private commandAtom(name: string): MathNode | null {
    if (GREEK[name]) return identifier(GREEK[name])
    if (UPRIGHT[name]) return identifier(UPRIGHT[name], { mathvariant: 'normal' })
    if (SPACES[name]) return space(SPACES[name])
    if (name === ' ') return node('mtext', [' '])
    if (LARGE_OPERATORS[name]) {
      const large = operator(LARGE_OPERATORS[name], { largeop: 'true', movablelimits: 'true' })
      if (this.isDisplay) this.takesLimits.add(large)
      return large
    }
    if (INTEGRALS[name]) return operator(INTEGRALS[name], { largeop: 'true' })
    if (FUNCTIONS.has(name)) return row([identifier(name, { mathvariant: 'normal' }), operator(APPLY_FUNCTION)])
    if (LIMIT_FUNCTIONS.has(name)) {
      const words = name.startsWith('arg') ? `arg ${name.slice(3)}` : name.replace(/^lim(inf|sup)$/, 'lim $1')
      const limit = identifier(words, { mathvariant: 'normal' })
      if (this.isDisplay) this.takesLimits.add(limit)
      return limit
    }
    if (OPERATORS[name]) {
      const isFence = '{}|‖⟨⟩⌊⌋⌈⌉'.includes(OPERATORS[name])
      return operator(OPERATORS[name], isFence ? { stretchy: 'false' } : undefined)
    }
    if (name === 'frac' || name === 'dfrac' || name === 'tfrac' || name === 'cfrac') {
      return node('mfrac', [this.argument(), this.argument()])
    }
    if (name === 'binom') {
      const numerator = this.argument()
      return row([
        operator('(', { stretchy: 'true' }),
        node('mfrac', [numerator, this.argument()], { linethickness: '0' }),
        operator(')', { stretchy: 'true' }),
      ])
    }
    if (name === 'sqrt') {
      if (isCharacter(this.peek(), '[')) {
        this.next()
        const index = row(this.row(']'))
        this.next()
        return node('mroot', [this.argument(), index])
      }
      return node('msqrt', [this.argument()])
    }
    if (TEXT_COMMANDS.has(name)) {
      const words = this.rawGroup().replace(/^ | $/g, ' ')
      const style = { textbf: 'bold', textit: 'italic', textsf: 'sans-serif', texttt: 'monospace' }[name]
      return node('mtext', [style && style !== 'italic' ? restyled(words, style) : words])
    }
    if (name === 'operatorname') {
      return row([identifier(this.rawGroup(), { mathvariant: 'normal' }), operator(APPLY_FUNCTION)])
    }
    if (STYLE_COMMANDS[name]) return restyle(this.argument(), STYLE_COMMANDS[name])
    if (ACCENTS[name]) {
      return node(
        'mover',
        [
          this.argument(),
          operator(ACCENTS[name], {
            stretchy: name.startsWith('wide') || name === 'overline' || name === 'overrightarrow' ? 'true' : 'false',
          }),
        ],
        { accent: 'true' },
      )
    }
    if (name === 'underline')
      return node('munder', [this.argument(), operator('_', { stretchy: 'true' })], { accentunder: 'true' })
    if (name === 'overbrace' || name === 'underbrace') {
      const braced = this.argument()
      const brace = node(name === 'overbrace' ? 'mover' : 'munder', [
        braced,
        operator(name === 'overbrace' ? '⏞' : '⏟', { stretchy: 'true' }),
      ])
      this.takesLimits.add(brace)
      return brace
    }
    if (name === 'not') {
      const negated = this.atom()
      if (!negated) return null
      const text = negated.children.join('')
      const negations: Record<string, string> = {
        '=': '≠',
        '∈': '∉',
        '<': '≮',
        '>': '≯',
        '≤': '≰',
        '≥': '≱',
        '⊂': '⊄',
        '≡': '≢',
      }
      return operator(negations[text] ?? `${text}̸`)
    }
    if (name === 'left') {
      this.fenceDepth++
      const opening = this.delimiter(true)
      const inside = this.row()
      this.fenceDepth--
      let closingFence: MathNode = space('0em')
      if (isCommand(this.peek(), 'right')) {
        this.next()
        closingFence = this.delimiter(true)
      }
      return node('mrow', [opening, ...inside, closingFence])
    }
    if (name === 'right') return this.delimiter(false)
    const bigSize = /^(big|Big|bigg|Bigg)[lrm]?$/.exec(name)
    if (bigSize) return this.delimiter(true, BIG_SIZES[bigSize[1]])
    if (name === 'begin') return this.environment(this.rawGroup())
    if (name === 'end') {
      this.rawGroup()
      return null
    }
    if (name === 'textcolor') {
      this.rawGroup()
      return this.argument()
    }
    if (name in IGNORED_COMMANDS) {
      for (let argument = 0; argument < IGNORED_COMMANDS[name]; argument++) this.rawGroup()
      return null
    }
    // What this does not know is drawn as it was written, rather than
    // dropped: a reader still sees what the formula said.
    return node('mtext', [`\\${name}`])
  }

  private environment(environmentName: string): MathNode {
    const name = environmentName.replace(/\*$/, '')
    // An array's column layout, {lcr}, is not drawn.
    if (name === 'array') this.rawGroup()
    this.environmentDepth++
    const rows = this.rows()
    this.environmentDepth--
    if (isCommand(this.peek(), 'end')) {
      this.next()
      this.rawGroup()
    }
    const isAligned = ['aligned', 'align', 'split', 'eqnarray', 'alignat', 'alignedat'].includes(name)
    const table = tableOf(rows, isAligned ? 'aligned' : name === 'cases' ? 'left' : 'center')
    const fences = ENVIRONMENT_FENCES[name]
    if (!fences) return table
    return node('mrow', [
      fences[0] ? operator(fences[0], { stretchy: 'true' }) : space('0em'),
      table,
      fences[1] ? operator(fences[1], { stretchy: 'true' }) : space('0em'),
    ])
  }
}

// tableOf lays rows of cells out as a table. Aligned rows alternate right
// and left, so the = in "a &= b" lines up down the rows.
function tableOf(rows: MathNode[][][], alignment: 'aligned' | 'left' | 'center'): MathNode {
  const columnCount = Math.max(...rows.map((cells) => cells.length))
  const columnAlignment = Array.from({ length: columnCount }, (_, column) =>
    alignment === 'aligned' ? (column % 2 === 0 ? 'right' : 'left') : alignment,
  ).join(' ')
  return node(
    'mtable',
    rows.map((cells) =>
      node(
        'mtr',
        cells.map((cell, column) =>
          // Browsers draw no columnalign, so a class of the dashboard's
          // stylesheet aligns each cell.
          node('mtd', [row(cell)], {
            className: `formula-${columnAlignment.split(' ')[column]}${alignment === 'aligned' ? ' formula-aligned' : ''}`,
          }),
        ),
      ),
    ),
    { columnalign: columnAlignment, displaystyle: alignment === 'aligned' ? 'true' : 'false' },
  )
}

function formulaOf(tex: string, isDisplay: boolean): MathNode {
  const parser = new FormulaParser(tokenize(tex), isDisplay)
  const rows = parser.rows()
  const body = rows.length === 1 && rows[0].length === 1 ? row(rows[0][0]) : tableOf(rows, 'aligned')
  return node('math', [node('semantics', [body, node('annotation', [tex], { encoding: 'application/x-tex' })])], {
    display: isDisplay ? 'block' : 'inline',
  })
}

function draw(drawn: MathNode, key: string): React.ReactElement {
  return createElement(
    drawn.tag,
    { key, ...drawn.attributes },
    ...drawn.children.map((child, index) => (typeof child === 'string' ? child : draw(child, `${key}-${index}`))),
  )
}

// Formula draws a formula written in TeX. One this cannot read is shown
// as the TeX it was written in.
export function Formula({ tex, isDisplay }: { tex: string; isDisplay: boolean }) {
  const formula = useMemo(() => {
    try {
      return formulaOf(tex.trim(), isDisplay)
    } catch {
      return null
    }
  }, [tex, isDisplay])
  const box = useRef<HTMLDivElement>(null)
  useLayoutEffect(() => {
    const drawnBox = box.current
    if (!drawnBox || !drawnBox.querySelector('mtable') || typeof ResizeObserver === 'undefined') return
    alignCells(drawnBox)
    // The widths change once the math font has loaded, or the drawer is
    // made wider.
    const observer = new ResizeObserver(() => alignCells(drawnBox))
    observer.observe(drawnBox)
    return () => observer.disconnect()
  }, [formula])
  if (!formula) return <code>{tex}</code>
  const drawn = draw(formula, 'math')
  return isDisplay ? (
    <div className="markdown-formula" ref={box}>
      {drawn}
    </div>
  ) : (
    drawn
  )
}

// A MathML element has a style in a browser, and none where there is no
// layout, as in tests.
type StyledElement = Element & { style?: CSSStyleDeclaration }

// alignCells moves what is in each table cell to the right or the middle
// of it, as its class says. Chrome lays the inside of a cell out from its
// start whatever columnalign or text-align say, so the gap is measured and
// given as a margin. Where a browser aligned the cell itself the gap is
// none and nothing moves.
function alignCells(drawnBox: HTMLElement) {
  const cells = Array.from(drawnBox.querySelectorAll('mtd.formula-right, mtd.formula-center'))
  const contents = cells.map((cell) => cell.firstElementChild as StyledElement | null)
  for (const content of contents) if (content?.style) content.style.marginInlineStart = ''
  const gaps = cells.map((cell, index) => {
    const content = contents[index]
    if (!content) return 0
    const cellBox = cell.getBoundingClientRect()
    const contentBox = content.getBoundingClientRect()
    const cellStyle = getComputedStyle(cell)
    const cellRight = cellBox.right - parseFloat(cellStyle.paddingRight)
    if (cell.classList.contains('formula-right')) return cellRight - contentBox.right
    const cellCenter = (cellBox.left + parseFloat(cellStyle.paddingLeft) + cellRight) / 2
    return cellCenter - (contentBox.left + contentBox.right) / 2
  })
  contents.forEach((content, index) => {
    if (content?.style && gaps[index] > 0.5) content.style.marginInlineStart = `${gaps[index]}px`
  })
}

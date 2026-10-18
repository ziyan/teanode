// Fails on a button or list row whose symbol and words are different
// colors (ios/DESIGN.md, "A symbol is the color of its words").
//
// A button with `role: .destructive` takes red for its words, but its symbol
// takes the tint, which in this app is green. `.tint(.red)` straight after
// the button makes the two the same in a swipe action, a menu or a toolbar,
// and every destructive button with a symbol carries it, so that the rule
// has no exceptions to remember.
//
// In a form or list row the tint does not reach the symbol: a row's symbol
// takes the app's green whatever color its words are. So every row symbol
// is given its color: a button's or menu's label `.actionColor()` (green, or
// grey while it cannot be pressed), a destructive button's
// `.actionColor(isDestructive: true)` after its tint, and any other label
// `.foregroundStyle(.primary)`, the color of the words beside it. A row is
// a label whose nearest container is a Section, Form or List.
//
//   node ios/scripts/check-button-colors.mjs         lists them
//   node ios/scripts/check-button-colors.mjs --fix   colors each
//
// A button whose role is chosen at run time (`role: isRisky ? .destructive :
// nil`) is checked too, and may give its own tint to match (`.tint(isRisky ?
// .red : .accentColor)`): what is checked is that a tint follows.
//
// Written against the file text, like web/scripts/check-link-buttons.mjs, so
// it needs no parser and no dependencies. Comments and the inside of strings
// are blanked out first, so a comment that quotes a button is not one.

import { readFileSync, readdirSync, statSync, writeFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, join, relative } from 'node:path'

const here = dirname(fileURLToPath(import.meta.url))
const iosDirectory = join(here, '..')
const repositoryDirectory = join(iosDirectory, '..')
const isFixing = process.argv.includes('--fix')

function swiftFiles(directory) {
  return readdirSync(directory).flatMap((name) => {
    const path = join(directory, name)
    if (statSync(path).isDirectory()) return name === 'build' ? [] : swiftFiles(path)
    return path.endsWith('.swift') ? [path] : []
  })
}

// stringEnd returns the index just past the string literal that starts at
// `start`: plain (`"`), multi-line (`"""`) or raw (`#"`, `##"""`), stepping
// over escapes and `\( ... )` interpolations, which may hold strings of
// their own.
function stringEnd(text, start) {
  let hashCount = 0
  while (text[start + hashCount] === '#') hashCount++
  const quoteStart = start + hashCount
  const isMultiline = text.startsWith('"""', quoteStart)
  const closing = (isMultiline ? '"""' : '"') + '#'.repeat(hashCount)
  const escape = '\\' + '#'.repeat(hashCount)
  let index = quoteStart + (isMultiline ? 3 : 1)
  while (index < text.length) {
    if (text.startsWith(escape, index)) {
      const after = index + escape.length
      if (text[after] === '(') {
        index = interpolationEnd(text, after)
        continue
      }
      index = after + 1
      continue
    }
    if (text.startsWith(closing, index)) return index + closing.length
    if (!isMultiline && text[index] === '\n') return index
    index++
  }
  return index
}

// interpolationEnd returns the index just past the `)` that closes the
// interpolation opened at `start`.
function interpolationEnd(text, start) {
  let depth = 0
  let index = start
  while (index < text.length) {
    if (isStringStart(text, index)) {
      index = stringEnd(text, index)
      continue
    }
    if (text[index] === '(') depth++
    else if (text[index] === ')') {
      depth--
      if (depth === 0) return index + 1
    }
    index++
  }
  return index
}

function isStringStart(text, index) {
  if (text[index] === '"') return true
  if (text[index] !== '#') return false
  let next = index
  while (text[next] === '#') next++
  return text[next] === '"'
}

// codeOnly is the text with every comment and the inside of every string
// replaced by spaces, the same length and on the same lines, so that offsets
// into it are offsets into the file.
function codeOnly(text) {
  const characters = text.split('')
  const blank = (from, to) => {
    for (let index = from; index < to; index++) if (characters[index] !== '\n') characters[index] = ' '
  }
  let index = 0
  while (index < text.length) {
    if (text.startsWith('//', index)) {
      const lineEnd = text.indexOf('\n', index)
      const end = lineEnd === -1 ? text.length : lineEnd
      blank(index, end)
      index = end
      continue
    }
    if (text.startsWith('/*', index)) {
      // Swift's block comments nest.
      let depth = 0
      let end = index
      while (end < text.length) {
        if (text.startsWith('/*', end)) {
          depth++
          end += 2
        } else if (text.startsWith('*/', end)) {
          depth--
          end += 2
          if (depth === 0) break
        } else end++
      }
      blank(index, end)
      index = end
      continue
    }
    if (isStringStart(text, index)) {
      const end = stringEnd(text, index)
      // The quotes stay, so `Button("x"` still reads as a call with an argument.
      blank(index + 1, end - 1)
      index = end
      continue
    }
    index++
  }
  return characters.join('')
}

// skipBalanced returns the index just past the bracket that closes the one
// at `start`, in text that has no comments or strings left in it.
function skipBalanced(code, start) {
  const opening = code[start]
  const closing = { '(': ')', '{': '}', '[': ']' }[opening]
  let depth = 0
  for (let index = start; index < code.length; index++) {
    if (code[index] === opening) depth++
    else if (code[index] === closing) {
      depth--
      if (depth === 0) return index + 1
    }
  }
  return code.length
}

// buttonEnd returns where the button expression that starts at `start` ends:
// its argument list, then its trailing closures (`{ ... }` and any
// `label: { ... }` after it).
function buttonEnd(code, start) {
  let index = skipBalanced(code, code.indexOf('(', start))
  for (;;) {
    const rest = code.slice(index)
    const closure = rest.match(/^\s*(?:[a-zA-Z]+:\s*)?\{/)
    if (!closure) return index
    // A trailing closure starts on the same line as what it follows, or is a
    // labelled one (`label: {`); a `{` on a later line belongs to something else.
    if (!closure[0].includes(':') && closure[0].includes('\n')) return index
    index = skipBalanced(code, index + closure[0].length - 1)
  }
}

// containerOf returns the name of the nearest container around `start`
// that says where a button is drawn, skipping the `if` and `ForEach` blocks
// between: a Section, Form or List is a row, and anything else is not.
const containerPattern =
  /^(\.swipeActions|\.contextMenu|Menu|ToolbarItem|ToolbarItemGroup|\.toolbar|\.confirmationDialog|\.alert|Section|Form|List|VStack|HStack|LazyVStack|ScrollView|\.safeAreaInset|\.overlay|Picker)$/
function containerOf(code, start) {
  let index = start - 1
  for (let level = 0; level < 12 && index > 0; level++) {
    let depth = 0
    for (; index >= 0; index--) {
      if (code[index] === '}') depth++
      else if (code[index] === '{') {
        if (depth === 0) break
        depth--
      }
    }
    const before = code.slice(Math.max(0, index - 200), index)
    // A section's header and footer are grey words under the rows, not one.
    if (/\b(header|footer):\s*$/.test(before)) return 'header'
    const words = before.match(/([A-Za-z_.]+)\s*(\((?:[^()]|\([^()]*\))*\))?\s*(?:[a-z]+:\s*)?$/)
    if (words && containerPattern.test(words[1])) return words[1]
    if (/-> some View\s*$|var body: some View\s*$/.test(before)) return null
    index--
  }
  return null
}
const rowContainers = new Set(['Section', 'Form', 'List'])

// expressionEnd returns where the call that starts at `start` ends: its
// argument list, then its trailing closures.
function expressionEnd(code, start) {
  return buttonEnd(code, start)
}

// chainOf returns the names of the modifiers chained after `end`.
function chainOf(code, end) {
  const names = []
  let index = end
  for (;;) {
    const step = code.slice(index).match(/^\s*\.([A-Za-z]+)\s*/)
    if (!step) return names
    names.push(step[1])
    index += step[0].length
    if (code[index] === '(') index = skipBalanced(code, index)
    for (;;) {
      const closure = code.slice(index).match(/^\s*(?:[a-zA-Z]+:\s*)?\{/)
      if (!closure || (!closure[0].includes(':') && closure[0].includes('\n'))) break
      index = skipBalanced(code, index + closure[0].length - 1)
    }
  }
}

// controlOf returns the control whose argument list or closure holds the
// label at `start`, such as the NavigationLink it is the label of, stepping
// out of `if` blocks: its name and where it starts.
function controlOf(code, start) {
  let index = start - 1
  for (let level = 0; level < 6 && index > 0; level++) {
    let depth = 0
    for (; index >= 0; index--) {
      const character = code[index]
      if (character === '}' || character === ')') depth++
      else if (character === '{' || character === '(') {
        if (depth === 0) break
        depth--
      }
    }
    if (index < 0) return null
    if (code[index] === '(') {
      const name = code.slice(Math.max(0, index - 40), index).match(/([A-Za-z]+)\s*$/)
      return name ? { name: name[1], start: index - name[0].length } : null
    }
    // A `{`: the closure of `Name(...) {`, or of `label: {` after one, or
    // of an `if`.
    let headStart = Math.max(0, index - 300)
    let head = code.slice(headStart, index).replace(/[a-z]+:\s*$/, '')
    while (/\}\s*$/.test(head)) {
      let closeIndex = headStart + head.search(/\}\s*$/)
      let closeDepth = 0
      for (; closeIndex >= 0; closeIndex--) {
        if (code[closeIndex] === '}') closeDepth++
        else if (code[closeIndex] === '{' && --closeDepth === 0) break
      }
      headStart = Math.max(0, closeIndex - 300)
      head = code.slice(headStart, closeIndex)
      index = closeIndex
    }
    const call = head.match(/([A-Za-z]+)\s*(\((?:[^()]|\((?:[^()]|\([^()]*\))*\))*\))?\s*$/)
    if (call && !/^(if|else|in|let|var)$/.test(call[1])) return { name: call[1], start: headStart + call.index }
    index--
  }
  return null
}

// The controls whose label is an action, green like a button; any other
// row with a symbol shows something or opens a page, and its symbol takes
// the color of its words.
const actionControls = new Set(['Button', 'Menu', 'ShareLink'])
// Labels whose color is the control's own: a link's words are already the
// tint like its symbol, and a placeholder screen is not a row.
const ownColorControls = new Set(['Link', 'ContentUnavailableView', 'PhotosPicker'])
const colorModifiers = /^(foregroundStyle|foregroundColor|actionColor)$/

const findings = []
for (const path of swiftFiles(iosDirectory)) {
  let text = readFileSync(path, 'utf8')
  const code = codeOnly(text)
  const isAppSource = relative(iosDirectory, path).startsWith('TeaNode/')
  const insertions = []
  const place = (start) => `${relative(repositoryDirectory, path)}:${text.slice(0, start).split('\n').length}`
  for (const match of code.matchAll(/\bButton\s*\(/g)) {
    const start = match.index
    const argumentsEnd = skipBalanced(code, code.indexOf('(', start))
    // From the role to the end of the arguments, so a role worked out by a
    // call with arguments of its own is read whole.
    const argumentText = code.slice(start, argumentsEnd)
    const roleIndex = argumentText.search(/\brole:/)
    if (roleIndex === -1 || !/\.destructive\b/.test(argumentText.slice(roleIndex))) continue
    const end = buttonEnd(code, start)
    const buttonText = code.slice(start, end)
    const hasSymbol = /systemImage:|\bLabel\s*\(|\bImage\s*\(/.test(buttonText)
    if (!hasSymbol) continue
    // A role chosen at run time needs a tint chosen the same way, which only
    // a person can write; any tint passes there. A role that always destroys
    // takes red.
    const isAlwaysDestructive = /^role:\s*\.destructive\b/.test(argumentText.slice(roleIndex))
    const isRow = isAlwaysDestructive && rowContainers.has(containerOf(code, start))
    const following = code.slice(end)
    const tint = following.match(/^\s*\.tint\(\s*\.red\s*\)/)
    const hasRowColor = tint && /^\s*\.actionColor\(\s*isDestructive:\s*true\s*\)/.test(following.slice(tint[0].length))
    if (isAlwaysDestructive ? tint && (!isRow || hasRowColor) : /^\s*\.tint\(/.test(following)) continue
    findings.push({
      place: place(start),
      note: !isAlwaysDestructive
        ? 'a destructive button whose role is chosen at run time: give it a tint chosen the same way'
        : isRow
          ? 'a destructive button in a form or list row: .tint(.red), then .actionColor(isDestructive: true)'
          : 'a destructive button: .tint(.red) straight after it',
      isFixable: isAlwaysDestructive,
    })
    if (!isAlwaysDestructive) continue
    // Indented as the modifiers after a button are: one step in when the
    // button is on one line, level with it when it closes on a later one.
    const lineStart = text.lastIndexOf('\n', start) + 1
    const buttonIndentation = text.slice(lineStart).match(/^[ \t]*/)[0]
    const indentation = buttonText.includes('\n') ? buttonIndentation : `${buttonIndentation}    `
    const modifiers = [...(tint ? [] : ['.tint(.red)']), ...(isRow ? ['.actionColor(isDestructive: true)'] : [])]
    insertions.push({ end: tint ? end + tint[0].length : end, indentation, modifiers })
  }
  // In a form or list row the symbol of a label takes the app's green,
  // whatever color its words are, so every row symbol is given a color:
  // an action's with .actionColor(), anything else's with the color of its
  // words. A destructive button is the pass above's.
  for (const match of code.matchAll(/\b(Label|Button|NavigationLink|Toggle|Menu|ShareLink)\s*\(/g)) {
    const start = match.index
    const kind = match[1]
    const argumentsEnd = skipBalanced(code, code.indexOf('(', start))
    const argumentText = code.slice(start, argumentsEnd)
    const end = expressionEnd(code, start)
    if (kind === 'Label') {
      if (!/systemImage:|icon:\s*\{/.test(code.slice(start, end))) continue
    } else if (!/systemImage:/.test(argumentText) || /\brole:/.test(argumentText)) continue
    if (!rowContainers.has(containerOf(code, start))) continue
    const control = kind === 'Label' ? controlOf(code, start) : { name: kind, start }
    if (control && ownColorControls.has(control.name)) continue
    if (chainOf(code, end).some((name) => colorModifiers.test(name))) continue
    // A color on the control around the label is the label's too.
    if (kind === 'Label' && control && chainOf(code, expressionEnd(code, control.start)).some((name) => colorModifiers.test(name))) continue
    const isAction = actionControls.has(control?.name ?? '')
    const modifier = isAction ? '.actionColor()' : '.foregroundStyle(.primary)'
    findings.push({
      place: place(start),
      note: isAction ? 'an action in a form or list row: .actionColor()' : 'a row symbol beside words in the text color: .foregroundStyle(.primary)',
      isFixable: !isAction || isAppSource,
    })
    if (isAction && !isAppSource) continue
    // On its own line when the label ends one, after it on the same line
    // when something follows it there.
    const isLineEnd = /^[ \t]*\n/.test(code.slice(end))
    const lineStart = text.lastIndexOf('\n', start) + 1
    const labelIndentation = text.slice(lineStart).match(/^[ \t]*/)[0]
    const indentation = code.slice(start, end).includes('\n') ? labelIndentation : `${labelIndentation}    `
    insertions.push({ end, indentation, modifiers: [modifier], isInline: !isLineEnd })
  }
  if (isFixing && insertions.length > 0) {
    insertions.sort((first, second) => second.end - first.end)
    for (const { end, indentation, modifiers, isInline } of insertions) {
      const added = isInline ? modifiers.join('') : modifiers.map((modifier) => `\n${indentation}${modifier}`).join('')
      text = `${text.slice(0, end)}${added}${text.slice(end)}`
    }
    writeFileSync(path, text)
  }
}

const unfixed = isFixing ? findings.filter((finding) => !finding.isFixable) : findings
if (isFixing) console.log(`colored ${findings.length - unfixed.length} symbols`)
if (unfixed.length > 0) {
  console.error("A symbol is the color of its words (ios/DESIGN.md), and these are not:")
  for (const finding of unfixed) console.error(`  ${finding.place}: ${finding.note}`)
  if (!isFixing) console.error('Run node ios/scripts/check-button-colors.mjs --fix to color them.')
  process.exit(1)
}

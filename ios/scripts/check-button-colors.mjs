// Fails on a destructive button whose symbol and words are different colors.
//
// A button with `role: .destructive` takes red for its words, but its symbol
// takes the tint, which in this app is green: in a Form or List row the
// result is a green trash can beside red words (ios/DESIGN.md, "Destructive
// ones are red, symbol and words alike"). `.tint(.red)` straight after the
// button makes the two the same. Every destructive button with a symbol
// carries it, inside a swipe action or a menu too, where it is what the
// system does anyway, so that the rule has no exceptions to remember.
//
//   node ios/scripts/check-button-colors.mjs         lists the buttons
//   node ios/scripts/check-button-colors.mjs --fix   adds the tint to each
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

const findings = []
for (const path of swiftFiles(iosDirectory)) {
  let text = readFileSync(path, 'utf8')
  const code = codeOnly(text)
  const insertions = []
  for (const match of code.matchAll(/\bButton\s*\(/g)) {
    const start = match.index
    const argumentsEnd = skipBalanced(code, code.indexOf('(', start))
    if (!/\brole:[^,)]*\.destructive\b/.test(code.slice(start, argumentsEnd))) continue
    const end = buttonEnd(code, start)
    const buttonText = code.slice(start, end)
    const hasSymbol = /systemImage:|\bLabel\s*\(|\bImage\s*\(/.test(buttonText)
    if (!hasSymbol) continue
    if (/^\s*\.tint\(/.test(code.slice(end))) continue
    const line = text.slice(0, start).split('\n').length
    // A role chosen at run time needs a tint chosen the same way, which only
    // a person can write.
    const isAlwaysDestructive = /\brole:\s*\.destructive\b/.test(code.slice(start, argumentsEnd))
    findings.push({ place: `${relative(repositoryDirectory, path)}:${line}`, isAlwaysDestructive })
    if (!isAlwaysDestructive) continue
    // Indented as the modifiers after a button are: one step in when the
    // button is on one line, level with it when it closes on a later one.
    const lineStart = text.lastIndexOf('\n', start) + 1
    const buttonIndentation = text.slice(lineStart).match(/^[ \t]*/)[0]
    const indentation = buttonText.includes('\n') ? buttonIndentation : `${buttonIndentation}    `
    insertions.push({ end, indentation })
  }
  if (isFixing && insertions.length > 0) {
    for (const { end, indentation } of insertions.reverse()) {
      text = `${text.slice(0, end)}\n${indentation}.tint(.red)${text.slice(end)}`
    }
    writeFileSync(path, text)
  }
}

const unfixed = isFixing ? findings.filter((finding) => !finding.isAlwaysDestructive) : findings
if (isFixing) console.log(`added .tint(.red) to ${findings.length - unfixed.length} buttons`)
if (unfixed.length > 0) {
  console.error('A destructive button with a symbol needs a tint straight after it, .tint(.red), so the symbol is red like its words:')
  for (const finding of unfixed) {
    console.error(`  ${finding.place}${finding.isAlwaysDestructive ? '' : ' (its role is chosen at run time: give it a tint chosen the same way)'}`)
  }
  if (!isFixing) console.error('Run node ios/scripts/check-button-colors.mjs --fix to add it.')
  process.exit(1)
}

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
// Written against the file text, like web/scripts/check-link-buttons.mjs, so
// it needs no parser and no dependencies: it skips strings (with their
// interpolations) and comments while it matches brackets.

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

// skipString returns the index just past the string literal that starts at
// `start` (a `"` or `"""`), stepping over escapes and `\( ... )`
// interpolations, which may hold strings of their own.
function skipString(text, start) {
  const isMultiline = text.startsWith('"""', start)
  let index = start + (isMultiline ? 3 : 1)
  while (index < text.length) {
    if (text[index] === '\\') {
      if (text[index + 1] === '(') {
        index = skipBalanced(text, index + 1)
        continue
      }
      index += 2
      continue
    }
    if (isMultiline ? text.startsWith('"""', index) : text[index] === '"') return index + (isMultiline ? 3 : 1)
    index++
  }
  return index
}

// skipBalanced returns the index just past the bracket that closes the one
// at `start`, ignoring brackets in strings and comments.
function skipBalanced(text, start) {
  const opening = text[start]
  const closing = { '(': ')', '{': '}', '[': ']' }[opening]
  let depth = 0
  let index = start
  while (index < text.length) {
    const character = text[index]
    if (character === '"') {
      index = skipString(text, index)
      continue
    }
    if (text.startsWith('//', index)) {
      index = text.indexOf('\n', index)
      if (index === -1) return text.length
      continue
    }
    if (text.startsWith('/*', index)) {
      index = text.indexOf('*/', index) + 2
      continue
    }
    if (character === opening) depth++
    else if (character === closing) {
      depth--
      if (depth === 0) return index + 1
    }
    index++
  }
  return index
}

// buttonEnd returns where the button expression that starts at `start` ends:
// its argument list, then its trailing closures (`{ ... }` and any
// `label: { ... }` after it).
function buttonEnd(text, start) {
  let index = skipBalanced(text, text.indexOf('(', start))
  for (;;) {
    const rest = text.slice(index)
    const closure = rest.match(/^\s*(?:[a-zA-Z]+:\s*)?\{/)
    if (!closure) return index
    // A trailing closure starts on the same line as what it follows, or is a
    // labelled one (`label: {`); a `{` on a later line belongs to something else.
    if (!closure[0].includes(':') && closure[0].includes('\n')) return index
    index = skipBalanced(text, index + closure[0].length - 1)
  }
}

const findings = []
for (const path of swiftFiles(iosDirectory)) {
  let text = readFileSync(path, 'utf8')
  const insertions = []
  for (const match of text.matchAll(/\bButton\s*\(/g)) {
    const start = match.index
    const argumentsEnd = skipBalanced(text, text.indexOf('(', start))
    const argumentText = text.slice(start, argumentsEnd)
    if (!/role:\s*\.destructive/.test(argumentText)) continue
    const end = buttonEnd(text, start)
    const buttonText = text.slice(start, end)
    const hasSymbol = /systemImage:|\bLabel\s*\(|\bImage\s*\(/.test(buttonText)
    if (!hasSymbol) continue
    if (/^\s*\.tint\(\.red\)/.test(text.slice(end))) continue
    const line = text.slice(0, start).split('\n').length
    findings.push(`${relative(repositoryDirectory, path)}:${line}`)
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

if (findings.length > 0 && !isFixing) {
  console.error('A destructive button with a symbol needs .tint(.red) straight after it, so the symbol is red like its words:')
  for (const finding of findings) console.error(`  ${finding}`)
  console.error('Run node ios/scripts/check-button-colors.mjs --fix to add it.')
  process.exit(1)
}
if (isFixing) console.log(`added .tint(.red) to ${findings.length} buttons`)

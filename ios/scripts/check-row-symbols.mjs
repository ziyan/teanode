// Fails on a list row that opens a page and draws its symbol on its own.
//
// A NavigationLink whose label is a `Label(...)` takes `.labelStyle(.pageRow)`
// (ios/DESIGN.md, "A symbol is the color of its words"): without it a list
// greens the row's symbol on iOS 26 and gives each symbol the width of its
// own glyph, so a wide symbol pushes its title to the right of the others.
// The style keeps every row's symbol in one column and in the text color.
//
// A symbol that is much wider than tall, or drawn in two tones, is not
// fixed by the style: the names in `wideSymbols` are refused in a page row,
// and the narrower one that says the same thing is used instead.
//
//   node ios/scripts/check-row-symbols.mjs         lists them
//   node ios/scripts/check-row-symbols.mjs --fix   adds the style
//
// Written against the file text, like check-button-colors.mjs, so it needs
// no parser and no dependencies.

import { readFileSync, writeFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, join, relative } from 'node:path'
import { codeOnly, skipBalanced, swiftFiles } from './swift-text.mjs'

const here = dirname(fileURLToPath(import.meta.url))
const iosDirectory = join(here, '..')
const repositoryDirectory = join(iosDirectory, '..')
const isFixing = process.argv.includes('--fix')

// Symbols that are wide, or filled in two tones, mapped to what to use.
const wideSymbols = new Map([
  ['person.badge.key', 'key.horizontal'],
  ['person.crop.circle.badge.checkmark', 'checkmark.shield'],
  ['internaldrive', 'cylinder'],
  ['externaldrive.connected.to.line.below', 'cylinder.split.1x2'],
  ['mappin.and.ellipse', 'mappin'],
  ['externaldrive', 'cylinder'],
  ['person.3', 'person.2.circle'],
])

// callEnd returns where the call that starts at `start` ends: its argument
// list, then its trailing closures.
function callEnd(code, start) {
  let index = skipBalanced(code, code.indexOf('(', start))
  for (;;) {
    const closure = code.slice(index).match(/^\s*(?:[a-zA-Z]+:\s*)?\{/)
    if (!closure) return index
    if (!closure[0].includes(':') && closure[0].includes('\n')) return index
    index = skipBalanced(code, index + closure[0].length - 1)
  }
}

// linkEnd returns where the NavigationLink at `start` ends: its optional
// argument list, then its closures.
function linkEnd(code, start) {
  let index = start + 'NavigationLink'.length
  if (/^\s*\(/.test(code.slice(index))) index = skipBalanced(code, code.indexOf('(', index))
  for (;;) {
    const closure = code.slice(index).match(/^\s*(?:[a-zA-Z]+:\s*)?\{/)
    if (!closure) return index
    index = skipBalanced(code, index + closure[0].length - 1)
  }
}

// chainOf returns the names of the modifiers chained after `end`.
function chainOf(code, end) {
  const names = []
  const stop = chainEnd(code, end)
  for (const match of code.slice(end, stop).matchAll(/\.([A-Za-z]+)/g)) names.push(match[1])
  return names
}

// chainEnd returns where the modifiers chained after `end` stop.
function chainEnd(code, end) {
  let index = end
  for (;;) {
    const step = code.slice(index).match(/^\s*\.([A-Za-z]+)/)
    if (!step) return index
    index += step[0].length
    if (code[index] === '(') index = skipBalanced(code, index)
  }
}

// linkStart returns where the NavigationLink starts when the label at
// `start` is its label, or -1: inside its argument list or its closures, through the
// `if` blocks and stacks that lay the label out.
function linkStart(code, start) {
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
    if (index < 0) return -1
    if (code[index] === '(') {
      const name = code.slice(Math.max(0, index - 40), index).match(/\bNavigationLink\s*$/)
      return name ? index - name[0].length : -1
    }
    // A `{`: the closure of `NavigationLink {...} label: {`, of
    // `NavigationLink(...) {`, of a stack or of an `if`.
    let headStart = Math.max(0, index - 400)
    let head = code.slice(headStart, index)
    const isLabelClosure = /\blabel:\s*$/.test(head)
    head = head.replace(/\blabel:\s*$/, '')
    if (isLabelClosure) {
      // Step over the destination closure before `label:` to its owner.
      let closeIndex = headStart + head.search(/\}\s*$/)
      if (closeIndex < headStart) return -1
      let closeDepth = 0
      for (; closeIndex >= 0; closeIndex--) {
        if (code[closeIndex] === '}') closeDepth++
        else if (code[closeIndex] === '{' && --closeDepth === 0) break
      }
      const owner = code.slice(Math.max(0, closeIndex - 400), closeIndex).match(/\bNavigationLink\s*(\((?:[^()]|\((?:[^()]|\([^()]*\))*\))*\))?\s*$/)
      if (owner) return Math.max(0, closeIndex - 400) + owner.index
      // A LabeledContent or other wrapper: the link, if any, is further out.
      index = closeIndex - 1
      continue
    }
    const owner = head.match(/\bNavigationLink\s*(\((?:[^()]|\((?:[^()]|\([^()]*\))*\))*\))?\s*$/)
    if (owner) return headStart + owner.index
    index--
  }
  return -1
}

const findings = []
for (const path of swiftFiles(iosDirectory)) {
  let text = readFileSync(path, 'utf8')
  const code = codeOnly(text)
  const place = (start) => `${relative(repositoryDirectory, path)}:${text.slice(0, start).split('\n').length}`
  const edits = []
  for (const match of code.matchAll(/\bLabel\s*\(/g)) {
    const start = match.index
    const link = linkStart(code, start)
    if (link < 0) continue
    // A link drawn as a button (a prominent one in an empty screen) is an
    // action, which draws its own colors.
    if (chainOf(code, linkEnd(code, link)).includes('buttonStyle')) continue
    const end = callEnd(code, start)
    const labelText = text.slice(start, end)
    const chain = code.slice(end, chainEnd(code, end))
    for (const [name] of wideSymbols) {
      if (new RegExp(`systemImage:\\s*"${name.replaceAll('.', '\\.')}"`).test(labelText)) {
        findings.push({ place: place(start), note: `${name} is wide or two-toned in a page row: use ${wideSymbols.get(name)}`, isFixable: false })
      }
    }
    if (/\.symbolRenderingMode\(\s*\.(multicolor|palette)/.test(chain)) {
      findings.push({ place: place(start), note: 'a page row symbol is one color: remove .symbolRenderingMode(.multicolor or .palette)', isFixable: false })
    }
    if (/\.labelStyle\(\s*\.pageRow\s*\)/.test(chain)) continue
    findings.push({ place: place(start), note: 'a page row label: .labelStyle(.pageRow)', isFixable: true })
    const primary = code.slice(end).match(/^(\s*)\.foregroundStyle\(\s*\.primary\s*\)/)
    if (primary) {
      edits.push({ from: end + primary[1].length, to: end + primary[0].length, text: '.labelStyle(.pageRow)' })
    } else {
      const isLineEnd = /^[ \t]*\n/.test(code.slice(end))
      const lineStart = text.lastIndexOf('\n', start) + 1
      const indentation = text.slice(lineStart).match(/^[ \t]*/)[0]
      const extra = labelText.includes('\n') ? indentation : `${indentation}    `
      edits.push({ from: end, to: end, text: isLineEnd ? `\n${extra}.labelStyle(.pageRow)` : '.labelStyle(.pageRow)' })
    }
  }
  if (isFixing && edits.length > 0) {
    edits.sort((first, second) => second.from - first.from)
    for (const edit of edits) text = `${text.slice(0, edit.from)}${edit.text}${text.slice(edit.to)}`
    writeFileSync(path, text)
  }
}

const unfixed = isFixing ? findings.filter((finding) => !finding.isFixable) : findings
if (isFixing) console.log(`styled ${findings.length - unfixed.length} rows`)
if (unfixed.length > 0) {
  console.error('A page row keeps its symbol in one column and in the text color (ios/DESIGN.md), and these do not:')
  for (const finding of unfixed) console.error(`  ${finding.place}: ${finding.note}`)
  if (!isFixing) console.error('Run node ios/scripts/check-row-symbols.mjs --fix to style them.')
  process.exit(1)
}

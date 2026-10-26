// Fails on a sheet, full-screen cover or popover attached inside a list
// (ios/DESIGN.md, "Sheets and covers belong to the screen").
//
// A list builds its sections and rows as it shows them, and builds them again
// when its data changes, a swipe closes or the row scrolls away. A
// presentation attached to one of them goes with it: the sheet opens and
// closes at once, or closes while the person is still in it. So a
// presentation is attached to the List, the Form or the screen, driven by an
// item the row sets (`sheet(item:)`), never to a Section, a row, a ForEach's
// content, a lazy stack's content, or what a Menu, contextMenu or
// swipeActions builds. A view whose body starts with a Section or a ForEach is
// one a list shows, so the same holds inside it.
//
// It reads one file at a time, so it cannot see a card defined in one file and
// listed in another; a card that opens something hands the tap to its screen
// (MessagePresentations).
//
// A presentation that has a reason to be where it is says so on the line
// before it with a `// check-presentations: allow, <why>` comment.
//
//   node ios/scripts/check-presentations.mjs [directory]

import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, join, relative } from 'node:path'
import { codeOnly, swiftFiles } from './swift-text.mjs'

const here = dirname(fileURLToPath(import.meta.url))
const iosDirectory = join(here, '..')
const repositoryDirectory = join(iosDirectory, '..')
const directory = process.argv[2] ?? iosDirectory

// The blocks whose content a list or a menu builds and lets go of.
const rebuiltBlocks = new Set([
  'List',
  'Section',
  'ForEach',
  'LazyVStack',
  'LazyHStack',
  'LazyVGrid',
  'LazyHGrid',
  'Menu',
  'contextMenu',
  'swipeActions',
])

// What a view a list shows as its part starts with.
const rowStarts = new Set(['Section', 'ForEach'])

const presentation = /\.(sheet|fullScreenCover|popover)\s*\(/g
const allowComment = /\/\/\s*check-presentations:\s*allow\b/
const viewDeclaration = /(?:\bvar\s+\w+\s*:\s*some\s+View|->\s*some\s+View)\s*$/

// blockName is the name of what opens the block at `brace`: the identifier
// before it, past an argument list (`Section(header: ...) {`, `ForEach(items)
// { item in`). A computed view is named by the first view in it, so a
// `var row: some View { Section {` counts as a Section.
function blockName(code, brace) {
  let index = brace - 1
  while (index >= 0 && /\s/.test(code[index])) index--
  if (code[index] === ')') {
    let depth = 0
    for (; index >= 0; index--) {
      if (code[index] === ')') depth++
      else if (code[index] === '(') {
        depth--
        if (depth === 0) break
      }
    }
    index--
    while (index >= 0 && /\s/.test(code[index])) index--
  }
  const before = code.slice(Math.max(0, index - 200), index + 1)
  if (viewDeclaration.test(before)) {
    const first = /^\s*(?:@\w+\s*)*([A-Za-z_]\w*)/.exec(code.slice(brace + 1))
    return first && rowStarts.has(first[1]) ? first[1] : 'View'
  }
  const name = /([A-Za-z_]\w*)$/.exec(before)
  return name ? name[1] : ''
}

const findings = []
for (const path of swiftFiles(directory)) {
  const text = readFileSync(path, 'utf8')
  const code = codeOnly(text)
  const lines = text.split('\n')
  const opened = []
  const presentations = [...code.matchAll(presentation)].map((match) => match.index)
  let next = 0
  for (let index = 0; index < code.length; index++) {
    while (next < presentations.length && presentations[next] === index) {
      const inside = opened.filter((name) => rebuiltBlocks.has(name))
      if (inside.length > 0) {
        const lineNumber = code.slice(0, index).split('\n').length
        if (!allowComment.test(lines[lineNumber - 2] ?? '')) {
          const kind = /\.(\w+)/.exec(code.slice(index))[1]
          findings.push(`${relative(repositoryDirectory, path)}:${lineNumber}: .${kind} inside ${inside.join(' > ')}`)
        }
      }
      next++
    }
    if (code[index] === '{') opened.push(blockName(code, index))
    else if (code[index] === '}') opened.pop()
  }
}

if (findings.length > 0) {
  console.error('A sheet, cover or popover is attached inside a list or a menu, which closes it as soon as the')
  console.error('list builds that part again. Attach it to the List or the screen, driven by an item the row sets')
  console.error('(ios/DESIGN.md, "Sheets and covers belong to the screen"):')
  for (const finding of findings) console.error(`  ${finding}`)
  process.exit(1)
}

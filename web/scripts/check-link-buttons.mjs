// Fails on a button styled as a link that does not say why it is one.
//
// A row's actions are icon buttons, and the dashboard's other actions are
// buttons with a box (docs/coding/frontend-design.md, "Row actions"). A
// button with the link class reads as text: beside another it runs into it,
// and on a row it does not read as something to press. It is still right in
// a few places: words inside a sentence, a show or hide toggle beside the
// text it shows, a component's own small control. Each of those says so in
// a comment that starts with "link-button:" and gives the reason, inside its
// opening tag (<button /* link-button: ... */ ...>, which is valid wherever
// the button sits) or just above it, so a new one is a decision somebody
// wrote down rather than a habit.
//
// An anchor with the link class is not checked: it goes somewhere, which is
// what a link is.
//
// Written against the file text, like check-catalogs.mjs, so it needs no
// parser and no dependencies.

import { readFileSync, readdirSync, statSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, join, relative } from 'node:path'

const here = dirname(fileURLToPath(import.meta.url))
const sourceDirectory = join(here, '..', 'src')

// How far above the class the reason may be: the element's opening tag can
// run over several lines, and the comment sits above the tag.
const REASON_LINES_ABOVE = 8

// The link class among an element's classes, however the attribute is
// written: a plain string, or a template that starts with it.
const LINK_CLASS = /className=(?:"link(?:\s[^"]*)?"|\{`link[\s`])/

function sourceFiles(directory) {
  return readdirSync(directory).flatMap((name) => {
    const path = join(directory, name)
    if (statSync(path).isDirectory()) return sourceFiles(path)
    return path.endsWith('.tsx') && !path.endsWith('.test.tsx') ? [path] : []
  })
}

// openingTag is the tag the attribute on this line belongs to: the nearest
// line at or above it that opens one.
function openingTag(lines, index) {
  for (let line = index; line >= Math.max(0, index - REASON_LINES_ABOVE); line--) {
    const match = /<([A-Za-z][\w.]*)/.exec(lines[line].slice(0, line === index ? lines[line].search(LINK_CLASS) : undefined))
    if (match) return { tagName: match[1], line }
  }
  return null
}

const unexplained = []
for (const path of sourceFiles(sourceDirectory)) {
  const lines = readFileSync(path, 'utf8').split('\n')
  lines.forEach((text, index) => {
    if (!LINK_CLASS.test(text)) return
    const tag = openingTag(lines, index)
    if (!tag || tag.tagName !== 'button') return
    const around = lines.slice(Math.max(0, tag.line - REASON_LINES_ABOVE), index + 1).join('\n')
    if (!/link-button:/.test(around)) {
      unexplained.push(`${relative(join(here, '..'), path)}:${index + 1}`)
    }
  })
}

if (unexplained.length > 0) {
  console.error('FAIL: buttons styled as links with no "link-button:" reason:')
  for (const place of unexplained) console.error(`  ${place}`)
  console.error('Use an icon button or a button with a box, or say why this one is a link:')
  console.error('<button /* link-button: <reason> */ ...>. See docs/coding/frontend-design.md.')
  process.exit(1)
}
console.log('every button styled as a link says why')

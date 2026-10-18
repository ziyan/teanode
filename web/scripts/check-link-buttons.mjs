// Fails on a button styled as a link that does not say why it is one.
//
// A row's actions are icon buttons, and the dashboard's other actions are
// buttons with a box (docs/coding/frontend-design.md, "Row actions"). A
// button with the link class reads as text: beside another it runs into it,
// and on a row it does not read as something to press. It is still right in
// a few places: words inside a sentence, a show or hide toggle beside the
// text it shows. Each of those says so in a comment that starts with
// "link-button:" and gives the reason, on the line of the tag or the line
// directly above it or first inside it (<button /* link-button: ... */ ...>,
// valid wherever the button sits), so a new one is a decision somebody wrote down
// rather than a habit.
//
// The link class is found anywhere in a className: a string, a template or
// a ternary, as a whole class ("linked" is not it). So is any class that
// style.css draws the way the link class is drawn: text in the accent color
// with no border and no background, which reads as a link whatever it is
// called. An anchor with an href
// is not checked: it goes somewhere, which is what a link is. An anchor
// without one is a button in disguise, and a router Link needs no link class
// to look like what it is; both are flagged with no exemption.
//
// Written against the file text, like check-catalogs.mjs, so it needs no
// parser and no dependencies.

import { readFileSync, readdirSync, statSync, realpathSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, join, relative } from 'node:path'

const here = dirname(fileURLToPath(import.meta.url))
const sourceDirectory = join(here, '..', 'src')

// The end of the quoted string or template that starts at index, or -1.
function stringEnd(text, index) {
  const quote = text[index]
  for (let position = index + 1; position < text.length; position++) {
    if (text[position] === '\\') position++
    else if (text[position] === quote) return position
  }
  return -1
}

// The index just past the braces that open at index, quotes respected.
function braceEnd(text, index) {
  let depth = 0
  for (let position = index; position < text.length; position++) {
    const character = text[position]
    if (character === '"' || character === "'" || character === '`') {
      const end = stringEnd(text, position)
      if (end < 0) return text.length
      position = end
    } else if (character === '{') depth++
    else if (character === '}' && --depth === 0) return position + 1
  }
  return text.length
}

// The index of the ">" that closes the tag opened at index: the first one
// outside braces and quotes, and not the arrow of "=>".
function tagEnd(text, index) {
  for (let position = index; position < text.length; position++) {
    const character = text[position]
    if (character === '{') position = braceEnd(text, position) - 1
    else if (character === '"' || character === "'") {
      const end = stringEnd(text, position)
      if (end < 0) return text.length
      position = end
    } else if (character === '>' && text[position - 1] !== '=') return position
  }
  return text.length
}

// The classes a stylesheet draws as links: a rule for one class alone whose
// text is the accent color, with no border and no background.
export function linkStyledClasses(css) {
  const classes = new Set(['link'])
  const rule = /(^|\})\s*\.([A-Za-z][\w-]*)\s*\{([^}]*)\}/g
  let match
  while ((match = rule.exec(css))) {
    const body = match[3]
    if (
      /(^|[;\s])color:\s*var\(--accent\)/.test(body) &&
      /border:\s*(none|0)\b/.test(body) &&
      /background:\s*(none|transparent)\b/.test(body)
    ) {
      classes.add(match[2])
    }
    rule.lastIndex = match.index + match[0].length - 1
  }
  return classes
}

// Whether a className value has a link class: any string, template or
// ternary branch in it with one of linkClasses as a whole class.
function hasLinkClass(value, linkClasses) {
  const literals = value.match(/"[^"]*"|'[^']*'|`[^`]*`/g) ?? []
  return literals.some((literal) =>
    literal
      .slice(1, -1)
      .replace(/\$\{[^}]*\}/g, ' ')
      .split(/\s+/)
      .some((name) => linkClasses.has(name)),
  )
}

// Every problem in one file's text, as { line, problem } with line from 1.
export function findProblems(text, linkClasses = new Set(['link'])) {
  const lines = text.split('\n')
  const lineOf = (index) => text.slice(0, index).split('\n').length
  const problems = []
  const attribute = /\bclassName=/g
  let match
  while ((match = attribute.exec(text))) {
    const valueStart = match.index + match[0].length
    let valueEnd
    if (text[valueStart] === '{') valueEnd = braceEnd(text, valueStart)
    else if (text[valueStart] === '"' || text[valueStart] === "'") valueEnd = stringEnd(text, valueStart) + 1
    else continue
    if (!hasLinkClass(text.slice(valueStart, valueEnd), linkClasses)) continue

    // The tag that carries the attribute: the nearest opening tag before
    // it that has not closed by then.
    let tagStart = -1
    const opening = /<([A-Za-z][\w.]*)/g
    let candidate
    while ((candidate = opening.exec(text.slice(0, match.index)))) {
      if (tagEnd(text, candidate.index) > match.index) tagStart = candidate.index
    }
    if (tagStart < 0) continue
    const tagName = /<([A-Za-z][\w.]*)/.exec(text.slice(tagStart))[1]
    const tagText = text.slice(tagStart, tagEnd(text, tagStart))
    const classLine = lineOf(match.index)
    const startLine = lineOf(tagStart)

    if (tagName === 'Link') {
      problems.push({ line: classLine, problem: 'a router Link does not need the link class' })
      continue
    }
    if (tagName === 'a' && !/\bhref=/.test(tagText)) {
      problems.push({ line: classLine, problem: 'an anchor with no href is a button: use a button' })
      continue
    }
    if (tagName !== 'button') continue

    // The reason: on the line of the tag or of the class, or on the line
    // directly above either, or first inside the tag.
    const reasonLines = new Set([startLine - 1, startLine, startLine + 1, classLine - 1, classLine])
    const explained = [...reasonLines].some((line) => /link-button:/.test(lines[line - 1] ?? ''))
    if (!explained) problems.push({ line: classLine, problem: 'button styled as a link with no "link-button:" reason' })
  }
  return problems
}

function sourceFiles(directory) {
  return readdirSync(directory).flatMap((name) => {
    const path = join(directory, name)
    if (statSync(path).isDirectory()) return sourceFiles(path)
    return path.endsWith('.tsx') && !path.endsWith('.test.tsx') ? [path] : []
  })
}

function main() {
  const linkClasses = linkStyledClasses(readFileSync(join(sourceDirectory, 'style.css'), 'utf8'))
  const found = []
  for (const path of sourceFiles(sourceDirectory)) {
    for (const { line, problem } of findProblems(readFileSync(path, 'utf8'), linkClasses)) {
      found.push(`${relative(join(here, '..'), path)}:${line}  ${problem}`)
    }
  }
  if (found.length > 0) {
    console.error('FAIL: link-styled controls that should be real buttons:')
    for (const place of found) console.error(`  ${place}`)
    console.error('Use an icon button or a button with a box, or say why this one is a link:')
    console.error('<button /* link-button: <reason> */ ...>. See docs/coding/frontend-design.md.')
    process.exit(1)
  }
  console.log('every button styled as a link says why')
}

if (process.argv[1] && realpathSync(process.argv[1]) === realpathSync(fileURLToPath(import.meta.url))) main()

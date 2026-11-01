// Fails on a title menu attached with `.toolbarTitleMenu` instead of
// `.titleMenu` (ios/DESIGN.md, the parts that choose their list in the menu
// under the screen's title).
//
// iOS draws a title menu's chevron only on the small title in the bar. Under a
// large title the small one shows only once the page has scrolled, so a page
// with nothing to scroll has no way to its other pages. `.titleMenu` keeps the
// small title and attaches the menu, so a screen never has one without the
// other.
//
// A title menu that has a reason to be attached on its own says so on the line
// before it with a `// check-title-menus: allow, <why>` comment.
//
//   node ios/scripts/check-title-menus.mjs [directory]

import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, join, relative } from 'node:path'
import { codeOnly, swiftFiles } from './swift-text.mjs'

const here = dirname(fileURLToPath(import.meta.url))
const iosDirectory = join(here, '..')
const repositoryDirectory = join(iosDirectory, '..')
const directory = process.argv[2] ?? iosDirectory

const titleMenu = /\.toolbarTitleMenu\b/g
const allowComment = /\/\/\s*check-title-menus:\s*allow\b/

const findings = []
for (const path of swiftFiles(directory)) {
  const text = readFileSync(path, 'utf8')
  const code = codeOnly(text)
  const lines = text.split('\n')
  for (const match of code.matchAll(titleMenu)) {
    const lineNumber = code.slice(0, match.index).split('\n').length
    if (allowComment.test(lines[lineNumber - 2] ?? '')) continue
    findings.push(`${relative(repositoryDirectory, path)}:${lineNumber}: .toolbarTitleMenu`)
  }
}

if (findings.length > 0) {
  console.error('A title menu is attached with .toolbarTitleMenu, which leaves a large title without the menu')
  console.error('until the page scrolls. Use .titleMenu, which keeps the small title with the menu on it:')
  for (const finding of findings) console.error(`  ${finding}`)
  process.exit(1)
}

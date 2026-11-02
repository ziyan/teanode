// Fails on an iPhone screen that puts a bar of its own at the bottom and
// leaves the tab bar showing (ios/DESIGN.md, "A screen with a bar of its own
// at the bottom ... hides the tab bar").
//
// On iOS 26 the tab bar shrinks to a button at each end of the bottom edge
// while a screen scrolls, and a screen's own bottom bar is drawn in the same
// place: the two overlap, and the tab bar's button covers the screen's.
// Every file that places an item in the bottom bar says how the tab bar goes
// away (`.toolbarVisibility(..., for: .tabBar)`), or says why it need not
// with a `// check-tab-bars: allow, <why>` comment, such as a screen shown
// as a full screen cover, which has no tab bar under it.
//
// It reads one file at a time, so a bottom bar in one file and the tab bar
// hidden in another is a finding; hide it in the file with the bar.
//
//   node ios/scripts/check-tab-bars.mjs [directory]

import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, join, relative } from 'node:path'
import { codeOnly, swiftFiles } from './swift-text.mjs'

const here = dirname(fileURLToPath(import.meta.url))
const iosDirectory = join(here, '..')
const repositoryDirectory = join(iosDirectory, '..')
const directory = process.argv[2] ?? iosDirectory

const bottomBar = /placement:\s*\.bottomBar\b/
const tabBarVisibility = /\.toolbarVisibility\([^)]*for:\s*\.tabBar\s*\)/
const allowComment = /\/\/\s*check-tab-bars:\s*allow\b/

const findings = []
for (const path of swiftFiles(directory)) {
  const text = readFileSync(path, 'utf8')
  const code = codeOnly(text)
  const match = bottomBar.exec(code)
  if (!match || tabBarVisibility.test(code) || allowComment.test(text)) continue
  const lineNumber = code.slice(0, match.index).split('\n').length
  findings.push(`${relative(repositoryDirectory, path)}:${lineNumber}: a bottom bar with the tab bar left showing`)
}

if (findings.length > 0) {
  console.error('A screen puts a bar of its own at the bottom and leaves the tab bar showing, which iOS draws on top')
  console.error('of it. Hide the tab bar while the bar is there (.toolbarVisibility(.hidden, for: .tabBar)):')
  for (const finding of findings) console.error(`  ${finding}`)
  process.exit(1)
}

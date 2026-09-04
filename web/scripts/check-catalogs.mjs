// Checks the translation catalogues against the English one.
//
// TypeScript already guarantees that every catalogue has exactly the English
// keys — they are typed as Catalog, so a missing one will not compile. What it
// cannot see is a translation that dropped a {placeholder}, which produces a
// sentence with a hole in it rather than an error, or one that was copied and
// never translated. Both are caught here.
//
// Written against the file text rather than by importing the modules, so this
// needs no test runner and no dependencies: the catalogues are flat object
// literals of string keys to string values, and stay that way.

import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, join } from 'node:path'

const here = dirname(fileURLToPath(import.meta.url))
const catalogDirectory = join(here, '..', 'src', 'i18n')

// Same in every language on purpose: a product name, a protocol name, a dash.
// Values that are the same in every language because they are not words: a
// product name, a literal URL, the name of a program.
const SAME_ON_PURPOSE = new Set([
  'app.name',
  'common.none',
  'domains.dns',
  'domain.tabDns',
  'domain.kindWebhook',
  'integrations.route53',
  'integrations.endpointPlaceholder',
  'server.supervision.systemd',
  // Header names and DMARC's own vocabulary. They appear in mail and in DNS
  // records with these spellings, so translating them would stop somebody
  // matching what is on screen against what is in the record.
  'mailDetail.messageId',
  'mailDetail.html',
  'mailDetail.alignmentRelaxed',
  'mailDetail.alignmentStrict',
  // An example address, which is an address rather than a sentence.
  'profile.emailPlaceholder',
  // A protocol's name, and the name of the tab that configures it.
  'integrations.tabDns',
  // The name of the format a template is written in, and the two header
  // names a message is addressed with. Written this way in every client.
  'editor.html',
  'compose.carbonCopy',
  'compose.blindCarbonCopy',
])

// entries pulls "key: value" pairs out of a catalogue.
//
// Values may be single or double quoted — the formatter switches to double
// quotes for a string containing an apostrophe — and may be split across lines
// with +, so a value is read to its closing quote rather than to the end of
// the line.
function entries(source) {
  const found = new Map()
  const quoted = String.raw`'(?:[^'\\]|\\.)*'|"(?:[^"\\]|\\.)*"`
  const pattern = new RegExp(String.raw`^\s*'([\w.]+)':\s*((?:(?:${quoted})\s*\+?\s*)+),?\s*$`, 'gms')

  for (const match of source.matchAll(pattern)) {
    const pieces = [...match[2].matchAll(new RegExp(quoted, 'g'))].map((piece) =>
      piece[0]
        .slice(1, -1)
        .replace(/\\'/g, "'")
        .replace(/\\"/g, '"'),
    )
    found.set(match[1], pieces.join(''))
  }
  return found
}

function placeholders(text) {
  return [...(text.match(/\{\w+\}/g) ?? [])].sort().join(',')
}

const english = entries(readFileSync(join(catalogDirectory, 'en.ts'), 'utf8'))
if (english.size < 100) {
  console.error(`check-catalogs: only read ${english.size} English keys; the parser is not working`)
  process.exit(1)
}

let failures = 0
function fail(message) {
  console.error(`check-catalogs: ${message}`)
  failures += 1
}

for (const language of ['zh', 'ja']) {
  const catalog = entries(readFileSync(join(catalogDirectory, `${language}.ts`), 'utf8'))

  for (const [key, source] of english) {
    if (!catalog.has(key)) {
      fail(`${language} is missing ${key}`)
      continue
    }
    const translated = catalog.get(key)

    if (placeholders(translated) !== placeholders(source)) {
      fail(
        `${language} ${key} uses ${placeholders(translated) || 'no placeholders'}, ` +
          `but the English uses ${placeholders(source) || 'none'}`,
      )
    }
    if (translated === source && !SAME_ON_PURPOSE.has(key)) {
      fail(`${language} ${key} is identical to the English; was it translated?`)
    }
  }

  for (const key of catalog.keys()) {
    if (!english.has(key)) {
      fail(`${language} has ${key}, which English does not`)
    }
  }
}

if (failures > 0) {
  console.error(`\n${failures} problem(s) in the translation catalogues.`)
  process.exit(1)
}
console.log(`catalogues agree: ${english.size} keys in en, zh and ja`)

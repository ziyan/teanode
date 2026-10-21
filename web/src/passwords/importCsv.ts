// Reads the CSV files that Chrome, Apple Passwords and Bitwarden export. This
// is internal/passwords/importcsv.go in TypeScript: the same detection by
// header row, the same column mapping, and the same cases in the tests.

import { emptyLoginFields, emptyNoteFields, LoginFields, NoteFields } from './fields'
import { parseOneTimeCodeUri } from './oneTimeCode'

// byteOrderMark starts some exports (Excel adds it), and would otherwise stick
// to the first column name.
const BYTE_ORDER_MARK = '﻿'

export type CsvFormat = 'chrome' | 'apple' | 'bitwarden'

export type ImportedItem = { itemKind: 'login'; login: LoginFields } | { itemKind: 'note'; note: NoteFields }

export interface CsvImport {
  format: CsvFormat
  items: ImportedItem[]
  // skippedItemNames are the names of rows the importer does not take, such as
  // a Bitwarden card or identity.
  skippedItemNames: string[]
}

export type ImportCsvProblem = 'empty' | 'unknownFormat'

// ImportCsvError says which problem it was, so the screen can say it in the
// person's language.
export class ImportCsvError extends Error {
  readonly problem: ImportCsvProblem

  constructor(problem: ImportCsvProblem, message: string) {
    super(message)
    this.name = 'ImportCsvError'
    this.problem = problem
  }
}

// parseCsvRows reads comma separated records. A field may be quoted, with a
// doubled quote for a quote and line breaks allowed inside; a quote that does
// not end a quoted field is kept as it is, as Go's reader does with lazy
// quotes. Lines with nothing on them are skipped.
export function parseCsvRows(text: string): string[][] {
  const rows: string[][] = []
  let row: string[] = []
  let field = ''
  let isQuoted = false
  let isFieldStart = true

  function endField() {
    row.push(field)
    field = ''
    isFieldStart = true
  }

  function endRow() {
    endField()
    if (!(row.length === 1 && row[0] === '')) rows.push(row)
    row = []
  }

  const content = text.startsWith(BYTE_ORDER_MARK) ? text.slice(1) : text
  for (let index = 0; index < content.length; index++) {
    const character = content[index]
    if (isQuoted) {
      if (character === '"') {
        const next = content[index + 1]
        if (next === '"') {
          field += '"'
          index++
        } else if (next === undefined || next === ',' || next === '\n' || next === '\r') {
          isQuoted = false
        } else {
          field += '"'
        }
      } else if (character === '\r' && content[index + 1] === '\n') {
        // A line break inside a quoted field is read as one "\n".
        field += '\n'
        index++
      } else {
        field += character
      }
      continue
    }
    if (character === '"' && isFieldStart) {
      isQuoted = true
      isFieldStart = false
    } else if (character === ',') {
      endField()
    } else if (character === '\n') {
      endRow()
    } else if (character === '\r') {
      if (content[index + 1] === '\n') index++
      endRow()
    } else {
      field += character
      isFieldStart = false
    }
  }
  if (field !== '' || row.length > 0 || isQuoted) endRow()
  return rows
}

// columnIndexes maps each lower-cased column name of a header row to its
// position.
function columnIndexes(header: string[]): Map<string, number> {
  const columns = new Map<string, number>()
  header.forEach((name, index) => {
    const cleaned = name.replace(BYTE_ORDER_MARK, '').trim().toLowerCase()
    if (!columns.has(cleaned)) columns.set(cleaned, index)
  })
  return columns
}

// detectCsvFormat tells which manager's export has this header row, whatever
// the case and order of the columns.
export function detectCsvFormat(header: string[]): CsvFormat {
  const columns = columnIndexes(header)
  const hasColumns = (...names: string[]) => names.every((name) => columns.has(name))
  if (hasColumns('login_uri', 'login_username', 'login_password', 'type', 'name')) return 'bitwarden'
  if (hasColumns('title', 'url', 'username', 'password')) return 'apple'
  if (hasColumns('name', 'url', 'username', 'password')) return 'chrome'
  throw new ImportCsvError('unknownFormat', 'That CSV file is not a Chrome, Apple Passwords or Bitwarden export')
}

interface CsvLogin {
  name: string
  sites: string[]
  userName: string
  password: string
  notes: string
  oneTimeCode?: string
  folder?: string
}

// appendSite adds a site as an address: one with no scheme gets https://, and
// one already in the list is not added twice.
function appendSite(sites: string[], site: string): string[] {
  let address = site.trim()
  if (!address.includes('://')) address = `https://${address}`
  return sites.includes(address) ? sites : [...sites, address]
}

// siteHost is the host name of the first site that has one, to name a login
// that came without a name.
function siteHost(sites: string[]): string {
  for (const site of sites) {
    try {
      const hostname = new URL(site).hostname
      if (hostname !== '') return hostname
    } catch {
      // Not an address; try the next.
    }
  }
  return ''
}

// oneTimeCodeUriFrom is the otpauth address for what a CSV column holds: the
// address itself, or a bare base32 secret, which gets an address made around
// it. It is empty for anything else.
function oneTimeCodeUriFrom(text: string, name: string): string {
  let uri = text
  if (!uri.toLowerCase().startsWith('otpauth://')) {
    uri = `otpauth://totp/${encodeURIComponent(name)}?secret=${encodeURIComponent(uri.replace(/ /g, ''))}`
  }
  try {
    parseOneTimeCodeUri(uri)
  } catch {
    return ''
  }
  return uri
}

function loginFromCsv(row: CsvLogin): ImportedItem {
  const login: LoginFields = {
    ...emptyLoginFields(),
    name: row.name.trim(),
    userName: row.userName.trim(),
    password: row.password,
    notes: row.notes.trim(),
    folder: (row.folder ?? '').trim(),
  }
  for (const site of row.sites) {
    if (site.trim() !== '') login.sites = appendSite(login.sites, site)
  }
  if (login.name === '') login.name = siteHost(login.sites)
  if (login.name === '') login.name = login.userName
  const oneTimeCode = (row.oneTimeCode ?? '').trim()
  if (oneTimeCode !== '') {
    login.oneTimeCodeUri = oneTimeCodeUriFrom(oneTimeCode, login.name)
    if (login.oneTimeCodeUri === '') {
      // Not a code this importer can use (a Steam code, say): keep the text
      // rather than lose it.
      login.notes = `${login.notes}\nOne-time code: ${oneTimeCode}`.trim()
    }
  }
  return { itemKind: 'login', login }
}

// parseCsv reads an export from Chrome, Apple Passwords or Bitwarden, found by
// its header row. Rows that are entirely empty are skipped.
export function parseCsv(text: string): CsvImport {
  const rows = parseCsvRows(text)
  if (rows.length === 0) throw new ImportCsvError('empty', 'The CSV file is empty')
  const format = detectCsvFormat(rows[0])
  const columns = columnIndexes(rows[0])

  const csvImport: CsvImport = { format, items: [], skippedItemNames: [] }
  for (const row of rows.slice(1)) {
    // cell is a column of this row as written, or empty when the file has no
    // such column or the row is short.
    const cell = (name: string): string => {
      const index = columns.get(name)
      return index === undefined || index >= row.length ? '' : row[index]
    }
    if (row.every((value) => value.trim() === '')) continue
    if (format === 'chrome') {
      csvImport.items.push(
        loginFromCsv({
          name: cell('name'),
          sites: [cell('url')],
          userName: cell('username'),
          password: cell('password'),
          notes: cell('note'),
        }),
      )
    } else if (format === 'apple') {
      csvImport.items.push(
        loginFromCsv({
          name: cell('title'),
          sites: [cell('url')],
          userName: cell('username'),
          password: cell('password'),
          notes: cell('notes'),
          oneTimeCode: cell('otpauth'),
        }),
      )
    } else {
      const name = cell('name').trim()
      const rowType = cell('type').trim().toLowerCase()
      if (rowType === '' || rowType === 'login') {
        let notes = cell('notes').trim()
        const customFields = cell('fields').trim()
        if (customFields !== '') notes = `${notes}\n${customFields}`.trim()
        csvImport.items.push(
          loginFromCsv({
            name,
            sites: cell('login_uri').split('\n'),
            userName: cell('login_username'),
            password: cell('login_password'),
            notes,
            oneTimeCode: cell('login_totp'),
            folder: cell('folder'),
          }),
        )
      } else if (rowType === 'note') {
        csvImport.items.push({
          itemKind: 'note',
          note: { ...emptyNoteFields(), name, text: cell('notes'), folder: cell('folder').trim() },
        })
      } else {
        csvImport.skippedItemNames.push(name)
      }
    }
  }
  return csvImport
}

// importedItemKey is what makes two items the same one for an import: the
// name, the site and the user name, ignoring case. A note has only its name.
export function importedItemKey(itemKind: 'login' | 'note', fields: LoginFields | NoteFields): string {
  if (itemKind === 'note') return `note\u0000${fields.name.trim().toLowerCase()}`
  const login = fields as LoginFields
  const sites = login.sites
    .map((site) => site.trim().toLowerCase())
    .sort()
    .join('\n')
  return `login\u0000${login.name.trim().toLowerCase()}\u0000${sites}\u0000${login.userName.trim().toLowerCase()}`
}

// withoutDuplicates drops the items whose key is in existingKeys, and the
// later of two items in the file with the same key.
export function withoutDuplicates(
  items: ImportedItem[],
  existingKeys: Set<string>,
): { kept: ImportedItem[]; duplicateCount: number } {
  const seen = new Set(existingKeys)
  const kept: ImportedItem[] = []
  for (const item of items) {
    const key = item.itemKind === 'login' ? importedItemKey('login', item.login) : importedItemKey('note', item.note)
    if (seen.has(key)) continue
    seen.add(key)
    kept.push(item)
  }
  return { kept, duplicateCount: items.length - kept.length }
}

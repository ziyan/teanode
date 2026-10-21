import { expect, it } from 'vitest'

import { emptyLoginFields } from './fields'
import {
  detectCsvFormat,
  ImportCsvError,
  importedItemKey,
  parseCsv,
  parseCsvRows,
  withoutDuplicates,
} from './importCsv'

// The cases are the ones in internal/passwords/importcsv_test.go.

const BYTE_ORDER_MARK = '﻿'

it('detects the format from the header row, whatever the case and order', () => {
  const cases: [string[], string][] = [
    [['name', 'url', 'username', 'password', 'note'], 'chrome'],
    [['name', 'url', 'username', 'password'], 'chrome'],
    [['Title', 'URL', 'Username', 'Password', 'Notes', 'OTPAuth'], 'apple'],
    [
      [
        'folder',
        'favorite',
        'type',
        'name',
        'notes',
        'fields',
        'reprompt',
        'login_uri',
        'login_username',
        'login_password',
        'login_totp',
      ],
      'bitwarden',
    ],
    [[BYTE_ORDER_MARK + 'Name', 'URL', 'Username', 'Password'], 'chrome'],
  ]
  for (const [header, expected] of cases) expect(detectCsvFormat(header)).toBe(expected)
  expect(() => detectCsvFormat(['a', 'b'])).toThrow(ImportCsvError)
})

it('reads quoted fields with commas, doubled quotes and line breaks, and skips blank lines', () => {
  expect(parseCsvRows('a,"b,c","line one\r\nline two","say ""hi"""\n\nlast,,')).toEqual([
    ['a', 'b,c', 'line one\nline two', 'say "hi"'],
    ['last', '', ''],
  ])
  // A quote in the middle of a field is kept, as Go's lazy quotes do.
  expect(parseCsvRows('5" nail,x')).toEqual([['5" nail', 'x']])
})

it('reads a Chrome export', () => {
  const file =
    'name,url,username,password,note\n' +
    'example.com,https://example.com/login,ada@example.com,"pass,word","line one\nline two"\n' +
    ',https://www.example.net,grace,pw2,\n' +
    '\n' +
    'Kept,,ada,,\n'
  const csvImport = parseCsv(file)
  expect(csvImport.format).toBe('chrome')
  expect(csvImport.items).toHaveLength(3)
  expect(csvImport.items[0]).toEqual({
    itemKind: 'login',
    login: {
      ...emptyLoginFields(),
      name: 'example.com',
      userName: 'ada@example.com',
      password: 'pass,word',
      sites: ['https://example.com/login'],
      notes: 'line one\nline two',
    },
  })
  // A row with no name is named after its site's host.
  expect(csvImport.items[1].itemKind === 'login' && csvImport.items[1].login.name).toBe('www.example.net')
  const third = csvImport.items[2]
  expect(third.itemKind === 'login' && third.login.name).toBe('Kept')
  expect(third.itemKind === 'login' && third.login.sites).toEqual([])
})

it('reads an Apple Passwords export, making an address from a bare secret and keeping a code it cannot use', () => {
  const file =
    BYTE_ORDER_MARK +
    'Title,URL,Username,Password,Notes,OTPAuth\n' +
    'Example,https://example.com,ada,pw,,otpauth://totp/Example:ada?secret=JBSWY3DPEHPK3PXP\n' +
    'Plain,example.org,grace,pw2,remember,JBSWY3DP EHPK3PXP\n' +
    'Odd,example.net,alan,pw3,,steam://abc\n'
  const csvImport = parseCsv(file)
  expect(csvImport.format).toBe('apple')
  expect(csvImport.items).toHaveLength(3)
  const [first, second, third] = csvImport.items.map((item) => (item.itemKind === 'login' ? item.login : null))
  expect(first?.name).toBe('Example')
  expect(first?.oneTimeCodeUri).toBe('otpauth://totp/Example:ada?secret=JBSWY3DPEHPK3PXP')
  expect(second?.oneTimeCodeUri).toBe('otpauth://totp/Plain?secret=JBSWY3DPEHPK3PXP')
  expect(second?.notes).toBe('remember')
  expect(second?.sites[0]).toBe('https://example.org')
  expect(third?.oneTimeCodeUri).toBe('')
  expect(third?.notes).toBe('One-time code: steam://abc')
})

it('reads a Bitwarden export: logins with folders and codes, notes, and skips cards and identities', () => {
  const file =
    'folder,favorite,type,name,notes,fields,reprompt,login_uri,login_username,login_password,login_totp\n' +
    'Work,1,login,Intranet,some notes,"pin: 1234",0,"https://intranet.example.com\nhttps://sso.example.com",ada,pw,otpauth://totp/Intranet?secret=JBSWY3DPEHPK3PXP\n' +
    ',0,note,Router,"The router is in the hall.",,0,,,,\n' +
    ',0,card,Visa,,,0,,,,\n' +
    ',0,identity,Me,,,0,,,,\n'
  const csvImport = parseCsv(file)
  expect(csvImport.format).toBe('bitwarden')
  expect(csvImport.items).toHaveLength(2)
  expect(csvImport.items[0]).toEqual({
    itemKind: 'login',
    login: {
      ...emptyLoginFields(),
      name: 'Intranet',
      userName: 'ada',
      password: 'pw',
      sites: ['https://intranet.example.com', 'https://sso.example.com'],
      oneTimeCodeUri: 'otpauth://totp/Intranet?secret=JBSWY3DPEHPK3PXP',
      notes: 'some notes\npin: 1234',
      folder: 'Work',
    },
  })
  const note = csvImport.items[1]
  expect(note.itemKind).toBe('note')
  expect(note.itemKind === 'note' && note.note.name).toBe('Router')
  expect(note.itemKind === 'note' && note.note.text).toBe('The router is in the hall.')
  expect(csvImport.skippedItemNames).toEqual(['Visa', 'Me'])
})

it('refuses an empty file and an unknown one', () => {
  expect(() => parseCsv('')).toThrow(ImportCsvError)
  expect(() => parseCsv('a,b\n1,2\n')).toThrow(ImportCsvError)
})

it('skips an item already kept, or earlier in the file, with the same name, site and user name', () => {
  const file =
    'name,url,username,password\n' +
    'Example,https://example.com,ada,one\n' +
    'EXAMPLE,https://example.com,Ada,two\n' +
    'Example,https://example.com,grace,three\n' +
    'Other,https://example.org,ada,four\n'
  const csvImport = parseCsv(file)
  const existing = new Set([
    importedItemKey('login', { ...emptyLoginFields(), name: 'other', sites: ['https://example.org'], userName: 'ada' }),
  ])
  const { kept, duplicateCount } = withoutDuplicates(csvImport.items, existing)
  expect(duplicateCount).toBe(2)
  expect(kept.map((item) => (item.itemKind === 'login' ? item.login.password : ''))).toEqual(['one', 'three'])
})

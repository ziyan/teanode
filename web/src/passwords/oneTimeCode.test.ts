import { existsSync, readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'
import { encodeBase32 } from './crypto'
import { oneTimeCode, oneTimeCodeSecondsLeft, parseOneTimeCodeUri } from './oneTimeCode'
import { bytesFromText, expectBytes } from './testSupport'

// RFC 6238 appendix B: 8 digit codes, one shared secret per hash, written as
// the ASCII the RFC gives.
const secrets = {
  SHA1: '12345678901234567890',
  SHA256: '12345678901234567890123456789012',
  SHA512: '1234567890123456789012345678901234567890123456789012345678901234',
}
const rfcCases: Array<[number, string, string, string]> = [
  // unix seconds, SHA1, SHA256, SHA512
  [59, '94287082', '46119246', '90693936'],
  [1111111109, '07081804', '68084774', '25091201'],
  [1111111111, '14050471', '67062674', '99943326'],
  [1234567890, '89005924', '91819424', '93441116'],
  [2000000000, '69279037', '90698825', '38618901'],
  [20000000000, '65353130', '77737706', '47863826'],
]

function uriFor(algorithm: keyof typeof secrets): string {
  return `otpauth://totp/Example:ada@example.com?secret=${encodeBase32(bytesFromText(secrets[algorithm]))}&issuer=Example&algorithm=${algorithm}&digits=8`
}

describe('oneTimeCode', () => {
  it.each(rfcCases)('matches RFC 6238 at %i seconds', async (unixSeconds, sha1, sha256, sha512) => {
    expect(await oneTimeCode(parseOneTimeCodeUri(uriFor('SHA1')), unixSeconds)).toBe(sha1)
    expect(await oneTimeCode(parseOneTimeCodeUri(uriFor('SHA256')), unixSeconds)).toBe(sha256)
    expect(await oneTimeCode(parseOneTimeCodeUri(uriFor('SHA512')), unixSeconds)).toBe(sha512)
  })

  it('gives six digits by default, with leading zeros kept', async () => {
    const settings = parseOneTimeCodeUri(`otpauth://totp/Example?secret=${encodeBase32(bytesFromText(secrets.SHA1))}`)
    // The eight digit code at this time is 07081804, so the six digit one is its last six.
    expect(await oneTimeCode(settings, 1111111109)).toBe('081804')
  })

  it('stays the same inside a period and changes at its edge', async () => {
    const settings = parseOneTimeCodeUri(uriFor('SHA1'))
    expect(await oneTimeCode(settings, 60)).toBe(await oneTimeCode(settings, 89))
    expect(await oneTimeCode(settings, 59)).not.toBe(await oneTimeCode(settings, 60))
  })

  it('honors another period', async () => {
    const settings = parseOneTimeCodeUri(`${uriFor('SHA1')}&period=60`)
    expect(await oneTimeCode(settings, 61)).toBe(await oneTimeCode(settings, 119))
  })
})

describe('oneTimeCodeSecondsLeft', () => {
  it('counts down from the period to 1', () => {
    const settings = parseOneTimeCodeUri(uriFor('SHA1'))
    expect(oneTimeCodeSecondsLeft(settings, 60)).toBe(30)
    expect(oneTimeCodeSecondsLeft(settings, 61)).toBe(29)
    expect(oneTimeCodeSecondsLeft(settings, 89)).toBe(1)
    expect(oneTimeCodeSecondsLeft(settings, 90.7)).toBe(30)
  })

  it('follows the period', () => {
    const settings = parseOneTimeCodeUri(`${uriFor('SHA1')}&period=45`)
    expect(oneTimeCodeSecondsLeft(settings, 100)).toBe(35)
  })
})

describe('parseOneTimeCodeUri', () => {
  it('reads a full address', () => {
    const settings = parseOneTimeCodeUri(uriFor('SHA256'))
    expect(settings).toMatchObject({
      algorithm: 'SHA256',
      digitCount: 8,
      periodSeconds: 30,
      issuer: 'Example',
      accountName: 'ada@example.com',
    })
    expectBytes(settings.secret, bytesFromText(secrets.SHA256))
  })

  it('defaults to SHA1, 6 digits and 30 seconds, and reads a lower case secret without padding', () => {
    const settings = parseOneTimeCodeUri(
      `otpauth://totp/ada?secret=${encodeBase32(bytesFromText(secrets.SHA1)).toLowerCase()}`,
    )
    expect(settings).toMatchObject({
      algorithm: 'SHA1',
      digitCount: 6,
      periodSeconds: 30,
      issuer: '',
      accountName: 'ada',
    })
    expectBytes(settings.secret, bytesFromText(secrets.SHA1))
  })

  it('drops padding at the end of the secret and white space around the address, however long the run', () => {
    const secretText = encodeBase32(bytesFromText(secrets.SHA1)).replace(/=+$/, '')
    const plain = parseOneTimeCodeUri(`otpauth://totp/ada?secret=${secretText}`)
    for (const padding of ['=', '======', '='.repeat(100_000)]) {
      expectBytes(parseOneTimeCodeUri(`otpauth://totp/ada?secret=${secretText}${padding}`).secret, plain.secret)
    }
    for (const white of [' ', '\t\n\r\f\v ', ' '.repeat(100_000)]) {
      expectBytes(parseOneTimeCodeUri(`${white}otpauth://totp/ada?secret=${secretText}${white}`).secret, plain.secret)
    }
    // Only ASCII white space is trimmed: a no-break space stays and is refused.
    expect(() => parseOneTimeCodeUri(`otpauth://totp/ada?secret=${secretText}\u00a0`)).toThrow()
    // An "=" inside the secret is not padding.
    expect(() =>
      parseOneTimeCodeUri(`otpauth://totp/ada?secret=${secretText.slice(0, 4)}=${secretText.slice(4)}`),
    ).toThrow('not base32')
  })

  it('refuses a long run of "=" and of spaces that does not end the text, in a short time', () => {
    const started = performance.now()
    expect(() => parseOneTimeCodeUri(`otpauth://totp/ada?secret=GEZDGNBV${'='.repeat(200_000)}x`)).toThrow()
    expect(() => parseOneTimeCodeUri(` ${' '.repeat(200_000)}x otpauth://totp/ada?secret=GEZDGNBV`)).toThrow()
    expect(performance.now() - started).toBeLessThan(2000)
  })

  it('takes the issuer from the label when the address names none, and decodes the label', () => {
    const settings = parseOneTimeCodeUri('otpauth://totp/Example%20Inc:ada%40example.test?secret=GEZDGNBV')
    expect(settings.issuer).toBe('Example Inc')
    expect(settings.accountName).toBe('ada@example.test')
  })

  it('refuses what it cannot make a code from', () => {
    expect(() => parseOneTimeCodeUri('not an address')).toThrow()
    expect(() => parseOneTimeCodeUri('https://example.com/?secret=GEZDGNBV')).toThrow(/otpauth/)
    expect(() => parseOneTimeCodeUri('otpauth://hotp/ada?secret=GEZDGNBV&counter=1')).toThrow(/time based/)
    expect(() => parseOneTimeCodeUri('otpauth://totp/ada')).toThrow(/no secret/)
    expect(() => parseOneTimeCodeUri('otpauth://totp/ada?secret=!!!')).toThrow()
    expect(() => parseOneTimeCodeUri('otpauth://totp/ada?secret=GEZDGNBV&algorithm=MD5')).toThrow(/algorithm/)
    expect(() => parseOneTimeCodeUri('otpauth://totp/ada?secret=GEZDGNBV&digits=7')).toThrow(/6 or 8/)
    expect(() => parseOneTimeCodeUri('otpauth://totp/ada?secret=GEZDGNBV&period=0')).toThrow(/period/)
    expect(() => parseOneTimeCodeUri('otpauth://totp/ada?secret=GEZDGNBV&period=x')).toThrow(/period/)
  })
})

// The cases in internal/passwords/testdata/onetimecode.json are read by the Go
// tests of the same reading (internal/passwords/onetimecode_test.go) and here.
// Both must give the same answers, so an address that is accepted in one place
// is accepted in the other, with the same secret, settings and codes.
interface SharedCases {
  unixSeconds: number[]
  cases: {
    name: string
    address: string
    isValid: boolean
    secretHex?: string
    algorithm?: string
    digitCount?: number
    periodSeconds?: number
    issuer?: string
    accountName?: string
    expectedCodes?: string[]
  }[]
}

const sharedCasesPath = resolve(import.meta.dirname, '../../../internal/passwords/testdata/onetimecode.json')

describe('the cases shared with the Go reading', () => {
  it('has the file', () => {
    expect(existsSync(sharedCasesPath)).toBe(true)
  })

  const shared = existsSync(sharedCasesPath) ? (JSON.parse(readFileSync(sharedCasesPath, 'utf8')) as SharedCases) : null

  it('reads every address the way Go does, or refuses it', async () => {
    expect(shared?.cases.length).toBeGreaterThan(50)
    for (const each of shared?.cases ?? []) {
      if (!each.isValid) {
        expect(() => parseOneTimeCodeUri(each.address), each.name).toThrow()
        continue
      }
      const settings = parseOneTimeCodeUri(each.address)
      expect(Array.from(settings.secret, (byte) => byte.toString(16).padStart(2, '0')).join(''), each.name).toBe(
        each.secretHex,
      )
      expect(settings.algorithm, each.name).toBe(each.algorithm)
      expect(settings.digitCount, each.name).toBe(each.digitCount)
      expect(settings.periodSeconds, each.name).toBe(each.periodSeconds)
      expect(settings.issuer, each.name).toBe(each.issuer)
      expect(settings.accountName, each.name).toBe(each.accountName)
      for (const [index, unixSeconds] of (shared?.unixSeconds ?? []).entries()) {
        expect(await oneTimeCode(settings, unixSeconds), `${each.name} at ${unixSeconds}`).toBe(
          each.expectedCodes?.[index],
        )
      }
    }
  })
})

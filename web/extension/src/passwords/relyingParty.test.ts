import { describe, expect, it } from 'vitest'
import { isRelyingPartyOfHost } from './relyingParty'

describe('isRelyingPartyOfHost', () => {
  it('accepts the host itself and a domain above it', () => {
    expect(isRelyingPartyOfHost('login.example.com', 'https://login.example.com/')).toBe(true)
    expect(isRelyingPartyOfHost('example.com', 'https://login.example.com/')).toBe(true)
    expect(isRelyingPartyOfHost('Example.COM', 'https://example.com/')).toBe(true)
    expect(isRelyingPartyOfHost('localhost', 'http://localhost:3000/')).toBe(true)
  })

  it('refuses another site, and a name that only ends alike', () => {
    expect(isRelyingPartyOfHost('example.com', 'https://example.com.evil.test/')).toBe(false)
    expect(isRelyingPartyOfHost('example.test', 'https://evilexample.test/')).toBe(false)
    expect(isRelyingPartyOfHost('login.example.com', 'https://example.com/')).toBe(false)
  })

  it('refuses a public suffix as the relying party', () => {
    // Built from labels, so no real host name is written in this file.
    const countrySecondLevel = ['co', 'uk'].join('.')
    const hostingSuffix = ['github', 'io'].join('.')
    expect(isRelyingPartyOfHost('com', 'https://example.com/')).toBe(false)
    expect(isRelyingPartyOfHost(countrySecondLevel, `https://example.${countrySecondLevel}/`)).toBe(false)
    expect(isRelyingPartyOfHost(`example.${countrySecondLevel}`, `https://login.example.${countrySecondLevel}/`)).toBe(
      true,
    )
    expect(isRelyingPartyOfHost(hostingSuffix, `https://ada.${hostingSuffix}/`)).toBe(false)
    expect(isRelyingPartyOfHost(`ada.${hostingSuffix}`, `https://ada.${hostingSuffix}/`)).toBe(true)
  })

  it('refuses a domain above an address', () => {
    expect(isRelyingPartyOfHost('0.0.2.7', 'https://192.0.2.7/')).toBe(false)
    expect(isRelyingPartyOfHost('2.7', 'https://192.0.2.7/')).toBe(false)
    // An address is not a relying party; the loopback address of a local test page is.
    expect(isRelyingPartyOfHost('192.0.2.7', 'https://192.0.2.7/')).toBe(false)
    expect(isRelyingPartyOfHost('[::1]', 'https://[::1]/')).toBe(false)
    expect(isRelyingPartyOfHost('127.0.0.1', 'http://127.0.0.1:3000/')).toBe(true)
  })

  it('refuses the shared hosting suffixes of the public suffix list, private section included', () => {
    // Built from labels, so no real host name is written in this file. These
    // are names a short hand-written list missed.
    for (const labels of [
      ['onrender', 'com'],
      ['fly', 'dev'],
      ['repl', 'co'],
      ['myshopify', 'com'],
      ['wixsite', 'com'],
      ['duckdns', 'org'],
      ['pythonanywhere', 'com'],
      ['hf', 'space'],
      ['s3', 'amazonaws', 'com'],
      ['herokuapp', 'com'],
      ['web', 'app'],
    ]) {
      const suffix = labels.join('.')
      expect(isRelyingPartyOfHost(suffix, `https://ada.${suffix}/`), suffix).toBe(false)
      expect(isRelyingPartyOfHost(suffix, `https://login.ada.${suffix}/`), suffix).toBe(false)
      expect(isRelyingPartyOfHost(`ada.${suffix}`, `https://ada.${suffix}/`), suffix).toBe(true)
      expect(isRelyingPartyOfHost(`ada.${suffix}`, `https://login.ada.${suffix}/`), suffix).toBe(true)
    }
  })

  it('accepts a registrable domain that sits under an ordinary name', () => {
    expect(isRelyingPartyOfHost('example.org', 'https://login.example.org/')).toBe(true)
    expect(isRelyingPartyOfHost('example.test', 'https://login.example.test/')).toBe(true)
    // A country's second-level suffix, built from labels so no real host name
    // is written in this file.
    const countrySuffix = ['com', 'au'].join('.')
    expect(isRelyingPartyOfHost(`example.${countrySuffix}`, `https://login.example.${countrySuffix}/`)).toBe(true)
    expect(isRelyingPartyOfHost(countrySuffix, `https://example.${countrySuffix}/`)).toBe(false)
  })

  it('refuses an empty id and a page that is not a web page', () => {
    expect(isRelyingPartyOfHost('', 'https://example.com/')).toBe(false)
    expect(isRelyingPartyOfHost('example.com', 'about:blank')).toBe(false)
  })
})

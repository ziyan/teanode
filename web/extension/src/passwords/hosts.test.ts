import { describe, expect, it } from 'vitest'
import {
  doesHostMatch,
  doesSiteMatchPage,
  doSitesMatchPage,
  effectiveDomain,
  isRelyingPartyOfHost,
  isSecureContextPage,
  isWebPage,
  normalizeHost,
} from './hosts'

describe('normalizeHost', () => {
  it('lower-cases, drops a leading www., the port, the path and the user', () => {
    expect(normalizeHost('https://WWW.Example.com:8443/sign-in?next=/x#top')).toBe('example.com')
    expect(normalizeHost('https://ada:secret@example.com/')).toBe('example.com')
    expect(normalizeHost('example.com/sign-in')).toBe('example.com')
    expect(normalizeHost('//example.com')).toBe('example.com')
    expect(normalizeHost('https://example.com.')).toBe('example.com')
  })

  it('keeps www. when nothing is left of the name without it', () => {
    expect(normalizeHost('https://www.test')).toBe('www.test')
  })

  it('keeps an address and a bracketed address whole', () => {
    expect(normalizeHost('http://192.0.2.7:8080/x')).toBe('192.0.2.7')
    expect(normalizeHost('http://[2001:db8::1]:3000/')).toBe('[2001:db8::1]')
  })

  it('is empty for what is not a web address', () => {
    expect(normalizeHost('ftp://example.com')).toBe('')
    expect(normalizeHost('chrome-extension://abc/page.html')).toBe('')
    expect(normalizeHost('')).toBe('')
    expect(normalizeHost('https://exa\tmple.example')).toBe('')
    expect(normalizeHost('https://example.com:abc')).toBe('')
  })
})

describe('doesSiteMatchPage', () => {
  it('matches the same host, www. ignored in either', () => {
    expect(doesSiteMatchPage('example.com', 'https://www.example.com/login')).toBe(true)
    expect(doesSiteMatchPage('https://www.example.com', 'https://example.com/')).toBe(true)
  })

  it('does not match a subdomain, a parent, or a name that only ends alike', () => {
    expect(doesSiteMatchPage('example.com', 'https://login.example.com/')).toBe(false)
    expect(doesSiteMatchPage('login.example.com', 'https://example.com/')).toBe(false)
    expect(doesSiteMatchPage('example.com', 'https://example.com.evil.test/')).toBe(false)
    expect(doesSiteMatchPage('example.test', 'https://evilexample.test/')).toBe(false)
  })

  it('never matches a page on http unless the saved site is http', () => {
    expect(doesSiteMatchPage('example.com', 'http://example.com/')).toBe(false)
    expect(doesSiteMatchPage('https://example.com', 'http://example.com/')).toBe(false)
    expect(doesSiteMatchPage('http://example.com', 'http://example.com/')).toBe(true)
    // A secure page may use a login saved for the plain address.
    expect(doesSiteMatchPage('http://example.com', 'https://example.com/')).toBe(true)
  })

  it('matches nothing on a page that is not a web page', () => {
    expect(doesSiteMatchPage('example.com', 'about:blank')).toBe(false)
    expect(doesSiteMatchPage('example.com', 'file:///srv/pages/sign-in.html')).toBe(false)
    expect(doesSiteMatchPage('example.com', 'chrome-extension://abc/passwords.html')).toBe(false)
  })

  it('reads a list of sites', () => {
    expect(doSitesMatchPage(['other.test', 'example.com'], 'https://example.com/')).toBe(true)
    expect(doSitesMatchPage([], 'https://example.com/')).toBe(false)
  })
})

describe('doesHostMatch', () => {
  it('compares a tab address with the host the server named', () => {
    expect(doesHostMatch('https://www.example.com/a', 'example.com')).toBe(true)
    expect(doesHostMatch('https://example.com/a', 'WWW.Example.com')).toBe(true)
    expect(doesHostMatch('https://evil.test/a', 'example.com')).toBe(false)
    expect(doesHostMatch('about:blank', 'example.com')).toBe(false)
    expect(doesHostMatch('https://example.com/', '')).toBe(false)
  })
})

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
    expect(isRelyingPartyOfHost('192.0.2.7', 'https://192.0.2.7/')).toBe(true)
  })

  it('refuses an empty id and a page that is not a web page', () => {
    expect(isRelyingPartyOfHost('', 'https://example.com/')).toBe(false)
    expect(isRelyingPartyOfHost('example.com', 'about:blank')).toBe(false)
  })
})

describe('isSecureContextPage and effectiveDomain', () => {
  it('allows https and local development on http', () => {
    expect(isSecureContextPage('https://example.com/')).toBe(true)
    expect(isSecureContextPage('http://localhost:3000/')).toBe(true)
    expect(isSecureContextPage('http://127.0.0.1/')).toBe(true)
    expect(isSecureContextPage('http://example.com/')).toBe(false)
    expect(isSecureContextPage('about:blank')).toBe(false)
  })

  it('keeps www. in the default relying party id', () => {
    expect(effectiveDomain('https://www.example.com:8443/x')).toBe('www.example.com')
    expect(isWebPage('https://www.example.com/')).toBe(true)
  })
})

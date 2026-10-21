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
    // A login saved for the plain address is on the plain port, so it is
    // not the secure page's origin either.
    expect(doesSiteMatchPage('http://example.com', 'https://example.com/')).toBe(false)
  })

  it('matches the port too: a named port only that port, none only the default of the site scheme', () => {
    expect(doesSiteMatchPage('https://example.com:8443', 'https://example.com:8443/x')).toBe(true)
    expect(doesSiteMatchPage('https://example.com:8443', 'https://example.com/')).toBe(false)
    expect(doesSiteMatchPage('https://example.com:8443', 'https://example.com:9443/')).toBe(false)
    expect(doesSiteMatchPage('example.com', 'https://example.com:8443/')).toBe(false)
    expect(doesSiteMatchPage('example.com', 'https://example.com:443/')).toBe(true)
    expect(doesSiteMatchPage('https://example.com:443', 'https://example.com/')).toBe(true)
    expect(doesSiteMatchPage('http://localhost:8080', 'http://localhost:8080/')).toBe(true)
    expect(doesSiteMatchPage('http://localhost:8080', 'http://localhost:9090/')).toBe(false)
    expect(doesSiteMatchPage('localhost:8080', 'http://localhost:8080/')).toBe(false)
  })

  it('reads a site with a scheme the way the browser reads an address, and refuses what parsers disagree on', () => {
    // The browser reads a backslash as a slash; a text split would read a user name.
    expect(doesSiteMatchPage('https://example.com\\@evil.test/', 'https://evil.test/')).toBe(false)
    expect(doesSiteMatchPage('https://example.com\\@evil.test/', 'https://example.com/')).toBe(false)
    expect(normalizeHost('https://example.com\\@evil.test/')).toBe('')
    expect(normalizeHost('https://exa\u0000mple.test/')).toBe('')
    expect(normalizeHost('https://example.test/\nHost: evil.test')).toBe('')
    expect(normalizeHost('https://example.com@evil.test/')).toBe('evil.test')
    expect(normalizeHost('https://example.test:0/')).toBe('')
    expect(normalizeHost('https://example.test:99999/')).toBe('')
    expect(normalizeHost('javascript:alert(1)')).toBe('')
    expect(normalizeHost('about:blank')).toBe('')
    expect(normalizeHost('localhost:8080')).toBe('localhost')
  })

  it('adds https:// to a site typed as a bare host', () => {
    expect(doesSiteMatchPage('example.com/sign-in?x=1', 'https://example.com/')).toBe(true)
    expect(doesSiteMatchPage('//example.com', 'https://example.com/')).toBe(true)
    expect(doesSiteMatchPage('example.com', 'http://example.com/')).toBe(false)
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
    // An address is not a relying party; the loopback address of a local test page is.
    expect(isRelyingPartyOfHost('192.0.2.7', 'https://192.0.2.7/')).toBe(false)
    expect(isRelyingPartyOfHost('[::1]', 'https://[::1]/')).toBe(false)
    expect(isRelyingPartyOfHost('127.0.0.1', 'http://127.0.0.1:3000/')).toBe(true)
  })

  it('refuses the shared hosting suffixes a short list would miss', () => {
    // Built from labels, so no real host name is written in this file.
    for (const labels of [
      ['onrender', 'com'],
      ['fly', 'dev'],
      ['repl', 'co'],
      ['glitch', 'me'],
      ['myshopify', 'com'],
      ['wixsite', 'com'],
    ]) {
      const suffix = labels.join('.')
      expect(isRelyingPartyOfHost(suffix, `https://ada.${suffix}/`), suffix).toBe(false)
      expect(isRelyingPartyOfHost(`ada.${suffix}`, `https://ada.${suffix}/`), suffix).toBe(true)
      expect(isRelyingPartyOfHost(`ada.${suffix}`, `https://login.ada.${suffix}/`), suffix).toBe(true)
    }
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

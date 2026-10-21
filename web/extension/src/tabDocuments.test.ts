import { describe, expect, it } from 'vitest'
import { ScriptedOrigins, TabDocuments, originOf, withoutRequestBodies } from './tabDocuments'

describe('TabDocuments', () => {
  it('reports a main frame commit with its document, address, kind and count', () => {
    const documents = new TabDocuments()
    expect(
      documents.committed({ tabId: 7, frameId: 0, documentId: 'doc-1', url: 'https://shop.example.test/a' }, true),
    ).toEqual({
      tab: 7,
      documentId: 'doc-1',
      url: 'https://shop.example.test/a',
      isBackForward: false,
      count: 1,
    })
    const back = documents.committed(
      {
        tabId: 7,
        frameId: 0,
        documentId: 'doc-2',
        url: 'https://shop.example.test/b',
        transitionQualifiers: ['forward_back', 'from_address_bar'],
      },
      true,
    )
    expect(back).toMatchObject({ isBackForward: true, count: 2 })
    expect(documents.takeReports().map((report) => report.count)).toEqual([1, 2])
    expect(documents.takeReports()).toEqual([])
  })

  it('reports nothing for a frame inside the page or for a tab the agent cannot act in', () => {
    const documents = new TabDocuments()
    expect(
      documents.committed({ tabId: 7, frameId: 3, documentId: 'frame', url: 'https://ads.example.test/' }, true),
    ).toBeNull()
    expect(
      documents.committed({ tabId: 8, frameId: 0, documentId: 'other', url: 'https://bank.example.test/' }, false),
    ).toBeNull()
    expect(documents.takeReports()).toEqual([])
    // Still counted, so that a tab listed later keeps numbering in order.
    expect(documents.countOf(8)).toBe(1)
    expect(documents.countOf(7)).toBe(0)
  })

  it('knows a tab shows a filled document until it commits another, and again when it comes back', () => {
    const documents = new TabDocuments()
    documents.committed({ tabId: 7, frameId: 0, documentId: 'doc-1', url: 'https://shop.example.test/sign-in' }, true)
    expect(documents.isShowingFilledDocument(7)).toBe(false)
    documents.markFilled(7, 'doc-1')
    expect(documents.isShowingFilledDocument(7)).toBe(true)
    // Another tab is not the filled one.
    expect(documents.isShowingFilledDocument(8)).toBe(false)
    documents.committed({ tabId: 7, frameId: 0, documentId: 'doc-2', url: 'https://shop.example.test/account' }, true)
    expect(documents.isShowingFilledDocument(7)).toBe(false)
    // The history's cache brings the same document back.
    documents.committed(
      {
        tabId: 7,
        frameId: 0,
        documentId: 'doc-1',
        url: 'https://shop.example.test/sign-in',
        transitionQualifiers: ['forward_back'],
      },
      true,
    )
    expect(documents.isShowingFilledDocument(7)).toBe(true)
  })

  it('does not take a fill without a document for a filled document', () => {
    const documents = new TabDocuments()
    documents.markFilled(7, undefined)
    documents.markFilled(7, '')
    expect(documents.isShowingFilledDocument(7)).toBe(false)
  })

  it('keeps the newest reports when the server cannot be told', () => {
    const documents = new TabDocuments()
    for (let index = 0; index < 150; index++) {
      documents.committed({ tabId: 7, frameId: 0, documentId: `doc-${index}`, url: 'https://shop.example.test/' }, true)
    }
    const reports = documents.takeReports()
    expect(reports).toHaveLength(100)
    expect(reports[0].count).toBe(51)
    expect(reports[99].count).toBe(150)
  })

  it('forgets a tab', () => {
    const documents = new TabDocuments()
    documents.committed({ tabId: 7, frameId: 0, documentId: 'doc-1', url: 'https://shop.example.test/' }, true)
    documents.markFilled(7, 'doc-1')
    documents.forget(7)
    expect(documents.isShowingFilledDocument(7)).toBe(false)
    expect(documents.countOf(7)).toBe(0)
  })
})

describe('withoutRequestBodies', () => {
  it('takes the body and its marker out of a request event, and nothing else', () => {
    const event = {
      requestId: '1',
      request: {
        url: 'https://shop.example.test/login',
        method: 'POST',
        postData: 'user=ada&password=hunter2',
        hasPostData: true,
        postDataEntries: [{ bytes: 'abc' }],
      },
    }
    const cleaned = withoutRequestBodies(event)
    expect(cleaned).toEqual({ requestId: '1', request: { url: 'https://shop.example.test/login', method: 'POST' } })
    expect(JSON.stringify(cleaned)).not.toContain('hunter2')
    // The event that came in is not changed.
    expect(event.request.postData).toBe('user=ada&password=hunter2')
  })

  it('leaves an event without a request alone', () => {
    const event = { timestamp: 3 }
    expect(withoutRequestBodies(event)).toBe(event)
    expect(withoutRequestBodies(undefined)).toBeUndefined()
  })
})

describe('TabDocuments between runs of the worker', () => {
  it('keeps and restores the count, the current document and the filled ones', () => {
    const before = new TabDocuments()
    before.committed({ tabId: 7, frameId: 0, documentId: 'doc-1', url: 'https://shop.example.test/sign-in' }, true)
    before.markFilled(7, 'doc-1')
    const kept = JSON.parse(JSON.stringify(before.keep()))

    const after = new TabDocuments()
    after.restore(kept)
    expect(after.isShowingFilledDocument(7)).toBe(true)
    expect(after.countOf(7)).toBe(1)
    // The next commit goes on counting, and leaves the filled document.
    after.committed({ tabId: 7, frameId: 0, documentId: 'doc-2', url: 'https://shop.example.test/account' }, true)
    expect(after.countOf(7)).toBe(2)
    expect(after.isShowingFilledDocument(7)).toBe(false)
  })

  it('keeps no address or value, and leaves out what is not in the shape it makes', () => {
    const documents = new TabDocuments()
    documents.committed({ tabId: 7, frameId: 0, documentId: 'doc-1', url: 'https://shop.example.test/secret' }, true)
    expect(JSON.stringify(documents.keep())).not.toContain('shop.example.test')
    const restored = new TabDocuments()
    restored.restore({ counts: 'x', current: [['seven', 'doc']], filled: [[7, [1, 'doc-1']]] })
    restored.restore(null)
    expect(restored.countOf(7)).toBe(0)
    expect(restored.isShowingFilledDocument(7)).toBe(false)
  })

  it('does not overwrite what this run already knows with the older saved record', () => {
    const documents = new TabDocuments()
    documents.committed({ tabId: 7, frameId: 0, documentId: 'doc-9', url: 'https://shop.example.test/' }, true)
    documents.restore({ counts: [[7, 1]], current: [[7, 'doc-1']], filled: [[7, ['doc-1']]] })
    expect(documents.isShowingFilledDocument(7)).toBe(false)
  })
})

describe('originOf', () => {
  it('writes a site the one way, and nothing for an address that is not a web page', () => {
    expect(originOf('https://Shop.Example.test/a?b#c')).toBe('https://shop.example.test')
    expect(originOf('https://www.shop.example.test:443/')).toBe('https://shop.example.test')
    expect(originOf('http://shop.example.test:80/')).toBe('http://shop.example.test')
    expect(originOf('https://shop.example.test:8443/')).toBe('https://shop.example.test:8443')
    // A bare "www" is a host, not a prefix.
    expect(originOf('https://www/')).toBe('https://www')
    for (const address of [
      'about:blank',
      'javascript:1',
      'data:text/html,x',
      'chrome://settings',
      '',
      'not an address',
      undefined,
      7,
    ]) {
      expect(originOf(address), String(address)).toBe('')
    }
  })
})

describe('ScriptedOrigins', () => {
  it('keeps sites, not pages, tells what is news, and forgets all at once', () => {
    const scripted = new ScriptedOrigins()
    expect(scripted.add('https://shop.example.test/sign-in')).toBe(true)
    expect(scripted.add('https://www.shop.example.test/account')).toBe(false)
    expect(scripted.add('about:blank')).toBe(false)
    expect(scripted.has('https://shop.example.test/anything')).toBe(true)
    expect(scripted.has('https://shop.example.test:8443/')).toBe(false)
    expect(scripted.has('https://bank.example.test/')).toBe(false)
    expect(scripted.list()).toEqual(['https://shop.example.test'])
    scripted.clear()
    expect(scripted.list()).toEqual([])
  })

  it('reads back a stored list and ignores what is not an origin', () => {
    const scripted = new ScriptedOrigins()
    scripted.replace(['https://b.example.test', 7, null, 'nonsense', 'https://a.example.test'])
    expect(scripted.list()).toEqual(['https://a.example.test', 'https://b.example.test'])
    scripted.replace('not a list')
    expect(scripted.list()).toEqual([])
  })
})

describe('TabDocuments.originsSince', () => {
  it('names the sites a tab committed after a count, for the tabs the agent can act in', () => {
    const documents = new TabDocuments()
    documents.committed({ tabId: 7, frameId: 0, documentId: 'a', url: 'https://one.example.test/' }, true)
    documents.committed({ tabId: 7, frameId: 0, documentId: 'b', url: 'https://two.example.test/x' }, true)
    documents.committed({ tabId: 7, frameId: 0, documentId: 'c', url: 'about:blank' }, true)
    documents.committed({ tabId: 8, frameId: 0, documentId: 'd', url: 'https://other.example.test/' }, false)
    expect(documents.originsSince(7, 1)).toEqual(['https://two.example.test'])
    expect(documents.originsSince(7, 0)).toEqual(['https://one.example.test', 'https://two.example.test'])
    expect(documents.originsSince(7, Number.POSITIVE_INFINITY)).toEqual([])
    expect(documents.originsSince(8, 0)).toEqual([])
    documents.forget(7)
    expect(documents.originsSince(7, 0)).toEqual([])
  })
})

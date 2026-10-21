import { describe, expect, it } from 'vitest'
import { TabDocuments, withoutRequestBodies } from './tabDocuments'

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

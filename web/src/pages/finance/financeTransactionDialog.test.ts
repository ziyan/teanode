import { expect, it } from 'vitest'

import { providerMetadataText } from './financeTransactionDialog'

it('lays out the provider object as indented JSON', () => {
  expect(providerMetadataText({ name: 'Corner Shop', amount: 4.5 })).toBe(
    '{\n  "name": "Corner Shop",\n  "amount": 4.5\n}',
  )
  expect(providerMetadataText('{"name":"Corner Shop"}')).toBe('{\n  "name": "Corner Shop"\n}')
})

// Markup in what the provider wrote stays text: the dialog shows this
// string in a pre, never as HTML.
it('keeps markup as text and says nothing for nothing', () => {
  expect(providerMetadataText({ name: '<b>bold</b>' })).toContain('"<b>bold</b>"')
  expect(providerMetadataText('not json <i>')).toBe('not json <i>')
  expect(providerMetadataText(null)).toBe('')
  expect(providerMetadataText(undefined)).toBe('')
})

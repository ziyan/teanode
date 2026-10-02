import { cleanup, render, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, expect, it, vi } from 'vitest'

import { Column, DataTable, Range } from './dataTable'

vi.mock('../i18n/i18n', () => ({
  useTranslation: () => ({
    t: (key: string, values?: Record<string, unknown>) => (values ? `${key} ${JSON.stringify(values)}` : key),
    plural: (count: number, forms: { one: string; other: string }) => `${count === 1 ? forms.one : forms.other} ${count}`,
    language: 'en',
  }),
}))

type Row = { id: string; label: string; size: number }

// An invented list: the label cannot be sorted by, the size can.
const columns: Column<Row>[] = [
  { key: 'label', header: 'Label', value: (row) => row.label },
  { key: 'size', header: 'Size', value: (row) => String(row.size), sort: (first, second) => first.size - second.size },
]

afterEach(() => {
  cleanup()
  window.sessionStorage.clear()
})

// renderRemembering draws a table the server pages, at a path whose
// remembered order is the one given, and says the ranges it asked for.
function renderRemembering(order: Range['order']) {
  window.sessionStorage.setItem('teanode.table:/invented-list', JSON.stringify({ order }))
  const onRange = vi.fn()
  render(
    <MemoryRouter initialEntries={['/invented-list']}>
      <DataTable
        columns={columns}
        rows={[{ id: 'row-one', label: 'Invented row', size: 3 }]}
        rowKey={(row) => row.id}
        emptyMessage="nothing"
        countLabel={(count) => String(count)}
        remote={{ total: 1, onRange }}
      />
    </MemoryRouter>,
  )
  return onRange
}

// A remembered order outlives the column it named: one that is no longer
// sortable is dropped rather than sent to the server, and one that still
// is, is kept.
it('drops a remembered order whose column cannot be sorted by', async () => {
  const onRange = renderRemembering({ key: 'label', direction: 'descending' })
  await waitFor(() => expect(onRange).toHaveBeenCalled())
  expect(onRange.mock.calls.every(([range]) => range.order === null)).toBe(true)
  expect(JSON.parse(window.sessionStorage.getItem('teanode.table:/invented-list') ?? '{}').order).toBeNull()
})

it('keeps a remembered order whose column can still be sorted by', async () => {
  const onRange = renderRemembering({ key: 'size', direction: 'descending' })
  await waitFor(() => expect(onRange).toHaveBeenCalled())
  expect(onRange.mock.calls[0][0].order).toEqual({ key: 'size', direction: 'descending' })
})

it('drops a remembered order whose column is gone', async () => {
  const onRange = renderRemembering({ key: 'removed-column', direction: 'ascending' })
  await waitFor(() => expect(onRange).toHaveBeenCalled())
  expect(onRange.mock.calls[0][0].order).toBeNull()
})

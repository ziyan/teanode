import { cleanup, render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, expect, it, vi } from 'vitest'

import { graphql } from '../api'
import { ServerStoragePage, formatStorageBytes } from './serverStorage'

vi.mock('../api', () => ({ graphql: vi.fn() }))
vi.mock('../i18n/i18n', () => ({
  useTranslation: () => ({
    t: (key: string, values?: Record<string, string>) => (values ? `${key} ${Object.values(values).join(' ')}` : key),
    plural: (count: number) => String(count),
    language: 'en',
  }),
}))
vi.mock('../components/seriesChart', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../components/seriesChart')>()),
  SeriesChart: ({ series }: { series: { id: string }[] }) => (
    <div data-testid="chart">{series.map((one) => one.id).join(',')}</div>
  ),
}))
const execute = vi.mocked(graphql)
afterEach(cleanup)

const gigabyte = 1024 ** 3

const growth = {
  growthSampleCount: 12,
  isGrowthProjectable: true,
  growthBytesPerDay: gigabyte,
  currentSizeBytes: 100 * gigabyte,
  projectedSize30DaysBytes: 130 * gigabyte,
  projectedSize90DaysBytes: 190 * gigabyte,
  projectedSize365DaysBytes: 465 * gigabyte,
  areFilesCounted: true,
}

function answer(overrides: object = {}) {
  return {
    StorageUsage: {
      breakdownBy: 'area',
      databaseSizeBytes: 60 * gigabyte,
      fileSizeBytes: 40 * gigabyte,
      totalSizeBytes: 100 * gigabyte,
      usageRows: [
        { rowKey: 'search', rowLabel: 'search', rowSizeBytes: 30 * gigabyte, rowTableCount: 7, rowFileSizes: [] },
        { rowKey: 'mail', rowLabel: 'mail', rowSizeBytes: 5 * gigabyte, rowTableCount: 18, rowFileSizes: [] },
      ],
      historyDays: [
        {
          sampledOn: '2030-05-01',
          databaseSizeBytes: 59 * gigabyte,
          fileSizeBytes: 40 * gigabyte,
          totalSizeBytes: 99 * gigabyte,
          breakdownSizes: [
            { sizeKey: 'search', sizeBytes: 30 * gigabyte },
            { sizeKey: 'mail', sizeBytes: 5 * gigabyte },
          ],
        },
      ],
      disks: [
        {
          diskLabel: 'dataDirectory',
          isDiskMeasured: true,
          diskTotalBytes: 200 * gigabyte,
          diskFreeBytes: 20 * gigabyte,
          diskUsedBytes: 180 * gigabyte,
          projectedFullOn: '2030-05-21',
          daysUntilFull: 20,
          isProjectedFull: true,
        },
        { diskLabel: 'objectStore', isDiskMeasured: false, isProjectedFull: false },
      ],
      growth,
      ...overrides,
    },
  }
}

function renderPage(address = '/manage/server/storage') {
  render(
    <MemoryRouter initialEntries={[address]}>
      <Routes>
        <Route path="/manage/server/:tab" element={<ServerStoragePage />} />
      </Routes>
    </MemoryRouter>,
  )
}

it('warns about a disk that fills within thirty days and says an object store is not measured', async () => {
  execute.mockResolvedValue(answer())
  renderPage()
  expect(await screen.findByRole('alert')).toBeTruthy()
  expect(screen.getByRole('alert').textContent).toContain('2030-05-21')
  expect(screen.getByText('storage.notMeasured')).toBeTruthy()
  expect(screen.getByText('storage.fullOn 2030-05-21')).toBeTruthy()
})

it('shows no banner when no disk is projected to fill soon', async () => {
  const quiet = answer()
  quiet.StorageUsage.disks[0].isProjectedFull = false
  execute.mockResolvedValue(quiet)
  renderPage()
  expect(await screen.findByText('storage.area.search')).toBeTruthy()
  expect(screen.queryByRole('alert')).toBeNull()
})

it('takes the breakdown from the address and asks the server for it', async () => {
  execute.mockResolvedValue(
    answer({
      breakdownBy: 'person',
      usageRows: [
        {
          rowKey: 'person-one',
          rowLabel: 'Alice Example',
          rowSizeBytes: 3 * gigabyte,
          rowTableCount: 0,
          rowFileSizes: [{ sizeKey: 'mail', sizeBytes: 3 * gigabyte }],
        },
        { rowKey: '', rowLabel: '', rowSizeBytes: gigabyte, rowTableCount: 0, rowFileSizes: [] },
      ],
    }),
  )
  renderPage('/manage/server/storage?by=person')
  expect(await screen.findByText('Alice Example')).toBeTruthy()
  expect(screen.getByText('storage.unowned')).toBeTruthy()
  expect(execute.mock.calls[0][1]).toMatchObject({ by: 'person' })
  expect(screen.getByTestId('chart').textContent).toBe('database,files')
})

it('draws the largest areas apart and the rest together', async () => {
  const sizes = ['a', 'b', 'c', 'd', 'e', 'f', 'g'].map((key, index) => ({
    sizeKey: key,
    sizeBytes: (10 - index) * gigabyte,
  }))
  const many = answer()
  many.StorageUsage.historyDays[0].breakdownSizes = sizes
  execute.mockResolvedValue(many)
  renderPage()
  expect((await screen.findByTestId('chart')).textContent).toBe('a,b,c,d,e,rest')
})

it('writes sizes in 1024-based units', () => {
  expect(formatStorageBytes(512)).toBe('512 B')
  expect(formatStorageBytes(1536 * 1024, 1)).toBe('1.5 MB')
  expect(formatStorageBytes(3 * gigabyte * 1024)).toBe('3.0 TB')
})

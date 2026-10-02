import { useEffect } from 'react'
import { useSearchParams } from 'react-router-dom'

import { useTranslation } from '../i18n/i18n'
import { ChevronRightIcon } from './icons'
import { Select } from './select'
import { Tooltip } from './tooltip'

// Every list that is longer than a screen pages: a range said in words,
// the way to the page before and the page after, and for a table the
// number of rows on a page. Asking for more rows under the ones shown
// grew the page without end, lost the place on a reload, and gave Back
// nothing to go back to, so the page is in the address instead.

export const PAGE_SIZES = [25, 50, 100, 200]

// usePageInAddress is which page of a list is shown, and how many rows a
// page holds, read from the address and written back to it. The page is
// counted from one in the address, for a person to read, and from zero
// here. Two lists on one screen name their pages apart, by
// pageParameter. Writing one leaves the rest of the address alone, since
// the filters that narrowed the list live there too.
export function usePageInAddress({
  pageParameter = 'page',
  pageSizeParameter = 'rows',
  defaultPageSize = 50,
}: { pageParameter?: string; pageSizeParameter?: string; defaultPageSize?: number } = {}) {
  const [parameters, setParameters] = useSearchParams()
  const askedPageSize = Number(parameters.get(pageSizeParameter))
  const pageSize = PAGE_SIZES.includes(askedPageSize) ? askedPageSize : defaultPageSize
  const pageIndex = Math.max(0, (Number(parameters.get(pageParameter)) || 1) - 1)
  const setPageIndex = (nextPageIndex: number, options: { replace?: boolean } = {}) =>
    setParameters(
      (previous) => {
        const written = new URLSearchParams(previous)
        if (nextPageIndex <= 0) {
          written.delete(pageParameter)
        } else {
          written.set(pageParameter, String(nextPageIndex + 1))
        }
        return written
      },
      { replace: options.replace },
    )
  const setPageSize = (nextPageSize: number) =>
    setParameters((previous) => {
      const written = new URLSearchParams(previous)
      written.set(pageSizeParameter, String(nextPageSize))
      // A different page size means different pages; page four of fifty
      // is not page four of two hundred.
      written.delete(pageParameter)
      return written
    })
  return { pageIndex, pageSize, offset: pageIndex * pageSize, setPageIndex, setPageSize }
}

// useKeepPageInRange goes back to the last page when the one in the
// address is past the end: a link to page nine of a list that has since
// shrunk, or the last row of the last page acted on. Replacing rather
// than adding a step, since nobody chose the page being left.
export function useKeepPageInRange(
  pageIndex: number,
  pageSize: number,
  totalCount: number | null,
  setPageIndex: (pageIndex: number, options?: { replace?: boolean }) => void,
) {
  const lastPageIndex = totalCount === null ? null : Math.max(0, Math.ceil(totalCount / pageSize) - 1)
  useEffect(() => {
    if (lastPageIndex !== null && pageIndex > lastPageIndex) {
      setPageIndex(lastPageIndex, { replace: true })
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [pageIndex, lastPageIndex])
}

// Pager is the range of a list shown and the way to its other pages:
// "51 to 100 of 1,234", or "of at least 300" where the server stopped
// counting, the previous and next buttons, and how many rows a page
// holds where the list lets that be chosen. summary stands in for the
// range where a page holds more than one kind of row and a single range
// would not be true of both; hasNextPage then says whether there is a
// page after this one, which the range would otherwise have said.
export function Pager({
  pageIndex,
  pageSize,
  shownCount,
  totalCount,
  isTotalCountLowerBound = false,
  summary,
  hasNextPage,
  isLoading = false,
  onPageIndex,
  onPageSize,
}: {
  pageIndex: number
  pageSize: number
  shownCount: number
  totalCount: number
  isTotalCountLowerBound?: boolean
  summary?: React.ReactNode
  hasNextPage?: boolean
  isLoading?: boolean
  onPageIndex: (pageIndex: number) => void
  onPageSize?: (pageSize: number) => void
}) {
  const { t, language } = useTranslation()
  const offset = pageIndex * pageSize
  const formatted = (count: number) => count.toLocaleString(language)
  const range = {
    first: formatted(shownCount > 0 ? offset + 1 : Math.min(offset, totalCount)),
    last: formatted(offset + shownCount),
    total: formatted(totalCount),
  }
  const canGoToNextPage = hasNextPage ?? offset + shownCount < totalCount
  return (
    <span className="table-pagination">
      {/* Part of the list's furniture rather than a field of a form, so
          it is drawn the way the rest of the page is: a native select
          opens its list in the operating system's colors, which on a dark
          page is a white rectangle. */}
      {onPageSize ? (
        <span className="table-rows">
          <span className="muted">{t('table.rowsPerPage')}</span>
          <Select
            className="select-number"
            label={t('table.rowsPerPage')}
            value={String(pageSize)}
            options={PAGE_SIZES.map((size) => ({ value: String(size), label: String(size) }))}
            onChange={(value) => onPageSize(Number(value))}
          />
        </span>
      ) : null}

      <span className="muted" aria-live="polite">
        {isLoading
          ? t('common.loading')
          : (summary ?? (isTotalCountLowerBound ? t('pager.rangeAtLeast', range) : t('table.range', range)))}
      </span>

      <Tooltip label={t('table.previous')}>
        <button
          type="button"
          className="icon-button"
          aria-label={t('table.previous')}
          disabled={pageIndex === 0 || isLoading}
          onClick={() => onPageIndex(pageIndex - 1)}
        >
          <span className="flip">
            <ChevronRightIcon size={16} />
          </span>
        </button>
      </Tooltip>
      <Tooltip label={t('table.next')}>
        <button
          type="button"
          className="icon-button"
          aria-label={t('table.next')}
          disabled={!canGoToNextPage || isLoading}
          onClick={() => onPageIndex(pageIndex + 1)}
        >
          <ChevronRightIcon size={16} />
        </button>
      </Tooltip>
    </span>
  )
}

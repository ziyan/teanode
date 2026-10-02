import { useEffect, useMemo, useRef, useState } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'

import { Key, useTranslation } from '../i18n/i18n'
import { CloseIcon, FilterIcon, SortIcon } from './icons'
import { MultiSelectFilter, Option, TextFilter, matchesSelection, matchesText } from './filters'
import { PAGE_SIZES, Pager, usePageInAddress } from './pager'
import { Tooltip } from './tooltip'

// One table, used by every list. The mail list and the queue had the same
// header row, the same filter row, the same "nothing matches" and the same
// count written twice, which is two places for them to drift apart.
//
// Follows the reference's table: small uppercase headers, a filter row under
// them, alternating rows, and a pagination bar underneath.
//
// A list remembers where you were. Opening a row and coming back used to
// land on page one, at fifty rows, scrolled to the top — the three things
// you had just set — because all three lived in this component's state and
// the component was gone. They are kept per list path in sessionStorage now:
// per tab, for this visit, and forgotten with it, which is exactly how long
// "where I was" should last.

type Remembered = {
  pageSize?: number
  page?: number
  order?: Sort | null
  scroll?: number
}

function rememberedKey(path: string): string {
  return 'teanode.table:' + path
}

function readRemembered(path: string): Remembered {
  try {
    const raw = window.sessionStorage.getItem(rememberedKey(path))
    return raw ? (JSON.parse(raw) as Remembered) : {}
  } catch {
    return {}
  }
}

function writeRemembered(path: string, patch: Remembered): void {
  try {
    window.sessionStorage.setItem(rememberedKey(path), JSON.stringify({ ...readRemembered(path), ...patch }))
  } catch {
    // A browser that refuses storage still gets a working list.
  }
}

// The page scrolls inside .content, not the window, so that is what has to
// be measured and restored.
function scroller(): HTMLElement | null {
  return document.querySelector('.content')
}

export type Column<Row> = {
  // Identifies the column, and keys its filter state.
  key: string

  header: string

  // Fixed width, for columns whose content has a known size — a timestamp, a
  // status. Everything else shares what is left.
  width?: string

  // Dropped on a narrow screen. For columns that are context rather than the
  // answer somebody came for.
  optional?: boolean

  // One line with an ellipsis rather than a cell that grows the row. For
  // subjects and error messages, which are sentences.
  truncate?: boolean

  // Right-aligned in a tabular face, header and cells alike, so amounts
  // read down the column by their decimal places.
  numeric?: boolean

  // The value the filters and the sort see. Kept apart from render so a cell
  // can show a tag or a link while still being filtered as text.
  value?: (row: Row) => string | undefined

  render?: (row: Row) => React.ReactNode

  // How this column can be narrowed. "select" is a searchable multiselect,
  // whose options are the values present unless the caller supplies them.
  filter?: 'text' | 'select'

  // Options for a select filter, when the useful set is not the set present:
  // a domain with no mail yet is still a sensible thing to ask about.
  options?: Option[]

  // Whether the column can be ordered by, and how to compare two rows when
  // it is. Omitted means the column is not sortable — an actions column has
  // no order.
  sort?: (first: Row, second: Row) => number
}

export type Sort = { key: string; direction: 'ascending' | 'descending' }

// Remote is a list the server pages: the rows given are one page of it,
// total is how long the whole list is, and onRange is told which page,
// how many rows, which filters and which order to fetch whenever any of
// them change. The table then filters, sorts and slices nothing itself.
// For a list that is hundreds of thousands long -- every run the agent
// made -- a table that had to hold all of it to page it could not.
export type Range = {
  offset: number
  limit: number
  filters: Record<string, string | string[]>
  order: Sort | null
}

export function DataTable<Row>({
  columns,
  rows,
  rowKey,
  rowLink,
  onRowOpen,
  rowOpenLabel,
  loading,
  emptyMessage,
  initialFilters,
  pinned,
  countLabel,
  selected,
  onSelect,
  selectionActions,
  remote,
}: {
  columns: Column<Row>[]
  rows: Row[]
  rowKey: (row: Row) => string
  remote?: { total: number; onRange: (range: Range) => void }

  // Where a row goes when it is clicked. A row that has somewhere to go is a
  // bigger target than the one link inside it, and people aim at the row.
  // The cell that holds the link keeps it, so the keyboard and the middle
  // button still work; this only widens the target for the pointer.
  rowLink?: (row: Row) => string | undefined

  // What a row opens in place when it has no page to go to: its details
  // in a dialog. The row is then a target of its own, for the pointer and
  // for the keyboard (Tab to it, Enter), and rowOpenLabel names it for a
  // screen reader. The controls inside the row keep their clicks.
  onRowOpen?: (row: Row) => void
  rowOpenLabel?: (row: Row) => string

  loading?: boolean
  emptyMessage: string

  // Filters to start with, for a page arrived at from a link that already
  // said what it wanted to see.
  initialFilters?: Record<string, string | string[]>

  // Filters the caller applied from outside the table -- the runs of one
  // dream, arrived at from the dream's own row -- shown as chips beside
  // the table's own, each with its clearing, so that a narrowed table
  // says so where filters are said and not only in a heading.
  pinned?: { key: string; label: string; onClear: () => void }[]
  // The noun is the caller's: this component does not know whether it is
  // holding messages or deliveries, and English needs to be told which before
  // it can pluralise. `filtering` says whether the count is a subset, so the
  // caller can say "84 of 256" rather than the uninformative "256 of 256".
  countLabel: (count: number, filtering: boolean) => React.ReactNode

  // Choosing rows to act on together. Absent on a table that is only read,
  // which is most of them, and nothing about such a table changes.
  selected?: Set<string>
  onSelect?: (next: Set<string>) => void
  // What can be done to the rows that are chosen, drawn where the filter
  // button sits — and only while something is chosen, so a list at rest is a
  // list rather than a row of controls that do nothing.
  selectionActions?: (chosen: string[]) => React.ReactNode
}) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const { pathname } = useLocation()
  const remembered = useRef(readRemembered(pathname))
  const [filters, setFilters] = useState<Record<string, string | string[]>>(initialFilters ?? {})
  const [order, setOrder] = useState<Sort | null>(remembered.current.order ?? null)
  // Open when the page was reached from a link that already narrowed the
  // list, so nobody wonders why they are looking at a subset. Otherwise the
  // fields stay out of the way: a row of empty inputs under every header is
  // the widest thing on the page and is used on a fraction of visits.
  const [filtersOpen, setFiltersOpen] = useState(() =>
    Object.values(initialFilters ?? {}).some((filter) => filter.length > 0),
  )
  // Which page, and how many rows on it, are in the address. They were state
  // kept in session storage, so a list opened at page four could not be sent
  // to anybody, came back as page one after a reload, and gave the back
  // button nothing to return to.
  const paging = usePageInAddress({
    defaultPageSize: PAGE_SIZES.includes(remembered.current.pageSize ?? 0)
      ? (remembered.current.pageSize as number)
      : 50,
  })
  const pageSize = paging.pageSize
  const page = paging.pageIndex
  const setPage = (next: number) => paging.setPageIndex(next)
  const setPageSize = (next: number) => {
    paging.setPageSize(next)
    // Remembered as well as written down, so the size somebody chose is
    // still theirs on the next list they open.
    writeRemembered(pathname, { pageSize: next })
  }

  useEffect(() => {
    writeRemembered(pathname, { order })
  }, [pathname, order])

  const filtered = useMemo(
    () =>
      remote
        ? rows
        : rows.filter((row) =>
            columns.every((column) => {
              const filter = filters[column.key]
              if (!filter || filter.length === 0) {
                return true
              }
              const value = column.value?.(row)
              return Array.isArray(filter) ? matchesSelection(value, filter) : matchesText(value, filter)
            }),
          ),
    [rows, columns, filters, remote],
  )

  const sorted = useMemo(() => {
    if (!order || remote) {
      return filtered
    }
    const column = columns.find((candidate) => candidate.key === order.key)
    if (!column?.sort) {
      return filtered
    }
    // Copied before sorting: the array belongs to the caller's query result,
    // and sorting in place mutates what React is holding.
    const rows = [...filtered]
    rows.sort(column.sort)
    return order.direction === 'descending' ? rows.reverse() : rows
  }, [filtered, order, columns, remote])

  // A filter that shortens the list must not leave you on a page past its
  // end, staring at nothing. Not on mount, though: the first run would throw
  // away the page that was just remembered.
  const mounted = useRef(false)
  useEffect(() => {
    if (!mounted.current) {
      mounted.current = true
      return
    }
    setPage(0)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [filters, pageSize, order])

  // How long the list is: the rows in hand, or what the server says. A
  // remote list's total is not known before its first answer, so the page
  // in the address is asked for as it is rather than cut to the one page
  // a total of nothing has, which fetched page one and then the page
  // asked for.
  const total = remote ? remote.total : filtered.length
  const pageCount = Math.max(1, Math.ceil(total / pageSize))
  const current = remote && total === 0 ? page : Math.min(page, pageCount - 1)
  const visible = remote ? rows : sorted.slice(current * pageSize, current * pageSize + pageSize)

  // A remote list is fetched for the page, size, filters and order in
  // force, whenever any of them change.
  const onRange = remote?.onRange
  useEffect(() => {
    onRange?.({ offset: current * pageSize, limit: pageSize, filters, order })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [onRange, current, pageSize, JSON.stringify(filters), order?.key, order?.direction])

  // Scroll position: saved as it changes, restored once the rows exist —
  // there is nothing to scroll to before then.
  const scrollRestored = useRef(false)
  useEffect(() => {
    const element = scroller()
    if (!element) {
      return
    }
    // Throttled with a timer rather than an animation frame: the browser
    // stops animation frames for a tab that is not in front, and a write that
    // waits for one can wait for ever.
    let timer = 0
    const onScroll = () => {
      window.clearTimeout(timer)
      timer = window.setTimeout(() => writeRemembered(pathname, { scroll: element.scrollTop }), 100)
    }
    element.addEventListener('scroll', onScroll, { passive: true })
    return () => {
      window.clearTimeout(timer)
      element.removeEventListener('scroll', onScroll)
    }
  }, [pathname])
  useEffect(() => {
    if (scrollRestored.current || visible.length === 0) {
      return
    }
    scrollRestored.current = true
    const target = remembered.current.scroll
    if (target) {
      scroller()?.scrollTo({ top: target })
    }
  }, [visible.length])
  const filtering = Object.values(filters).some((filter) => filter.length > 0)
  const pinnedFilters = pinned ?? []
  const narrowed = filtering || pinnedFilters.length > 0
  const selecting = onSelect !== undefined
  const chosen = selected ? [...selected] : []
  // The row whose box was clicked last, where a shift-click's run starts.
  const rangeAnchor = useRef<string | null>(null)
  const filterable = columns.some((column) => column.filter)

  return (
    <>
      {(filterable || chosen.length > 0 || pinnedFilters.length > 0) && (
        <div className="table-tools">
          {pinnedFilters.map((filter) => (
            <span key={filter.key} className="table-pinned">
              {filter.label}
              <button type="button" className="icon-button" aria-label={t('filter.clearAll')} onClick={filter.onClear}>
                <CloseIcon size={12} />
              </button>
            </span>
          ))}
          {filterable && (
            <>
              <button
                type="button"
                className={filtersOpen || filtering ? 'active' : undefined}
                aria-expanded={filtersOpen}
                onClick={() => setFiltersOpen((previous) => !previous)}
              >
                <FilterIcon size={16} />
                {t('filter.toggle')}
              </button>
              {filtering && (
                <button /* link-button: the table's own clear-filters control */ type="button" className="link" onClick={() => setFilters({})}>
                  {t('filter.clearAll')}
                </button>
              )}
            </>
          )}
          {/* The count is not said here: what can be done with the chosen
              rows says how many it will act on, and saying it twice in one
              row is a row that repeats itself. */}
          {chosen.length > 0 && selectionActions?.(chosen)}
        </div>
      )}

      <div className="table-surface">
        <table>
          <thead>
            <tr>
              {selecting && (
                <th className="table-choose">
                  <input
                    type="checkbox"
                    aria-label={t('table.chooseAll')}
                    checked={visible.length > 0 && visible.every((row) => selected?.has(rowKey(row)))}
                    ref={(input) => {
                      if (input) {
                        // Some but not all of the page: the box says so
                        // rather than looking empty when half of it is
                        // chosen. Rows chosen on other pages are not this
                        // box's to show.
                        input.indeterminate =
                          visible.some((row) => selected?.has(rowKey(row))) &&
                          !visible.every((row) => selected?.has(rowKey(row)))
                      }
                    }}
                    onChange={(event) => {
                      const next = new Set(selected)
                      for (const row of visible) {
                        if (event.target.checked) {
                          next.add(rowKey(row))
                        } else {
                          next.delete(rowKey(row))
                        }
                      }
                      onSelect?.(next)
                    }}
                  />
                </th>
              )}
              {columns.map((column) => (
                <th
                  key={column.key}
                  className={
                    [column.optional ? 'optional' : '', column.numeric ? 'numeric' : ''].filter(Boolean).join(' ') ||
                    undefined
                  }
                  style={column.width ? { width: column.width } : undefined}
                  aria-sort={
                    order?.key === column.key
                      ? order.direction === 'ascending'
                        ? 'ascending'
                        : 'descending'
                      : undefined
                  }
                >
                  {column.sort ? (
                    <Tooltip
                      label={describeSort(t, column.header, order?.key === column.key ? order.direction : undefined)}
                    >
                      <button
                        type="button"
                        className={order?.key === column.key ? 'sort-header active' : 'sort-header'}
                        onClick={() => setOrder(nextSort(order, column.key))}
                      >
                        {column.header}
                        <SortIcon size={13} direction={order?.key === column.key ? order.direction : undefined} />
                      </button>
                    </Tooltip>
                  ) : (
                    column.header
                  )}
                </th>
              ))}
            </tr>
            {filterable && filtersOpen && (
              <tr className="filter-row">
                {selecting && <td className="table-choose" />}
                {columns.map((column) => (
                  <td key={column.key} className={column.optional ? 'optional' : undefined}>
                    <ColumnFilter
                      column={column}
                      rows={rows}
                      value={filters[column.key]}
                      onChange={(next) => setFilters((previous) => ({ ...previous, [column.key]: next }))}
                    />
                  </td>
                ))}
              </tr>
            )}
          </thead>
          <tbody>
            {visible.map((row) => {
              const href = rowLink?.(row)
              const isOpenable = !href && onRowOpen !== undefined
              return (
                <tr
                  key={rowKey(row)}
                  className={href || isOpenable ? 'linked' : undefined}
                  tabIndex={isOpenable ? 0 : undefined}
                  aria-label={isOpenable ? rowOpenLabel?.(row) : undefined}
                  onKeyDown={
                    isOpenable
                      ? (event) => {
                          if (event.key === 'Enter' && event.target === event.currentTarget) {
                            event.preventDefault()
                            onRowOpen(row)
                          }
                        }
                      : undefined
                  }
                  onClick={
                    href || isOpenable
                      ? (event) => {
                          // A click that landed on something of its own — the
                          // subject link, a button, a text selection someone is
                          // dragging out, an option of a list a control in the
                          // row opened — belongs to that thing, not the row.
                          if (
                            event.defaultPrevented ||
                            (event.target as HTMLElement).closest(
                              'a, button, input, select, textarea, label, [role="option"], .select-list',
                            ) ||
                            window.getSelection()?.toString()
                          ) {
                            return
                          }
                          if (!href) {
                            onRowOpen?.(row)
                            return
                          }
                          if (event.metaKey || event.ctrlKey) {
                            window.open(href, '_blank', 'noopener')
                            return
                          }
                          navigate(href)
                        }
                      : undefined
                  }
                >
                  {selecting && (
                    <td className="table-choose">
                      <input
                        type="checkbox"
                        aria-label={t('table.choose')}
                        checked={selected?.has(rowKey(row)) ?? false}
                        onChange={(event) => {
                          // Shift chooses, or lets go of, every row shown
                          // from the last box clicked to this one, the way
                          // a mail program's list does.
                          const index = visible.indexOf(row)
                          const anchorIndex = visible.findIndex((candidate) => rowKey(candidate) === rangeAnchor.current)
                          const isRange = (event.nativeEvent as MouseEvent).shiftKey && anchorIndex >= 0
                          const span = isRange
                            ? visible.slice(Math.min(index, anchorIndex), Math.max(index, anchorIndex) + 1)
                            : [row]
                          const next = new Set(selected)
                          for (const spanned of span) {
                            if (event.target.checked) {
                              next.add(rowKey(spanned))
                            } else {
                              next.delete(rowKey(spanned))
                            }
                          }
                          rangeAnchor.current = rowKey(row)
                          onSelect?.(next)
                        }}
                      />
                    </td>
                  )}
                  {columns.map((column) => (
                    <td
                      key={column.key}
                      className={[
                        column.optional ? 'optional' : '',
                        column.truncate ? 'truncate' : '',
                        column.numeric ? 'numeric' : '',
                      ]
                        .filter(Boolean)
                        .join(' ')}
                    >
                      {column.truncate && column.value?.(row) ? (
                        <Tooltip label={column.value(row) ?? ''}>
                          <span>{column.render ? column.render(row) : column.value(row)}</span>
                        </Tooltip>
                      ) : column.render ? (
                        column.render(row)
                      ) : (
                        column.value?.(row)
                      )}
                    </td>
                  ))}
                </tr>
              )
            })}
          </tbody>
        </table>

        {!loading && visible.length === 0 && (
          <p className="table-empty muted">{filtering ? t('filter.nothingMatches') : emptyMessage}</p>
        )}
      </div>

      {total > 0 && (
        <div className="table-bar">
          <span className="muted">
            {countLabel(total, remote ? narrowed : narrowed || filtered.length !== rows.length)}
          </span>

          {filtering && (
            <button /* link-button: the table's own clear-filters control */ className="link" onClick={() => setFilters({})}>
              {t('filter.clearAll')}
            </button>
          )}

          <Pager
            pageIndex={current}
            pageSize={pageSize}
            shownCount={remote ? rows.length : Math.max(0, Math.min(total, (current + 1) * pageSize) - current * pageSize)}
            totalCount={total}
            onPageIndex={setPage}
            onPageSize={setPageSize}
          />
        </div>
      )}
    </>
  )
}

function ColumnFilter<Row>({
  column,
  rows,
  value,
  onChange,
}: {
  column: Column<Row>
  rows: Row[]
  value: string | string[] | undefined
  onChange: (value: string | string[]) => void
}) {
  const { t } = useTranslation()

  // The options a select offers are the values actually present, so a filter
  // can never offer one that would return nothing — unless the caller knows
  // better and supplies its own.
  const options = useMemo(() => {
    const counts = new Map<string, number>()
    for (const row of rows) {
      const entry = column.value?.(row)
      if (entry) {
        counts.set(entry, (counts.get(entry) ?? 0) + 1)
      }
    }

    // A caller can supply the options — the domain list comes from the
    // configuration, so that a domain with no mail is still offered. The
    // counts are attached either way, and a zero is worth showing: it says
    // that domain has received nothing, which is often the thing being
    // checked.
    if (column.options) {
      return (
        column.options
          .map((option) => ({ ...option, count: option.count ?? counts.get(option.value) ?? 0 }))
          // Busiest first, like the derived options. A list of twenty domains
          // in alphabetical order buries the one with four hundred messages
          // under six that have none.
          .sort((first, second) => second.count - first.count || first.label.localeCompare(second.label))
      )
    }

    return [...counts.entries()]
      .sort((first, second) => second[1] - first[1] || first[0].localeCompare(second[0]))
      .map(([entry, count]) => ({ value: entry, label: entry, count }))
  }, [column, rows])

  if (column.filter === 'text') {
    return (
      <TextFilter
        value={typeof value === 'string' ? value : ''}
        placeholder={t('filter.filterBy', { column: column.header })}
        onChange={onChange}
      />
    )
  }
  if (column.filter === 'select') {
    return (
      <MultiSelectFilter
        label={column.header}
        options={options}
        selected={Array.isArray(value) ? value : []}
        onChange={onChange}
      />
    )
  }
  return null
}

// nextSort cycles a column: ascending, descending, then back to the table's
// own order. The third state matters — without it there is no way to undo a
// sort short of reloading the page.
function nextSort(current: Sort | null, key: string): Sort | null {
  if (current?.key !== key) {
    return { key, direction: 'ascending' }
  }
  return current.direction === 'ascending' ? { key, direction: 'descending' } : null
}

function describeSort(
  t: (key: Key, values?: Record<string, string | number>) => string,
  column: string,
  direction?: 'ascending' | 'descending',
): string {
  if (direction === 'ascending') {
    return t('table.sortedAscending', { column })
  }
  if (direction === 'descending') {
    return t('table.sortedDescending', { column })
  }
  return t('table.sortBy', { column })
}

/* A link can arrive with the filters it wants already in the query string, as
   in ?domain=example.com&status=rejected. Every parameter is treated as a
   filter on the column of that name; the table quietly ignores any that name
   no column, so a stray parameter costs nothing. */
export function useFilterParams(search: URLSearchParams): Record<string, string[]> | undefined {
  return useMemo(() => {
    const filters: Record<string, string[]> = {}
    for (const key of new Set(search.keys())) {
      filters[key] = search.getAll(key)
    }
    return Object.keys(filters).length === 0 ? undefined : filters
  }, [search])
}

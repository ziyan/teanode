import type { ReactNode } from 'react'
import { FilterIcon } from '../../components/icons'
import { MenuButton } from '../../components/menuButton'
import { Tooltip } from '../../components/tooltip'
import { useTranslation } from '../../i18n/i18n'
import type { PhotoKind } from '../../photos/api'
import { FILTER_KINDS, parseDay } from '../../photos/filters'
import { DateFormatHint } from './dateFormatHint'

// The one row under the tabs that every page of Photos has: what the page
// searches and narrows with at the left, what can be done on the page at the
// right. Under the tabs, not above them, so that the tabs are where they are
// on every page; a page with nothing to search leaves the left empty and the
// row keeps its height.
export function PhotoToolbar({ tools, children }: { tools?: ReactNode; children?: ReactNode }) {
  return (
    <div className="photo-toolbar">
      <div className="photo-toolbar-tools">{tools}</div>
      <div className="photo-toolbar-actions">{children}</div>
    </div>
  )
}

// A page's action that is an icon: its name is in the tooltip and the label,
// as every icon button on the dashboard has it.
export function PhotoIconAction({
  label,
  icon,
  isDangerous = false,
  isDisabled = false,
  onClick,
}: {
  label: string
  icon: ReactNode
  isDangerous?: boolean
  isDisabled?: boolean
  onClick: () => void
}) {
  return (
    <Tooltip label={label}>
      <button
        type="button"
        className={isDangerous ? 'icon-action danger' : 'icon-action'}
        aria-label={label}
        disabled={isDisabled}
        onClick={onClick}
      >
        {icon}
      </button>
    </Tooltip>
  )
}

const KIND_LABELS = {
  picture: 'photos.kindPicture',
  livePhoto: 'photos.kindLivePhoto',
  video: 'photos.kindVideo',
} as const

// A Filter button with the same name and icon as the one on the dashboard's
// tables (Recordings, Contacts), but not the same control: a table's opens a
// row of filters under its own columns, and a library is a grid with no
// columns to put them under. This one is a popover over the grid holding a
// small form: the kind of photo and the dates it was taken between, so it is
// announced as a dialog with a name, not as a menu. Lit while anything is set,
// so that a list that is narrower than the library says so where the filters
// are said.
export function PhotoFilterMenu({
  photoKind,
  fromDay,
  toDay,
  onChange,
  onClear,
}: {
  photoKind: PhotoKind | ''
  fromDay: string
  toDay: string
  onChange: (name: 'kind' | 'from' | 'to', value: string) => void
  onClear: () => void
}) {
  const { t, language } = useTranslation()
  const isFiltering = photoKind !== '' || fromDay !== '' || toDay !== ''
  return (
    <MenuButton
      label={t('filter.toggle')}
      popup="dialog"
      className={isFiltering ? 'photo-filter-button active' : 'photo-filter-button'}
      icon={
        <>
          <FilterIcon size={16} />
          {t('filter.toggle')}
        </>
      }
      render={(close) => (
        <div className="photo-filter-form">
          <fieldset className="photo-filter-kinds">
            <legend>{t('photos.filterKind')}</legend>
            {['', ...FILTER_KINDS].map((kind) => (
              <label key={kind} className="checkbox is-plain">
                <input
                  type="radio"
                  name="photo-filter-kind"
                  checked={photoKind === kind}
                  onChange={() => onChange('kind', kind)}
                />
                <span>{kind === '' ? t('photos.kindAll') : t(KIND_LABELS[kind as PhotoKind])}</span>
              </label>
            ))}
          </fieldset>
          <label>
            <span>{t('photos.filterFrom')}</span>
            <input
              type="date"
              lang={language}
              value={fromDay}
              max={toDay || undefined}
              onChange={(event) => onChange('from', parseDay(event.target.value))}
            />
          </label>
          <label>
            <span>{t('photos.filterTo')}</span>
            <input
              type="date"
              lang={language}
              value={toDay}
              min={fromDay || undefined}
              onChange={(event) => onChange('to', parseDay(event.target.value))}
            />
          </label>
          <DateFormatHint />
          {isFiltering && (
            <button
              type="button"
              className="photo-filter-clear"
              onClick={() => {
                // The button goes once nothing is filtered: closing hands the
                // keyboard back to Filter rather than dropping it on the page.
                onClear()
                close()
              }}
            >
              {t('photos.clearFilters')}
            </button>
          )}
        </div>
      )}
    />
  )
}

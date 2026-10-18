import { Children, createContext, useContext, useEffect, useState, type ReactNode } from 'react'
import { FilterIcon, MoreIcon } from '../../components/icons'
import { MenuButton } from '../../components/menuButton'
import { Tooltip } from '../../components/tooltip'
import { useTranslation } from '../../i18n/i18n'
import type { PhotoKind } from '../../photos/api'
import { FILTER_KINDS, parseDay } from '../../photos/filters'
import type { OverviewScale } from '../../photos/overview'
import { DateFormatHint } from './dateFormatHint'

// The width at which the stylesheet turns the toolbar into one row, and so the
// width at which the actions go behind a More menu.
const PHONE_QUERY = '(max-width: 600px)'

function useIsPhone(): boolean {
  const read = () => typeof window.matchMedia === 'function' && window.matchMedia(PHONE_QUERY).matches
  const [isPhone, setIsPhone] = useState(read)
  useEffect(() => {
    if (typeof window.matchMedia !== 'function') return
    const query = window.matchMedia(PHONE_QUERY)
    const onChange = (event: MediaQueryListEvent) => setIsPhone(event.matches)
    query.addEventListener('change', onChange)
    return () => query.removeEventListener('change', onChange)
  }, [])
  return isPhone
}

// Set while the actions are drawn in the More menu: the way to close it, so
// that choosing an action closes the menu the way it does in every other one.
const PhotoToolbarMenuContext = createContext<{ close: () => void } | null>(null)

// The one row under the tabs that every page of Photos has: what the page
// searches and narrows with at the left, what can be done on the page at the
// right. Under the tabs, not above them, so that the tabs are where they are
// on every page; a page with nothing to search leaves the left empty and the
// row keeps its height.
//
// On a phone the row stays one row: the search and the filters keep their
// place, the essentials (the page's own way of looking at the photos, which
// has to be in reach) stay beside them, and every other action goes into a
// More menu, since a row of words wrapped onto lines of their own is three
// rows of different lengths above the photos. A page with nothing to search
// has the whole row to itself, and keeps its one action in it.
//
// A row with nothing at all in it, such as the Trash with no photos in it, is
// not drawn: its minimum height would leave a blank band above the note.
export function PhotoToolbar({
  tools,
  essentials,
  children,
}: {
  tools?: ReactNode
  essentials?: ReactNode
  children?: ReactNode
}) {
  const { t } = useTranslation()
  const isPhone = useIsPhone()
  const hasTools = Children.toArray(tools).length > 0
  const hasActions = Children.toArray(children).length > 0
  if (!hasTools && !hasActions && Children.toArray(essentials).length === 0) return null
  const isOverflowing = isPhone && hasTools && hasActions
  return (
    <div className="photo-toolbar">
      <div className="photo-toolbar-tools">{tools}</div>
      <div className="photo-toolbar-actions">
        {essentials}
        {isOverflowing ? (
          <MenuButton
            className="icon-action photo-toolbar-more"
            label={t('photos.more')}
            icon={<MoreIcon size={18} />}
            render={(close) => (
              <PhotoToolbarMenuContext.Provider value={{ close }}>
                <div className="photo-toolbar-menu">{children}</div>
              </PhotoToolbarMenuContext.Provider>
            )}
          />
        ) : (
          children
        )}
      </div>
    </div>
  )
}

// A page's action with words: a button of the row, or a row of the More menu
// on a phone, with the same name in both. A button with words alone in the row
// has an icon in the menu all the same, so that the names of its rows line up.
export function PhotoToolbarButton({
  label,
  icon,
  menuIcon,
  isPrimary = false,
  isPressed,
  onClick,
}: {
  label: string
  icon?: ReactNode
  menuIcon?: ReactNode
  isPrimary?: boolean
  isPressed?: boolean
  onClick: () => void
}) {
  const menu = useContext(PhotoToolbarMenuContext)
  if (menu) {
    return (
      <button
        type="button"
        role="menuitem"
        onClick={() => {
          menu.close()
          onClick()
        }}
      >
        {icon ?? menuIcon}
        {label}
      </button>
    )
  }
  return (
    <button type="button" className={isPrimary ? 'primary' : undefined} aria-pressed={isPressed} onClick={onClick}>
      {icon} {label}
    </button>
  )
}

// A page's action that is an icon: its name is in the tooltip and the label,
// as every icon button on the dashboard has it. In the More menu the name is
// written out beside the icon, since a menu row has the room.
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
  const menu = useContext(PhotoToolbarMenuContext)
  if (menu) {
    return (
      <button
        type="button"
        role="menuitem"
        className={isDangerous ? 'is-dangerous' : undefined}
        disabled={isDisabled}
        onClick={() => {
          menu.close()
          onClick()
        }}
      >
        {icon}
        {label}
      </button>
    )
  }
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

// The switch between the library as a grid (All) and as an overview with one
// picture for each month or year. It is a group of buttons and not tabs: it
// changes how the same photos are drawn, and it is in the address, so a link
// and Back keep it.
export function PhotoScaleSwitch({
  scale,
  onChange,
}: {
  scale: OverviewScale | ''
  onChange: (scale: OverviewScale | '') => void
}) {
  const { t } = useTranslation()
  const choices: { scale: OverviewScale | ''; label: string }[] = [
    { scale: 'years', label: t('photos.scaleYears') },
    { scale: 'months', label: t('photos.scaleMonths') },
    { scale: '', label: t('photos.scaleAll') },
  ]
  return (
    <div className="segmented photo-scale-switch" role="group" aria-label={t('photos.scaleGroup')}>
      {choices.map((choice) => (
        <button
          key={choice.scale || 'all'}
          type="button"
          className={choice.scale === scale ? 'active' : ''}
          aria-pressed={choice.scale === scale}
          onClick={() => choice.scale !== scale && onChange(choice.scale)}
        >
          {choice.label}
        </button>
      ))}
    </div>
  )
}

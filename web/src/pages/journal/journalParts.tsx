import { useEffect, useId, useRef } from 'react'
import { Link, useLocation, useNavigationType, useSearchParams } from 'react-router-dom'

import { PageHeadingActions } from '../../components/breadcrumb'
import { ChevronLeftIcon, ChevronRightIcon, MoodIcon } from '../../components/icons'
import { Tooltip } from '../../components/tooltip'
import { useTranslation } from '../../i18n/i18n'
import {
  JournalSettings,
  MOOD_LEVELS,
  MOOD_NAMES,
  POLL_GIVE_UP_MILLISECONDS,
  POLL_INTERVAL_MILLISECONDS,
  PeriodKind,
  addDays,
  addMonths,
  addYears,
  dayOf,
  dayPath,
  dayText,
  monthOf,
  periodPath,
  yearOf,
} from './journalApi'
import { CollapseButton, useRememberedCollapse } from './journalCollapse'

// The small pieces the Journal's pages share: a card with a head, the
// address flags, the poll while something is on its way, the faces, and the
// bar of controls at the top of every period page.

// JournalCard is a panel with the dashboard's own head (a title, a sentence
// under it, and the one action that belongs to the panel), written out here
// rather than with SettingsSection so a panel can be the paper-colored one
// the person's own words sit on, and so a panel can fold up to its head.
//
// A panel with a collapseId has a chevron in its head, and stays as the
// person left it on every day. The head is a grid of its own: the title and
// the buttons share the first line on a phone too, where the dashboard's
// head drops the action under the title.
export function JournalCard({
  title,
  description,
  action,
  isPaper = false,
  isFlush = false,
  id,
  collapseId,
  isCollapsedByDefault = false,
  children,
}: {
  title: React.ReactNode
  description?: React.ReactNode
  action?: React.ReactNode
  isPaper?: boolean
  // No padding at the bottom, for a panel that ends in rows with their own.
  isFlush?: boolean
  id?: string
  // Names the panel to the browser's memory of what is folded; left out, the
  // panel cannot be folded.
  collapseId?: string
  isCollapsedByDefault?: boolean
  children?: React.ReactNode
}) {
  const [isCollapsed, toggle] = useRememberedCollapse(`card:${collapseId ?? ''}`, isCollapsedByDefault)
  const bodyId = useId()
  const classNames = ['settings-section', 'card', 'journal-card']
  if (isPaper) classNames.push('journal-paper')
  if (isFlush && !(collapseId && isCollapsed)) classNames.push('journal-card-flush')
  const hasChildren = children !== undefined && children !== null && children !== false
  const isFolded = Boolean(collapseId) && isCollapsed
  return (
    <section id={id} className={classNames.join(' ')}>
      <div className={hasChildren && !isFolded ? 'journal-card-head' : 'journal-card-head is-alone'}>
        <h3>{title}</h3>
        {(action || collapseId) && (
          <div className="journal-card-head-actions">
            {action}
            {collapseId && hasChildren && (
              <CollapseButton
                title={typeof title === 'string' ? title : ''}
                isCollapsed={isCollapsed}
                controls={bodyId}
                onToggle={toggle}
              />
            )}
          </div>
        )}
        {description && <p className="muted">{description}</p>}
      </div>
      {collapseId ? (
        <div id={bodyId} hidden={isFolded}>
          {isFolded ? null : children}
        </div>
      ) : (
        children
      )}
    </section>
  )
}

// useAddressValue is one value kept in the address, so a reload and a copied
// link land where the reader was. Setting it to null takes it out.
export function useAddressValue(name: string): [string | null, (value: string | null) => void] {
  const [parameters, setParameters] = useSearchParams()
  const value = parameters.get(name)
  const set = (next: string | null) =>
    setParameters(
      (previous) => {
        const written = new URLSearchParams(previous)
        if (next === null) written.delete(name)
        else written.set(name, next)
        return written
      },
      { replace: true },
    )
  return [value, set]
}

// usePendingPoll asks again every few seconds while a touched-up text or a
// narrative is on its way, and stops after two minutes: one that has not
// arrived by then is not coming soon, and a page left open should not ask
// forever. When it stops being pending the clock starts again, for the next
// one.
export function usePendingPoll(isPending: boolean, reload: () => Promise<void> | void) {
  const reloadRef = useRef(reload)
  reloadRef.current = reload
  useEffect(() => {
    if (!isPending) return
    const startedAt = Date.now()
    const timer = window.setInterval(() => {
      if (Date.now() - startedAt >= POLL_GIVE_UP_MILLISECONDS) {
        window.clearInterval(timer)
        return
      }
      void reloadRef.current()
    }, POLL_INTERVAL_MILLISECONDS)
    return () => window.clearInterval(timer)
  }, [isPending])
}

// Where the page was scrolled to, by the history entry it was shown for, so
// that Back from a record opened on another page lands where the person was
// reading and not at the top. Kept in memory only: a reload starts at the top
// like any page. The dashboard scrolls inside .content, which the browser's
// own restoring does not reach.
const scrollPositions = new Map<string, number>()

export function useKeptScroll(isReady: boolean) {
  const location = useLocation()
  const navigationType = useNavigationType()
  const entry = `${location.key}:${location.pathname}`
  const hasRestored = useRef(false)
  useEffect(() => {
    const scroller = document.querySelector('.content')
    if (!scroller) return
    const remember = () => scrollPositions.set(entry, scroller.scrollTop)
    scroller.addEventListener('scroll', remember, { passive: true })
    return () => scroller.removeEventListener('scroll', remember)
  }, [entry])
  useEffect(() => {
    if (!isReady || hasRestored.current) return
    hasRestored.current = true
    // Only going back or forward returns to a place; a link starts at the top.
    if (navigationType !== 'POP') return
    const saved = scrollPositions.get(entry)
    const scroller = document.querySelector('.content')
    if (saved && scroller) scroller.scrollTo({ top: saved })
  }, [isReady, entry, navigationType])
}

// MoodDot is a small filled circle in the color of a mood, or in the empty
// color for none, for the places that show a mood without a face.
export function MoodDot({ level }: { level: number }) {
  return <span className="journal-mood-dot" data-level={level > 0 ? level : undefined} aria-hidden="true" />
}

// MoodFace is a face drawn for a mood, in its color when it is the one the
// day has.
export function MoodFace({ level, size = 24, isFilled = true }: { level: number; size?: number; isFilled?: boolean }) {
  return (
    <span className="journal-mood-face" data-level={level > 0 ? level : undefined} data-filled={isFilled}>
      <MoodIcon level={level > 0 ? level : 3} size={size} />
    </span>
  )
}

// MoodRadios are the five faces to choose a day's mood from, as a radio
// group: the arrow keys move among them, and the chosen one is the one a
// screen reader announces.
export function MoodRadios({
  level,
  onChoose,
  isBusy,
}: {
  level: number
  onChoose: (level: number) => void
  isBusy: boolean
}) {
  const { t } = useTranslation()
  const group = useRef<HTMLDivElement>(null)
  // The arrows move the choice, like any radio group. The group is one stop
  // in the tab order: the chosen face, or the first when none is.
  function onKeyDown(event: React.KeyboardEvent) {
    const step =
      event.key === 'ArrowRight' || event.key === 'ArrowDown'
        ? 1
        : event.key === 'ArrowLeft' || event.key === 'ArrowUp'
          ? -1
          : 0
    if (step === 0) return
    event.preventDefault()
    const current = level > 0 ? level : step > 0 ? 0 : 6
    const next = Math.min(5, Math.max(1, current + step))
    onChoose(next)
    group.current?.querySelectorAll<HTMLButtonElement>('[role="radio"]')[next - 1]?.focus()
  }
  return (
    <div
      ref={group}
      className="journal-mood-radios"
      role="radiogroup"
      aria-label={t('journal.moodGroup')}
      onKeyDown={onKeyDown}
    >
      {MOOD_LEVELS.map((each) => (
        <button
          key={each}
          type="button"
          role="radio"
          aria-checked={level === each}
          tabIndex={level === each || (level === 0 && each === 1) ? 0 : -1}
          className="journal-mood-pick"
          data-level={each}
          disabled={isBusy}
          onClick={() => onChoose(each)}
        >
          <MoodIcon level={each} size={28} />
          <span>{t(MOOD_NAMES[each])}</span>
        </button>
      ))}
    </div>
  )
}

// The controls at the top of a period page, in the place the heading keeps
// for its action: which size of period to look at, the way to the one before
// and the one after, back to the present one, and out to the timeline. The
// size switch moves to the period of that size around the same date.
export function PeriodToolbar({
  periodKind,
  referenceDay,
  previousPath,
  nextPath,
  currentPath,
  currentLabel,
  previousLabel,
  nextLabel,
  isNextDisabled,
}: {
  // day for the page of one day.
  periodKind: PeriodKind | 'day'
  // The date the other sizes are opened around.
  referenceDay: string
  previousPath: string
  nextPath: string
  currentPath: string
  currentLabel: string
  previousLabel: string
  nextLabel: string
  isNextDisabled: boolean
}) {
  const { t } = useTranslation()
  const sizes: { id: PeriodKind | 'day'; label: string; path: string }[] = [
    { id: 'day', label: t('journal.zoomDay'), path: dayPath(dayOf(referenceDay)) },
    { id: 'week', label: t('journal.zoomWeek'), path: periodPath('week', referenceDay) },
    { id: 'month', label: t('journal.zoomMonth'), path: periodPath('month', referenceDay) },
    { id: 'year', label: t('journal.zoomYear'), path: periodPath('year', referenceDay) },
  ]
  return (
    <PageHeadingActions>
      <div className="journal-toolbar">
        <div className="segmented journal-zoom" role="group" aria-label={t('journal.zoomGroup')}>
          {sizes.map((size) => (
            <Link
              key={size.id}
              to={size.path}
              className={size.id === periodKind ? 'active' : undefined}
              aria-current={size.id === periodKind ? 'page' : undefined}
            >
              {size.label}
            </Link>
          ))}
        </div>
        <div className="journal-toolbar-nav">
          <div className="journal-stepper">
            <Tooltip label={previousLabel}>
              <Link className="icon-button" to={previousPath} aria-label={previousLabel}>
                <ChevronLeftIcon size={18} />
              </Link>
            </Tooltip>
            <Link className="button-quiet" to={currentPath}>
              {currentLabel}
            </Link>
            <Tooltip label={nextLabel}>
              {isNextDisabled ? (
                <button type="button" className="icon-button" aria-label={nextLabel} disabled>
                  <ChevronRightIcon size={18} />
                </button>
              ) : (
                <Link className="icon-button" to={nextPath} aria-label={nextLabel}>
                  <ChevronRightIcon size={18} />
                </Link>
              )}
            </Tooltip>
          </div>
          <Link className="button-quiet" to="/journal">
            {t('journal.timeline')}
          </Link>
        </div>
      </div>
    </PageHeadingActions>
  )
}

// What the day-sized steps of the toolbar are: the day before, the day after
// (never past today), and today.
export function dayStepPaths(day: string, today: string) {
  return {
    previousPath: dayPath(addDays(day, -1)),
    nextPath: dayPath(addDays(day, 1)),
    currentPath: dayPath(today),
    isNextDisabled: day >= today,
  }
}

// The same for a week, by its first day; the week after may not start after
// today.
export function weekStepPaths(periodStart: string, today: string) {
  return {
    previousPath: periodPath('week', addDays(periodStart, -7)),
    nextPath: periodPath('week', addDays(periodStart, 7)),
    currentPath: periodPath('week', today),
    isNextDisabled: addDays(periodStart, 7) > today,
  }
}

export function monthStepPaths(month: string, today: string) {
  return {
    previousPath: periodPath('month', `${addMonths(month, -1)}-01`),
    nextPath: periodPath('month', `${addMonths(month, 1)}-01`),
    currentPath: periodPath('month', today),
    isNextDisabled: addMonths(month, 1) > monthOf(today),
  }
}

export function yearStepPaths(year: string, today: string) {
  return {
    previousPath: periodPath('year', `${addYears(year, -1)}-01-01`),
    nextPath: periodPath('year', `${addYears(year, 1)}-01-01`),
    currentPath: periodPath('year', today),
    isNextDisabled: addYears(year, 1) > yearOf(today),
  }
}

// today is the reader's today, by the browser's calendar: the person's
// days are lived in the zone their browser sends the server.
export function browserToday(): string {
  return dayText(new Date())
}

// weekStartsOnOf is the day a week starts on, until the settings arrive the
// day the browser's locale starts it.
export function weekStartsOnOf(settings: JournalSettings | null, language: string): string {
  if (settings) return settings.weekStartsOn === 'sunday' ? 'sunday' : 'monday'
  try {
    const info = new Intl.Locale(language) as Intl.Locale & {
      getWeekInfo?: () => { firstDay: number }
      weekInfo?: { firstDay: number }
    }
    const first = info.getWeekInfo?.().firstDay ?? info.weekInfo?.firstDay
    return first === 7 ? 'sunday' : 'monday'
  } catch {
    return 'monday'
  }
}

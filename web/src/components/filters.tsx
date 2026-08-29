import { useMemo, useState } from 'react'

import { useTranslation } from '../i18n/i18n'
import { ChevronRightIcon } from './icons'
import { MenuButton } from './menuButton'

// Column filters. Two kinds, because columns come in two kinds: those whose
// values are a short known set — a domain, a status — where the useful
// question is "which of these", and those that are free text, where it is
// "does it contain".

export function TextFilter({
  value,
  placeholder,
  onChange,
}: {
  value: string
  placeholder: string
  onChange: (value: string) => void
}) {
  return (
    <input
      className="column-filter"
      type="search"
      value={value}
      placeholder={placeholder}
      aria-label={placeholder}
      onChange={(event) => onChange(event.target.value)}
    />
  )
}

// A filter option, and how many rows carry it. The count is what makes a
// menu of twenty domains navigable: the one with four hundred messages is
// probably the one being looked for, and an option with none should not be
// clicked at all.
export type Option = { value: string; label: string; count?: number }

// MultiSelectFilter is a menu of checkboxes with a search box above them.
//
// Search, because a server with fifty domains makes a plain list of fifty
// checkboxes useless; checkboxes rather than one choice, because "these three
// domains" is a question people actually have and a single select cannot ask
// it. Nothing selected means no filter, which is the same thing as everything
// selected and one fewer state to explain.
export function MultiSelectFilter({
  label,
  options,
  selected,
  onChange,
}: {
  label: string
  options: Option[]
  selected: string[]
  onChange: (selected: string[]) => void
}) {
  const { t } = useTranslation()
  const [search, setSearch] = useState('')

  const matching = useMemo(() => {
    const needle = search.trim().toLowerCase()
    if (!needle) {
      return options
    }
    return options.filter((option) => option.label.toLowerCase().includes(needle))
  }, [options, search])

  const summary = selected.length === 0 ? t('filter.all') : t('filter.selected', { count: selected.length })

  return (
    <MenuButton
      className={selected.length > 0 ? 'column-filter-button active' : 'column-filter-button'}
      label={`${label}: ${summary}`}
      icon={
        <>
          <span className="column-filter-summary">{summary}</span>
          <ChevronRightIcon size={14} />
        </>
      }
      render={() => (
        <>
          <input
            className="menu-search"
            type="search"
            value={search}
            placeholder={t('filter.search')}
            aria-label={t('filter.search')}
            onChange={(event) => setSearch(event.target.value)}
          />

          {selected.length > 0 && (
            <button type="button" className="menu-clear" onClick={() => onChange([])}>
              {t('filter.clear')}
            </button>
          )}

          <div className="menu-options">
            {matching.length === 0 && <div className="menu-empty">{t('filter.noMatches')}</div>}
            {matching.map((option) => {
              const checked = selected.includes(option.value)
              return (
                <label key={option.value} className="menu-option">
                  <input
                    type="checkbox"
                    checked={checked}
                    onChange={() =>
                      onChange(
                        checked
                          ? selected.filter((value) => value !== option.value)
                          : [...selected, option.value],
                      )
                    }
                  />
                  <span>{option.label}</span>
                  {option.count !== undefined && <span className="menu-count">{option.count}</span>}
                </label>
              )
            })}
          </div>
        </>
      )}
    />
  )
}

// matchesText is the free-text rule, in one place so every column agrees on
// what "contains" means: case insensitive, and an empty filter matches.
export function matchesText(value: string | undefined | null, filter: string): boolean {
  const needle = filter.trim().toLowerCase()
  if (!needle) {
    return true
  }
  return (value ?? '').toLowerCase().includes(needle)
}

// matchesSelection is the same for the multiselect columns: nothing selected
// means no filter rather than nothing shown.
export function matchesSelection(value: string | undefined | null, selected: string[]): boolean {
  return selected.length === 0 || selected.includes(value ?? '')
}

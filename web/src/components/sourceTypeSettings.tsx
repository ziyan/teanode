import { Key, Values } from '../i18n/i18n'
import { useTranslation } from '../i18n/i18n'

// Source types: a file of declarations saying how a knowledge source is
// read, installed by the operator from the registry or from a file of their
// own. A person adds a source of an installed type by filling in the
// settings the type declares, so the form for a source is built from those
// declarations rather than written out here once per kind of place.

export type SourceTypeSetting = {
  name: string
  description: string
  // string, path, array (a list of text), boolean or integer.
  settingType: string
  pattern: string
  itemPattern: string
  // What leaving it unset means, as a JSON value; null when there is no
  // default, which is what makes a setting required.
  default: unknown
  isRequired: boolean
  minimum: number | null
  maximum: number | null
}

export type SourceType = {
  name: string
  description: string
  version: string
  publisher: string
  url: string
  isLocal: boolean
  installedAt: string
  // The reader built into the server that reads it (files, journal, sent,
  // web), or empty for a type that runs commands on a computer.
  reader: string
  runs: string[]
  requires: string[]
  guide: string
  readable: boolean
  problem?: string | null
  settings: SourceTypeSetting[]
}

export const SOURCE_TYPE_FIELDS = `name description version publisher url isLocal installedAt reader runs requires guide readable problem
    settings { name description settingType pattern itemPattern default isRequired minimum maximum }`

export const LIST_SOURCE_TYPES = `query { ListAgentSourceTypes { ${SOURCE_TYPE_FIELDS} } }`

// The readers built into the server, in the order a person is offered them:
// the places most people add first.
const BUILT_IN_READERS = ['files', 'journal', 'sent', 'web']

// orderedSourceTypes puts the types read by a built-in reader first, then
// the ones that run commands, each by name. Types whose file no longer
// parses are left out: a source cannot be added of a type the server cannot
// read.
export function orderedSourceTypes(sourceTypes: SourceType[]): SourceType[] {
  const rankOf = (sourceType: SourceType) => {
    const rank = BUILT_IN_READERS.indexOf(sourceType.reader)
    return rank === -1 ? BUILT_IN_READERS.length : rank
  }
  return sourceTypes
    .filter((sourceType) => sourceType.readable)
    .sort((first, second) => rankOf(first) - rankOf(second) || first.name.localeCompare(second.name))
}

// isReadOnComputer says whether a source of the type is read on one of the
// person's computers, and so has to say which: the files and journal
// readers read a folder there, and a type of commands runs them there.
export function isReadOnComputer(sourceType: SourceType): boolean {
  return sourceType.reader === 'files' || sourceType.reader === 'journal' || sourceType.reader === ''
}

// sourceTypeReaderKey is the word for how a type is read, as a key.
export function sourceTypeReaderKey(sourceType: SourceType): Key {
  switch (sourceType.reader) {
    case 'files':
      return 'sourceTypes.readerFiles'
    case 'journal':
      return 'sourceTypes.readerJournal'
    case 'sent':
      return 'sourceTypes.readerSent'
    case 'web':
      return 'sourceTypes.readerWeb'
    case '':
      return 'sourceTypes.readerCommands'
  }
  return 'sourceTypes.readerUnknown'
}

// What a setting holds while it is being typed: the text in its box, or
// whether its box is ticked.
export type SettingDrafts = Record<string, string | boolean>

// storedSettings reads a source's settings, which arrive as a JSON object
// or, from an older server, as the text of one.
export function storedSettings(stored: unknown): Record<string, unknown> {
  let parsed = stored
  if (typeof stored === 'string') {
    try {
      parsed = JSON.parse(stored)
    } catch {
      return {}
    }
  }
  return parsed && typeof parsed === 'object' && !Array.isArray(parsed) ? (parsed as Record<string, unknown>) : {}
}

// asText is a JSON value as a person would type it: a list as its items
// separated by commas.
function asText(value: unknown): string {
  if (value === null || value === undefined) return ''
  if (Array.isArray(value)) return value.map((item) => String(item)).join(', ')
  if (typeof value === 'object') return JSON.stringify(value)
  return String(value)
}

// settingDrafts is the form's boxes filled in from what a source already
// says, or empty for a new one so each shows its default as a placeholder.
export function settingDrafts(sourceType: SourceType, stored: Record<string, unknown> = {}): SettingDrafts {
  const drafts: SettingDrafts = {}
  for (const setting of sourceType.settings) {
    const value = stored[setting.name]
    if (setting.settingType === 'boolean') {
      drafts[setting.name] = typeof value === 'boolean' ? value : setting.default === true
    } else {
      drafts[setting.name] = asText(value)
    }
  }
  return drafts
}

function listOf(text: string): string[] {
  return text
    .split(',')
    .map((item) => item.trim())
    .filter((item) => item !== '')
}

// matches runs one of the type's patterns, which are written for the
// server's regular expressions. A pattern this browser cannot compile is
// left to the server to judge rather than refusing what was typed.
function matches(pattern: string, text: string): boolean {
  if (!pattern) return true
  try {
    return new RegExp(pattern).test(text)
  } catch {
    return true
  }
}

// settingProblem is what is wrong with what was typed for one setting, or
// null. Said before the form is sent, so the person hears which box it is.
export function settingProblem(
  setting: SourceTypeSetting,
  draft: string | boolean | undefined,
  t: (key: Key, values?: Values) => string,
): string | null {
  if (setting.settingType === 'boolean') return null
  const text = typeof draft === 'string' ? draft.trim() : ''
  if (text === '') {
    return setting.isRequired ? t('sourceTypes.settingMissing', { name: setting.name }) : null
  }
  if (setting.settingType === 'integer') {
    const number = Number(text)
    if (!Number.isInteger(number)) return t('sourceTypes.settingNotWhole', { name: setting.name })
    if (setting.minimum !== null && number < setting.minimum)
      return t('sourceTypes.settingTooSmall', { name: setting.name, minimum: setting.minimum })
    if (setting.maximum !== null && number > setting.maximum)
      return t('sourceTypes.settingTooLarge', { name: setting.name, maximum: setting.maximum })
    return null
  }
  if (setting.settingType === 'array') {
    const refused = listOf(text).find((item) => !matches(setting.itemPattern, item))
    return refused === undefined ? null : t('sourceTypes.settingItemRefused', { name: setting.name, item: refused })
  }
  return matches(setting.pattern, text) ? null : t('sourceTypes.settingRefused', { name: setting.name })
}

// settingsProblem is the first thing wrong with the whole form, or null.
export function settingsProblem(
  sourceType: SourceType,
  drafts: SettingDrafts,
  t: (key: Key, values?: Values) => string,
  skipped: string[] = [],
): string | null {
  for (const setting of sourceType.settings) {
    if (skipped.includes(setting.name)) continue
    const problem = settingProblem(setting, drafts[setting.name], t)
    if (problem) return problem
  }
  return null
}

// settingValues is the JSON object sent with the source. A box left empty
// is left out, so the type's default applies; a tick box is always said.
export function settingValues(sourceType: SourceType, drafts: SettingDrafts): Record<string, unknown> {
  const values: Record<string, unknown> = {}
  for (const setting of sourceType.settings) {
    const draft = drafts[setting.name]
    if (setting.settingType === 'boolean') {
      values[setting.name] = draft === true
      continue
    }
    const text = typeof draft === 'string' ? draft.trim() : ''
    if (text === '') continue
    if (setting.settingType === 'integer') values[setting.name] = Number(text)
    else if (setting.settingType === 'array') values[setting.name] = listOf(text)
    else values[setting.name] = text
  }
  return values
}

// SourceTypeSettingsFields is one box per setting the type declares, each
// with what the type says about it under it. A setting the caller draws
// itself -- the mailbox of a sent source, picked from the person's own --
// is passed in `replaced` and drawn in its place.
export function SourceTypeSettingsFields({
  sourceType,
  drafts,
  onChange,
  replaced = {},
}: {
  sourceType: SourceType
  drafts: SettingDrafts
  onChange: (name: string, draft: string | boolean) => void
  replaced?: Record<string, React.ReactNode>
}) {
  const { t } = useTranslation()
  return (
    <>
      {sourceType.settings.map((setting) => {
        if (setting.name in replaced) {
          return <div key={setting.name}>{replaced[setting.name]}</div>
        }
        const draft = drafts[setting.name]
        const text = typeof draft === 'string' ? draft : ''
        const problem = text.trim() === '' ? null : settingProblem(setting, draft, t)
        const help = setting.description ? (
          <p className="muted field-hint">
            {setting.description}
            {setting.settingType === 'array' ? ` ${t('sourceTypes.settingListHint')}` : ''}
          </p>
        ) : setting.settingType === 'array' ? (
          <p className="muted field-hint">{t('sourceTypes.settingListHint')}</p>
        ) : null
        if (setting.settingType === 'boolean') {
          return (
            <div key={setting.name}>
              <label className="checkbox">
                <input
                  type="checkbox"
                  checked={draft === true}
                  onChange={(event) => onChange(setting.name, event.target.checked)}
                />
                {setting.name}
              </label>
              {help}
            </div>
          )
        }
        const placeholder = asText(setting.default) || (setting.settingType === 'path' ? '~/notes' : '')
        return (
          <div key={setting.name}>
            <label>
              <span>
                {setting.name}
                {setting.isRequired ? ` · ${t('sourceTypes.settingRequired')}` : ''}
              </span>
              {setting.settingType === 'integer' ? (
                <input
                  type="number"
                  inputMode="numeric"
                  step={1}
                  min={setting.minimum ?? undefined}
                  max={setting.maximum ?? undefined}
                  value={text}
                  placeholder={placeholder}
                  aria-required={setting.isRequired || undefined}
                  aria-invalid={problem ? true : undefined}
                  onChange={(event) => onChange(setting.name, event.target.value)}
                />
              ) : (
                <input
                  value={text}
                  placeholder={placeholder}
                  aria-required={setting.isRequired || undefined}
                  autoComplete="off"
                  spellCheck={false}
                  aria-invalid={problem ? true : undefined}
                  onChange={(event) => onChange(setting.name, event.target.value)}
                />
              )}
            </label>
            {help}
          </div>
        )
      })}
    </>
  )
}

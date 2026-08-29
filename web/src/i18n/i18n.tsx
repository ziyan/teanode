import React, { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react'

import { MenuButton } from '../components/menuButton'
import { GlobeIcon } from '../components/icons'
import { en } from './en'
import { ja } from './ja'
import { zh } from './zh'

// No i18n library. The dashboard needs lookup and substitution, which is forty
// lines; a library would be more code than the thing it translates, and this
// server already refuses to ship a megabyte of dependency to list messages.
//
// English is the source of truth. The other catalogues are typed against it,
// so removing a key breaks the build and adding one without translating it
// does too — there is no way to end up with a screen that is silently half
// translated.

export type Catalog = typeof en
export type Key = keyof Catalog

export const LANGUAGES = {
  en: 'English',
  zh: '简体中文',
  ja: '日本語',
} as const

export type Language = keyof typeof LANGUAGES

const CATALOGS: Record<Language, Catalog> = { en, zh, ja }

const STORAGE_KEY = 'teanode.language'

function isLanguage(value: string | null): value is Language {
  return value === 'en' || value === 'zh' || value === 'ja'
}

// detectLanguage prefers what the reader chose, then what the browser asks
// for. navigator.language is a tag like "zh-CN" or "ja-JP", so only the part
// before the dash is compared; a reader on zh-TW gets Simplified rather than
// English, which is closer to right than falling back.
export function detectLanguage(): Language {
  try {
    const stored = window.localStorage.getItem(STORAGE_KEY)
    if (isLanguage(stored)) {
      return stored
    }
  } catch {
    // Private browsing refuses local storage.
  }

  for (const tag of navigator.languages ?? [navigator.language]) {
    const base = tag.toLowerCase().split('-')[0]
    if (isLanguage(base)) {
      return base
    }
  }
  return 'en'
}

export type Values = Record<string, string | number>

// translate looks a key up and substitutes {name} placeholders. A missing key
// cannot happen — the catalogues are typed — but a missing placeholder value
// can, and leaving the placeholder visible says so louder than an empty gap.
export function translate(catalog: Catalog, key: Key, values?: Values): string {
  const template = catalog[key]
  if (!values) {
    return template
  }
  return template.replace(/\{(\w+)\}/g, (placeholder, name: string) => {
    const value = values[name]
    return value === undefined ? placeholder : String(value)
  })
}

interface Translation {
  t: (key: Key, values?: Values) => string
  // plural picks the right form for a count in the reader's language, through
  // the platform's own rules. English needs two forms, Chinese and Japanese
  // need one, and other languages need up to six — hard-coding "add an s"
  // works for exactly one of them.
  plural: (count: number, forms: { one: Key; other: Key }, values?: Values) => string
  language: Language
  setLanguage: (language: Language) => void
}

const TranslationContext = createContext<Translation | null>(null)

export function TranslationProvider({ children }: { children: React.ReactNode }) {
  const [language, setLanguageState] = useState<Language>(detectLanguage)

  useEffect(() => {
    // Screen readers and hyphenation both depend on this being right.
    document.documentElement.lang = language === 'zh' ? 'zh-Hans' : language
  }, [language])

  const setLanguage = useCallback((next: Language) => {
    setLanguageState(next)
    try {
      window.localStorage.setItem(STORAGE_KEY, next)
    } catch {
      // Not remembering it is not a reason to ignore it for this visit.
    }
  }, [])

  const value = useMemo<Translation>(() => {
    const catalog = CATALOGS[language]
    const rules = new Intl.PluralRules(language)
    const t = (key: Key, values?: Values) => translate(catalog, key, values)

    return {
      t,
      plural: (count, forms, values) =>
        t(rules.select(count) === 'one' ? forms.one : forms.other, { count, ...values }),
      language,
      setLanguage,
    }
  }, [language, setLanguage])

  return <TranslationContext.Provider value={value}>{children}</TranslationContext.Provider>
}

export function useTranslation(): Translation {
  const value = useContext(TranslationContext)
  if (value === null) {
    throw new Error('useTranslation was called outside a TranslationProvider')
  }
  return value
}

// Trans is for a sentence with something other than text in the middle: a
// link, a piece of code. It splits the template on the placeholders and puts
// the nodes back between the pieces, so a translator can move {link} to
// wherever the sentence needs it — which in Chinese and Japanese is rarely
// where English puts it.
export function Trans({ k, values, nodes }: { k: Key; values?: Values; nodes: Record<string, React.ReactNode> }) {
  const { t } = useTranslation()
  const template = t(k, values)

  const parts = template.split(/(\{\w+\})/g)
  return (
    <>
      {parts.map((part, index) => {
        const name = part.startsWith('{') && part.endsWith('}') ? part.slice(1, -1) : null
        if (name !== null && name in nodes) {
          return <React.Fragment key={index}>{nodes[name]}</React.Fragment>
        }
        return part
      })}
    </>
  )
}

// LanguagePicker is an icon on the bar and a short menu behind it. Each
// language is written in its own script: somebody looking for theirs should
// not have to read English to find it.
export function LanguagePicker() {
  const { t } = useTranslation()

  return (
    <MenuButton
      className="icon-button"
      label={t('language.label')}
      icon={<GlobeIcon />}
      render={(close) => <LanguageItems close={close} />}
    />
  )
}

// The same options as rows, for a menu that has other things in it too. A
// language is something somebody chooses once, and once is not worth its own
// button on every page — it belongs with the rest of what is about the reader
// rather than about the page.
export function LanguageItems({ close }: { close: () => void }) {
  const { language, setLanguage } = useTranslation()

  return (
    <>
      {(Object.keys(LANGUAGES) as Language[]).map((code) => (
        <button
          key={code}
          type="button"
          role="menuitemradio"
          aria-checked={code === language}
          className={code === language ? 'selected' : undefined}
          onClick={() => {
            setLanguage(code)
            close()
          }}
        >
          <GlobeIcon />
          {LANGUAGES[code]}
        </button>
      ))}
    </>
  )
}

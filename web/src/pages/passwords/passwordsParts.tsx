import { useEffect, useMemo, useRef, useState } from 'react'

import { CopyIconButton } from '../../components/common'
import { EyeIcon, EyeOffIcon, RefreshIcon } from '../../components/icons'
import { useToast } from '../../components/toast'
import { Tooltip } from '../../components/tooltip'
import { useTranslation } from '../../i18n/i18n'
import { decodeBase64, safetyCode } from '../../passwords/crypto'
import {
  DEFAULT_PASSWORD_OPTIONS,
  generatePassword,
  MAXIMUM_PASSWORD_LENGTH,
  PasswordOptions,
} from '../../passwords/generator'
import {
  oneTimeCode,
  oneTimeCodeSecondsLeft,
  OneTimeCodeSettings,
  parseOneTimeCodeUri,
} from '../../passwords/oneTimeCode'

// The small parts the Passwords screens share: a value with its copy button, a
// hidden value with Show, the running one-time code, the recovery key with
// its Copy and Download, the password generator, and a safety code.

// Attributes that ask the common password manager extensions to leave a field
// alone (the data attributes are the ones those extensions document), so a
// passphrase or an item's password typed here is not offered for saving
// somewhere else. They do not keep the browser's own manager out: the browser
// decides that, and ignores autoComplete="off" on a password field. The
// fields that take the person's passphrase say autoComplete="current-password"
// on purpose, which offers the browser's own saved passphrase if the person
// chose to save one; the fields that take an item's text say "off" or
// "new-password" and are only a request.
export const NO_PASSWORD_MANAGER = {
  'data-1p-ignore': true,
  'data-lpignore': 'true',
  'data-bwignore': 'true',
} as const

// secretNumberInputProps are the attributes of a card number or security code
// field. It is a text field masked with CSS where the browser can (so the
// browser does not offer to save a password or a card), and a password field
// where it cannot. isShown is the Show button: a plain text field.
export function secretNumberInputProps(isShown = false): { type: 'text' | 'password'; className?: string } {
  if (isShown) return { type: 'text' }
  const canMask =
    typeof CSS !== 'undefined' && typeof CSS.supports === 'function' && CSS.supports('-webkit-text-security', 'disc')
  return canMask ? { type: 'text', className: 'passwords-masked' } : { type: 'password' }
}

// How long a copied secret may stay on the clipboard.
export const CLIPBOARD_CLEAR_MILLISECONDS = 30_000

// clearClipboardLater empties the clipboard 30 seconds after a copy, but only
// if it still holds the copied secret (the person may have copied something
// else since, and that is theirs). The browser refuses to read the clipboard
// unless the window has focus, and at 30 seconds the person is usually in the
// site's own tab, pasting. So when the window has no focus then, the clear
// waits for the window to be focused or shown again and is tried then; it
// gives up only when the page is gone. A browser that will not let the page
// read the clipboard is left alone, since the page could not tell whose text
// it is about to remove. It is not tied to a component, so it still runs when
// the person has moved to another screen.
export function clearClipboardLater(copied: string) {
  let isWaitingForFocus = false

  function waitForFocus() {
    if (isWaitingForFocus) return
    isWaitingForFocus = true
    const tryAgain = () => {
      if (!document.hasFocus() || document.visibilityState === 'hidden') return
      window.removeEventListener('focus', tryAgain)
      document.removeEventListener('visibilitychange', tryAgain)
      isWaitingForFocus = false
      void clearIfUnchanged()
    }
    window.addEventListener('focus', tryAgain)
    document.addEventListener('visibilitychange', tryAgain)
  }

  async function clearIfUnchanged() {
    if (!document.hasFocus()) {
      waitForFocus()
      return
    }
    try {
      if ((await navigator.clipboard.readText()) === copied) await navigator.clipboard.writeText('')
    } catch {
      // Focus can go between the check and the read. If it did, the next
      // focus tries again; otherwise the clipboard could not be read or
      // written, and there is nothing to do.
      if (!document.hasFocus()) waitForFocus()
    }
  }

  window.setTimeout(() => void clearIfUnchanged(), CLIPBOARD_CLEAR_MILLISECONDS)
}

// SecretCopyButton copies a secret and clears it from the clipboard later.
export function SecretCopyButton({ value, label }: { value: string; label: string }) {
  return <CopyIconButton value={value} label={label} onCopied={clearClipboardLater} />
}

// SecretCopyTextButton is the labeled "Copy" button for a secret that sits in
// a row of other buttons (the recovery key), and clears the clipboard later
// like SecretCopyButton does.
export function SecretCopyTextButton({ value, label }: { value: string; label: string }) {
  const { t } = useTranslation()
  const toast = useToast()
  return (
    <button
      type="button"
      aria-label={label}
      onClick={() => {
        if (!navigator.clipboard) {
          toast.failed(t('common.copyFailed'))
          return
        }
        void navigator.clipboard.writeText(value).then(
          () => {
            clearClipboardLater(value)
            toast.done(t('common.copied'))
          },
          () => toast.failed(t('common.copyFailed')),
        )
      }}
    >
      {t('common.copy')}
    </button>
  )
}

// downloadText hands the browser a file to save. The address of the file is
// released after a second, not at once: some browsers have not started the
// download when the click returns, and would find nothing.
export function downloadText(fileName: string, content: string, contentType: string) {
  const file = new Blob([content], { type: contentType })
  const address = URL.createObjectURL(file)
  const link = document.createElement('a')
  link.href = address
  link.download = fileName
  document.body.appendChild(link)
  link.click()
  link.remove()
  window.setTimeout(() => URL.revokeObjectURL(address), 1000)
}

// PlainValue is a value that is not secret, with its copy button.
export function PlainValue({ label, value }: { label: string; value: string }) {
  const { t } = useTranslation()
  return (
    <div className="value-row">
      <span className="value-clamp">{value}</span>
      <CopyIconButton value={value} label={t('passwords.copyField', { field: label })} />
    </div>
  )
}

// SecretValue is a value that stays hidden until Show is pressed, with its
// copy button. It hides again when the item changes.
export function SecretValue({
  label,
  value,
  isMultiline = false,
}: {
  label: string
  value: string
  // isMultiline keeps the line breaks of a note when it is shown.
  isMultiline?: boolean
}) {
  const { t } = useTranslation()
  const [isShown, setIsShown] = useState(false)
  useEffect(() => setIsShown(false), [value])
  return (
    <div className="value-row">
      <span
        className={
          isShown && isMultiline ? 'passwords-multiline' : isShown ? 'value-clamp passwords-secret' : 'value-clamp'
        }
      >
        {isShown ? value : '••••••••••'}
      </span>
      <Tooltip
        label={isShown ? t('passwords.hideField', { field: label }) : t('passwords.showField', { field: label })}
      >
        <button
          type="button"
          className="icon-button copy-button"
          aria-label={isShown ? t('passwords.hideField', { field: label }) : t('passwords.showField', { field: label })}
          aria-pressed={isShown}
          onClick={() => setIsShown((previous) => !previous)}
        >
          {isShown ? <EyeOffIcon size={15} /> : <EyeIcon size={15} />}
        </button>
      </Tooltip>
      <SecretCopyButton value={value} label={t('passwords.copyField', { field: label })} />
    </div>
  )
}

// groupDigits writes a code in two halves, as authenticator apps do.
export function groupDigits(code: string): string {
  const middle = Math.ceil(code.length / 2)
  return code.length > 4 ? `${code.slice(0, middle)} ${code.slice(middle)}` : code
}

// OneTimeCodeValue shows the code a one-time code address makes now, and the
// seconds until it changes. The code is worked out here, each time, and is
// never stored.
export function OneTimeCodeValue({ uri }: { uri: string }) {
  const { t } = useTranslation()
  const settings: OneTimeCodeSettings | null = useMemo(() => {
    try {
      return parseOneTimeCodeUri(uri)
    } catch {
      return null
    }
  }, [uri])
  const [nowSeconds, setNowSeconds] = useState(() => Math.floor(Date.now() / 1000))
  const [code, setCode] = useState('')

  useEffect(() => {
    const timer = window.setInterval(() => setNowSeconds(Math.floor(Date.now() / 1000)), 1000)
    return () => window.clearInterval(timer)
  }, [])

  const step = settings ? Math.floor(nowSeconds / settings.periodSeconds) : 0
  useEffect(() => {
    if (!settings) return
    let isCurrent = true
    void oneTimeCode(settings, step * settings.periodSeconds).then((made) => {
      if (isCurrent) setCode(made)
    })
    return () => {
      isCurrent = false
    }
  }, [settings, step])

  if (!settings) return <span className="muted">{t('passwords.oneTimeCodeUnreadable')}</span>
  return (
    <div className="value-row">
      <span className="passwords-code" aria-live="off">
        {groupDigits(code)}
      </span>
      <span className="muted passwords-code-seconds">
        {t('passwords.oneTimeCodeSeconds', { seconds: oneTimeCodeSecondsLeft(settings, nowSeconds) })}
      </span>
      <SecretCopyButton value={code} label={t('passwords.copyField', { field: t('passwords.fieldOneTimeCode') })} />
    </div>
  )
}

// RecoveryKeyPanel shows a recovery key with Copy and Download. The person
// ticks the box when they have saved it; the caller keeps that.
export function RecoveryKeyPanel({
  recoveryKeyText,
  isSaved,
  onSavedChange,
}: {
  recoveryKeyText: string
  isSaved: boolean
  onSavedChange: (isSaved: boolean) => void
}) {
  const { t } = useTranslation()

  function download() {
    downloadText(
      'teanode-passwords-recovery-key.txt',
      t('passwords.recoveryKeyFile', { recoveryKey: recoveryKeyText }),
      'text/plain;charset=utf-8',
    )
  }

  return (
    <>
      <pre className="secret passwords-recovery-key" aria-label={t('passwords.recoveryKeyLabel')}>
        {recoveryKeyText}
      </pre>
      <div className="page-actions">
        <SecretCopyTextButton value={recoveryKeyText} label={t('passwords.recoveryKeyCopy')} />
        <button type="button" onClick={download}>
          {t('passwords.recoveryKeyDownload')}
        </button>
      </div>
      <label className="checkbox is-plain">
        <input type="checkbox" checked={isSaved} onChange={(event) => onSavedChange(event.target.checked)} />
        <span>{t('passwords.recoveryKeySaved')}</span>
      </label>
    </>
  )
}

// PasswordGenerator draws a password from the options and hands every one it
// draws to the editor at once (when it opens, and whenever an option or
// "Another password" changes it), so one click gives a password. The panel
// stays open for changes until Done.
export function PasswordGenerator({
  onGenerated,
  onDone,
}: {
  onGenerated: (password: string) => void
  onDone: () => void
}) {
  const { t } = useTranslation()
  const [options, setOptions] = useState<PasswordOptions>(DEFAULT_PASSWORD_OPTIONS)
  const [drawCount, setDrawCount] = useState(0)
  const generated = useMemo(() => {
    void drawCount
    try {
      return generatePassword(options)
    } catch {
      // No kind of character chosen, or a length out of range.
      return ''
    }
  }, [options, drawCount])

  // The latest callback is kept in a ref so that a new function on each render
  // of the editor does not draw a new password.
  const onGeneratedRef = useRef(onGenerated)
  onGeneratedRef.current = onGenerated
  useEffect(() => {
    if (generated !== '') onGeneratedRef.current(generated)
  }, [generated])

  function change(changes: Partial<PasswordOptions>) {
    setOptions((previous) => ({ ...previous, ...changes }))
  }

  return (
    <fieldset className="passwords-generator">
      <legend>{t('passwords.generatorTitle')}</legend>
      <div className="secret-box">
        <pre className="secret" aria-label={t('passwords.generatorPreview')}>
          {generated}
        </pre>
        <Tooltip label={t('passwords.generatorAnother')}>
          <button
            type="button"
            className="icon-button copy-button"
            aria-label={t('passwords.generatorAnother')}
            onClick={() => setDrawCount((previous) => previous + 1)}
          >
            <RefreshIcon size={15} />
          </button>
        </Tooltip>
      </div>
      <label>
        <span>{t('passwords.generatorLength')}</span>
        <input
          type="number"
          min={4}
          max={MAXIMUM_PASSWORD_LENGTH}
          value={options.length}
          onChange={(event) => change({ length: Number(event.target.value) })}
        />
      </label>
      <div className="passwords-generator-kinds">
        <label className="checkbox is-plain">
          <input
            type="checkbox"
            checked={options.shouldIncludeLetters}
            onChange={(event) => change({ shouldIncludeLetters: event.target.checked })}
          />
          <span>{t('passwords.generatorLetters')}</span>
        </label>
        <label className="checkbox is-plain">
          <input
            type="checkbox"
            checked={options.shouldIncludeDigits}
            onChange={(event) => change({ shouldIncludeDigits: event.target.checked })}
          />
          <span>{t('passwords.generatorDigits')}</span>
        </label>
        <label className="checkbox is-plain">
          <input
            type="checkbox"
            checked={options.shouldIncludeSymbols}
            onChange={(event) => change({ shouldIncludeSymbols: event.target.checked })}
          />
          <span>{t('passwords.generatorSymbols')}</span>
        </label>
        <label className="checkbox is-plain">
          <input
            type="checkbox"
            checked={options.shouldAvoidLookAlikes}
            onChange={(event) => change({ shouldAvoidLookAlikes: event.target.checked })}
          />
          <span>{t('passwords.generatorLookAlikes')}</span>
        </label>
      </div>
      <div className="page-actions">
        <button type="button" onClick={onDone}>
          {t('passwords.generatorDone')}
        </button>
      </div>
    </fieldset>
  )
}

// SafetyCode is the short form of a person's public key, which two people
// compare to be sure the key is the one they think it is. It is made from the
// key alone, so nothing the server supplies goes into it.
export function SafetyCode({ publicKey }: { publicKey: string }) {
  const { t } = useTranslation()
  const [code, setCode] = useState('')
  const [isUnreadable, setIsUnreadable] = useState(false)
  useEffect(() => {
    let isCurrent = true
    setCode('')
    setIsUnreadable(false)
    // A key that is not valid base64 throws here, not in the promise.
    let keyBytes: Uint8Array
    try {
      keyBytes = decodeBase64(publicKey)
    } catch {
      setIsUnreadable(true)
      return
    }
    safetyCode(keyBytes).then(
      (made) => {
        if (isCurrent) setCode(made)
      },
      () => {
        if (isCurrent) setIsUnreadable(true)
      },
    )
    return () => {
      isCurrent = false
    }
  }, [publicKey])
  if (isUnreadable) return <span className="muted">{t('passwords.safetyCodeUnreadable')}</span>
  return <code className="passwords-safety-code">{code}</code>
}

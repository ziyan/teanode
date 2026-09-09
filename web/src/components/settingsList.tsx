import { useTranslation } from '../i18n/i18n'

// The shape every list of account things takes: a heading with the one action
// that adds to it, then a row per thing — what it is, what is true of it, and
// what can be done to it.
//
// Rows rather than a table. A token and a session each carry a name, two or
// three dates, an address and two actions, and a table wide enough for all of
// that is a table nobody can read on a laptop, let alone a phone. A row can
// wrap; a column cannot.

export function SettingsSection({
  title,
  description,
  action,
  card,
  children,
}: {
  title?: string
  description?: React.ReactNode
  action?: React.ReactNode
  // Draw the section as a panel. Pages that show one list on a page let it
  // sit on the page itself; a page of several lists, or a tab beside other
  // tabs that are panels, asks for the border so the lists do not run
  // together.
  card?: boolean
  children?: React.ReactNode
}) {
  return (
    <section className={card ? 'settings-section card' : 'settings-section'}>
      {(title || action) && (
        <div className="settings-section-head">
          <div>
            {title && <h3>{title}</h3>}
            {description && <p className="muted">{description}</p>}
          </div>
          {action}
        </div>
      )}
      {children}
    </section>
  )
}

export function SettingsRow({
  title,
  subtitle,
  badge,
  actions,
  avatar,
}: {
  title: React.ReactNode
  subtitle?: React.ReactNode
  badge?: React.ReactNode
  actions?: React.ReactNode
  // A name to draw a monogram from, for a row that is about a person. The
  // face of a list of people, so a row is found by its shape before it is
  // read.
  avatar?: string
}) {
  return (
    <div className="settings-row">
      {avatar ? (
        <span className="settings-row-avatar" aria-hidden="true">
          {(Array.from(avatar.trim())[0] ?? '?').toUpperCase()}
        </span>
      ) : null}
      <div className="settings-row-body">
        <div className="settings-row-title">
          <strong>{title}</strong>
          {badge}
        </div>
        {subtitle && <div className="muted settings-row-subtitle">{subtitle}</div>}
      </div>
      {actions && <div className="settings-row-actions">{actions}</div>}
    </div>
  )
}

// Said rather than shown as an error: having none of something is ordinary.
export function SettingsEmpty({ children }: { children: React.ReactNode }) {
  return <p className="settings-empty muted">{children}</p>
}

// The secret behind a token, shown once. A dialog rather than a banner among
// the rows: what is in it cannot be shown again, so it should be the only
// thing on the screen until it has been dealt with. It does not close on the
// backdrop or on Escape for the same reason — every other dialog here can be
// dismissed by accident without cost, and this one cannot be reopened.
export function SecretDialog({
  title,
  intro,
  secret,
  extra,
  onDone,
}: {
  title: string
  intro: React.ReactNode
  secret: string
  extra?: React.ReactNode
  onDone: () => void
}) {
  const { t } = useTranslation()

  return (
    <div className="dialog-scrim">
      <div className="dialog dialog-wide" role="alertdialog" aria-modal="true" aria-label={title}>
        <h3>{title}</h3>
        <p className="muted">{intro}</p>
        <pre className="secret">{secret}</pre>
        {extra}
        <div className="dialog-actions">
          <CopyButton value={secret} />
          <button className="primary" type="button" onClick={onDone}>
            {t('common.done')}
          </button>
        </div>
      </div>
    </div>
  )
}

// Copy, when the browser will. The clipboard API is absent on an insecure
// origin, which a deployment behind plain HTTP is; the value is on the screen
// either way, so this offers what it can rather than failing silently.
export function CopyButton({ value }: { value: string }) {
  const { t } = useTranslation()

  return (
    <button
      type="button"
      onClick={(event) => {
        const button = event.currentTarget
        if (!navigator.clipboard) {
          button.textContent = t('common.copyFailed')
          return
        }
        void navigator.clipboard.writeText(value).then(
          () => {
            button.textContent = t('common.copied')
          },
          () => {
            button.textContent = t('common.copyFailed')
          },
        )
      }}
    >
      {t('common.copy')}
    </button>
  )
}

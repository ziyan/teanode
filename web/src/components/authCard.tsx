import { Logo } from './logo'
import { useTranslation } from '../i18n/i18n'

// The card on the two pages you can reach without an account: signing in, and
// claiming a server that has none.
//
// Shared so they cannot drift. They were the same markup written twice with
// slightly different spacing, and the second one had grown an extra paragraph
// that made it read as a different product.
//
// Centred, and identified before it asks for anything. The mark and the name
// say what you have reached, which matters on a page with no navigation, no
// heading and nothing else on it. Before, the card opened with a small mark
// beside "TeaNode" in the top-left corner and went straight into a field.
export function AuthCard({
  purpose,
  footnote,
  children,
  onSubmit,
}: {
  // Only where it is news. A card with a username, a password and a button
  // marked "Sign in" does not need telling what it is for; "this server has
  // no account yet" is something the reader does not already know.
  purpose?: string
  footnote?: React.ReactNode
  children: React.ReactNode
  onSubmit: (event: React.FormEvent) => void
}) {
  const { t } = useTranslation()

  return (
    <form className="auth-card" onSubmit={onSubmit}>
      <div className="auth-header">
        <Logo size={40} />
        <h1>{t('app.name')}</h1>
        {purpose && <p className="muted">{purpose}</p>}
      </div>

      {children}

      {footnote && <div className="auth-footnote muted">{footnote}</div>}
    </form>
  )
}

// A labelled field. The auth pages are the only forms somebody meets before
// they have learned anything about this dashboard, so their fields are a
// little larger than the ones inside it.
export function AuthField({
  label,
  hint,
  ...input
}: { label: string; hint?: React.ReactNode } & React.InputHTMLAttributes<HTMLInputElement>) {
  return (
    <label className="auth-field">
      <span>{label}</span>
      <input {...input} />
      {hint}
    </label>
  )
}

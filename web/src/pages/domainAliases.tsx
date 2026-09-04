import { useState } from 'react'
import { useParams } from 'react-router-dom'

import { graphql } from '../api'
import { Tag } from '../components/common'
import { TrashIcon } from '../components/icons'
import { useTranslation } from '../i18n/i18n'
import { DomainTabProps } from './domainTabs'

const CREATE_ALIAS = `
  mutation ($domainId: String!, $pattern: String!, $kind: String!, $email: String, $webhook: String) {
    CreateAlias(domainId: $domainId, aliasParameters: { pattern: $pattern, kind: $kind, email: $email, webhook: $webhook }) {
      id
    }
  }`
const DELETE_ALIAS = `mutation ($aliasId: String!) { DeleteAlias(aliasId: $aliasId) }`

// Who receives mail for this domain. The most-used page of a forwarding
// server, and the reason it has a tab of its own rather than being the fourth
// screen of a scroll.
export function DomainAliasesTab({ domain, run }: DomainTabProps) {
  const { t } = useTranslation()
  const { domainId } = useParams()

  const [pattern, setPattern] = useState('')
  const [kind, setKind] = useState('email')
  const [destination, setDestination] = useState('')

  return (
    <div className="card">
      <h3>{t('domain.aliasesTitle')}</h3>
      <p className="muted" style={{ marginTop: 0 }}>
        {t('domain.aliasesIntro')}
      </p>
      <table>
        <thead>
          <tr>
            <th>{t('domain.pattern')}</th>
            <th>{t('domain.goesTo')}</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          {domain.aliases.map((alias) => (
            <tr key={alias.id}>
              <td className="mono">
                {alias.pattern || <span className="muted">{t('domain.catchAll')}</span>}
                {alias.disabled && ' '}
                {alias.disabled && <Tag value={t('domain.disabled')} />}
              </td>
              <td>
                {alias.kind === 'email' && alias.email}
                {alias.kind === 'webhook' && <span className="mono">{alias.webhook}</span>}
                {alias.kind === 'mailServer' && alias.mailServer && (
                  <span className="mono">
                    {alias.mailServer.host}:{alias.mailServer.port}
                  </span>
                )}
                {alias.kind === 'null' && <span className="muted">{t('domain.discarded')}</span>}
              </td>
              <td className="shrink">
                <button
                  className="icon-button danger"
                  aria-label={t('common.remove')}
                  title={t('common.remove')}
                  onClick={() => void run(() => graphql(DELETE_ALIAS, { aliasId: alias.id }))}
                >
                  <TrashIcon />
                </button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>

      <form
        className="row"
        style={{ marginTop: 12 }}
        onSubmit={(event) => {
          event.preventDefault()
          void run(async () => {
            await graphql(CREATE_ALIAS, {
              domainId,
              pattern,
              kind,
              email: kind === 'email' ? destination : null,
              webhook: kind === 'webhook' ? destination : null,
            })
            setPattern('')
            setDestination('')
          })
        }}
      >
        <label style={{ margin: 0 }}>
          <span>{t('domain.pattern')}</span>
          {/* Left blank it is a catch-all, which is not guessable from an
              empty box, so the placeholder says so. */}
          <input
            value={pattern}
            onChange={(event) => setPattern(event.target.value)}
            placeholder={t('domain.patternPlaceholder')}
          />
        </label>
        <label style={{ margin: 0, maxWidth: 140 }}>
          <span>{t('domain.kind')}</span>
          <select value={kind} onChange={(event) => setKind(event.target.value)}>
            <option value="email">{t('domain.kindEmail')}</option>
            <option value="webhook">{t('domain.kindWebhook')}</option>
            <option value="null">{t('domain.kindDiscard')}</option>
          </select>
        </label>
        {kind !== 'null' && (
          <label style={{ margin: 0 }}>
            <span>{kind === 'email' ? t('domain.forwardTo') : t('domain.postTo')}</span>
            <input value={destination} onChange={(event) => setDestination(event.target.value)} />
          </label>
        )}
        <div className="shrink">
          <button className="primary" type="submit">
            {t('domain.addAlias')}
          </button>
        </div>
      </form>
    </div>
  )
}

import { useState } from 'react'

import { graphql } from '../../api'
import { ErrorMessage, Loading, Tag, formatTime } from '../../components/common'
import { TrashIcon } from '../../components/icons'
import { RelativeTime } from '../../components/relativeTime'
import { Tooltip } from '../../components/tooltip'
import { useQuery } from '../../components/useQuery'
import { ConfirmDialog, FormDialog } from '../../components/dialog'
import { SecretDialog, SettingsEmpty, SettingsRow, SettingsSection } from '../../components/settingsList'
import { useTranslation } from '../../i18n/i18n'

const TOKENS = `
  query ($includeRevoked: Boolean) {
    ListTokens(includeRevoked: $includeRevoked) {
      id name username created expires lastUsed lastUsedIp revoked
    }
  }`

const CREATE = `
  mutation ($name: String!, $lifetime: String) {
    CreateToken(name: $name, lifetime: $lifetime) {
      secret
      token { id name username created expires lastUsed lastUsedIp revoked }
    }
  }`

const REVOKE = `mutation ($tokenId: String!) { DeleteToken(tokenId: $tokenId) }`

type Token = {
  id: string
  name: string
  username: string
  created: string
  expires?: string | null
  lastUsed?: string | null
  lastUsedIp?: string | null
  revoked?: string | null
}

// A handful of sensible answers rather than a number to type. A field that
// accepts 137 invites somebody to think about which number rather than which
// policy.
const LIFETIMES = [
  { value: '720h', label: 'tokens.lifetime30' },
  { value: '2160h', label: 'tokens.lifetime90' },
  { value: '8760h', label: 'tokens.lifetime365' },
  { value: '', label: 'tokens.lifetimeNever' },
] as const

export function TokensPage() {
  const { t } = useTranslation()
  const [includeRevoked, setIncludeRevoked] = useState(false)
  const { data, error, loading, reload } = useQuery(
    () => graphql<{ ListTokens: Token[] }>(TOKENS, { includeRevoked }),
    [includeRevoked],
  )

  const [adding, setAdding] = useState(false)
  const [name, setName] = useState('')
  const [lifetime, setLifetime] = useState<string>('2160h')
  const [issued, setIssued] = useState<string | null>(null)
  const [revoking, setRevoking] = useState<Token | null>(null)
  const [busy, setBusy] = useState(false)
  const [problem, setProblem] = useState<string | null>(null)

  async function run(work: () => Promise<unknown>) {
    setBusy(true)
    setProblem(null)
    try {
      await work()
      await reload()
    } catch (caught) {
      setProblem(caught instanceof Error ? caught.message : t('domain.failed'))
    } finally {
      setBusy(false)
    }
  }

  const tokens = data?.ListTokens ?? []

  return (
    <>
      <SettingsSection
        description={t('tokens.intro')}
        action={
          <>
            {/* What the list shows, changed and looked at, rather than a
                setting kept: a button, like the one on the sessions page. */}
            <button
              type="button"
              className={includeRevoked ? 'active' : undefined}
              aria-pressed={includeRevoked}
              onClick={() => setIncludeRevoked((previous) => !previous)}
            >
              {includeRevoked ? t('tokens.hideRevoked') : t('tokens.showRevoked')}
            </button>
            <button className="primary" type="button" onClick={() => setAdding(true)}>
              {t('tokens.new')}
            </button>
          </>
        }
      >
        <ErrorMessage error={problem} />
        {loading && !data && <Loading />}
        {error ? <ErrorMessage error={error} /> : null}
        {data && tokens.length === 0 && <SettingsEmpty>{t('tokens.empty')}</SettingsEmpty>}

        {tokens.map((token) => (
          <SettingsRow
            key={token.id}
            title={token.name || t('tokens.unnamed')}
            badge={<Tag value={describeExpiry(token, t)} tone={expiryTone(token)} />}
            subtitle={
              <>
                <div className="mono">{token.id}</div>
                <div>
                  {t('tokens.createdAt', { time: formatTime(token.created) })}
                  {' · '}
                  {token.lastUsed ? (
                    <>
                      {t('tokens.lastUsedLabel')} <RelativeTime value={token.lastUsed} />
                      {token.lastUsedIp ? ` (${token.lastUsedIp})` : ''}
                    </>
                  ) : (
                    t('tokens.neverUsed')
                  )}
                  {token.username ? ` · ${t('tokens.actsAsName', { username: token.username })}` : ''}
                </div>
              </>
            }
            actions={
              token.revoked ? undefined : (
                <Tooltip label={t('tokens.revoke')}>
                  <button
                    className="icon-action danger"
                    type="button"
                    aria-label={`${token.name || t('tokens.unnamed')}: ${t('tokens.revoke')}`}
                    onClick={() => setRevoking(token)}
                  >
                    <TrashIcon size={16} />
                  </button>
                </Tooltip>
              )
            }
          />
        ))}
      </SettingsSection>

      {adding && (
        <FormDialog
          title={t('tokens.new')}
          submitLabel={t('common.create')}
          busy={busy}
          error={problem}
          canSubmit={name.length > 0}
          onClose={() => {
            setAdding(false)
            setProblem(null)
          }}
          onSubmit={() =>
            void run(async () => {
              const result = await graphql<{ CreateToken: { secret: string } }>(CREATE, {
                name,
                lifetime: lifetime || null,
              })
              setIssued(result.CreateToken.secret)
              setName('')
              setAdding(false)
            })
          }
        >
          <label>
            <span>{t('tokens.name')}</span>
            <input
              value={name}
              onChange={(event) => setName(event.target.value)}
              placeholder={t('tokens.namePlaceholder')}
            />
          </label>
          <label>
            <span>{t('tokens.lifetime')}</span>
            <select value={lifetime} onChange={(event) => setLifetime(event.target.value)}>
              {LIFETIMES.map((option) => (
                <option key={option.value} value={option.value}>
                  {t(option.label)}
                </option>
              ))}
            </select>
          </label>
        </FormDialog>
      )}

      {issued && (
        <SecretDialog
          title={t('tokens.saveNow')}
          intro={t('tokens.shownOnce')}
          secret={issued}
          extra={
            <>
              <p className="muted">{t('tokens.useIt')}</p>
              <pre className="secret">{`export TEANODE_URL=${window.location.origin}\nexport TEANODE_TOKEN=${issued}`}</pre>
            </>
          }
          onDone={() => setIssued(null)}
        />
      )}

      {revoking && (
        <ConfirmDialog
          title={t('tokens.revokeTitle')}
          body={t('tokens.revokeBody', { name: revoking.name || t('tokens.unnamed') })}
          confirmLabel={t('tokens.revoke')}
          busy={busy}
          onConfirm={() => {
            const tokenId = revoking.id
            setRevoking(null)
            void run(() => graphql(REVOKE, { tokenId }))
          }}
          onClose={() => setRevoking(null)}
        />
      )}
    </>
  )
}

type Translate = ReturnType<typeof useTranslation>['t']

function describeExpiry(token: Token, t: Translate): string {
  if (!token.expires) {
    return t('common.never')
  }
  if (new Date(token.expires).getTime() < Date.now()) {
    return t('common.expired')
  }
  return t('tokens.expiresAt', { time: formatTime(token.expires) })
}

// An expired token is spent rather than broken, and a token that never expires
// is a standing risk worth a color that says so quietly.
function expiryTone(token: Token): 'good' | 'bad' | 'warn' | undefined {
  if (!token.expires) {
    return 'warn'
  }
  return new Date(token.expires).getTime() < Date.now() ? 'bad' : undefined
}

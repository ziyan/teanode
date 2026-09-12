import { useCallback, useEffect, useState } from 'react'

import { graphql } from '../../api'
import { SettingsEmpty, SettingsRow, SettingsSection } from '../../components/settingsList'
import { Tag } from '../../components/common'
import { Tooltip } from '../../components/tooltip'
import { TrashIcon } from '../../components/icons'
import { ConfirmDialog } from '../../components/dialog'
import { useTranslation } from '../../i18n/i18n'

// Skills: tools that arrive without a release. The operator installs one
// from the registry and everybody's agent is offered what it declares,
// under the ordinary tool policy.

type SkillTool = { name: string; description: string; kind: string; needsComputer: boolean }

type Skill = {
  name: string
  description: string
  version: string
  publisher: string
  enabled: boolean
  readable: boolean
  problem?: string
  secrets: string[]
  tools: SkillTool[]
}

type Offer = {
  name: string
  description: string
  version: string
  tags: string[]
  installed?: string
  newer: boolean
}

const INSTALLED = `query {
  ListAgentSkills { name description version publisher enabled readable problem secrets
    tools { name description kind needsComputer } }
}`

const OFFERED = `query { SearchAgentSkills { name description version tags installed newer } }`

const INSTALL = `mutation ($name: String!) {
  InstallAgentSkill(name: $name) { name version }
}`

const REMOVE = `mutation ($name: String!) { RemoveAgentSkill(name: $name) }`

const SET_ENABLED = `mutation ($name: String!, $enabled: Boolean!) {
  SetAgentSkillEnabled(name: $name, enabled: $enabled) { name enabled }
}`

export function SkillsSection() {
  const { t } = useTranslation()
  const [installed, setInstalled] = useState<Skill[] | null>(null)
  const [offers, setOffers] = useState<Offer[] | null>(null)
  const [busy, setBusy] = useState('')
  const [problem, setProblem] = useState<string | null>(null)
  const [removing, setRemoving] = useState<Skill | null>(null)
  const [browsing, setBrowsing] = useState(false)

  const read = useCallback(async () => {
    const answer = await graphql<{ ListAgentSkills: Skill[] }>(INSTALLED)
    setInstalled(answer.ListAgentSkills)
  }, [])

  useEffect(() => {
    void read().catch(() => setInstalled([]))
  }, [read])

  // The registry is only asked when somebody opens the list, because it is
  // a fetch out of this server.
  const browse = async () => {
    setBrowsing(true)
    setProblem(null)
    try {
      const answer = await graphql<{ SearchAgentSkills: Offer[] }>(OFFERED)
      setOffers(answer.SearchAgentSkills)
    } catch (reason) {
      setProblem(reason instanceof Error ? reason.message : String(reason))
      setBrowsing(false)
    }
  }

  const act = async (name: string, work: () => Promise<unknown>) => {
    setBusy(name)
    setProblem(null)
    try {
      await work()
      await read()
      if (offers) {
        const answer = await graphql<{ SearchAgentSkills: Offer[] }>(OFFERED)
        setOffers(answer.SearchAgentSkills)
      }
    } catch (reason) {
      setProblem(reason instanceof Error ? reason.message : String(reason))
    } finally {
      setBusy('')
    }
  }

  const behind = (installed ?? []).length > 0 && (offers ?? []).some((offer) => offer.newer)

  return (
    <>
      <SettingsSection
        card
        title={t('agentSettings.skills')}
        description={t('agentSettings.skillsDescription')}
        action={
          <button type="button" className="primary" onClick={() => void browse()} disabled={browsing && offers === null}>
            {t('agentSettings.browseSkills')}
          </button>
        }
      >
        {problem ? <p className="form-error">{problem}</p> : null}
        {installed !== null && installed.length === 0 ? <SettingsEmpty>{t('agentSettings.noSkills')}</SettingsEmpty> : null}
        {(installed ?? []).map((skill) => {
          const detail = [skill.description]
          if (skill.tools.length > 0) detail.push(t('agentSettings.skillBrings', { tools: skill.tools.map((tool) => tool.name).join(', ') }))
          if (skill.secrets.length > 0) detail.push(t('agentSettings.skillNeeds', { keys: skill.secrets.join(', ') }))
          if (!skill.readable) detail.push(skill.problem ?? '')
          return (
            <SettingsRow
              key={skill.name}
              title={skill.name}
              badge={
                <>
                  <Tag value={skill.version} />
                  {skill.tools.some((tool) => tool.needsComputer) && <Tag value={t('agentSettings.skillRunsCommands')} />}
                  {!skill.readable && <Tag value={t('agentSettings.skillUnreadable')} />}
                  {!skill.enabled && <Tag value={t('agentSettings.disabled')} />}
                </>
              }
              subtitle={detail.filter(Boolean).join(' · ')}
              actions={
                <>
                  <button
                    type="button"
                    disabled={busy === skill.name}
                    onClick={() => void act(skill.name, () => graphql(SET_ENABLED, { name: skill.name, enabled: !skill.enabled }))}
                  >
                    {skill.enabled ? t('agentSettings.skillDisable') : t('agentSettings.skillEnable')}
                  </button>
                  <Tooltip label={t('agentSettings.remove')}>
                    <button
                      type="button"
                      className="icon-action danger"
                      aria-label={`${skill.name}: ${t('agentSettings.remove')}`}
                      disabled={busy === skill.name}
                      onClick={() => setRemoving(skill)}
                    >
                      <TrashIcon size={16} />
                    </button>
                  </Tooltip>
                </>
              }
            />
          )
        })}
        {behind ? <p className="muted">{t('agentSettings.skillsBehind')}</p> : null}
      </SettingsSection>

      {offers !== null && (
        <SettingsSection card title={t('agentSettings.skillRegistry')} description={t('agentSettings.skillRegistryDescription')}>
          {offers.length === 0 ? <SettingsEmpty>{t('agentSettings.noOffers')}</SettingsEmpty> : null}
          {offers.map((offer) => (
            <SettingsRow
              key={offer.name}
              title={offer.name}
              badge={
                <>
                  <Tag value={offer.version} />
                  {offer.tags.map((tag) => (
                    <Tag key={tag} value={tag} />
                  ))}
                </>
              }
              subtitle={offer.description}
              actions={
                <button
                  type="button"
                  className={offer.installed && !offer.newer ? '' : 'primary'}
                  disabled={busy === offer.name || Boolean(offer.installed && !offer.newer)}
                  onClick={() => void act(offer.name, () => graphql(INSTALL, { name: offer.name }))}
                >
                  {offer.newer
                    ? t('agentSettings.skillUpdate', { version: offer.version })
                    : offer.installed
                      ? t('agentSettings.skillInstalled')
                      : t('agentSettings.skillInstall')}
                </button>
              }
            />
          ))}
        </SettingsSection>
      )}

      {removing !== null ? (
        <ConfirmDialog
          title={t('agentSettings.removeSkillTitle', { name: removing.name })}
          body={t('agentSettings.removeSkillBody')}
          confirmLabel={t('agentSettings.remove')}
          destructive
          busy={busy === removing.name}
          onClose={() => setRemoving(null)}
          onConfirm={() => {
            const skill = removing
            setRemoving(null)
            void act(skill.name, () => graphql(REMOVE, { name: skill.name }))
          }}
        />
      ) : null}
    </>
  )
}

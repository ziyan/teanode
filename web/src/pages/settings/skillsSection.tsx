import { Fragment, useCallback, useEffect, useState } from 'react'

import { graphql } from '../../api'
import { SettingsEmpty, SettingsRow, SettingsSection } from '../../components/settingsList'
import { Tag } from '../../components/common'
import { Select } from '../../components/select'
import { Tooltip } from '../../components/tooltip'
import { TrashIcon } from '../../components/icons'
import { ConfirmDialog, FormDialog } from '../../components/dialog'
import { useToast } from '../../components/toast'
import { useTranslation } from '../../i18n/i18n'
import { UPDATE } from './integrations'

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
  scope: string
  secrets: string[]
  personalSecrets: string[]
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
  ListAgentSkills { name description version publisher enabled readable problem scope secrets personalSecrets
    tools { name description kind needsComputer } }
}`

const OFFERED = `query { SearchAgentSkills { name description version tags installed newer } }`

// Which of the keys the installed skills ask the operator for are filled
// in, without the values: the settings say, and the row says so beside
// the button that fills one.
const FILLED = `query { GetSettings { agent { skillSecrets { skill key hasValue } } } }`

const INSTALL = `mutation ($name: String!) {
  InstallAgentSkill(name: $name) { name version }
}`

const REMOVE = `mutation ($name: String!) { RemoveAgentSkill(name: $name) }`

const SET_ENABLED = `mutation ($name: String!, $enabled: Boolean!) {
  SetAgentSkillEnabled(name: $name, enabled: $enabled) { name enabled }
}`

// Who fills a skill's values in here. The skill's author says which of its
// secrets are the deployment's and which are each person's own, and is
// usually right; but the same skill serves a household with one camera
// system and an office where everybody has their own, and only the
// operator knows which this is.
const SET_SCOPE = `mutation ($name: String!, $scope: String!) {
  SetAgentSkillScope(name: $name, scope: $scope) { name scope }
}`

export function SkillsSection() {
  const { t } = useTranslation()
  const [installed, setInstalled] = useState<Skill[] | null>(null)
  const [offers, setOffers] = useState<Offer[] | null>(null)
  const [busy, setBusy] = useState('')
  // What happened is said in a toast: a line of text at the top of the
  // section was hundreds of pixels from the button that caused it, so a
  // refused install looked like a button that did nothing.
  const toast = useToast()
  const [removing, setRemoving] = useState<Skill | null>(null)
  // The registry, narrowed by what is typed: name, description or a tag.
  const [filter, setFilter] = useState('')
  // The operator's keys that are filled in, as "skill/key".
  const [filled, setFilled] = useState<Set<string>>(new Set())
  // A skill whose keys are being filled in, and the values typed so far,
  // by key. A skill may ask for several, and they are filled in together.
  const [filling, setFilling] = useState<Skill | null>(null)
  const [values, setValues] = useState<Record<string, string>>({})
  const [problem, setProblem] = useState<string | null>(null)

  const read = useCallback(async () => {
    const answer = await graphql<{ ListAgentSkills: Skill[] }>(INSTALLED)
    setInstalled(answer.ListAgentSkills)
    try {
      const settings = await graphql<{
        GetSettings: { agent: { skillSecrets: { skill: string; key: string; hasValue: boolean }[] } }
      }>(FILLED)
      setFilled(
        new Set(
          settings.GetSettings.agent.skillSecrets
            .filter((secret) => secret.hasValue)
            .map((secret) => `${secret.skill}/${secret.key}`),
        ),
      )
    } catch {
      // Somebody who installs skills but does not manage the server sees
      // the keys without whether they are filled.
    }
  }, [])

  useEffect(() => {
    void read().catch(() => setInstalled([]))
  }, [read])

  // The registry is read once the section opens: what it offers is the
  // way to find a skill, and a button to ask for it was a step between
  // the person and the list.
  useEffect(() => {
    graphql<{ SearchAgentSkills: Offer[] }>(OFFERED)
      .then((answer) => setOffers(answer.SearchAgentSkills))
      .catch((reason) => {
        toast.failure(reason, t('agentSettings.skillRegistryFailed'))
        setOffers([])
      })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const keep = async () => {
    if (!filling) return
    // Only what was typed is sent: a key left blank keeps its value.
    const typed = filling.secrets
      .filter((key) => (values[key] ?? '') !== '')
      .map((key) => ({ skill: filling.name, key, value: values[key] }))
    if (typed.length === 0) {
      setFilling(null)
      return
    }
    setBusy(filling.name)
    setProblem(null)
    try {
      await graphql(UPDATE, { agent: { skillSecrets: typed } })
      toast.done(
        t('agentSettings.skillSecretsKept', {
          keys: typed.map((secret) => secret.key).join(', '),
          skill: filling.name,
        }),
      )
      setFilling(null)
      setValues({})
      await read()
    } catch (reason) {
      setProblem(reason instanceof Error ? reason.message : String(reason))
    } finally {
      setBusy('')
    }
  }

  const act = async (name: string, done: string, work: () => Promise<unknown>) => {
    setBusy(name)
    try {
      await work()
      await read()
      if (offers) {
        const answer = await graphql<{ SearchAgentSkills: Offer[] }>(OFFERED)
        setOffers(answer.SearchAgentSkills)
      }
      toast.done(done)
    } catch (reason) {
      toast.failure(reason, t('agentSettings.skillFailed', { name }))
    } finally {
      setBusy('')
    }
  }

  const behind = (installed ?? []).length > 0 && (offers ?? []).some((offer) => offer.newer)

  return (
    <>
      <SettingsSection card title={t('agentSettings.skills')} description={t('agentSettings.skillsDescription')}>
        {installed !== null && installed.length === 0 ? (
          <SettingsEmpty>{t('agentSettings.noSkills')}</SettingsEmpty>
        ) : null}
        {(installed ?? []).map((skill) => {
          const detail = [skill.description]
          if (skill.tools.length > 0)
            detail.push(t('agentSettings.skillBrings', { tools: skill.tools.map((tool) => tool.name).join(', ') }))
          if (skill.secrets.length > 0) detail.push(t('agentSettings.skillNeeds', { keys: skill.secrets.join(', ') }))
          if (skill.personalSecrets.length > 0)
            detail.push(t('agentSettings.skillNeedsPersonal', { keys: skill.personalSecrets.join(', ') }))
          if (!skill.readable) detail.push(skill.problem ?? '')
          return (
            <SettingsRow
              key={skill.name}
              title={skill.name}
              badge={
                <>
                  <Tag value={skill.version} />
                  {skill.tools.some((tool) => tool.needsComputer) && (
                    <Tag value={t('agentSettings.skillRunsCommands')} />
                  )}
                  {!skill.readable && <Tag value={t('agentSettings.skillUnreadable')} />}
                  {!skill.enabled && <Tag value={t('agentSettings.disabled')} />}
                </>
              }
              subtitle={detail.filter(Boolean).join(' · ')}
              actions={
                <>
                  {/* A key the skill asks the operator for, filled in here
                      rather than in a configuration file nobody can reach
                      on a running server. */}
                  {skill.secrets.length > 0 ? (
                    <button
                      type="button"
                      disabled={busy === skill.name}
                      onClick={() => {
                        setValues({})
                        setProblem(null)
                        setFilling(skill)
                      }}
                    >
                      {t('agentSettings.skillKeys', {
                        filled: String(skill.secrets.filter((key) => filled.has(`${skill.name}/${key}`)).length),
                        total: String(skill.secrets.length),
                      })}
                    </button>
                  ) : null}
                  {skill.secrets.length + skill.personalSecrets.length > 0 ? (
                    <Select
                      value={skill.scope}
                      disabled={busy === skill.name}
                      label={t('agentSettings.skillScope')}
                      options={[
                        { value: '', label: t('agentSettings.skillScopeDeclared') },
                        { value: 'operator', label: t('agentSettings.skillScopeOperator') },
                        { value: 'person', label: t('agentSettings.skillScopePerson') },
                      ]}
                      onChange={(value) =>
                        void act(skill.name, t('agentSettings.skillScopeSaved', { name: skill.name }), () =>
                          graphql(SET_SCOPE, { name: skill.name, scope: value }),
                        )
                      }
                    />
                  ) : null}
                  <button
                    type="button"
                    disabled={busy === skill.name}
                    onClick={() =>
                      void act(
                        skill.name,
                        skill.enabled
                          ? t('agentSettings.skillDisabled', { name: skill.name })
                          : t('agentSettings.skillEnabled', { name: skill.name }),
                        () => graphql(SET_ENABLED, { name: skill.name, enabled: !skill.enabled }),
                      )
                    }
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

      <SettingsSection
        card
        title={t('agentSettings.skillRegistry')}
        description={t('agentSettings.skillRegistryDescription')}
        action={
          <input
            type="search"
            value={filter}
            placeholder={t('agentSettings.skillRegistryFilter')}
            aria-label={t('agentSettings.skillRegistryFilter')}
            onChange={(event) => setFilter(event.target.value)}
          />
        }
      >
        {offers === null ? <p className="muted">{t('agentSettings.skillRegistryLoading')}</p> : null}
        {offers !== null && offers.length === 0 ? <SettingsEmpty>{t('agentSettings.noOffers')}</SettingsEmpty> : null}
        {(offers ?? [])
          .filter((offer) => {
            const words = filter.trim().toLowerCase()
            if (!words) return true
            return [offer.name, offer.description, ...offer.tags].some((text) => text.toLowerCase().includes(words))
          })
          .map((offer) => (
            <Fragment key={offer.name}>
              <SettingsRow
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
                    onClick={() =>
                      void act(
                        offer.name,
                        t('agentSettings.skillInstalledToast', { name: offer.name, version: offer.version }),
                        () => graphql(INSTALL, { name: offer.name }),
                      )
                    }
                  >
                    {offer.newer
                      ? t('agentSettings.skillUpdate', { version: offer.version })
                      : offer.installed
                        ? t('agentSettings.skillInstalled')
                        : t('agentSettings.skillInstall')}
                  </button>
                }
              />
            </Fragment>
          ))}
      </SettingsSection>

      {filling !== null ? (
        <FormDialog
          title={t('agentSettings.skillSecretTitle', { skill: filling.name })}
          submitLabel={t('common.save')}
          busy={busy === filling.name}
          error={problem}
          onClose={() => setFilling(null)}
          onSubmit={() => void keep()}
        >
          <p className="muted">{t('agentSettings.skillSecretHint')}</p>
          {filling.secrets.map((key, index) => {
            const has = filled.has(`${filling.name}/${key}`)
            return (
              <label key={key}>
                <span>
                  {key} · {has ? t('agentSettings.skillKeyFilled') : t('agentSettings.skillKeyEmpty')}
                </span>
                <input
                  autoFocus={index === 0}
                  type="password"
                  autoComplete="off"
                  placeholder={has ? t('agentSettings.skillKeyKeep') : ''}
                  value={values[key] ?? ''}
                  onChange={(event) => setValues({ ...values, [key]: event.target.value })}
                />
              </label>
            )
          })}
        </FormDialog>
      ) : null}

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
            void act(skill.name, t('agentSettings.skillRemoved', { name: skill.name }), () =>
              graphql(REMOVE, { name: skill.name }),
            )
          }}
        />
      ) : null}
    </>
  )
}

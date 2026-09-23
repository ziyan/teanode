import { useCallback, useEffect, useState } from 'react'

import { graphql } from '../../api'
import { SettingsEmpty, SettingsRow, SettingsSection } from '../../components/settingsList'
import { Tag } from '../../components/common'
import { Tooltip } from '../../components/tooltip'
import { ListIcon, TrashIcon } from '../../components/icons'
import { ConfirmDialog, FormDialog } from '../../components/dialog'
import { Markdown } from '../../components/markdown'
import { useToast } from '../../components/toast'
import { useTranslation } from '../../i18n/i18n'
import { LIST_SOURCE_TYPES, SourceType, sourceTypeReaderKey } from '../../components/sourceTypeSettings'

// Source types: how a knowledge source is read. The operator installs one
// from the registry, or adds a file of their own, and everybody here can
// then add a source of it on their agent page.

type Offer = {
  name: string
  description: string
  version: string
  tags: string[]
  installed?: string | null
  newer: boolean
}

const OFFERED = `query { SearchAgentSourceTypes { name description version tags installed newer } }`

const INSTALL = `mutation ($name: String!) { InstallAgentSourceType(name: $name) { name version } }`

const ADD_LOCAL = `mutation ($content: String!) { AddLocalAgentSourceType(content: $content) { name version } }`

const REMOVE = `mutation ($name: String!) { RemoveAgentSourceType(name: $name) }`

export function SourceTypesSection() {
  const { t } = useTranslation()
  const toast = useToast()
  const [installed, setInstalled] = useState<SourceType[] | null>(null)
  const [offers, setOffers] = useState<Offer[] | null>(null)
  const [busy, setBusy] = useState('')
  const [removing, setRemoving] = useState<SourceType | null>(null)
  // The type whose guide is open.
  const [reading, setReading] = useState<SourceType | null>(null)
  // The registry, narrowed by what is typed: name, description or a tag.
  const [filter, setFilter] = useState('')
  // A file of the operator's own, pasted or chosen, while it is being added.
  const [addingLocal, setAddingLocal] = useState(false)
  const [localContent, setLocalContent] = useState('')
  const [localProblem, setLocalProblem] = useState<string | null>(null)

  const read = useCallback(async () => {
    const answer = await graphql<{ ListAgentSourceTypes: SourceType[] }>(LIST_SOURCE_TYPES)
    setInstalled(answer.ListAgentSourceTypes)
  }, [])

  useEffect(() => {
    void read().catch((reason) => {
      toast.failure(reason, t('sourceTypes.listFailed'))
      setInstalled([])
    })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [read])

  // Read once the section opens, as the skills' registry is.
  useEffect(() => {
    graphql<{ SearchAgentSourceTypes: Offer[] }>(OFFERED)
      .then((answer) => setOffers(answer.SearchAgentSourceTypes))
      .catch((reason) => {
        toast.failure(reason, t('sourceTypes.registryFailed'))
        setOffers([])
      })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const act = async (name: string, done: string, work: () => Promise<unknown>) => {
    setBusy(name)
    try {
      await work()
      await read()
      if (offers) {
        const answer = await graphql<{ SearchAgentSourceTypes: Offer[] }>(OFFERED)
        setOffers(answer.SearchAgentSourceTypes)
      }
      toast.done(done)
      return true
    } catch (reason) {
      toast.failure(reason, t('sourceTypes.failed', { name }))
      return false
    } finally {
      setBusy('')
    }
  }

  const addLocal = async () => {
    setBusy('local')
    setLocalProblem(null)
    try {
      const answer = await graphql<{ AddLocalAgentSourceType: { name: string; version: string } }>(ADD_LOCAL, {
        content: localContent,
      })
      toast.done(t('sourceTypes.localAdded', { name: answer.AddLocalAgentSourceType.name }))
      setAddingLocal(false)
      setLocalContent('')
      await read()
    } catch (reason) {
      // Said in the dialog, which stays open with the file still in it, and
      // in a toast, so a refusal is heard even with the dialog scrolled.
      const message = reason instanceof Error ? reason.message : String(reason)
      setLocalProblem(message)
      toast.failure(reason, t('sourceTypes.localFailed'))
    } finally {
      setBusy('')
    }
  }

  // A file chosen from disk lands in the box, where it can be read and
  // changed before it is sent.
  const chooseFile = (file: File | undefined) => {
    if (!file) return
    file.text().then(
      (text) => setLocalContent(text),
      (reason) => toast.failure(reason, t('sourceTypes.localUnreadable')),
    )
  }

  const isBehind = (installed ?? []).length > 0 && (offers ?? []).some((offer) => offer.newer)

  return (
    <>
      <SettingsSection
        card
        title={t('sourceTypes.title')}
        description={t('sourceTypes.description')}
        action={
          <button
            type="button"
            onClick={() => {
              setLocalContent('')
              setLocalProblem(null)
              setAddingLocal(true)
            }}
          >
            {t('sourceTypes.addLocal')}
          </button>
        }
      >
        {installed !== null && installed.length === 0 ? <SettingsEmpty>{t('sourceTypes.none')}</SettingsEmpty> : null}
        {(installed ?? []).map((sourceType) => {
          const detail = [sourceType.description]
          if (sourceType.runs.length > 0)
            detail.push(
              t('sourceTypes.runsOn', {
                places: sourceType.runs
                  .map((place) =>
                    place === 'server'
                      ? t('sourceTypes.runsServer')
                      : place === 'computer'
                        ? t('sourceTypes.runsComputer')
                        : place,
                  )
                  .join(', '),
              }),
            )
          if (sourceType.requires.length > 0)
            detail.push(t('sourceTypes.requires', { tools: sourceType.requires.join(', ') }))
          if (!sourceType.readable) detail.push(sourceType.problem ?? '')
          return (
            <SettingsRow
              key={sourceType.name}
              title={sourceType.name}
              badge={
                <>
                  {sourceType.version ? <Tag value={sourceType.version} /> : null}
                  {sourceType.isLocal ? <Tag value={t('sourceTypes.local')} tone="warn" /> : null}
                  {sourceType.readable ? <Tag value={t(sourceTypeReaderKey(sourceType))} /> : null}
                  {!sourceType.readable ? <Tag value={t('sourceTypes.unreadable')} tone="bad" /> : null}
                </>
              }
              subtitle={detail.filter(Boolean).join(' · ')}
              actions={
                <div className="row-actions">
                  {sourceType.guide.trim() !== '' ? (
                    <Tooltip label={t('sourceTypes.guide')}>
                      <button
                        type="button"
                        className="icon-action"
                        aria-label={`${sourceType.name}: ${t('sourceTypes.guide')}`}
                        onClick={() => setReading(sourceType)}
                      >
                        <ListIcon size={16} />
                      </button>
                    </Tooltip>
                  ) : null}
                  <Tooltip label={t('agentSettings.remove')}>
                    <button
                      type="button"
                      className="icon-action danger"
                      aria-label={`${sourceType.name}: ${t('agentSettings.remove')}`}
                      disabled={busy === sourceType.name}
                      onClick={() => setRemoving(sourceType)}
                    >
                      <TrashIcon size={16} />
                    </button>
                  </Tooltip>
                </div>
              }
            />
          )
        })}
        {isBehind ? <p className="muted">{t('sourceTypes.behind')}</p> : null}
      </SettingsSection>

      <SettingsSection
        card
        title={t('sourceTypes.registry')}
        description={t('sourceTypes.registryDescription')}
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
          .map((offer) => {
            const isCurrent = Boolean(offer.installed) && !offer.newer
            return (
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
                    className={isCurrent ? '' : 'primary'}
                    disabled={busy === offer.name || isCurrent}
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
            )
          })}
      </SettingsSection>

      {addingLocal ? (
        <FormDialog
          wide
          title={t('sourceTypes.addLocalTitle')}
          submitLabel={t('sourceTypes.addLocalSubmit')}
          busy={busy === 'local'}
          error={localProblem}
          canSubmit={localContent.trim() !== ''}
          onClose={() => setAddingLocal(false)}
          onSubmit={() => void addLocal()}
        >
          <p className="muted">{t('sourceTypes.addLocalHint')}</p>
          <label>
            <span>{t('sourceTypes.addLocalFile')}</span>
            <input
              type="file"
              accept=".md,text/markdown,text/plain"
              onChange={(event) => chooseFile(event.target.files?.[0])}
            />
          </label>
          <label>
            <span>{t('sourceTypes.addLocalContent')}</span>
            <textarea
              rows={14}
              className="mono"
              spellCheck={false}
              value={localContent}
              placeholder={'---\nname: notes-folder\ndescription: …\nreader: files\n---'}
              onChange={(event) => setLocalContent(event.target.value)}
            />
          </label>
        </FormDialog>
      ) : null}

      {reading !== null ? (
        <ConfirmDialog
          title={t('sourceTypes.guideTitle', { name: reading.name })}
          body={<Markdown text={reading.guide} />}
          onClose={() => setReading(null)}
        />
      ) : null}

      {removing !== null ? (
        <ConfirmDialog
          title={t('sourceTypes.removeTitle', { name: removing.name })}
          body={t('sourceTypes.removeBody')}
          confirmLabel={t('agentSettings.remove')}
          destructive
          busy={busy === removing.name}
          onClose={() => setRemoving(null)}
          onConfirm={() => {
            const sourceType = removing
            setRemoving(null)
            // Refused while a source is of this type; the server says which,
            // and the toast carries its words.
            void act(sourceType.name, t('sourceTypes.removed', { name: sourceType.name }), () =>
              graphql(REMOVE, { name: sourceType.name }),
            )
          }}
        />
      ) : null}
    </>
  )
}

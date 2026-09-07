import { useEffect, useState } from 'react'
import { Navigate, useNavigate, useParams } from 'react-router-dom'

import { Mailbox, MailboxAutoReply, MailboxFolder, MailboxRule, MailboxView, graphql } from '../api'
import { ErrorMessage, Loading, formatTime } from '../components/common'
import { RelativeTime } from '../components/relativeTime'
import { useQuery } from '../components/useQuery'
import { ConfirmDialog } from '../components/dialog'
import { Tabs, TabItem } from '../components/tabs'
import { Key, useTranslation } from '../i18n/i18n'
import { folderLabel, folderRows, useMailboxes } from '../mailboxes'

// What a mailbox is set up to do, in four tabs: what it is called and how
// it signs, its folders, the rules that sort what arrives, and the reply it
// sends while its owner is away. App passwords join when the IMAP server
// does, and addresses are managed on the domain: a mailbox is where
// addresses point, and pointing them is the domain manager's job.

const TABS: TabItem[] = [
  { id: 'general', label: 'mailboxSettings.tabGeneral' },
  { id: 'folders', label: 'mailboxSettings.tabFolders' },
  { id: 'rules', label: 'mailboxSettings.tabRules' },
  { id: 'autoreply', label: 'mailboxSettings.tabAutoReply' },
  { id: 'devices', label: 'mailboxSettings.tabDevices' },
]

const APP_PASSWORDS = `
  query ($mailboxId: String!) {
    ListMailboxAppPasswords(mailboxId: $mailboxId) { id name createdAt lastUsedAt }
    GetMailProgramSettings { imapHost imapPort imapsPort submissionHost submissionPort }
  }`

const CREATE_APP_PASSWORD = `
  mutation ($mailboxId: String!, $name: String!) {
    CreateMailboxAppPassword(mailboxId: $mailboxId, name: $name) { password username appPassword { id name } }
  }`

const DELETE_APP_PASSWORD = `
  mutation ($appPasswordId: String!) {
    DeleteMailboxAppPassword(appPasswordId: $appPasswordId)
  }`

const UPDATE = `
  mutation ($mailboxId: String!, $name: String, $signatureText: String, $signatureHtml: String, $rules: [MailboxRuleInput!], $autoReply: MailboxAutoReplyInput, $clearAutoReply: Boolean) {
    UpdateMailbox(mailboxId: $mailboxId, name: $name, signatureText: $signatureText, signatureHtml: $signatureHtml, rules: $rules, autoReply: $autoReply, clearAutoReply: $clearAutoReply) {
      mailbox { id }
    }
  }`

const CREATE_FOLDER = `
  mutation ($mailboxId: String!, $name: String!, $parentId: String) {
    CreateMailboxFolder(mailboxId: $mailboxId, name: $name, parentId: $parentId) { id }
  }`

const UPDATE_FOLDER = `
  mutation ($folderId: String!, $name: String, $parentId: String) {
    UpdateMailboxFolder(folderId: $folderId, name: $name, parentId: $parentId) { id }
  }`

const TEST_RULES = `
  query ($mailboxId: String!, $rules: [MailboxRuleInput!]!, $first: Int) {
    TestMailboxRules(mailboxId: $mailboxId, rules: $rules, first: $first) {
      matched
      item { id mail { from sender subject receivedAt } }
    }
  }`

const DELETE_FOLDER = `
  mutation ($folderId: String!) {
    DeleteMailboxFolder(folderId: $folderId)
  }`

export function MailboxSettingsPage() {
  const { tab } = useParams()
  const navigate = useNavigate()
  const { t } = useTranslation()
  const mailboxes = useMailboxes()

  if (!mailboxes.loaded) {
    return <Loading />
  }
  const view = mailboxes.current
  if (!view) {
    return <p className="muted">{t('mailbox.none')}</p>
  }
  if (!TABS.some((candidate) => candidate.id === tab)) {
    return <Navigate to="/mailbox/settings/general" replace />
  }
  return (
    <>
      <Tabs items={TABS} active={tab} onSelect={(id) => navigate(`/mailbox/settings/${id}`)} />
      {tab === 'general' && <GeneralTab key={view.mailbox.id} view={view} />}
      {tab === 'folders' && <FoldersTab key={view.mailbox.id} view={view} />}
      {tab === 'rules' && <RulesTab key={view.mailbox.id} view={view} />}
      {tab === 'autoreply' && <AutoReplyTab key={view.mailbox.id} view={view} />}
      {tab === 'devices' && <DevicesTab key={view.mailbox.id} view={view} />}
    </>
  )
}

// useSave is the save button's state: busy, failed, or saved.
function useSave() {
  const mailboxes = useMailboxes()
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<unknown>(null)
  const [saved, setSaved] = useState(false)
  const save = async (mutation: string, variables: Record<string, unknown>) => {
    setBusy(true)
    setSaved(false)
    try {
      await graphql(mutation, variables)
      await mailboxes.refresh()
      setError(null)
      setSaved(true)
    } catch (failure) {
      setError(failure)
    } finally {
      setBusy(false)
    }
  }
  return { busy, error, saved, save, touch: () => setSaved(false) }
}

function GeneralTab({ view }: { view: MailboxView }) {
  const { t } = useTranslation()
  const mailbox: Mailbox = view.mailbox
  const [name, setName] = useState(mailbox.name)
  const [signatureText, setSignatureText] = useState(mailbox.signatureText ?? '')
  const [signatureHtml, setSignatureHtml] = useState(mailbox.signatureHtml ?? '')
  const { busy, error, saved, save, touch } = useSave()

  const changed =
    name !== mailbox.name ||
    signatureText !== (mailbox.signatureText ?? '') ||
    signatureHtml !== (mailbox.signatureHtml ?? '')

  return (
    <>
      <form
        className="card form-narrow"
        onSubmit={(event) => {
          event.preventDefault()
          void save(UPDATE, { mailboxId: mailbox.id, name: name.trim(), signatureText, signatureHtml })
        }}
      >
        <label>
          {t('mailboxSettings.name')}
          <input
            value={name}
            onChange={(event) => {
              setName(event.target.value)
              touch()
            }}
            required
          />
        </label>
        <p className="muted field-hint">{t('mailboxSettings.nameHint')}</p>

        <label>
          {t('mailboxSettings.signatureText')}
          <textarea
            rows={4}
            value={signatureText}
            onChange={(event) => {
              setSignatureText(event.target.value)
              touch()
            }}
          />
        </label>
        <label>
          {t('mailboxSettings.signatureHtml')}
          <textarea
            rows={4}
            className="mono"
            value={signatureHtml}
            onChange={(event) => {
              setSignatureHtml(event.target.value)
              touch()
            }}
          />
        </label>
        <p className="muted field-hint">{t('mailboxSettings.signatureHint')}</p>

        {error ? <ErrorMessage error={error} /> : null}
        <div className="page-actions">
          <button className="primary" type="submit" disabled={busy || !changed || !name.trim()}>
            {t('common.save')}
          </button>
          {saved && !changed && <span className="muted">{t('common.saved')}</span>}
        </div>
      </form>

      <div className="card">
        <h3>{t('mailboxSettings.addresses')}</h3>
        {mailbox.addresses?.length ? (
          <table>
            <tbody>
              {mailbox.addresses.map((address) => (
                <tr key={address.aliasId}>
                  <td className="mono">{address.address}</td>
                  <td className="shrink muted">{address.domain}</td>
                </tr>
              ))}
            </tbody>
          </table>
        ) : (
          <p className="muted" style={{ margin: 0 }}>
            {t('mailbox.noAddress')}
          </p>
        )}
        <p className="muted field-hint">{t('mailboxSettings.addressesHint')}</p>
      </div>
    </>
  )
}

function FoldersTab({ view }: { view: MailboxView }) {
  const { t } = useTranslation()
  const { busy, error, save } = useSave()
  const [name, setName] = useState('')
  const [parentId, setParentId] = useState('')
  const [renaming, setRenaming] = useState<MailboxFolder | null>(null)
  const [renameTo, setRenameTo] = useState('')
  const [deleting, setDeleting] = useState<MailboxFolder | null>(null)
  const rows = folderRows(view.folders)

  return (
    <>
      <div className="card">
        <h3>{t('mailboxSettings.folders')}</h3>
        <table>
          <tbody>
            {rows.map(({ folder, depth }) => (
              <tr key={folder.id}>
                <td style={{ paddingLeft: 8 + depth * 20 }}>
                  {renaming?.id === folder.id ? (
                    <form
                      className="inline-form"
                      onSubmit={(event) => {
                        event.preventDefault()
                        void save(UPDATE_FOLDER, { folderId: folder.id, name: renameTo.trim() }).then(() => setRenaming(null))
                      }}
                    >
                      <input value={renameTo} onChange={(event) => setRenameTo(event.target.value)} autoFocus required />
                      <button type="submit" className="primary" disabled={busy || !renameTo.trim()}>
                        {t('common.save')}
                      </button>
                      <button type="button" onClick={() => setRenaming(null)}>
                        {t('common.cancel')}
                      </button>
                    </form>
                  ) : (
                    folderLabel(t, folder)
                  )}
                </td>
                <td className="shrink muted">{folder.total}</td>
                <td className="shrink">
                  {/* Only the owner's own folders can be renamed or removed;
                      the system folders are what the mailbox is. */}
                  {!folder.kind && renaming?.id !== folder.id && (
                    <div className="row-actions">
                      <button
                        type="button"
                        className="link"
                        onClick={() => {
                          setRenaming(folder)
                          setRenameTo(folder.name)
                        }}
                      >
                        {t('common.rename')}
                      </button>
                      <button type="button" className="link danger" onClick={() => setDeleting(folder)}>
                        {t('common.delete')}
                      </button>
                    </div>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      <form
        className="card form-narrow"
        onSubmit={(event) => {
          event.preventDefault()
          void save(CREATE_FOLDER, { mailboxId: view.mailbox.id, name: name.trim(), parentId: parentId || undefined }).then(() =>
            setName(''),
          )
        }}
      >
        <h3>{t('mailboxSettings.newFolder')}</h3>
        <label>
          {t('mailboxSettings.folderName')}
          <input value={name} onChange={(event) => setName(event.target.value)} required />
        </label>
        <label>
          {t('mailboxSettings.folderParent')}
          <select value={parentId} onChange={(event) => setParentId(event.target.value)}>
            <option value="">{t('mailboxSettings.folderTop')}</option>
            {rows.map(({ folder, depth }) => (
              <option key={folder.id} value={folder.id}>
                {'  '.repeat(depth) + folderLabel(t, folder)}
              </option>
            ))}
          </select>
        </label>
        {error ? <ErrorMessage error={error} /> : null}
        <div className="page-actions">
          <button className="primary" type="submit" disabled={busy || !name.trim()}>
            {t('common.create')}
          </button>
        </div>
      </form>

      {deleting && (
        <ConfirmDialog
          title={t('mailboxSettings.deleteFolder')}
          body={t('mailboxSettings.deleteFolderConfirm', { name: deleting.name, count: deleting.total })}
          confirmLabel={t('common.delete')}
          busy={busy}
          onConfirm={() => save(DELETE_FOLDER, { folderId: deleting.id }).then(() => setDeleting(null))}
          onClose={() => setDeleting(null)}
        />
      )}
    </>
  )
}

const FIELDS = ['from', 'to', 'subject', 'header', 'score', 'sender-known', 'any'] as const
const OPERATORS = ['contains', 'equals', 'matches', 'above', 'below'] as const
const ACTIONS = ['move', 'markRead', 'flag', 'forward', 'delete'] as const

const FIELD_LABELS: Record<string, Key> = {
  from: 'mailboxSettings.fieldFrom',
  to: 'mailboxSettings.fieldTo',
  subject: 'mailboxSettings.fieldSubject',
  header: 'mailboxSettings.fieldHeader',
  score: 'mailboxSettings.fieldScore',
  'sender-known': 'mailboxSettings.fieldSenderKnown',
  any: 'mailboxSettings.fieldAny',
}
const OPERATOR_LABELS: Record<string, Key> = {
  contains: 'mailboxSettings.operatorContains',
  equals: 'mailboxSettings.operatorEquals',
  matches: 'mailboxSettings.operatorMatches',
  above: 'mailboxSettings.operatorAbove',
  below: 'mailboxSettings.operatorBelow',
}
const ACTION_LABELS: Record<string, Key> = {
  move: 'mailboxSettings.actionMove',
  markRead: 'mailboxSettings.actionMarkRead',
  flag: 'mailboxSettings.actionFlag',
  forward: 'mailboxSettings.actionForward',
  delete: 'mailboxSettings.actionDelete',
}

function emptyRule(): MailboxRule {
  return {
    name: '',
    enabled: true,
    conditions: [{ field: 'from', operator: 'contains', value: '' }],
    actions: [{ kind: 'move', folderId: '' }],
    stop: false,
  }
}

// What the server accepts: every field the input type has, and nothing it
// does not — the objects that come back from a query carry a __typename
// and would be refused as input.
function cleanRules(rules: MailboxRule[]): MailboxRule[] {
  return rules.map((rule) => ({
    name: rule.name,
    enabled: rule.enabled,
    stop: rule.stop,
    conditions: rule.conditions.map((condition) => ({
      field: condition.field,
      header: condition.field === 'header' ? condition.header ?? '' : '',
      operator: ['sender-known', 'any'].includes(condition.field) ? '' : condition.operator,
      value: condition.value ?? '',
    })),
    actions: rule.actions.map((action) => ({
      kind: action.kind,
      folderId: action.kind === 'move' ? action.folderId ?? '' : '',
      address: action.kind === 'forward' ? action.address ?? '' : '',
    })),
  }))
}

function RulesTab({ view }: { view: MailboxView }) {
  const { t } = useTranslation()
  const { busy, error, saved, save, touch } = useSave()
  const [rules, setRules] = useState<MailboxRule[]>(() => cleanRules(view.mailbox.rules ?? []))
  const [dirty, setDirty] = useState(false)
  const folders = folderRows(view.folders)

  // The dry run: the rules as they are on the page, against the newest
  // messages in the Inbox, saved or not.
  type Trial = { matched: number[]; item: { id: string; mail?: { from?: string; sender?: string; subject?: string } | null } }
  const [trials, setTrials] = useState<Trial[] | null>(null)
  const [trying, setTrying] = useState(false)
  const [trialError, setTrialError] = useState<unknown>(null)
  const tryRules = async () => {
    setTrying(true)
    try {
      const response = await graphql<{ TestMailboxRules: Trial[] }>(TEST_RULES, {
        mailboxId: view.mailbox.id,
        rules: cleanRules(rules),
        first: 20,
      })
      setTrials(response.TestMailboxRules)
      setTrialError(null)
    } catch (failure) {
      setTrialError(failure)
    } finally {
      setTrying(false)
    }
  }

  const update = (index: number, change: (rule: MailboxRule) => MailboxRule) => {
    setRules((previous) => previous.map((rule, at) => (at === index ? change(rule) : rule)))
    setDirty(true)
    touch()
  }
  const move = (index: number, by: number) => {
    setRules((previous) => {
      const next = [...previous]
      const target = index + by
      if (target < 0 || target >= next.length) {
        return previous
      }
      ;[next[index], next[target]] = [next[target], next[index]]
      return next
    })
    setDirty(true)
    touch()
  }

  return (
    <form
      onSubmit={(event) => {
        event.preventDefault()
        void save(UPDATE, { mailboxId: view.mailbox.id, rules: cleanRules(rules) }).then(() => setDirty(false))
      }}
    >
      <p className="muted">{t('mailboxSettings.rulesHint')}</p>
      {rules.length === 0 && (
        <div className="card">
          <p className="muted" style={{ margin: 0 }}>
            {t('mailboxSettings.noRules')}
          </p>
        </div>
      )}
      {rules.map((rule, index) => (
        <div className="card rule" key={index}>
          <div className="rule-head">
            <input
              placeholder={t('mailboxSettings.ruleName')}
              aria-label={t('mailboxSettings.ruleName')}
              value={rule.name}
              onChange={(event) => update(index, (current) => ({ ...current, name: event.target.value }))}
              required
            />
            <label className="check">
              <input
                type="checkbox"
                checked={rule.enabled}
                onChange={(event) => update(index, (current) => ({ ...current, enabled: event.target.checked }))}
              />
              {t('mailboxSettings.ruleEnabled')}
            </label>
            <label className="check">
              <input
                type="checkbox"
                checked={rule.stop}
                onChange={(event) => update(index, (current) => ({ ...current, stop: event.target.checked }))}
              />
              {t('mailboxSettings.ruleStop')}
            </label>
            <div className="row-actions">
              <button type="button" className="link" disabled={index === 0} onClick={() => move(index, -1)}>
                {t('mailboxSettings.moveUp')}
              </button>
              <button type="button" className="link" disabled={index === rules.length - 1} onClick={() => move(index, 1)}>
                {t('mailboxSettings.moveDown')}
              </button>
              <button
                type="button"
                className="link danger"
                onClick={() => {
                  setRules((previous) => previous.filter((_, at) => at !== index))
                  setDirty(true)
                  touch()
                }}
              >
                {t('common.remove')}
              </button>
            </div>
          </div>

          <h4>{t('mailboxSettings.conditions')}</h4>
          {rule.conditions.map((condition, conditionIndex) => (
            <div className="rule-line" key={conditionIndex}>
              <select
                value={condition.field}
                onChange={(event) =>
                  update(index, (current) => ({
                    ...current,
                    conditions: current.conditions.map((item, at) =>
                      at === conditionIndex ? { ...item, field: event.target.value } : item,
                    ),
                  }))
                }
              >
                {FIELDS.map((field) => (
                  <option key={field} value={field}>
                    {t(FIELD_LABELS[field])}
                  </option>
                ))}
              </select>
              {condition.field === 'header' && (
                <input
                  placeholder={t('mailboxSettings.headerName')}
                  value={condition.header ?? ''}
                  onChange={(event) =>
                    update(index, (current) => ({
                      ...current,
                      conditions: current.conditions.map((item, at) =>
                        at === conditionIndex ? { ...item, header: event.target.value } : item,
                      ),
                    }))
                  }
                />
              )}
              {!['sender-known', 'any'].includes(condition.field) && (
                <>
                  <select
                    value={condition.operator}
                    onChange={(event) =>
                      update(index, (current) => ({
                        ...current,
                        conditions: current.conditions.map((item, at) =>
                          at === conditionIndex ? { ...item, operator: event.target.value } : item,
                        ),
                      }))
                    }
                  >
                    {OPERATORS.filter((operator) =>
                      condition.field === 'score' ? ['above', 'below'].includes(operator) : !['above', 'below'].includes(operator),
                    ).map((operator) => (
                      <option key={operator} value={operator}>
                        {t(OPERATOR_LABELS[operator])}
                      </option>
                    ))}
                  </select>
                  <input
                    value={condition.value ?? ''}
                    onChange={(event) =>
                      update(index, (current) => ({
                        ...current,
                        conditions: current.conditions.map((item, at) =>
                          at === conditionIndex ? { ...item, value: event.target.value } : item,
                        ),
                      }))
                    }
                  />
                </>
              )}
              <button
                type="button"
                className="link danger"
                disabled={rule.conditions.length === 1}
                onClick={() =>
                  update(index, (current) => ({
                    ...current,
                    conditions: current.conditions.filter((_, at) => at !== conditionIndex),
                  }))
                }
              >
                {t('common.remove')}
              </button>
            </div>
          ))}
          <button
            type="button"
            className="link"
            onClick={() =>
              update(index, (current) => ({
                ...current,
                conditions: [...current.conditions, { field: 'subject', operator: 'contains', value: '' }],
              }))
            }
          >
            {t('mailboxSettings.addCondition')}
          </button>

          <h4>{t('mailboxSettings.actions')}</h4>
          {rule.actions.map((action, actionIndex) => (
            <div className="rule-line" key={actionIndex}>
              <select
                value={action.kind}
                onChange={(event) =>
                  update(index, (current) => ({
                    ...current,
                    actions: current.actions.map((item, at) =>
                      at === actionIndex ? { ...item, kind: event.target.value } : item,
                    ),
                  }))
                }
              >
                {ACTIONS.map((kind) => (
                  <option key={kind} value={kind}>
                    {t(ACTION_LABELS[kind])}
                  </option>
                ))}
              </select>
              {action.kind === 'move' && (
                <select
                  value={action.folderId ?? ''}
                  required
                  onChange={(event) =>
                    update(index, (current) => ({
                      ...current,
                      actions: current.actions.map((item, at) =>
                        at === actionIndex ? { ...item, folderId: event.target.value } : item,
                      ),
                    }))
                  }
                >
                  <option value="">{t('mailboxSettings.chooseFolder')}</option>
                  {folders.map(({ folder, depth }) => (
                    <option key={folder.id} value={folder.id}>
                      {'  '.repeat(depth) + folderLabel(t, folder)}
                    </option>
                  ))}
                </select>
              )}
              {action.kind === 'forward' && (
                <input
                  type="email"
                  required
                  placeholder={t('mailboxSettings.forwardTo')}
                  value={action.address ?? ''}
                  onChange={(event) =>
                    update(index, (current) => ({
                      ...current,
                      actions: current.actions.map((item, at) =>
                        at === actionIndex ? { ...item, address: event.target.value } : item,
                      ),
                    }))
                  }
                />
              )}
              <button
                type="button"
                className="link danger"
                disabled={rule.actions.length === 1}
                onClick={() =>
                  update(index, (current) => ({
                    ...current,
                    actions: current.actions.filter((_, at) => at !== actionIndex),
                  }))
                }
              >
                {t('common.remove')}
              </button>
            </div>
          ))}
          <button
            type="button"
            className="link"
            onClick={() =>
              update(index, (current) => ({ ...current, actions: [...current.actions, { kind: 'markRead' }] }))
            }
          >
            {t('mailboxSettings.addAction')}
          </button>
        </div>
      ))}

      {error ? <ErrorMessage error={error} /> : null}
      <div className="page-actions">
        <button
          type="button"
          onClick={() => {
            setRules((previous) => [...previous, emptyRule()])
            setDirty(true)
            touch()
          }}
        >
          {t('mailboxSettings.addRule')}
        </button>
        <button type="button" disabled={trying || rules.length === 0} onClick={() => tryRules()}>
          {t('mailboxSettings.tryRules')}
        </button>
        <button className="primary" type="submit" disabled={busy || !dirty}>
          {t('common.save')}
        </button>
        {saved && !dirty && <span className="muted">{t('common.saved')}</span>}
      </div>

      {trialError ? <ErrorMessage error={trialError} /> : null}
      {trials && (
        <div className="card">
          <h3>{t('mailboxSettings.trialTitle')}</h3>
          <p className="muted field-hint">{t('mailboxSettings.trialHint')}</p>
          {trials.length === 0 ? (
            <p className="muted" style={{ margin: 0 }}>
              {t('mailbox.nothing')}
            </p>
          ) : (
            <table>
              <tbody>
                {trials.map((trial) => (
                  <tr key={trial.item.id}>
                    <td className="shrink muted">{trial.item.mail?.from || trial.item.mail?.sender}</td>
                    <td>{trial.item.mail?.subject || t('mailbox.noSubject')}</td>
                    <td className="shrink">
                      {trial.matched.length === 0 ? (
                        <span className="muted">{t('mailboxSettings.trialNoMatch')}</span>
                      ) : (
                        trial.matched.map((index) => rules[index]?.name || `#${index + 1}`).join(', ')
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      )}
    </form>
  )
}

// A moment on the clock as an <input type="datetime-local"> wants it: local
// time, to the minute, or nothing.
function toLocalInput(value?: string | null): string {
  if (!value) {
    return ''
  }
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return ''
  }
  const pad = (number: number) => String(number).padStart(2, '0')
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`
}

function fromLocalInput(value: string): string | null {
  return value ? new Date(value).toISOString() : null
}

function AutoReplyTab({ view }: { view: MailboxView }) {
  const { t } = useTranslation()
  const { busy, error, saved, save, touch } = useSave()
  const existing: MailboxAutoReply | null | undefined = view.mailbox.autoReply
  const [enabled, setEnabled] = useState(existing?.enabled ?? false)
  const [from, setFrom] = useState(toLocalInput(existing?.from))
  const [until, setUntil] = useState(toLocalInput(existing?.until))
  const [subject, setSubject] = useState(existing?.subject ?? '')
  const [text, setText] = useState(existing?.text ?? '')
  const [dirty, setDirty] = useState(false)

  useEffect(() => {
    setDirty(false)
  }, [view.mailbox.id])

  const change = <T,>(set: (value: T) => void) => (value: T) => {
    set(value)
    setDirty(true)
    touch()
  }

  return (
    <form
      className="card form-narrow"
      onSubmit={(event) => {
        event.preventDefault()
        void save(UPDATE, {
          mailboxId: view.mailbox.id,
          autoReply: { enabled, from: fromLocalInput(from), until: fromLocalInput(until), subject, text, html: '' },
        }).then(() => setDirty(false))
      }}
    >
      <p className="muted">{t('mailboxSettings.autoReplyHint')}</p>
      <label className="check">
        <input type="checkbox" checked={enabled} onChange={(event) => change(setEnabled)(event.target.checked)} />
        {t('mailboxSettings.autoReplyEnabled')}
      </label>
      <label>
        {t('mailboxSettings.autoReplyFrom')}
        <input type="datetime-local" value={from} onChange={(event) => change(setFrom)(event.target.value)} />
      </label>
      <label>
        {t('mailboxSettings.autoReplyUntil')}
        <input type="datetime-local" value={until} onChange={(event) => change(setUntil)(event.target.value)} />
      </label>
      <p className="muted field-hint">{t('mailboxSettings.autoReplyWhenHint')}</p>
      <label>
        {t('mailboxSettings.autoReplySubject')}
        <input
          value={subject}
          placeholder={t('mailboxSettings.autoReplySubjectHint')}
          onChange={(event) => change(setSubject)(event.target.value)}
        />
      </label>
      <label>
        {t('mailboxSettings.autoReplyText')}
        <textarea rows={6} value={text} required={enabled} onChange={(event) => change(setText)(event.target.value)} />
      </label>
      {error ? <ErrorMessage error={error} /> : null}
      <div className="page-actions">
        <button className="primary" type="submit" disabled={busy || !dirty}>
          {t('common.save')}
        </button>
        {saved && !dirty && <span className="muted">{t('common.saved')}</span>}
      </div>
    </form>
  )
}

// --- devices ------------------------------------------------------------------
//
// App passwords: one per mail program, named for the device, shown once.
// Beside them, what to type into the program: the server, the ports, and
// the address to sign in as.

type AppPassword = { id: string; name: string; createdAt: string; lastUsedAt?: string | null }
type ServerAddresses = {
  imapHost: string
  imapPort: number
  imapsPort: number
  submissionHost: string
  submissionPort: number
}

function DevicesTab({ view }: { view: MailboxView }) {
  const { t } = useTranslation()
  const query = useQuery(
    () =>
      graphql<{ ListMailboxAppPasswords: AppPassword[]; GetMailProgramSettings: ServerAddresses }>(APP_PASSWORDS, {
        mailboxId: view.mailbox.id,
      }),
    [view.mailbox.id],
    { refresh: false },
  )
  const [name, setName] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<unknown>(null)
  const [created, setCreated] = useState<{ password: string; username: string; name: string } | null>(null)
  const [deleting, setDeleting] = useState<AppPassword | null>(null)

  const create = async () => {
    setBusy(true)
    try {
      const response = await graphql<{
        CreateMailboxAppPassword: { password: string; username: string; appPassword: { name: string } }
      }>(CREATE_APP_PASSWORD, { mailboxId: view.mailbox.id, name: name.trim() })
      setCreated({
        password: response.CreateMailboxAppPassword.password,
        username: response.CreateMailboxAppPassword.username,
        name: response.CreateMailboxAppPassword.appPassword.name,
      })
      setName('')
      setError(null)
      await query.reload()
    } catch (failure) {
      setError(failure)
    } finally {
      setBusy(false)
    }
  }

  const remove = async (appPassword: AppPassword) => {
    setBusy(true)
    try {
      await graphql(DELETE_APP_PASSWORD, { appPasswordId: appPassword.id })
      setDeleting(null)
      setError(null)
      await query.reload()
    } catch (failure) {
      setError(failure)
    } finally {
      setBusy(false)
    }
  }

  if (query.loading && !query.data) {
    return <Loading />
  }
  const addresses = query.data?.GetMailProgramSettings
  const appPasswords = query.data?.ListMailboxAppPasswords ?? []
  const address = view.mailbox.addresses?.[0]?.address

  return (
    <>
      {created && (
        <div className="card">
          <h3>{t('mailboxSettings.appPasswordCreated', { name: created.name })}</h3>
          <p className="muted">{t('mailboxSettings.appPasswordOnce')}</p>
          <table className="detail">
            <tbody>
              <tr>
                <td className="shrink muted">{t('mailboxSettings.deviceUsername')}</td>
                <td className="mono">{created.username}</td>
              </tr>
              <tr>
                <td className="shrink muted">{t('mailboxSettings.devicePassword')}</td>
                <td className="mono">{created.password}</td>
              </tr>
            </tbody>
          </table>
          <div className="page-actions">
            <button type="button" onClick={() => setCreated(null)}>
              {t('mailboxSettings.appPasswordDone')}
            </button>
          </div>
        </div>
      )}

      <div className="card">
        <h3>{t('mailboxSettings.devices')}</h3>
        <p className="muted field-hint">{t('mailboxSettings.devicesHint')}</p>
        {appPasswords.length === 0 ? (
          <p className="muted" style={{ margin: 0 }}>
            {t('mailboxSettings.noDevices')}
          </p>
        ) : (
          <table>
            <tbody>
              {appPasswords.map((appPassword) => (
                <tr key={appPassword.id}>
                  <td>{appPassword.name}</td>
                  <td className="shrink muted">{formatTime(appPassword.createdAt)}</td>
                  <td className="shrink muted">
                    {appPassword.lastUsedAt ? <RelativeTime value={appPassword.lastUsedAt} /> : t('mailboxSettings.neverUsed')}
                  </td>
                  <td className="shrink">
                    <button type="button" className="link danger" onClick={() => setDeleting(appPassword)}>
                      {t('mailboxSettings.revoke')}
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>

      <form
        className="card form-narrow"
        onSubmit={(event) => {
          event.preventDefault()
          void create()
        }}
      >
        <h3>{t('mailboxSettings.newDevice')}</h3>
        <label>
          {t('mailboxSettings.deviceName')}
          <input value={name} onChange={(event) => setName(event.target.value)} placeholder={t('mailboxSettings.deviceNameHint')} required />
        </label>
        {!address && <p className="muted field-hint">{t('mailbox.noAddress')}</p>}
        {error ? <ErrorMessage error={error} /> : null}
        <div className="page-actions">
          <button className="primary" type="submit" disabled={busy || !name.trim() || !address}>
            {t('common.create')}
          </button>
        </div>
      </form>

      {addresses && (
        <div className="card">
          <h3>{t('mailboxSettings.programSettings')}</h3>
          <p className="muted field-hint">{t('mailboxSettings.programSettingsHint')}</p>
          <table className="detail">
            <tbody>
              <tr>
                <td className="shrink muted">{t('mailboxSettings.incomingServer')}</td>
                <td className="mono">
                  {addresses.imapHost} · IMAP · {t('mailboxSettings.portTls', { port: addresses.imapsPort })}
                  {addresses.imapPort ? ` · ${t('mailboxSettings.portStartTls', { port: addresses.imapPort })}` : ''}
                </td>
              </tr>
              <tr>
                <td className="shrink muted">{t('mailboxSettings.outgoingServer')}</td>
                <td className="mono">
                  {addresses.submissionHost} · SMTP · {t('mailboxSettings.portStartTls', { port: addresses.submissionPort })}
                </td>
              </tr>
              <tr>
                <td className="shrink muted">{t('mailboxSettings.deviceUsername')}</td>
                <td className="mono">{address ?? '—'}</td>
              </tr>
              <tr>
                <td className="shrink muted">{t('mailboxSettings.devicePassword')}</td>
                <td>{t('mailboxSettings.devicePasswordHint')}</td>
              </tr>
            </tbody>
          </table>
        </div>
      )}

      {deleting && (
        <ConfirmDialog
          title={t('mailboxSettings.revoke')}
          body={t('mailboxSettings.revokeConfirm', { name: deleting.name })}
          confirmLabel={t('mailboxSettings.revoke')}
          busy={busy}
          onConfirm={() => remove(deleting)}
          onClose={() => setDeleting(null)}
        />
      )}
    </>
  )
}

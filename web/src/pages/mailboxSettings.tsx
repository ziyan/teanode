import { useEffect, useState } from 'react'
import { Navigate, useNavigate, useParams } from 'react-router-dom'

import { Mailbox, MailboxAutoReply, MailboxFolder, MailboxRule, MailboxView, graphql } from '../api'
import { ErrorMessage, Loading, formatTime } from '../components/common'
import { RelativeTime } from '../components/relativeTime'
import { useQuery } from '../components/useQuery'
import { ConfirmDialog, FormDialog } from '../components/dialog'
import { SecretDialog, SettingsEmpty, SettingsRow, SettingsSection } from '../components/settingsList'
import { Tabs, TabItem } from '../components/tabs'
import { Key, useTranslation } from '../i18n/i18n'
import { folderLabel, folderRows, useMailboxes } from '../mailboxes'
import { FolderKindIcon } from '../components/folderIcon'
import { RichTextEditor, htmlToText, textToHtml } from '../components/richText'
import { ArrowDownIcon, ArrowUpIcon, PencilIcon, PinIcon, PinOffIcon, TrashIcon } from '../components/icons'

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

const PIN_FOLDER = `
  mutation ($folderId: String!, $pinned: Boolean!) {
    SetMailboxFolderPinned(folderId: $folderId, pinned: $pinned) { id }
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
  // Whether it worked, so a form closes only on success.
  const save = async (mutation: string, variables: Record<string, unknown>): Promise<boolean> => {
    setBusy(true)
    setSaved(false)
    try {
      await graphql(mutation, variables)
      await mailboxes.refresh()
      setError(null)
      setSaved(true)
      return true
    } catch (failure) {
      setError(failure)
      return false
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
  // A signature is written the way a message is: formatted, or not. Two boxes
  // side by side, one of them asking for HTML source, made the reader answer
  // a question about storage that is the program's to answer — and the second
  // box is what actually goes out, so leaving it empty quietly meant the
  // signature only appeared on plain messages.
  const [editor, setEditor] = useState<'rich' | 'plain'>(mailbox.signatureHtml ? 'rich' : 'plain')
  const { busy, error, saved, save, touch } = useSave()

  // What will be sent, which is the editor in front of you and the other form
  // derived from it. A signature written as rich text keeps a plain rendering
  // for plain messages; one written as plain text has no HTML form at all,
  // and the server falls back to the plain one.
  const signature =
    editor === 'rich'
      ? { signatureHtml, signatureText: htmlToText(signatureHtml) }
      : { signatureHtml: '', signatureText }

  const changed =
    name !== mailbox.name ||
    signature.signatureText !== (mailbox.signatureText ?? '') ||
    signature.signatureHtml !== (mailbox.signatureHtml ?? '')

  return (
    <>
      <form
        className="card"
        onSubmit={(event) => {
          event.preventDefault()
          void save(UPDATE, { mailboxId: mailbox.id, name: name.trim(), ...signature })
        }}
      >
        <h3>{t('mailboxSettings.tabGeneral')}</h3>
        <p className="muted">{t('mailboxSettings.generalHint')}</p>
        {/* The fields are capped, not the card: a narrow card beside a wide
            one reads as two different pages. */}
        <div className="form-narrow">
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

          <div className="field-label">{t('mailboxSettings.signature')}</div>
          <div className="segmented compose-editor-switch" role="group">
            <button
              type="button"
              className={editor === 'rich' ? 'active' : ''}
              onClick={() => {
                if (editor === 'plain') {
                  setSignatureHtml(textToHtml(signatureText))
                }
                setEditor('rich')
                touch()
              }}
            >
              {t('compose.mailbox.richText')}
            </button>
            <button
              type="button"
              className={editor === 'plain' ? 'active' : ''}
              onClick={() => {
                if (editor === 'rich') {
                  setSignatureText(htmlToText(signatureHtml))
                }
                setEditor('plain')
                touch()
              }}
            >
              {t('compose.mailbox.plainText')}
            </button>
          </div>
          <div className="signature-editor">
            {editor === 'rich' ? (
              <RichTextEditor
                value={signatureHtml}
                onChange={(next) => {
                  setSignatureHtml(next)
                  touch()
                }}
              />
            ) : (
              <textarea
                rows={5}
                aria-label={t('mailboxSettings.signature')}
                value={signatureText}
                onChange={(event) => {
                  setSignatureText(event.target.value)
                  touch()
                }}
              />
            )}
          </div>
          <p className="muted field-hint">{t('mailboxSettings.signatureHint')}</p>

          {error ? <ErrorMessage error={error} /> : null}
          <div className="page-actions">
            <button className="primary" type="submit" disabled={busy || !changed || !name.trim()}>
              {t('common.save')}
            </button>
            {saved && !changed && <span className="muted">{t('common.saved')}</span>}
          </div>
        </div>
      </form>

      <SettingsSection card title={t('mailboxSettings.addresses')} description={t('mailboxSettings.addressesHint')}>
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
          <SettingsEmpty>{t('mailbox.noAddress')}</SettingsEmpty>
        )}
      </SettingsSection>
    </>
  )
}

function FoldersTab({ view }: { view: MailboxView }) {
  const { t } = useTranslation()
  const { busy, error, save } = useSave()
  // Adding a folder and changing one are the same two questions — what it is
  // called, and what it sits inside — so they are one dialog, opened empty or
  // filled in. They were forms in the page: a card of two fields under the
  // list for adding, and a row that turned into a form for changing, which
  // made the list jump about while it was being read.
  const [editing, setEditing] = useState<MailboxFolder | null>(null)
  const [adding, setAdding] = useState(false)
  const [name, setName] = useState('')
  const [parentId, setParentId] = useState('')
  const [deleting, setDeleting] = useState<MailboxFolder | null>(null)
  const rows = folderRows(view.folders)

  // A folder cannot go inside itself or anything under it.
  const insideOf = (folder: MailboxFolder): Set<string> => {
    const inside = new Set<string>([folder.id])
    let grew = true
    while (grew) {
      grew = false
      for (const candidate of view.folders) {
        if (candidate.parentId && inside.has(candidate.parentId) && !inside.has(candidate.id)) {
          inside.add(candidate.id)
          grew = true
        }
      }
    }
    return inside
  }

  const open = (folder: MailboxFolder | null) => {
    setName(folder ? folder.name : '')
    setParentId(folder ? (folder.parentId ?? '') : '')
    setEditing(folder)
    setAdding(folder === null)
  }
  const close = () => {
    setEditing(null)
    setAdding(false)
  }

  // What a folder may sit inside: anything but itself and its own children.
  const parents = editing ? rows.filter((row) => !insideOf(editing).has(row.folder.id)) : rows

  return (
    <>
      <SettingsSection
        card
        title={t('mailboxSettings.folders')}
        action={
          <button className="primary" type="button" onClick={() => open(null)}>
            {t('mailboxSettings.newFolder')}
          </button>
        }
      >
        <ErrorMessage error={error} />
        <table className="folders-table">
          <tbody>
            {rows.map(({ folder, depth }) => (
              <tr key={folder.id}>
                <td style={{ paddingLeft: 8 + depth * 20 }}>
                  <span className="folder-name">
                    <FolderKindIcon kind={folder.kind} size={16} />
                    {folderLabel(t, folder)}
                  </span>
                </td>
                <td className="shrink muted hide-narrow">{folder.total}</td>
                <td className="shrink">
                  {/* Only the owner's own folders can be renamed or removed;
                      the system folders are what the mailbox is. Any of
                      them but the Inbox, which is always at the top, can
                      be pinned up there beside it. */}
                  {folder.kind !== 'inbox' && (
                    <div className="row-actions">
                      <button
                        type="button"
                        className={folder.pinnedAt ? 'icon-action pinned' : 'icon-action'}
                        title={t(folder.pinnedAt ? 'mailbox.unpin' : 'mailbox.pinToTop')}
                        aria-label={`${folderLabel(t, folder)}: ${t(folder.pinnedAt ? 'mailbox.unpin' : 'mailbox.pinToTop')}`}
                        aria-pressed={Boolean(folder.pinnedAt)}
                        disabled={busy}
                        onClick={() => void save(PIN_FOLDER, { folderId: folder.id, pinned: !folder.pinnedAt })}
                      >
                        {folder.pinnedAt ? <PinOffIcon size={16} /> : <PinIcon size={16} />}
                      </button>
                      {!folder.kind && (
                        <>
                          <button
                            type="button"
                            className="icon-action"
                            title={t('common.edit')}
                            aria-label={`${folder.name}: ${t('common.edit')}`}
                            disabled={busy}
                            onClick={() => open(folder)}
                          >
                            <PencilIcon size={16} />
                          </button>
                          <button
                            type="button"
                            className="icon-action danger"
                            title={t('common.delete')}
                            aria-label={`${folder.name}: ${t('common.delete')}`}
                            disabled={busy}
                            onClick={() => setDeleting(folder)}
                          >
                            <TrashIcon size={16} />
                          </button>
                        </>
                      )}
                    </div>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </SettingsSection>

      {(adding || editing) && (
        <FormDialog
          title={editing ? t('mailboxSettings.editFolder', { name: editing.name }) : t('mailboxSettings.newFolder')}
          submitLabel={editing ? t('common.save') : t('common.create')}
          busy={busy}
          error={error instanceof Error ? error.message : null}
          canSubmit={name.trim() !== ''}
          onClose={close}
          onSubmit={() => {
            const done = (ok: boolean) => ok && close()
            if (editing) {
              void save(UPDATE_FOLDER, { folderId: editing.id, name: name.trim(), parentId }).then(done)
              return
            }
            void save(CREATE_FOLDER, {
              mailboxId: view.mailbox.id,
              name: name.trim(),
              parentId: parentId || undefined,
            }).then(done)
          }}
        >
          <label>
            {t('mailboxSettings.folderName')}
            <input value={name} onChange={(event) => setName(event.target.value)} autoFocus required />
          </label>
          <label>
            {t('mailboxSettings.folderParent')}
            <select value={parentId} onChange={(event) => setParentId(event.target.value)}>
              <option value="">{t('mailboxSettings.folderTop')}</option>
              {parents.map(({ folder, depth }) => (
                <option key={folder.id} value={folder.id}>
                  {'  '.repeat(depth) + folderLabel(t, folder)}
                </option>
              ))}
            </select>
          </label>
        </FormDialog>
      )}

      {deleting && (
        <ConfirmDialog
          title={t('mailboxSettings.deleteFolder')}
          body={t('mailboxSettings.deleteFolderConfirm', { name: deleting.name, count: deleting.total })}
          confirmLabel={t('common.delete')}
          busy={busy}
          error={error instanceof Error ? error.message : null}
          onConfirm={() => save(DELETE_FOLDER, { folderId: deleting.id }).then((done) => done && setDeleting(null))}
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

// operatorsFor is what a condition's field can be compared with: a score
// is above or below a number, everything else matches text.
function operatorsFor(field: string): string[] {
  if (field === 'score') {
    return ['above', 'below']
  }
  if (field === 'sender-known' || field === 'any') {
    return ['']
  }
  return OPERATORS.filter((operator) => operator !== 'above' && operator !== 'below')
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
      header: condition.field === 'header' ? (condition.header ?? '') : '',
      operator: ['sender-known', 'any'].includes(condition.field) ? '' : condition.operator,
      value: condition.value ?? '',
    })),
    actions: rule.actions.map((action) => ({
      kind: action.kind,
      folderId: action.kind === 'move' ? (action.folderId ?? '') : '',
      address: action.kind === 'forward' ? (action.address ?? '') : '',
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
  type Trial = {
    matched: number[]
    item: { id: string; mail?: { from?: string; sender?: string; subject?: string } | null }
  }
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
        void save(UPDATE, { mailboxId: view.mailbox.id, rules: cleanRules(rules) }).then(
          (done) => done && setDirty(false),
        )
      }}
    >
      <SettingsSection
        title={t('mailboxSettings.tabRules')}
        description={t('mailboxSettings.rulesHint')}
        action={
          <button
            className="primary"
            type="button"
            onClick={() => {
              setRules((previous) => [...previous, emptyRule()])
              setDirty(true)
              touch()
            }}
          >
            {t('mailboxSettings.addRule')}
          </button>
        }
      >
        {rules.length === 0 && <SettingsEmpty>{t('mailboxSettings.noRules')}</SettingsEmpty>}
      </SettingsSection>
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
              <button
                type="button"
                className="icon-action"
                title={t('mailboxSettings.moveUp')}
                aria-label={`${rule.name || t('mailboxSettings.ruleName')}: ${t('mailboxSettings.moveUp')}`}
                disabled={index === 0}
                onClick={() => move(index, -1)}
              >
                <ArrowUpIcon size={16} />
              </button>
              <button
                type="button"
                className="icon-action"
                title={t('mailboxSettings.moveDown')}
                aria-label={`${rule.name || t('mailboxSettings.ruleName')}: ${t('mailboxSettings.moveDown')}`}
                disabled={index === rules.length - 1}
                onClick={() => move(index, 1)}
              >
                <ArrowDownIcon size={16} />
              </button>
              <button
                type="button"
                className="icon-action danger"
                title={t('common.remove')}
                aria-label={`${rule.name || t('mailboxSettings.ruleName')}: ${t('common.remove')}`}
                onClick={() => {
                  setRules((previous) => previous.filter((_, at) => at !== index))
                  setDirty(true)
                  touch()
                }}
              >
                <TrashIcon size={16} />
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
                      at === conditionIndex
                        ? {
                            ...item,
                            field: event.target.value,
                            // A field has its own operators; keep one only if
                            // the new field can use it.
                            operator: operatorsFor(event.target.value).includes(item.operator)
                              ? item.operator
                              : operatorsFor(event.target.value)[0],
                          }
                        : item,
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
                    {operatorsFor(condition.field).map((operator) => (
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
        <SettingsSection card title={t('mailboxSettings.trialTitle')} description={t('mailboxSettings.trialHint')}>
          {trials.length === 0 ? (
            <SettingsEmpty>{t('mailbox.nothing')}</SettingsEmpty>
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
        </SettingsSection>
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

  const change =
    <T,>(set: (value: T) => void) =>
    (value: T) => {
      set(value)
      setDirty(true)
      touch()
    }

  return (
    <form
      className="card"
      onSubmit={(event) => {
        event.preventDefault()
        void save(UPDATE, {
          mailboxId: view.mailbox.id,
          autoReply: { enabled, from: fromLocalInput(from), until: fromLocalInput(until), subject, text, html: '' },
        }).then(() => setDirty(false))
      }}
    >
      <h3>{t('mailboxSettings.tabAutoReply')}</h3>
      <p className="muted">{t('mailboxSettings.autoReplyHint')}</p>
      <div className="form-narrow">
        <label className="checkbox">
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
          <textarea
            rows={6}
            value={text}
            required={enabled}
            onChange={(event) => change(setText)(event.target.value)}
          />
        </label>
        {error ? <ErrorMessage error={error} /> : null}
        <div className="page-actions">
          <button className="primary" type="submit" disabled={busy || !dirty}>
            {t('common.save')}
          </button>
          {saved && !dirty && <span className="muted">{t('common.saved')}</span>}
        </div>
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
  const [adding, setAdding] = useState(false)
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
      setAdding(false)
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
      <SettingsSection
        card
        title={t('mailboxSettings.devices')}
        description={t('mailboxSettings.devicesHint')}
        action={
          <button
            className="primary"
            type="button"
            disabled={!address}
            title={address ? undefined : t('mailbox.noAddress')}
            onClick={() => {
              setName('')
              setError(null)
              setAdding(true)
            }}
          >
            {t('mailboxSettings.newDevice')}
          </button>
        }
      >
        {error && !adding ? <ErrorMessage error={error} /> : null}
        {appPasswords.length === 0 ? (
          <SettingsEmpty>{t('mailboxSettings.noDevices')}</SettingsEmpty>
        ) : (
          appPasswords.map((appPassword) => (
            <SettingsRow
              key={appPassword.id}
              title={appPassword.name}
              subtitle={
                <>
                  {t('mailboxSettings.deviceCreated', { time: formatTime(appPassword.createdAt) })}
                  {' · '}
                  {appPassword.lastUsedAt ? (
                    <>
                      {t('mailboxSettings.deviceLastUsed')} <RelativeTime value={appPassword.lastUsedAt} />
                    </>
                  ) : (
                    t('mailboxSettings.neverUsed')
                  )}
                </>
              }
              actions={
                <button className="link danger" type="button" onClick={() => setDeleting(appPassword)}>
                  {t('mailboxSettings.revoke')}
                </button>
              }
            />
          ))
        )}
      </SettingsSection>

      {adding && (
        <FormDialog
          title={t('mailboxSettings.newDevice')}
          submitLabel={t('common.create')}
          busy={busy}
          error={error instanceof Error ? error.message : null}
          canSubmit={name.trim() !== ''}
          onClose={() => setAdding(false)}
          onSubmit={() => void create()}
        >
          <label>
            <span>{t('mailboxSettings.deviceName')}</span>
            <input
              value={name}
              onChange={(event) => setName(event.target.value)}
              placeholder={t('mailboxSettings.deviceNameHint')}
              autoFocus
              required
            />
          </label>
        </FormDialog>
      )}

      {/* The password is shown this once, so it is shown the way every other
          one-time secret in the dashboard is: alone, with a copy button, and
          no way to dismiss it by accident. */}
      {created && (
        <SecretDialog
          title={t('mailboxSettings.appPasswordCreated', { name: created.name })}
          intro={t('mailboxSettings.appPasswordOnce')}
          secret={created.password}
          extra={<p className="muted">{t('mailboxSettings.appPasswordUsername', { username: created.username })}</p>}
          onDone={() => setCreated(null)}
        />
      )}

      {addresses && (
        <div className="card">
          <h3>{t('mailboxSettings.programSettings')}</h3>
          <p className="muted">{t('mailboxSettings.programSettingsHint')}</p>
          <table className="detail program-settings">
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
                  {addresses.submissionHost} · SMTP ·{' '}
                  {t('mailboxSettings.portStartTls', { port: addresses.submissionPort })}
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

// --- contacts ------------------------------------------------------------------
//
// Whoever has written to the mailbox, and whoever its owner added. Names
// here are what the compose page completes and what a rule's "sender is a
// contact" reads.

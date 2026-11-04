import { MailboxRuleCondition } from '../api'
import { Key, useTranslation } from '../i18n/i18n'
import { TrashIcon } from './icons'
import { Combobox, Select } from './select'
import { Tooltip } from './tooltip'

// The conditions a message has to meet, edited as a list of lines: a mailbox
// rule is made of them, and so is a schedule that runs when mail arrives.
// Every condition has to pass.

export const MAIL_FIELDS = [
  'from',
  'to',
  'subject',
  'header',
  'score',
  'sender-known',
  'category',
  'priority',
  'needs-reply',
  'any',
] as const

// The fields a schedule can wait on: what is written on the message, which
// is all it has to go on when the message arrives.
export const SCHEDULE_MAIL_FIELDS = ['from', 'to', 'subject', 'header', 'any'] as const

// The fields that need no value: the condition is the field.
export const BARE_FIELDS = ['sender-known', 'needs-reply', 'any']

// The categories the agent always knows; a person's own go in by name.
const CATEGORIES = [
  'personal',
  'work',
  'newsletter',
  'notification',
  'receipt',
  'promotion',
  'social',
  'invitation',
  'other',
]
const PRIORITIES = ['high', 'normal', 'low']
const OPERATORS = ['contains', 'equals', 'matches', 'above', 'below'] as const

const FIELD_LABELS: Record<string, Key> = {
  from: 'mailboxSettings.fieldFrom',
  to: 'mailboxSettings.fieldTo',
  subject: 'mailboxSettings.fieldSubject',
  header: 'mailboxSettings.fieldHeader',
  score: 'mailboxSettings.fieldScore',
  'sender-known': 'mailboxSettings.fieldSenderKnown',
  category: 'mailboxSettings.fieldCategory',
  priority: 'mailboxSettings.fieldPriority',
  'needs-reply': 'mailboxSettings.fieldNeedsReply',
  any: 'mailboxSettings.fieldAny',
}
const PRIORITY_LABELS: Record<string, Key> = {
  high: 'mailboxSettings.priorityHigh',
  normal: 'mailboxSettings.priorityNormal',
  low: 'mailboxSettings.priorityLow',
}
const OPERATOR_LABELS: Record<string, Key> = {
  contains: 'mailboxSettings.operatorContains',
  equals: 'mailboxSettings.operatorEquals',
  matches: 'mailboxSettings.operatorMatches',
  above: 'mailboxSettings.operatorAbove',
  below: 'mailboxSettings.operatorBelow',
}

// operatorsFor is what a condition's field can be compared with: a score
// is above or below a number, a priority is one of three, everything else
// matches text.
function operatorsFor(field: string): string[] {
  if (field === 'score') {
    return ['above', 'below']
  }
  if (field === 'priority') {
    return ['equals']
  }
  if (BARE_FIELDS.includes(field)) {
    return ['']
  }
  return OPERATORS.filter((operator) => operator !== 'above' && operator !== 'below')
}

// describeMailCondition says what one condition asks in a few words, for a
// list that is read far more often than it is edited.
export function describeMailCondition(
  condition: MailboxRuleCondition,
  t: (key: Key, values?: Record<string, string | number>) => string,
): string {
  const field = t(FIELD_LABELS[condition.field] ?? 'mailboxSettings.fieldAny')
  if (BARE_FIELDS.includes(condition.field)) {
    return field
  }
  const name = condition.field === 'header' && condition.header ? ` ${condition.header}` : ''
  const operator = t(OPERATOR_LABELS[condition.operator] ?? 'mailboxSettings.operatorContains')
  const value =
    condition.field === 'priority' && PRIORITY_LABELS[condition.value ?? '']
      ? t(PRIORITY_LABELS[condition.value ?? ''])
      : (condition.value ?? '')
  return `${field}${name} ${operator} ${value}`.trim()
}

// describeMailSchedule is how a schedule that runs on mail says when: the
// conditions, in words, after "When mail arrives".
export function describeMailSchedule(
  conditions: MailboxRuleCondition[],
  t: (key: Key, values?: Record<string, string | number>) => string,
): string {
  return t('agent.scheduleOnMail', {
    conditions: conditions.map((condition) => describeMailCondition(condition, t)).join(', '),
  })
}

// describeScheduleTiming is when a schedule runs, for a list: its mail
// conditions in words, or its cron line when it runs on the clock.
export function describeScheduleTiming(
  schedule: { cron: string; mailConditions?: MailboxRuleCondition[] | null },
  t: (key: Key, values?: Record<string, string | number>) => string,
): string {
  return schedule.mailConditions?.length ? describeMailSchedule(schedule.mailConditions, t) : schedule.cron
}

// MailConditionsEditor is a line for each condition and a button to add
// another. The last condition cannot be removed: a rule or a schedule with
// none would match every message.
export function MailConditionsEditor({
  conditions,
  onChange,
  fields = MAIL_FIELDS,
  firstField = 'subject',
}: {
  conditions: MailboxRuleCondition[]
  onChange: (conditions: MailboxRuleCondition[]) => void
  fields?: readonly string[]
  // The field a condition added with the button starts on.
  firstField?: string
}) {
  const { t } = useTranslation()
  const update = (conditionIndex: number, make: (condition: MailboxRuleCondition) => MailboxRuleCondition) =>
    onChange(conditions.map((item, at) => (at === conditionIndex ? make(item) : item)))

  return (
    <>
      {conditions.map((condition, conditionIndex) => (
        <div className="rule-line" key={conditionIndex}>
          <Select
            label={t('mailboxSettings.conditions')}
            value={condition.field}
            options={fields.map((field) => ({ value: field, label: t(FIELD_LABELS[field]) }))}
            onChange={(value) =>
              update(conditionIndex, (item) => ({
                ...item,
                field: value,
                // A field has its own operators; keep one only if
                // the new field can use it.
                operator: operatorsFor(value).includes(item.operator) ? item.operator : operatorsFor(value)[0],
              }))
            }
          />
          {condition.field === 'header' && (
            <input
              placeholder={t('mailboxSettings.headerName')}
              aria-label={t('mailboxSettings.headerName')}
              value={condition.header ?? ''}
              onChange={(event) => update(conditionIndex, (item) => ({ ...item, header: event.target.value }))}
            />
          )}
          {!BARE_FIELDS.includes(condition.field) && (
            <>
              <Select
                label={t('mailboxSettings.operator')}
                value={condition.operator}
                options={operatorsFor(condition.field).map((operator) => ({
                  value: operator,
                  label: t(OPERATOR_LABELS[operator]),
                }))}
                onChange={(value) => update(conditionIndex, (item) => ({ ...item, operator: value }))}
              />
              {condition.field === 'priority' ? (
                <Select
                  label={t('mailboxSettings.value')}
                  value={PRIORITIES.includes(condition.value ?? '') ? (condition.value ?? '') : ''}
                  options={[
                    { value: '', label: t('mailboxSettings.choose') },
                    ...PRIORITIES.map((level) => ({ value: level, label: t(PRIORITY_LABELS[level]) })),
                  ]}
                  onChange={(value) => update(conditionIndex, (item) => ({ ...item, value }))}
                />
              ) : (
                <Combobox
                  label={t('mailboxSettings.value')}
                  suggestions={
                    condition.field === 'category'
                      ? CATEGORIES.filter((name) => name.includes((condition.value ?? '').toLowerCase()))
                      : []
                  }
                  placeholder={condition.field === 'category' ? t('mailboxSettings.categoryHint') : undefined}
                  value={condition.value ?? ''}
                  onChange={(value) => update(conditionIndex, (item) => ({ ...item, value }))}
                />
              )}
            </>
          )}
          <Tooltip label={t('common.remove')}>
            <button
              type="button"
              className="icon-action danger"
              aria-label={t('common.remove')}
              disabled={conditions.length === 1}
              onClick={() => onChange(conditions.filter((_, at) => at !== conditionIndex))}
            >
              <TrashIcon size={16} />
            </button>
          </Tooltip>
        </div>
      ))}
      <button
        type="button"
        onClick={() => onChange([...conditions, { field: firstField, operator: 'contains', value: '' }])}
      >
        {t('mailboxSettings.addCondition')}
      </button>
    </>
  )
}

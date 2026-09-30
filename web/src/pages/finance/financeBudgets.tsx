import { useMemo, useState } from 'react'

import { graphql } from '../../api'
import { ErrorMessage, Loading, Tag, formatMoney } from '../../components/common'
import { ConfirmDialog, FormDialog } from '../../components/dialog'
import { PencilIcon, TrashIcon } from '../../components/icons'
import { Select } from '../../components/select'
import { SettingsEmpty, SettingsRow, SettingsSection } from '../../components/settingsList'
import { Tooltip } from '../../components/tooltip'
import { useQuery } from '../../components/useQuery'
import { useTranslation } from '../../i18n/i18n'
import {
  BUDGETS,
  Budget,
  CREATE_SPENDING_CATEGORY,
  CREATE_SPENDING_RULE,
  DELETE_SPENDING_CATEGORY,
  DELETE_SPENDING_RULE,
  FINANCE_ACCOUNTS,
  FinanceAccount,
  SET_BUDGET,
  SPENDING_CATEGORIES,
  SPENDING_RULES,
  SpendingCategory,
  SpendingRule,
  UPDATE_SPENDING_CATEGORY,
  UPDATE_SPENDING_RULE,
  amountOf,
  isDecimal,
  monthLabel,
  personMonth,
} from './financeApi'
import {
  CurrencyPicker,
  accountLabel,
  spendingCategoryLabel,
  spendingCategoryOptions,
  useAct,
  useReportingCurrency,
} from './financeCommon'
import { useSpendingCategoryDisplayName } from './spendingCategoryName'

// The Budgets section: a monthly budget per spending category, the
// person's own list of spending categories, and the spending rules that
// file finance transactions under them. A budget is changed by setting a new
// amount from a month on; the months before keep the budget they had.
export function FinanceBudgetsSection() {
  const categories = useQuery(() => graphql<{ SpendingCategories: SpendingCategory[] }>(SPENDING_CATEGORIES), [], {
    refresh: false,
  })
  const categoryList = categories.data?.SpendingCategories ?? []
  return (
    <>
      <BudgetsPanel categories={categoryList} />
      <SpendingCategoriesPanel
        categories={categoryList}
        loading={categories.loading && !categories.data}
        error={categories.error}
        reload={categories.reload}
      />
      <SpendingRulesPanel categories={categoryList} />
    </>
  )
}

function BudgetsPanel({ categories }: { categories: SpendingCategory[] }) {
  const { t } = useTranslation()
  const categoryName = useSpendingCategoryDisplayName()
  const budgets = useQuery(() => graphql<{ Budgets: Budget[] }>(BUDGETS), [], { refresh: false })
  const { reportingCurrencyCode } = useReportingCurrency()
  const { busy, act } = useAct(budgets.reload)
  const [editing, setEditing] = useState<{ spendingCategoryId: string } | null>(null)
  const [spendingCategoryId, setSpendingCategoryId] = useState('')
  const [monthlyAmount, setMonthlyAmount] = useState('')
  const [currencyCode, setCurrencyCode] = useState('')
  const [effectiveFrom, setEffectiveFrom] = useState(() => personMonth())

  // The budget in force for each spending category this month: its latest
  // row from this month or before. A budget of zero has ended and is not
  // listed. Rows from a later month are changes already set to come, listed
  // after the ones in force.
  const { current, scheduled } = useMemo(() => {
    const thisMonth = personMonth()
    const latest = new Map<string, Budget>()
    const later: Budget[] = []
    for (const budget of budgets.data?.Budgets ?? []) {
      if (budget.effectiveFrom.slice(0, 7) > thisMonth) {
        later.push(budget)
        continue
      }
      const seen = latest.get(budget.spendingCategoryId)
      if (!seen || seen.effectiveFrom < budget.effectiveFrom) latest.set(budget.spendingCategoryId, budget)
    }
    later.sort((left, right) => left.effectiveFrom.localeCompare(right.effectiveFrom))
    return {
      current: [...latest.values()].filter((budget) => amountOf(budget.monthlyAmount) > 0),
      scheduled: later,
    }
  }, [budgets.data])

  const nameOf = (id: string) => {
    const category = categories.find((candidate) => candidate.id === id)
    return category ? spendingCategoryLabel(category, categories, categoryName) : t('finance.deletedSpendingCategory')
  }

  const open = (budget?: Budget) => {
    setSpendingCategoryId(budget?.spendingCategoryId ?? spendingCategoryOptions(categories, categoryName)[0]?.value ?? '')
    setMonthlyAmount(budget ? String(amountOf(budget.monthlyAmount)) : '')
    setCurrencyCode(budget?.currencyCode ?? reportingCurrencyCode)
    setEffectiveFrom(personMonth())
    setEditing({ spendingCategoryId: budget?.spendingCategoryId ?? '' })
  }

  return (
    <SettingsSection
      card
      title={t('finance.budgetsTitle')}
      description={t('finance.budgetsHint')}
      action={
        <button type="button" className="primary" disabled={categories.length === 0} onClick={() => open()}>
          {t('finance.setBudget')}
        </button>
      }
    >
      <ErrorMessage error={budgets.error} />
      {budgets.loading && !budgets.data ? <Loading /> : null}
      {budgets.data && current.length === 0 && scheduled.length === 0 ? (
        <SettingsEmpty>{t('finance.noBudgets')}</SettingsEmpty>
      ) : null}
      {current.map((budget) => (
        <SettingsRow
          key={budget.id}
          title={nameOf(budget.spendingCategoryId)}
          subtitle={t('finance.budgetFrom', {
            amount: formatMoney(amountOf(budget.monthlyAmount), budget.currencyCode),
            month: monthLabel(budget.effectiveFrom, 'long'),
          })}
          actions={
            <button
              type="button"
              className="link"
              aria-label={`${nameOf(budget.spendingCategoryId)}: ${t('finance.changeBudget')}`}
              onClick={() => open(budget)}
            >
              {t('finance.changeBudget')}
            </button>
          }
        />
      ))}
      {scheduled.map((budget) => (
        <SettingsRow
          key={budget.id}
          title={nameOf(budget.spendingCategoryId)}
          badge={<Tag value={t('finance.scheduled')} />}
          subtitle={
            amountOf(budget.monthlyAmount) > 0
              ? t('finance.budgetFrom', {
                  amount: formatMoney(amountOf(budget.monthlyAmount), budget.currencyCode),
                  month: monthLabel(budget.effectiveFrom, 'long'),
                })
              : t('finance.budgetEndsFrom', { month: monthLabel(budget.effectiveFrom, 'long') })
          }
        />
      ))}
      {editing ? (
        <FormDialog
          title={editing.spendingCategoryId ? t('finance.changeBudget') : t('finance.setBudget')}
          submitLabel={t('finance.setBudget')}
          busy={busy}
          canSubmit={spendingCategoryId !== '' && isDecimal(monthlyAmount) && amountOf(monthlyAmount) >= 0}
          onClose={() => setEditing(null)}
          onSubmit={() => {
            void act(
              () =>
                graphql(SET_BUDGET, {
                  spendingCategoryId,
                  monthlyAmount: monthlyAmount.trim(),
                  currencyCode: currencyCode || undefined,
                  effectiveFrom,
                }),
              amountOf(monthlyAmount) > 0 ? t('finance.budgetSet') : t('finance.budgetEnded'),
            ).then((isDone) => {
              if (isDone) setEditing(null)
            })
          }}
        >
          <label>
            <span>{t('finance.spendingCategory')}</span>
            <Select
              block
              value={spendingCategoryId}
              label={t('finance.spendingCategory')}
              disabled={editing.spendingCategoryId !== ''}
              options={spendingCategoryOptions(categories, categoryName, spendingCategoryId)}
              onChange={setSpendingCategoryId}
            />
          </label>
          <div className="row">
            <label>
              <span>{t('finance.monthlyAmount')}</span>
              <input
                inputMode="decimal"
                value={monthlyAmount}
                onChange={(event) => setMonthlyAmount(event.target.value)}
              />
            </label>
            <label>
              <span>{t('finance.currency')}</span>
              <CurrencyPicker value={currencyCode} label={t('finance.currency')} onChange={setCurrencyCode} />
            </label>
          </div>
          <label>
            <span>{t('finance.effectiveFrom')}</span>
            <input type="month" value={effectiveFrom} onChange={(event) => setEffectiveFrom(event.target.value)} />
          </label>
          <p className="muted field-hint">{t('finance.budgetDialogHint')}</p>
        </FormDialog>
      ) : null}
    </SettingsSection>
  )
}

function SpendingCategoriesPanel({
  categories,
  loading,
  error,
  reload,
}: {
  categories: SpendingCategory[]
  loading: boolean
  error: unknown
  reload: () => Promise<void>
}) {
  const { t } = useTranslation()
  const categoryName = useSpendingCategoryDisplayName()
  const { busy, act, run } = useAct(reload)
  const [editing, setEditing] = useState<SpendingCategory | 'new' | null>(null)
  const [deleting, setDeleting] = useState<SpendingCategory | null>(null)
  const [name, setName] = useState('')
  const [parentId, setParentId] = useState('')
  const [isIncome, setIsIncome] = useState(false)
  const [isHidden, setIsHidden] = useState(false)

  const open = (category: SpendingCategory | 'new') => {
    const existing = category === 'new' ? null : category
    setName(existing?.spendingCategoryName ?? '')
    setParentId(existing?.parentSpendingCategoryId ?? '')
    setIsIncome(existing?.isIncome ?? false)
    setIsHidden(existing?.isHidden ?? false)
    setEditing(category)
  }

  // One level of parents: a spending category that has a parent cannot be
  // one, and a spending category cannot be its own.
  const parentChoices = categories.filter(
    (category) => !category.parentSpendingCategoryId && (editing === 'new' || category.id !== editing?.id),
  )
  const sorted = [...categories].sort((left, right) =>
    spendingCategoryLabel(left, categories, categoryName).localeCompare(spendingCategoryLabel(right, categories, categoryName)),
  )

  return (
    <SettingsSection
      card
      title={t('finance.spendingCategoriesTitle')}
      description={t('finance.spendingCategoriesHint')}
      action={
        <button type="button" className="primary" onClick={() => open('new')}>
          {t('finance.addSpendingCategory')}
        </button>
      }
    >
      <ErrorMessage error={error} />
      {loading ? <Loading /> : null}
      {!loading && categories.length === 0 ? <SettingsEmpty>{t('finance.noSpendingCategories')}</SettingsEmpty> : null}
      {sorted.map((category) => (
        <SettingsRow
          key={category.id}
          title={spendingCategoryLabel(category, categories, categoryName)}
          badge={
            <>
              {category.isIncome ? <Tag value={t('finance.income')} tone="good" /> : null}
              {category.isHidden ? <Tag value={t('finance.hidden')} /> : null}
            </>
          }
          actions={
            <div className="row-actions">
              <Tooltip label={t('common.edit')}>
                <button
                  type="button"
                  className="icon-action"
                  aria-label={`${categoryName(category.spendingCategoryName)}: ${t('common.edit')}`}
                  onClick={() => open(category)}
                >
                  <PencilIcon size={16} />
                </button>
              </Tooltip>
              <Tooltip label={t('common.delete')}>
                <button
                  type="button"
                  className="icon-action danger"
                  aria-label={`${categoryName(category.spendingCategoryName)}: ${t('common.delete')}`}
                  onClick={() => setDeleting(category)}
                >
                  <TrashIcon size={16} />
                </button>
              </Tooltip>
            </div>
          }
        />
      ))}
      {editing ? (
        <FormDialog
          title={editing === 'new' ? t('finance.addSpendingCategory') : t('finance.editSpendingCategory')}
          submitLabel={editing === 'new' ? t('common.create') : t('common.save')}
          busy={busy}
          canSubmit={name.trim() !== ''}
          onClose={() => setEditing(null)}
          onSubmit={() => {
            const variables = {
              spendingCategoryName: name.trim(),
              parentSpendingCategoryId: parentId,
              isIncome,
              isHidden,
            }
            void act(
              () =>
                editing === 'new'
                  ? graphql(CREATE_SPENDING_CATEGORY, variables)
                  : graphql(UPDATE_SPENDING_CATEGORY, { ...variables, spendingCategoryId: editing.id }),
              t('finance.spendingCategorySaved'),
            ).then((isDone) => {
              if (isDone) setEditing(null)
            })
          }}
        >
          <label>
            <span>{t('finance.spendingCategoryName')}</span>
            <input value={name} onChange={(event) => setName(event.target.value)} />
          </label>
          <label>
            <span>{t('finance.parentSpendingCategory')}</span>
            <Select
              block
              value={parentId}
              label={t('finance.parentSpendingCategory')}
              options={[
                { value: '', label: t('finance.noParent') },
                ...parentChoices.map((category) => ({ value: category.id, label: categoryName(category.spendingCategoryName) })),
              ]}
              onChange={setParentId}
            />
          </label>
          <label className="checkbox">
            <input type="checkbox" checked={isIncome} onChange={(event) => setIsIncome(event.target.checked)} />
            {t('finance.isIncome')}
          </label>
          <label className="checkbox">
            <input type="checkbox" checked={isHidden} onChange={(event) => setIsHidden(event.target.checked)} />
            {t('finance.isHidden')}
          </label>
        </FormDialog>
      ) : null}
      {deleting ? (
        <ConfirmDialog
          title={t('finance.deleteSpendingCategory')}
          body={t('finance.deleteSpendingCategoryBody', { name: categoryName(deleting.spendingCategoryName) })}
          confirmLabel={t('common.delete')}
          busy={busy}
          onClose={() => setDeleting(null)}
          onConfirm={() => {
            void run(
              DELETE_SPENDING_CATEGORY,
              { spendingCategoryId: deleting.id },
              t('finance.spendingCategoryDeleted'),
            ).then(() => setDeleting(null))
          }}
        />
      ) : null}
    </SettingsSection>
  )
}

function SpendingRulesPanel({ categories }: { categories: SpendingCategory[] }) {
  const { t } = useTranslation()
  const categoryName = useSpendingCategoryDisplayName()
  const rules = useQuery(() => graphql<{ SpendingRules: SpendingRule[] }>(SPENDING_RULES), [], { refresh: false })
  const accounts = useQuery(() => graphql<{ FinanceAccounts: FinanceAccount[] }>(FINANCE_ACCOUNTS), [], {
    refresh: false,
  })
  const { busy, act, run } = useAct(rules.reload)
  const [editing, setEditing] = useState<SpendingRule | 'new' | null>(null)
  const [deleting, setDeleting] = useState<SpendingRule | null>(null)
  const [matchText, setMatchText] = useState('')
  const [financeAccountId, setFinanceAccountId] = useState('')
  const [minimumAmount, setMinimumAmount] = useState('')
  const [maximumAmount, setMaximumAmount] = useState('')
  const [spendingCategoryId, setSpendingCategoryId] = useState('')
  const [isTransfer, setIsTransfer] = useState(false)
  const [rulePriority, setRulePriority] = useState('')

  const accountList = accounts.data?.FinanceAccounts ?? []
  const list = [...(rules.data?.SpendingRules ?? [])].sort((left, right) => left.rulePriority - right.rulePriority)

  const open = (rule: SpendingRule | 'new') => {
    const existing = rule === 'new' ? null : rule
    setMatchText(existing?.matchText ?? '')
    setFinanceAccountId(existing?.financeAccountId ?? '')
    setMinimumAmount(existing?.minimumAmount ?? '')
    setMaximumAmount(existing?.maximumAmount ?? '')
    setSpendingCategoryId(existing?.spendingCategoryId ?? spendingCategoryOptions(categories, categoryName)[0]?.value ?? '')
    setIsTransfer(existing?.isTransfer ?? false)
    setRulePriority(existing ? String(existing.rulePriority) : '')
    setEditing(rule)
  }

  const describe = (rule: SpendingRule): string => {
    const parts: string[] = []
    if (rule.spendingCategoryId || !rule.isTransfer) {
      const category = categories.find((candidate) => candidate.id === rule.spendingCategoryId)
      parts.push(
        t('finance.ruleFiles', {
          name: category ? spendingCategoryLabel(category, categories, categoryName) : t('finance.deletedSpendingCategory'),
        }),
      )
    }
    if (rule.isTransfer) parts.push(t('finance.ruleMarksTransfer'))
    const account = accountList.find((candidate) => candidate.id === rule.financeAccountId)
    if (account) parts.push(t('finance.ruleOnAccount', { account: accountLabel(account) }))
    if (rule.minimumAmount) parts.push(t('finance.ruleAtLeast', { amount: rule.minimumAmount }))
    if (rule.maximumAmount) parts.push(t('finance.ruleAtMost', { amount: rule.maximumAmount }))
    parts.push(t('finance.rulePriorityShown', { priority: rule.rulePriority }))
    return parts.join(' · ')
  }

  const isAmountValid = (typed: string) => typed.trim() === '' || isDecimal(typed)
  const canSubmit =
    matchText.trim() !== '' &&
    (isTransfer || spendingCategoryId !== '') &&
    isAmountValid(minimumAmount) &&
    isAmountValid(maximumAmount) &&
    (rulePriority.trim() === '' || /^\d+$/.test(rulePriority.trim()))

  return (
    <SettingsSection
      card
      title={t('finance.spendingRulesTitle')}
      description={t('finance.spendingRulesHint')}
      action={
        <button type="button" className="primary" onClick={() => open('new')}>
          {t('finance.addSpendingRule')}
        </button>
      }
    >
      <ErrorMessage error={rules.error} />
      {rules.loading && !rules.data ? <Loading /> : null}
      {rules.data && list.length === 0 ? <SettingsEmpty>{t('finance.noSpendingRules')}</SettingsEmpty> : null}
      {list.map((rule) => (
        <SettingsRow
          key={rule.id}
          title={rule.matchText}
          subtitle={describe(rule)}
          actions={
            <div className="row-actions">
              <Tooltip label={t('common.edit')}>
                <button
                  type="button"
                  className="icon-action"
                  aria-label={`${rule.matchText}: ${t('common.edit')}`}
                  onClick={() => open(rule)}
                >
                  <PencilIcon size={16} />
                </button>
              </Tooltip>
              <Tooltip label={t('common.delete')}>
                <button
                  type="button"
                  className="icon-action danger"
                  aria-label={`${rule.matchText}: ${t('common.delete')}`}
                  onClick={() => setDeleting(rule)}
                >
                  <TrashIcon size={16} />
                </button>
              </Tooltip>
            </div>
          }
        />
      ))}
      {editing ? (
        <FormDialog
          title={editing === 'new' ? t('finance.addSpendingRule') : t('finance.editSpendingRule')}
          submitLabel={editing === 'new' ? t('common.create') : t('common.save')}
          busy={busy}
          canSubmit={canSubmit}
          onClose={() => setEditing(null)}
          onSubmit={() => {
            const variables = {
              matchText: matchText.trim(),
              financeAccountId,
              minimumAmount: minimumAmount.trim(),
              maximumAmount: maximumAmount.trim(),
              // Sent as chosen whether or not it marks transfers too:
              // ticking the box once erased the rule's spending category.
              spendingCategoryId,
              isTransfer,
              rulePriority: rulePriority.trim() === '' ? undefined : Number(rulePriority.trim()),
            }
            void act(
              () =>
                editing === 'new'
                  ? graphql(CREATE_SPENDING_RULE, variables)
                  : graphql(UPDATE_SPENDING_RULE, { ...variables, spendingRuleId: editing.id }),
              t('finance.spendingRuleSaved'),
            ).then((isDone) => {
              if (isDone) setEditing(null)
            })
          }}
        >
          <label>
            <span>{t('finance.matchText')}</span>
            <input value={matchText} onChange={(event) => setMatchText(event.target.value)} />
          </label>
          <p className="muted field-hint">{t('finance.matchTextHint')}</p>
          <label className="checkbox">
            <input type="checkbox" checked={isTransfer} onChange={(event) => setIsTransfer(event.target.checked)} />
            {t('finance.ruleIsTransfer')}
          </label>
          <label>
            <span>{t('finance.spendingCategory')}</span>
            <Select
              block
              value={spendingCategoryId}
              label={t('finance.spendingCategory')}
              options={[
                // A rule that marks transfers need not file them anywhere.
                ...(isTransfer ? [{ value: '', label: t('finance.uncategorized') }] : []),
                ...spendingCategoryOptions(categories, categoryName, spendingCategoryId),
              ]}
              onChange={setSpendingCategoryId}
            />
          </label>
          <label>
            <span>{t('finance.account')}</span>
            <Select
              block
              value={financeAccountId}
              label={t('finance.account')}
              options={[
                { value: '', label: t('finance.allAccounts') },
                ...accountList.map((account) => ({ value: account.id, label: accountLabel(account) })),
              ]}
              onChange={setFinanceAccountId}
            />
          </label>
          <div className="row">
            <label>
              <span>{t('finance.minimumAmount')}</span>
              <input
                inputMode="decimal"
                value={minimumAmount}
                onChange={(event) => setMinimumAmount(event.target.value)}
              />
            </label>
            <label>
              <span>{t('finance.maximumAmount')}</span>
              <input
                inputMode="decimal"
                value={maximumAmount}
                onChange={(event) => setMaximumAmount(event.target.value)}
              />
            </label>
          </div>
          <p className="muted field-hint">{t('finance.amountRangeHint')}</p>
          <label>
            <span>{t('finance.rulePriority')}</span>
            <input inputMode="numeric" value={rulePriority} onChange={(event) => setRulePriority(event.target.value)} />
          </label>
          <p className="muted field-hint">{t('finance.rulePriorityHint')}</p>
        </FormDialog>
      ) : null}
      {deleting ? (
        <ConfirmDialog
          title={t('finance.deleteSpendingRule')}
          body={t('finance.deleteSpendingRuleBody', { match: deleting.matchText })}
          confirmLabel={t('common.delete')}
          busy={busy}
          onClose={() => setDeleting(null)}
          onConfirm={() => {
            void run(DELETE_SPENDING_RULE, { spendingRuleId: deleting.id }, t('finance.spendingRuleDeleted')).then(() =>
              setDeleting(null),
            )
          }}
        />
      ) : null}
    </SettingsSection>
  )
}

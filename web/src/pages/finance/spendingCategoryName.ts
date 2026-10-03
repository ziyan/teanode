import { Key, useTranslation } from '../../i18n/i18n'

// The spending categories every person starts with are stored under the
// server's built-in names, in English and in lowercase, because rules,
// budgets and the provider category mapping find them by those names. A
// person reads them in their own language, capitalized; a name they chose
// themselves is shown as they wrote it. Nothing stored is renamed: this is
// only how a name is shown, and a form that edits a name edits the stored
// one.

// The built-in names, as the server stores them, and the words each is
// shown with.
const BUILT_IN_SPENDING_CATEGORY_KEYS: Record<string, Key> = {
  income: 'finance.builtInSpendingCategory.income',
  housing: 'finance.builtInSpendingCategory.housing',
  utilities: 'finance.builtInSpendingCategory.utilities',
  groceries: 'finance.builtInSpendingCategory.groceries',
  dining: 'finance.builtInSpendingCategory.dining',
  transport: 'finance.builtInSpendingCategory.transport',
  travel: 'finance.builtInSpendingCategory.travel',
  shopping: 'finance.builtInSpendingCategory.shopping',
  health: 'finance.builtInSpendingCategory.health',
  entertainment: 'finance.builtInSpendingCategory.entertainment',
  subscriptions: 'finance.builtInSpendingCategory.subscriptions',
  fees: 'finance.builtInSpendingCategory.fees',
  'gifts and donations': 'finance.builtInSpendingCategory.giftsAndDonations',
  taxes: 'finance.builtInSpendingCategory.taxes',
  loans: 'finance.builtInSpendingCategory.loans',
  education: 'finance.builtInSpendingCategory.education',
  children: 'finance.builtInSpendingCategory.children',
  'business services': 'finance.builtInSpendingCategory.businessServices',
}

// The transfer category and the other category are found by their flags,
// never their names: a person who already had a "transfer" or an "other"
// of their own keeps it, and the built-in one beside it is named something
// else. Only the flagged one, still under its built-in name, is shown in
// the reader's words.
const FLAGGED_SPENDING_CATEGORY_NAMES: Record<string, { flag: keyof BuiltInFlags; key: Key }> = {
  transfer: { flag: 'isTransfer', key: 'finance.builtInSpendingCategory.transfer' },
  other: { flag: 'isOther', key: 'finance.builtInSpendingCategory.other' },
}

// BuiltInFlags say which built-in category a spending category is, as
// SpendingCategory carries them; a category passed whole will do.
export type BuiltInFlags = { isTransfer?: boolean; isOther?: boolean }

// spendingCategoryDisplayName is how a stored spending category name is
// shown: a built-in name, exactly as stored, in the reader's words, and
// anything else as it is. "Groceries" typed by a person is theirs, not the
// built-in "groceries", and is left alone. flags say whether the category
// is the transfer category or the other category.
export function spendingCategoryDisplayName(
  name: string,
  translate: (key: Key) => string,
  flags: BuiltInFlags = {},
): string {
  if (Object.prototype.hasOwnProperty.call(FLAGGED_SPENDING_CATEGORY_NAMES, name)) {
    const flagged = FLAGGED_SPENDING_CATEGORY_NAMES[name]
    return flags[flagged.flag] ? translate(flagged.key) : name
  }
  const key = Object.prototype.hasOwnProperty.call(BUILT_IN_SPENDING_CATEGORY_KEYS, name)
    ? BUILT_IN_SPENDING_CATEGORY_KEYS[name]
    : undefined
  return key ? translate(key) : name
}

// useSpendingCategoryDisplayName is spendingCategoryDisplayName in the
// reader's language.
export function useSpendingCategoryDisplayName(): (name: string, flags?: BuiltInFlags) => string {
  const { t } = useTranslation()
  return (name: string, flags?: BuiltInFlags) => spendingCategoryDisplayName(name, t, flags)
}

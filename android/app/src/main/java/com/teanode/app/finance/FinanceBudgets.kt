package com.teanode.app.finance

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.ExposedDropdownMenuBox
import androidx.compose.material3.ExposedDropdownMenuDefaults
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.session.LocalConnection
import com.teanode.app.shell.LocalAppNavigator
import com.teanode.app.ui.components.ConfirmationDialog
import com.teanode.app.ui.components.LoadedContent
import com.teanode.app.ui.components.LoadState
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.app.ui.components.TeaNodeRow
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.app.ui.components.rememberLoad
import com.teanode.kit.finance.Budget
import com.teanode.kit.finance.BudgetStatus
import com.teanode.kit.finance.FinanceMoney
import com.teanode.kit.finance.SpendingCategory
import com.teanode.kit.finance.SpendingRule
import com.teanode.kit.finance.budgetStatus
import com.teanode.kit.finance.budgets
import com.teanode.kit.finance.deleteSpendingCategory
import com.teanode.kit.finance.deleteSpendingRule
import com.teanode.kit.finance.saveSpendingCategory
import com.teanode.kit.finance.saveSpendingRule
import com.teanode.kit.finance.setBudget
import com.teanode.kit.finance.spendingRules
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch

private class BudgetsRead(val base: FinanceBase, val status: BudgetStatus?, val budgets: List<Budget>, val rules: List<SpendingRule>)

/** This month's budgets, and the categories and rules that decide where a charge goes. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun FinanceBudgetsScreen() {
    val connection = LocalConnection.current
    val navigator = LocalAppNavigator.current
    val snackbar = LocalSnackbar.current
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val read = rememberLoad(connection) {
        val base = connection.readFinanceBase(context)
        BudgetsRead(base, connection.budgetStatus(base.thisMonth.toString()), connection.budgets(), connection.spendingRules())
    }
    var budgetEditing by remember { mutableStateOf<BudgetDraft?>(null) }
    var categoryEditing by remember { mutableStateOf<CategoryDraft?>(null) }
    var ruleEditing by remember { mutableStateOf<RuleDraft?>(null) }
    var deletingCategory by remember { mutableStateOf<SpendingCategory?>(null) }
    var deletingRule by remember { mutableStateOf<SpendingRule?>(null) }

    fun act(saying: String, state: LoadState<BudgetsRead>, work: suspend () -> Unit) {
        scope.launch {
            try { work(); snackbar.show(saying) } catch (failure: CancellationException) { throw failure } catch (failure: Throwable) { snackbar.showError(failure) }
            state.refresh()
        }
    }

    TeaNodeScreen(title = stringResource(R.string.finance_part_budgets), onBack = { navigator.back() }) { padding ->
        LoadedContent(read, modifier = Modifier.padding(padding)) { loaded ->
            val base = loaded.base
            LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 24.dp)) {
                val rows = loaded.status?.spendingCategories.orEmpty().filterNotNull()
                if (rows.isNotEmpty()) {
                    item("this-month") { TeaNodeSectionHeader(stringResource(R.string.finance_this_month)) }
                    for (row in rows) {
                        item("budget-" + row.spendingCategoryId) {
                            val name = if (base.category(row.spendingCategoryId) != null) base.categoryName(row.spendingCategoryId) else FinanceCategoryNames.shown(context, row.spendingCategoryName ?: "")
                            androidx.compose.foundation.layout.Box(Modifier.clickable {
                                budgetEditing = BudgetDraft(row.spendingCategoryId, loaded.budgets.firstOrNull { budget -> budget.spendingCategoryId == row.spendingCategoryId }?.monthlyAmount.orEmpty(), row.currencyCode.orEmpty(), true)
                            }) { BudgetRow(name, row.spendingAmount, row.budgetAmount, row.projectedAmount, row.budgetPace, row.currencyCode) }
                        }
                    }
                }
                item("set-budget") { TextButton(onClick = { budgetEditing = BudgetDraft("", "", base.reportingCurrency, false) }, modifier = Modifier.padding(horizontal = 8.dp)) { Icon(Icons.Outlined.Add, null); Text(stringResource(R.string.finance_set_budget)) } }
                item("categories-header") { TeaNodeSectionHeader(stringResource(R.string.finance_categories)) }
                for (category in base.pickable(null)) {
                    item("category-" + category.id) {
                        TeaNodeRow(
                            base.label(category), supporting = if (category.isIncome == true) stringResource(R.string.finance_income) else null,
                            onClick = { categoryEditing = CategoryDraft(category.id, category.spendingCategoryName, category.parentSpendingCategoryId.orEmpty(), category.isIncome == true, category.isHidden == true) },
                        )
                    }
                }
                item("new-category") { TextButton(onClick = { categoryEditing = CategoryDraft() }, modifier = Modifier.padding(horizontal = 8.dp)) { Icon(Icons.Outlined.Add, null); Text(stringResource(R.string.finance_new_category)) } }
                item("rules-header") { TeaNodeSectionHeader(stringResource(R.string.finance_rules)) }
                for (rule in loaded.rules) {
                    item("rule-" + rule.id) {
                        TeaNodeRow(
                            "“${rule.matchText}”", supporting = base.categoryName(rule.spendingCategoryId),
                            trailing = { TextButton(onClick = { deletingRule = rule }) { Text(stringResource(R.string.finance_delete), color = MaterialTheme.colorScheme.error) } },
                            onClick = { ruleEditing = RuleDraft(rule.id, rule.matchText, rule.spendingCategoryId.orEmpty(), rule.financeAccountId.orEmpty(), rule.minimumAmount.orEmpty(), rule.maximumAmount.orEmpty(), rule.rulePriority?.toString().orEmpty()) },
                        )
                    }
                }
                item("new-rule") { TextButton(onClick = { ruleEditing = RuleDraft() }, modifier = Modifier.padding(horizontal = 8.dp)) { Icon(Icons.Outlined.Add, null); Text(stringResource(R.string.finance_new_rule)) } }
                item("rules-footer") { Footnote(stringResource(R.string.finance_rules_footer)) }
            }
            budgetEditing?.let { draft ->
                BudgetEditorDialog(draft, base, onDismiss = { budgetEditing = null }) { saved ->
                    budgetEditing = null
                    act(context.getString(R.string.finance_budget_saved), read) { connection.setBudget(saved.categoryId, saved.amount.ifBlank { "0" }, saved.currency, base.thisMonth.atDay(1).toString()) }
                }
            }
            categoryEditing?.let { draft ->
                CategoryEditorDialog(draft, base, onDismiss = { categoryEditing = null }, onDelete = { deletingCategory = base.category(draft.categoryId); categoryEditing = null }) { saved ->
                    categoryEditing = null
                    act(context.getString(R.string.finance_category_saved), read) { connection.saveSpendingCategory(saved.categoryId, saved.name.trim(), saved.parentId, saved.isIncome, saved.isHidden) }
                }
            }
            ruleEditing?.let { draft ->
                RuleEditorDialog(draft, base, onDismiss = { ruleEditing = null }) { saved ->
                    ruleEditing = null
                    act(context.getString(R.string.finance_rule_saved), read) {
                        connection.saveSpendingRule(saved.ruleId, saved.matchText.trim(), saved.categoryId, saved.accountId, saved.minimum.trim(), saved.maximum.trim(), saved.priority.trim().toIntOrNull())
                    }
                }
            }
            deletingCategory?.let { category ->
                ConfirmationDialog(
                    title = stringResource(R.string.finance_delete_category_question, base.label(category)), text = stringResource(R.string.finance_delete_category_message),
                    confirmLabel = stringResource(R.string.finance_delete), isDestructive = true,
                    onConfirm = { deletingCategory = null; act(context.getString(R.string.finance_category_deleted), read) { connection.deleteSpendingCategory(category.id) } },
                    onDismiss = { deletingCategory = null },
                )
            }
            deletingRule?.let { rule ->
                ConfirmationDialog(
                    title = stringResource(R.string.finance_delete_rule_question), confirmLabel = stringResource(R.string.finance_delete), isDestructive = true,
                    onConfirm = { deletingRule = null; act(context.getString(R.string.finance_rule_deleted), read) { connection.deleteSpendingRule(rule.id) } },
                    onDismiss = { deletingRule = null },
                )
            }
        }
    }
}

/** A budget being set: for a category, an amount a month, from this month on; zero ends it. */
internal class BudgetDraft(val categoryId: String, val amount: String, val currency: String, val isEditing: Boolean)

internal class CategoryDraft(val categoryId: String? = null, val name: String = "", val parentId: String = "", val isIncome: Boolean = false, val isHidden: Boolean = false)

internal class RuleDraft(
    val ruleId: String? = null, val matchText: String = "", val categoryId: String = "", val accountId: String = "", val minimum: String = "", val maximum: String = "", val priority: String = "",
)

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun CategoryPicker(base: FinanceBase, chosen: String, label: String, noneLabel: String?, onChoose: (String) -> Unit) {
    var isOpen by remember { mutableStateOf(false) }
    ExposedDropdownMenuBox(isOpen, { isOpen = it }) {
        OutlinedTextField(
            value = if (chosen.isEmpty()) noneLabel.orEmpty() else base.categoryName(chosen), onValueChange = {}, readOnly = true, label = { Text(label) },
            trailingIcon = { ExposedDropdownMenuDefaults.TrailingIcon(isOpen) }, modifier = Modifier.menuAnchor().fillMaxWidth(),
        )
        ExposedDropdownMenu(isOpen, { isOpen = false }) {
            if (noneLabel != null) DropdownMenuItem(text = { Text(noneLabel) }, onClick = { onChoose(""); isOpen = false })
            for (category in base.pickable(chosen)) DropdownMenuItem(text = { Text(base.label(category)) }, onClick = { onChoose(category.id); isOpen = false })
        }
    }
}

@Composable
private fun BudgetEditorDialog(draft: BudgetDraft, base: FinanceBase, onDismiss: () -> Unit, onSave: (BudgetDraft) -> Unit) {
    var categoryId by remember { mutableStateOf(draft.categoryId) }
    var amount by remember { mutableStateOf(draft.amount) }
    AlertDialog(
        onDismissRequest = onDismiss, title = { Text(stringResource(if (draft.isEditing) R.string.finance_change_budget else R.string.finance_set_budget)) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                if (draft.isEditing) Text(base.categoryName(categoryId)) else CategoryPicker(base, categoryId, stringResource(R.string.finance_category), null) { categoryId = it }
                OutlinedTextField(amount, { amount = it }, label = { Text(stringResource(R.string.finance_amount_a_month)) }, singleLine = true, supportingText = { Text(stringResource(R.string.finance_budget_zero_ends)) })
            }
        },
        confirmButton = { TextButton(onClick = { onSave(BudgetDraft(categoryId, amount.trim(), draft.currency, draft.isEditing)) }, enabled = categoryId.isNotEmpty() && FinanceMoney.isDecimal(amount.trim().ifEmpty { "0" })) { Text(stringResource(R.string.finance_save)) } },
        dismissButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.finance_cancel)) } },
    )
}

@Composable
private fun CategoryEditorDialog(draft: CategoryDraft, base: FinanceBase, onDismiss: () -> Unit, onDelete: () -> Unit, onSave: (CategoryDraft) -> Unit) {
    var name by remember { mutableStateOf(draft.name) }
    var parentId by remember { mutableStateOf(draft.parentId) }
    var isIncome by remember { mutableStateOf(draft.isIncome) }
    var isHidden by remember { mutableStateOf(draft.isHidden) }
    AlertDialog(
        onDismissRequest = onDismiss, title = { Text(stringResource(if (draft.categoryId == null) R.string.finance_new_category else R.string.finance_edit_category)) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                OutlinedTextField(name, { name = it }, label = { Text(stringResource(R.string.finance_name)) }, singleLine = true)
                CategoryPicker(base, parentId, stringResource(R.string.finance_inside), stringResource(R.string.finance_none)) { parentId = it }
                Row(verticalAlignment = Alignment.CenterVertically) { Text(stringResource(R.string.finance_income), Modifier.weight(1f)); Switch(isIncome, { isIncome = it }) }
                Row(verticalAlignment = Alignment.CenterVertically) { Text(stringResource(R.string.finance_hidden), Modifier.weight(1f)); Switch(isHidden, { isHidden = it }) }
                if (draft.categoryId != null) TextButton(onClick = onDelete) { Text(stringResource(R.string.finance_delete), color = MaterialTheme.colorScheme.error) }
            }
        },
        confirmButton = { TextButton(onClick = { onSave(CategoryDraft(draft.categoryId, name, parentId, isIncome, isHidden)) }, enabled = name.isNotBlank()) { Text(stringResource(R.string.finance_save)) } },
        dismissButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.finance_cancel)) } },
    )
}

@Composable
private fun RuleEditorDialog(draft: RuleDraft, base: FinanceBase, onDismiss: () -> Unit, onSave: (RuleDraft) -> Unit) {
    var matchText by remember { mutableStateOf(draft.matchText) }
    var categoryId by remember { mutableStateOf(draft.categoryId) }
    var minimum by remember { mutableStateOf(draft.minimum) }
    var maximum by remember { mutableStateOf(draft.maximum) }
    var priority by remember { mutableStateOf(draft.priority) }
    AlertDialog(
        onDismissRequest = onDismiss, title = { Text(stringResource(if (draft.ruleId == null) R.string.finance_new_rule else R.string.finance_edit_rule)) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                OutlinedTextField(matchText, { matchText = it }, label = { Text(stringResource(R.string.finance_rule_words)) }, singleLine = true)
                CategoryPicker(base, categoryId, stringResource(R.string.finance_file_under), null) { categoryId = it }
                OutlinedTextField(minimum, { minimum = it }, label = { Text(stringResource(R.string.finance_rule_minimum)) }, singleLine = true)
                OutlinedTextField(maximum, { maximum = it }, label = { Text(stringResource(R.string.finance_rule_maximum)) }, singleLine = true)
                OutlinedTextField(priority, { priority = it }, label = { Text(stringResource(R.string.finance_rule_priority)) }, singleLine = true)
            }
        },
        confirmButton = { TextButton(onClick = { onSave(RuleDraft(draft.ruleId, matchText, categoryId, draft.accountId, minimum, maximum, priority)) }, enabled = matchText.isNotBlank() && categoryId.isNotEmpty()) { Text(stringResource(R.string.finance_save)) } },
        dismissButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.finance_cancel)) } },
    )
}

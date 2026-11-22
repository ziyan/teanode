package com.teanode.app.shopping

import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.longClick
import androidx.compose.ui.test.performTextInput
import androidx.compose.ui.test.performTouchInput
import com.teanode.app.demo.DemoHost
import com.teanode.app.screenshots.Look
import com.teanode.app.screenshots.PHONE_QUALIFIERS
import com.teanode.app.screenshots.WithFontScale
import com.teanode.app.screenshots.photograph
import com.teanode.app.session.LocalConnection
import com.teanode.kit.shopping.ShoppingItem
import org.junit.After
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

/** Photographs of every Shopping screen and state, light and dark, and the dense ones at the largest text. */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], qualifiers = PHONE_QUALIFIERS)
class ShoppingScreenshotTest {
    @get:Rule
    val composeRule = createComposeRule()

    @After
    fun resetDemo() {
        ShoppingDemoData.listsOverride = null
        ShoppingDemoData.isResumable = false
        ShoppingDemoData.isShoppingNow = false
    }

    private fun shoot(name: String, look: Look, fontScale: Float? = null, isSheet: Boolean = false, content: @Composable () -> Unit) {
        composeRule.setContent {
            DemoHost(appearance = look.appearance) {
                CompositionLocalProvider(LocalShoppingSheetsInline provides isSheet) {
                    if (fontScale != null) WithFontScale(fontScale) { content() } else content()
                }
            }
        }
        composeRule.waitForIdle()
        composeRule.photograph("shopping-$name-${look.fileName}")
    }

    private fun item(id: String): ShoppingItem = ShoppingDemoData.itemById(id).let { demo ->
        com.teanode.kit.shopping.ShoppingJson.decodeFromString(ShoppingItem.serializer(), demo.json())
    }

    @Test fun `lists light`() = shoot("lists", Look.Light) { ShoppingSection() }
    @Test fun `lists dark`() = shoot("lists", Look.Dark) { ShoppingSection() }
    @Test fun `lists largest text`() = shoot("lists-largest-text", Look.Light, 2f) { ShoppingSection() }

    @Test fun `lists empty`() {
        ShoppingDemoData.listsOverride = "[]"
        shoot("lists-empty", Look.Light) { ShoppingSection() }
    }

    @Test fun `list light`() = shoot("list", Look.Light) { ShoppingListScreen(ShoppingDemoData.GROCERIES_LIST_ID) }
    @Test fun `list dark`() = shoot("list", Look.Dark) { ShoppingListScreen(ShoppingDemoData.GROCERIES_LIST_ID) }
    @Test fun `list largest text`() = shoot("list-largest-text", Look.Light, 2f) { ShoppingListScreen(ShoppingDemoData.GROCERIES_LIST_ID) }
    @Test fun `list pictures`() = shoot("list-pictures", Look.Light) { ShoppingListScreen(ShoppingDemoData.GROCERIES_LIST_ID, ShoppingLayoutChoice.Pictures) }
    @Test fun `list pictures dark`() = shoot("list-pictures", Look.Dark) { ShoppingListScreen(ShoppingDemoData.GROCERIES_LIST_ID, ShoppingLayoutChoice.Pictures) }
    @Test fun `not available to the account`() = shoot("not-available", Look.Light) { ShoppingNotAllowedView() }
    @Test fun `not available to the account dark`() = shoot("not-available", Look.Dark) { ShoppingNotAllowedView() }

    @Test fun `item menu after a long press`() {
        composeRule.setContent { DemoHost(appearance = Look.Light.appearance) { ShoppingListScreen(ShoppingDemoData.GROCERIES_LIST_ID) } }
        composeRule.waitForIdle()
        composeRule.onNodeWithText("Apples").performTouchInput { longClick() }
        composeRule.waitForIdle()
        composeRule.onNodeWithText("Remove").assertExists()
        composeRule.photograph("shopping-item-menu-light")
    }

    @Test fun `remove confirmation`() {
        composeRule.setContent { DemoHost(appearance = Look.Light.appearance) { ShoppingListScreen(ShoppingDemoData.GROCERIES_LIST_ID) } }
        composeRule.waitForIdle()
        composeRule.onNodeWithText("Apples").performTouchInput { longClick() }
        composeRule.waitForIdle()
        composeRule.onNodeWithText("Remove").performClick()
        composeRule.waitForIdle()
        composeRule.onNodeWithText("Remove from the list").assertExists()
        composeRule.photograph("shopping-remove-confirmation-light")
    }

    @Test fun `list shared with the person`() = shoot("list-shared", Look.Light) { ShoppingListScreen(ShoppingDemoData.PROJECT_LIST_ID) }

    @Test fun `store light`() = shoot("store", Look.Light) { ShoppingStoreScreen(ShoppingDemoData.EXAMPLE_MARKET, emptyList()) }
    @Test fun `store dark`() = shoot("store", Look.Dark) { ShoppingStoreScreen(ShoppingDemoData.EXAMPLE_MARKET, emptyList()) }
    @Test fun `store largest text`() = shoot("store-largest-text", Look.Light, 2f) { ShoppingStoreScreen(ShoppingDemoData.EXAMPLE_MARKET, emptyList()) }
    @Test fun `store pictures`() = shoot("store-pictures", Look.Light) { ShoppingStoreScreen(ShoppingDemoData.EXAMPLE_MARKET, emptyList(), ShoppingLayoutChoice.Pictures) }
    @Test fun `any store`() = shoot("store-any", Look.Light) { ShoppingStoreScreen("", emptyList()) }

    @Test fun `cart sheet light`() = shoot("cart", Look.Light, isSheet = true) {
        ShoppingCartSheet(listOf(item("demo-item-soup"), item("demo-item-bread")), false, null, {}, {}, {}, {})
    }
    @Test fun `cart sheet dark`() = shoot("cart", Look.Dark, isSheet = true) {
        ShoppingCartSheet(listOf(item("demo-item-soup"), item("demo-item-bread")), false, null, {}, {}, {}, {})
    }
    @Test fun `cart sheet largest text`() = shoot("cart-largest-text", Look.Light, 2f, isSheet = true) {
        ShoppingCartSheet(listOf(item("demo-item-soup"), item("demo-item-bread")), true, null, {}, {}, {}, {})
    }

    @Test fun `list with the resume row`() {
        ShoppingDemoData.isResumable = true
        shoot("list-resume", Look.Light) { ShoppingListScreen(ShoppingDemoData.GROCERIES_LIST_ID) }
    }
    @Test fun `list with the resume row dark`() {
        ShoppingDemoData.isResumable = true
        shoot("list-resume", Look.Dark) { ShoppingListScreen(ShoppingDemoData.GROCERIES_LIST_ID) }
    }
    @Test fun `store with the resume row`() {
        ShoppingDemoData.isResumable = true
        shoot("store-resume", Look.Light) { ShoppingStoreScreen(ShoppingDemoData.EXAMPLE_MARKET, emptyList()) }
    }
    @Test fun `home with the resume row`() {
        ShoppingDemoData.isResumable = true
        shoot("lists-resume", Look.Light) { ShoppingSection() }
    }
    @Test fun `home while shopping`() {
        ShoppingDemoData.isShoppingNow = true
        shoot("lists-shopping", Look.Light) { ShoppingSection() }
    }

    @Test fun `item sheet light`() = shoot("item-sheet", Look.Light, isSheet = true) {
        ShoppingItemSheet(LocalConnection.current, item("demo-item-apples"), {}, {}, {}, null)
    }
    @Test fun `item sheet dark`() = shoot("item-sheet", Look.Dark, isSheet = true) {
        ShoppingItemSheet(LocalConnection.current, item("demo-item-bananas"), {}, {}, {}, {})
    }
    @Test fun `add item sheet`() = shoot("add-item", Look.Light, isSheet = true) {
        ShoppingAddItemSheet(LocalConnection.current, ShoppingDemoData.GROCERIES_LIST_ID, "", {}, { true }, {}, { _, _ -> true })
    }
    @Test fun `add item sheet dark`() = shoot("add-item", Look.Dark, isSheet = true) {
        ShoppingAddItemSheet(LocalConnection.current, ShoppingDemoData.GROCERIES_LIST_ID, ShoppingDemoData.EXAMPLE_MARKET, {}, { true }, {}, { _, _ -> true })
    }
    @Test fun `need again sheet`() = shoot("need-again", Look.Light, isSheet = true) { ShoppingNeedAgainSheet(item("demo-item-bananas"), {}, {}) }
    @Test fun `prices sheet`() = shoot("prices", Look.Light, isSheet = true) {
        val prices = com.teanode.kit.shopping.ShoppingJson.decodeFromString(
            kotlinx.serialization.builtins.ListSerializer(com.teanode.kit.shopping.ShoppingItemPrice.serializer()), ShoppingDemoData.prices,
        )
        ShoppingPricesSheet(prices, "Apples") {}
    }
    @Test fun `share sheet light`() = shoot("share", Look.Light, isSheet = true) {
        ShoppingShareSheet(LocalConnection.current, ShoppingDemoData.GROCERIES_LIST_ID, true, {}, {}, {})
    }
    @Test fun `share sheet dark`() = shoot("share", Look.Dark, isSheet = true) {
        ShoppingShareSheet(LocalConnection.current, ShoppingDemoData.GROCERIES_LIST_ID, true, {}, {}, {})
    }
    @Test fun `import sheet`() = shoot("import", Look.Light, isSheet = true) {
        ShoppingImportSheet(LocalConnection.current, ShoppingDemoData.GROCERIES_LIST_ID, {}, {})
    }
    @Test fun `receipt sheet light`() = shoot("receipt", Look.Light, isSheet = true) {
        ShoppingReceiptSheet(LocalConnection.current, ShoppingDemoData.RECEIPT_ID, {}, {})
    }
    @Test fun `receipt sheet dark`() = shoot("receipt", Look.Dark, isSheet = true) {
        ShoppingReceiptSheet(LocalConnection.current, ShoppingDemoData.RECEIPT_ID, {}, {})
    }

    @Test fun `import sheet after reading`() {
        composeRule.setContent {
            DemoHost {
                CompositionLocalProvider(LocalShoppingSheetsInline provides true) {
                    ShoppingImportSheet(LocalConnection.current, ShoppingDemoData.GROCERIES_LIST_ID, {}, {})
                }
            }
        }
        composeRule.onNodeWithText("Paste or type a list").performTextInput("apples\nbread\noat milk")
        composeRule.waitForIdle()
        composeRule.onNodeWithText("Read the list").performClick()
        composeRule.waitForIdle()
        composeRule.photograph("shopping-import-read-light")
    }
}

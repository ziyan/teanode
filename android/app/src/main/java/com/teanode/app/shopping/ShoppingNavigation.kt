package com.teanode.app.shopping

import android.net.Uri
import androidx.navigation.NavGraphBuilder
import androidx.navigation.NavType
import androidx.navigation.compose.composable
import androidx.navigation.navArgument
import com.teanode.app.shell.AppNavigator
import com.teanode.app.shell.Area
import com.teanode.app.shell.Routes
import com.teanode.kit.AppSection

internal val shoppingListPath = "list/{listId}"
internal val shoppingStorePath = "store/{storeName}?lists={lists}"

/**
 * The routes of Shopping: the lists and stores (first screen), one list, and one store (with the lists to start with).
 * The item, the receipt, the import and the sharing are sheets over a list or a store.
 */
@Suppress("UNUSED_PARAMETER")
fun NavGraphBuilder.shoppingRoutes(navigator: AppNavigator) {
    composable(Routes.home(AppSection.Shopping), deepLinks = Routes.deepLinks(Routes.home(AppSection.Shopping))) { ShoppingSection() }
    val listRoute = Routes.child(AppSection.Shopping, shoppingListPath)
    composable(listRoute, arguments = listOf(navArgument("listId") { type = NavType.StringType }), deepLinks = Routes.deepLinks(listRoute)) { entry ->
        ShoppingListScreen(Uri.decode(entry.arguments?.getString("listId").orEmpty()))
    }
    val storeRoute = Routes.child(AppSection.Shopping, shoppingStorePath)
    composable(
        storeRoute,
        arguments = listOf(
            navArgument("storeName") { type = NavType.StringType },
            navArgument("lists") { type = NavType.StringType; defaultValue = "" },
        ),
        deepLinks = Routes.deepLinks(storeRoute),
    ) { entry ->
        val storeName = Uri.decode(entry.arguments?.getString("storeName").orEmpty()).let { name -> if (name == "-") "" else name }
        val listIds = Uri.decode(entry.arguments?.getString("lists").orEmpty()).split(',').filter { id -> id.isNotEmpty() }
        ShoppingStoreScreen(storeName, listIds)
    }
}

/** What Shopping plugs into the shell; listed in `Areas`. */
val shoppingArea = Area(
    section = AppSection.Shopping,
    routes = NavGraphBuilder::shoppingRoutes,
    demoAnswerers = shoppingDemoAnswerers,
)

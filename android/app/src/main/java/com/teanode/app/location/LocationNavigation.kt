package com.teanode.app.location

import androidx.navigation.NavGraphBuilder
import androidx.navigation.NavType
import com.teanode.app.location.recording.locationComposable
import androidx.navigation.navArgument
import com.teanode.app.R
import com.teanode.app.session.LocalConnection
import com.teanode.app.shell.Area
import com.teanode.app.shell.AppNavigator
import com.teanode.app.shell.AppPlace
import com.teanode.app.shell.Routes
import com.teanode.kit.AppSection
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.*
import androidx.compose.material.icons.outlined.*
import java.time.LocalDate

/** The routes of Location: the map with its drawer, one day, one place, the people, Travel, the settings and the address search. */
fun NavGraphBuilder.locationRoutes(navigator: AppNavigator) {
    locationComposable(LocationRoutes.home, deepLinks = Routes.deepLinks(LocationRoutes.home)) { LocationSection() }
    locationComposable(LocationRoutes.settings, deepLinks = Routes.deepLinks(LocationRoutes.settings)) {
        LocationSettingsScreen(rememberLocationModel(), onBack = { navigator.back() })
    }
    locationComposable(LocationRoutes.travel, deepLinks = Routes.deepLinks(LocationRoutes.travel)) {
        LocationTravelScreen(LocalConnection.current, onBack = { navigator.back() })
    }
    locationComposable(LocationRoutes.personTravel, arguments = listOf(navArgument(LocationRoutes.USER_ARGUMENT) { type = NavType.StringType })) { entry ->
        // Somebody's travel, as far as they share their history with the person.
        LocationTravelScreen(LocalConnection.current, onBack = { navigator.back() }, userId = entry.arguments?.getString(LocationRoutes.USER_ARGUMENT))
    }
    locationComposable(LocationRoutes.people, deepLinks = Routes.deepLinks(LocationRoutes.people)) {
        LocationPeopleScreen(rememberLocationModel(), onBack = { navigator.back() })
    }
    locationComposable(
        LocationRoutes.day, arguments = listOf(navArgument(LocationRoutes.DAY_ARGUMENT) { type = NavType.StringType }), deepLinks = Routes.deepLinks(LocationRoutes.day),
    ) { entry ->
        val day = runCatching { LocalDate.parse(entry.arguments?.getString(LocationRoutes.DAY_ARGUMENT)) }.getOrDefault(LocalDate.now())
        LocationDayScreen(rememberLocationModel(), day, onBack = { navigator.back() }, onOpenPlace = { place -> navigator.navigate(LocationRoutes.place(place.id)) })
    }
    locationComposable(
        LocationRoutes.place, arguments = listOf(navArgument(LocationRoutes.PLACE_ARGUMENT) { type = NavType.StringType }), deepLinks = Routes.deepLinks(LocationRoutes.place),
    ) { entry ->
        LocationPlaceScreen(
            rememberLocationModel(), entry.arguments?.getString(LocationRoutes.PLACE_ARGUMENT).orEmpty(), onBack = { navigator.back() },
            onChangeAddress = { place -> navigator.navigate(LocationRoutes.placeAddress(place.id)) },
        )
    }
    locationComposable(
        LocationRoutes.placeAddress, arguments = listOf(navArgument(LocationRoutes.PLACE_ARGUMENT) { type = NavType.StringType }),
    ) { entry ->
        val model = rememberLocationModel()
        val placeId = entry.arguments?.getString(LocationRoutes.PLACE_ARGUMENT).orEmpty()
        LocationAddressScreen(
            model, onBack = { navigator.back() }, placeId = placeId,
            keepAsTyped = { typed ->
                val result = model.saveAddress(com.teanode.kit.location.PlaceChange(placeId = placeId, addressText = typed))
                if (result.first != null) model.say(model.keptAddress(), false)
                result.second
            },
        ) { address ->
            model.saveAddress(
                com.teanode.kit.location.PlaceChange(placeId = placeId, latitudeDegrees = address.latitudeDegrees, longitudeDegrees = address.longitudeDegrees, addressText = model.chosenAddress(address)),
            ).second
        }
    }
    locationComposable(LocationRoutes.homeAddress) {
        val model = rememberLocationModel()
        LocationAddressScreen(model, onBack = { navigator.back() }) { address -> model.markHome(address) }
    }
}

/** The places Search offers by name. */
private fun locationPlaces(): List<AppPlace> = listOf(
    AppPlace("location-settings", R.string.location_settings_title, LocationRoutes.settings, Icons.Outlined.Settings, listOf(R.string.location_title)),
    AppPlace("location-travel", R.string.location_travel, LocationRoutes.travel, Icons.AutoMirrored.Outlined.DirectionsWalk, listOf(R.string.location_title)),
)

/** What Location plugs into the shell; listed in `Areas`. */
val locationArea = Area(
    section = AppSection.Location,
    routes = NavGraphBuilder::locationRoutes,
    places = ::locationPlaces,
    demoAnswerers = locationDemoAnswerers,
)

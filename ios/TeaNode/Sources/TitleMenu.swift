import SwiftUI

extension View {
    /// titleMenu is the menu under a screen's title that switches to the
    /// screen's other pages, as Finance chooses Receipts or Taxes.
    ///
    /// iOS draws the menu's chevron only on the small title in the bar. A
    /// large title has none, and the small title shows only once the page
    /// has scrolled, so a page with nothing to scroll would have no way to
    /// its other pages. A screen with a title menu therefore always uses the
    /// small title (make check-title-menus).
    func titleMenu<MenuContent: View>(@ViewBuilder _ menuContent: () -> MenuContent) -> some View {
        let menu = menuContent()
        return navigationBarTitleDisplayMode(.inline)
            // check-title-menus: allow, the one place the menu is attached
            .toolbarTitleMenu { menu }
    }
}

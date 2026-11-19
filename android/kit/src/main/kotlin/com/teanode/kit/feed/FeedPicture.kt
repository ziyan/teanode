package com.teanode.kit.feed

/**
 * FeedPictureKind is what a card's picture shows, named by the subject. The agent picks an SF Symbol name for a card
 * (`cloud.sun`, `lock.shield`); the app draws the Material icon for the kind that name stands for.
 */
enum class FeedPictureKind { Weather, Sun, Cloud, Rain, Snow, Storm, Night, Wind, Security, Chart, News, Tray, Mail, Calendar, Money, Shopping, Travel, Home, Health, Idea, Alert, Person, Place, Key, Code, Memory, Done, Time, Other }

/** FeedPicture is a card's picture: a kind of symbol, or an emoji the agent chose. */
sealed class FeedPicture {
    data class Symbol(val kind: FeedPictureKind) : FeedPicture()

    data class Emoji(val text: String) : FeedPicture()

    companion object {
        /**
         * The picture for [pictureSymbol]: an emoji is drawn as it is, a symbol name by the longest known prefix of it
         * (`cloud.sun.fill` is the weather like `cloud.sun`), and any name not known is a newspaper, as on the dashboard.
         */
        fun of(pictureSymbol: String): FeedPicture {
            val written = pictureSymbol.trim()
            if (written.isEmpty()) return Symbol(FeedPictureKind.News)
            if (written.any { character -> !character.isLetterOrDigit() && character != '.' && character != '-' && character != '_' && !character.isWhitespace() } ||
                written.codePoints().anyMatch { codePoint -> codePoint > 0x2000 }
            ) {
                return Emoji(written)
            }
            val name = written.lowercase()
            val known = symbolKinds.entries.filter { entry -> name == entry.key || name.startsWith(entry.key + ".") }.maxByOrNull { entry -> entry.key.length }
            return Symbol(known?.value ?: FeedPictureKind.News)
        }

        private val symbolKinds: Map<String, FeedPictureKind> = mapOf(
            "cloud.sun" to FeedPictureKind.Weather, "cloud.moon" to FeedPictureKind.Night, "cloud" to FeedPictureKind.Cloud, "cloud.fog" to FeedPictureKind.Cloud,
            "cloud.rain" to FeedPictureKind.Rain, "cloud.drizzle" to FeedPictureKind.Rain, "cloud.heavyrain" to FeedPictureKind.Rain, "umbrella" to FeedPictureKind.Rain,
            "cloud.snow" to FeedPictureKind.Snow, "snowflake" to FeedPictureKind.Snow, "cloud.bolt" to FeedPictureKind.Storm, "bolt" to FeedPictureKind.Storm,
            "sun" to FeedPictureKind.Sun, "moon" to FeedPictureKind.Night, "wind" to FeedPictureKind.Wind, "thermometer" to FeedPictureKind.Weather,
            "lock" to FeedPictureKind.Security, "shield" to FeedPictureKind.Security, "hand.raised" to FeedPictureKind.Security, "exclamationmark.shield" to FeedPictureKind.Security,
            "chart" to FeedPictureKind.Chart, "waveform" to FeedPictureKind.Chart, "gauge" to FeedPictureKind.Chart, "percent" to FeedPictureKind.Chart,
            "newspaper" to FeedPictureKind.News, "doc" to FeedPictureKind.News, "book" to FeedPictureKind.News, "text.bubble" to FeedPictureKind.News,
            "tray" to FeedPictureKind.Tray, "folder" to FeedPictureKind.Tray, "archivebox" to FeedPictureKind.Tray,
            "envelope" to FeedPictureKind.Mail, "mail" to FeedPictureKind.Mail, "paperplane" to FeedPictureKind.Mail,
            "calendar" to FeedPictureKind.Calendar, "clock" to FeedPictureKind.Time, "alarm" to FeedPictureKind.Time, "timer" to FeedPictureKind.Time, "hourglass" to FeedPictureKind.Time,
            "creditcard" to FeedPictureKind.Money, "dollarsign" to FeedPictureKind.Money, "banknote" to FeedPictureKind.Money, "building.columns" to FeedPictureKind.Money,
            "cart" to FeedPictureKind.Shopping, "bag" to FeedPictureKind.Shopping, "gift" to FeedPictureKind.Shopping, "tag" to FeedPictureKind.Shopping,
            "airplane" to FeedPictureKind.Travel, "car" to FeedPictureKind.Travel, "bus" to FeedPictureKind.Travel, "tram" to FeedPictureKind.Travel, "suitcase" to FeedPictureKind.Travel,
            "house" to FeedPictureKind.Home, "lightbulb" to FeedPictureKind.Idea, "sparkles" to FeedPictureKind.Idea, "brain" to FeedPictureKind.Memory,
            "heart" to FeedPictureKind.Health, "cross" to FeedPictureKind.Health, "figure" to FeedPictureKind.Health, "pills" to FeedPictureKind.Health,
            "exclamationmark" to FeedPictureKind.Alert, "bell" to FeedPictureKind.Alert, "flag" to FeedPictureKind.Alert, "megaphone" to FeedPictureKind.Alert,
            "person" to FeedPictureKind.Person, "location" to FeedPictureKind.Place, "mappin" to FeedPictureKind.Place, "map" to FeedPictureKind.Place,
            "key" to FeedPictureKind.Key, "terminal" to FeedPictureKind.Code, "chevron.left.forwardslash.chevron.right" to FeedPictureKind.Code,
            "checkmark" to FeedPictureKind.Done,
        )
    }
}

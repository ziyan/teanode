import SwiftUI
import TeaNodeKit

/// AvatarView is a person: their picture when they have one, otherwise the
/// first letter of their name on the color the server chose for them, as the
/// dashboard draws them.
struct AvatarView: View {
    let name: String
    /// The picture's path on the server, from the person's `avatarUrl`; nil
    /// when they have none.
    let avatarUrl: String?
    /// The color the server chose. When it has not said, one is worked out from
    /// the name, which is the same function the server runs over an id.
    let colorIndex: Int?
    /// The account whose server keeps the picture, when it is not the one in
    /// use: the list of accounts draws each from its own server.
    let account: Account?
    @ScaledMetric private var diameter: CGFloat
    @Environment(AppModel.self) private var app
    @Environment(\.displayScale) private var displayScale
    @State private var picture: UIImage?

    init(name: String, avatarUrl: String?, colorIndex: Int?, size: CGFloat, account: Account? = nil) {
        self.name = name
        self.avatarUrl = avatarUrl
        self.colorIndex = colorIndex
        self.account = account
        // Grows with the text size, like the row it sits in.
        _diameter = ScaledMetric(wrappedValue: size, relativeTo: .headline)
    }

    var body: some View {
        Group {
            if let picture {
                Image(uiImage: picture)
                    .resizable()
                    .scaledToFill()
            } else {
                Text(Monogram.letter(of: name))
                    .font(.system(size: diameter * 0.46, weight: .semibold))
                    .foregroundStyle(.white)
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
                    .background(AvatarPalette.color(at: AvatarColor.index(serverIndex: colorIndex, fallbackText: name)))
            }
        }
        .frame(width: diameter, height: diameter)
        .clipShape(.circle)
        .accessibilityHidden(true)
        // The address carries the picture's version, so a new picture is a new
        // address and is read again.
        .task(id: avatarUrl) {
            picture = nil
            guard let avatarUrl, !avatarUrl.isEmpty else { return }
            let path = Self.sized(avatarUrl, pixels: diameter * displayScale)
            if let account {
                picture = await app.picture(path, of: account)
            } else {
                picture = await app.picture(path)
            }
        }
    }

    /// The server draws a picture at 64 or 512 pixels square; the small one
    /// serves everything up to 64 pixels on screen.
    static func sized(_ avatarUrl: String, pixels: CGFloat) -> String {
        let renderedPixels = pixels <= 64 ? 64 : 512
        return avatarUrl + (avatarUrl.contains("?") ? "&" : "?") + "size=\(renderedPixels)"
    }
}

/// AvatarPalette is the twelve colors a person without a picture is drawn on,
/// the same as the dashboard's `--avatar-0` to `--avatar-11`. Each holds white
/// text at 4.5 to 1 or better; the dark set is the same hues one step down.
enum AvatarPalette {
    private static let lightHexValues: [UInt32] = [
        0xB23A2E, 0xB0521A, 0x8C6600, 0x5C7A1C, 0x1D7A45, 0x0F7A73, 0x0E6E94, 0x2F5FB3, 0x5B4FC2, 0x8A44B3, 0xA63A82, 0x5F6873,
    ]
    private static let darkHexValues: [UInt32] = [
        0xA8392E, 0xA4501C, 0x7D5C00, 0x55711C, 0x1B6D3F, 0x0E6D67, 0x0E6485, 0x2C56A3, 0x5348B0, 0x7C3FA3, 0x953676, 0x565E69,
    ]

    static func color(at index: Int) -> Color {
        let position = (0..<AvatarColor.colorCount).contains(index) ? index : 0
        return Color(
            UIColor { traits in
                color(hex: traits.userInterfaceStyle == .dark ? darkHexValues[position] : lightHexValues[position])
            })
    }

    private static func color(hex: UInt32) -> UIColor {
        UIColor(
            red: CGFloat((hex >> 16) & 0xFF) / 255, green: CGFloat((hex >> 8) & 0xFF) / 255, blue: CGFloat(hex & 0xFF) / 255, alpha: 1)
    }
}

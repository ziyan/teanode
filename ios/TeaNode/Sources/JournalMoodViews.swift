import SwiftUI
import TeaNodeKit

/// JournalMoodFace is one of the five faces a day can have. SF Symbols has a
/// smiling face and no frowning one, so the five are drawn the same way: a
/// circle in the mood's color, two eyes and a mouth that turns more upward
/// with each level.
struct JournalMoodFace: View {
    let level: JournalMoodLevel
    /// Whether the face is in its mood's color; a face that is not chosen is
    /// grey.
    var isColored = true

    /// Where the mouth sits and how far its middle is pulled, as fractions of
    /// the face: positive pulls it down, into a smile.
    private var mouth: (height: Double, pull: Double) {
        switch level {
        case .awful: (0.68, -0.18)
        case .low: (0.64, -0.09)
        case .okay: (0.62, 0)
        case .good: (0.56, 0.12)
        case .great: (0.52, 0.22)
        }
    }

    var body: some View {
        Canvas { context, size in
            let width = size.width
            let height = size.height
            let features = Color(.systemBackground)
            context.fill(Path(ellipseIn: CGRect(origin: .zero, size: size)), with: .color(isColored ? JournalText.moodColor(level) : Color(.tertiarySystemFill)))
            let eyeRadius = width * 0.055
            for eyeX in [0.37, 0.63] {
                let eye = CGRect(x: width * eyeX - eyeRadius, y: height * 0.38 - eyeRadius, width: eyeRadius * 2, height: eyeRadius * 2)
                context.fill(Path(ellipseIn: eye), with: .color(isColored ? features : Color(.secondaryLabel)))
            }
            var mouthPath = Path()
            mouthPath.move(to: CGPoint(x: width * 0.32, y: height * mouth.height))
            mouthPath.addQuadCurve(
                to: CGPoint(x: width * 0.68, y: height * mouth.height),
                control: CGPoint(x: width * 0.5, y: height * (mouth.height + mouth.pull)))
            context.stroke(
                mouthPath, with: .color(isColored ? features : Color(.secondaryLabel)),
                style: StrokeStyle(lineWidth: max(1.5, width * 0.06), lineCap: .round))
        }
        .aspectRatio(1, contentMode: .fit)
        .accessibilityHidden(true)
    }
}

/// JournalMoodPicker is the five faces in a row, each with its word under it.
/// A tap chooses that mood. The one chosen is in color and the others grey.
struct JournalMoodPicker: View {
    let chosenLevel: JournalMoodLevel?
    let choose: (JournalMoodLevel) -> Void
    @ScaledMetric(relativeTo: .body) private var faceSize = 36.0

    var body: some View {
        HStack(alignment: .top, spacing: 0) {
            ForEach(JournalMoodLevel.allCases) { level in
                Button {
                    choose(level)
                } label: {
                    VStack(spacing: 4) {
                        JournalMoodFace(level: level, isColored: chosenLevel == nil || chosenLevel == level)
                            .frame(width: faceSize, height: faceSize)
                        Text(JournalText.moodName(level))
                            .font(.caption)
                            .lineLimit(1)
                            .minimumScaleFactor(0.6)
                            .foregroundStyle(chosenLevel == level ? .primary : .secondary)
                    }
                    .frame(maxWidth: .infinity, minHeight: 44)
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .accessibilityLabel(JournalText.moodName(level))
                .accessibilityAddTraits(chosenLevel == level ? .isSelected : [])
            }
        }
        .sensoryFeedback(.selection, trigger: chosenLevel?.rawValue)
    }
}

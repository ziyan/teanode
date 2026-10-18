import Foundation

/// A terminal's screen as the program in it colored it. The text comes with
/// the codes that select a color or a style (SGR) and no other escape
/// sequence: the computer program keeps only those. terminalRuns splits it
/// into runs of text, each with the style the codes before it set.
public struct TerminalColor: Equatable, Sendable {
    public let red: Int
    public let green: Int
    public let blue: Int
}

public struct TerminalRun: Equatable, Sendable {
    public var text: String
    public var foreground: TerminalColor?
    public var background: TerminalColor?
    public var isBold = false
    public var isDim = false
    public var isItalic = false
    public var isUnderlined = false
    public var isInverse = false
}

/// The sixteen colors most terminals start with.
private let basicColors: [TerminalColor] = [
    (0, 0, 0), (205, 49, 49), (13, 188, 121), (229, 229, 16), (36, 114, 200), (188, 63, 188), (17, 168, 205), (229, 229, 229),
    (102, 102, 102), (241, 76, 76), (35, 209, 139), (245, 245, 67), (59, 142, 234), (214, 112, 214), (41, 184, 219), (255, 255, 255),
].map { TerminalColor(red: $0.0, green: $0.1, blue: $0.2) }

/// One of the 256 colors a code may name by number.
func terminalColor(ofIndex index: Int) -> TerminalColor? {
    switch index {
    case 0..<16: return basicColors[index]
    case 16..<232:
        let cube = index - 16
        func level(_ value: Int) -> Int { value == 0 ? 0 : 55 + value * 40 }
        return TerminalColor(red: level(cube / 36), green: level(cube / 6 % 6), blue: level(cube % 6))
    case 232...255:
        let gray = 8 + (index - 232) * 10
        return TerminalColor(red: gray, green: gray, blue: gray)
    default: return nil
    }
}

/// Changes a run's style as one code's numbers say.
func applyTerminalCodes(_ numbers: [Int], to style: inout TerminalRun) {
    var at = 0
    while at < numbers.count {
        let number = numbers[at]
        switch number {
        case 0: style = TerminalRun(text: "")
        case 1: style.isBold = true
        case 2: style.isDim = true
        case 3: style.isItalic = true
        case 4: style.isUnderlined = true
        case 7: style.isInverse = true
        case 22: style.isBold = false; style.isDim = false
        case 23: style.isItalic = false
        case 24: style.isUnderlined = false
        case 27: style.isInverse = false
        case 30...37: style.foreground = basicColors[number - 30]
        case 90...97: style.foreground = basicColors[number - 90 + 8]
        case 40...47: style.background = basicColors[number - 40]
        case 100...107: style.background = basicColors[number - 100 + 8]
        case 39: style.foreground = nil
        case 49: style.background = nil
        case 38, 48:
            var color: TerminalColor?
            if at + 2 < numbers.count, numbers[at + 1] == 5 {
                color = terminalColor(ofIndex: numbers[at + 2])
                at += 2
            } else if at + 4 < numbers.count, numbers[at + 1] == 2 {
                let parts = Array(numbers[(at + 2)...(at + 4)])
                if parts.allSatisfy({ (0...255).contains($0) }) {
                    color = TerminalColor(red: parts[0], green: parts[1], blue: parts[2])
                }
                at += 4
            }
            if number == 38 { style.foreground = color } else { style.background = color }
        default: break
        }
        at += 1
    }
}

/// The runs of a colored screen, in order; text without codes is one run.
public func terminalRuns(_ text: String) -> [TerminalRun] {
    var runs: [TerminalRun] = []
    var style = TerminalRun(text: "")
    var rest = Substring(text)
    while let start = rest.range(of: "\u{1B}[") {
        if start.lowerBound > rest.startIndex {
            var run = style
            run.text = String(rest[rest.startIndex..<start.lowerBound])
            runs.append(run)
        }
        let afterIntroducer = rest[start.upperBound...]
        guard let end = afterIntroducer.firstIndex(where: { !("0123456789;:".contains($0)) }), afterIntroducer[end] == "m" else {
            // Not a color code: skip the escape character and go on.
            rest = rest[rest.index(after: start.lowerBound)...]
            continue
        }
        let parameters = afterIntroducer[afterIntroducer.startIndex..<end]
        let numbers = parameters.isEmpty ? [0] : parameters.split(separator: ";", omittingEmptySubsequences: false).flatMap { group in
            var parts = group.split(separator: ":", omittingEmptySubsequences: false)
            // The colon form of a full color may name a color space before
            // the red, green and blue, often empty: 38:2::r:g:b.
            if parts.count == 6, ["38", "48"].contains(parts[0]), parts[1] == "2" { parts.remove(at: 2) }
            return parts
        }.map { Int($0) ?? 0 }
        applyTerminalCodes(numbers, to: &style)
        rest = afterIntroducer[afterIntroducer.index(after: end)...]
    }
    if !rest.isEmpty {
        var run = style
        run.text = String(rest)
        runs.append(run)
    }
    return runs
}

import Darwin
import Foundation
import Vision

// Inspect rendered content so a navigation fallback cannot count as the screenshot of a memory page with its
// records linked: the page's own name, the records under it, and a guess marked as one.
guard CommandLine.arguments.count == 3 else {
    fputs("Provide the screenshot name and PNG path.\n", stderr)
    exit(1)
}
let screenshotName = CommandLine.arguments[1]
let screenshotFileURL = URL(fileURLWithPath: CommandLine.arguments[2])
let appearanceSuffixes = ["-largest-text", "-light", "-dark"]
var screenName = screenshotName
if let appearanceSuffix = appearanceSuffixes.first(where: { screenName.hasSuffix($0) }) {
    screenName = String(screenName.dropLast(appearanceSuffix.count))
}
let expectedMarkersByScreen: [String: [String]] = [
    "knowledge-records": ["danafixture", "venueoptions", "aguess"]
]
guard let expectedMarkers = expectedMarkersByScreen[screenName] else {
    fputs("No rendered-content assertion for \(screenName).\n", stderr)
    exit(1)
}
let recognitionRequest = VNRecognizeTextRequest()
recognitionRequest.recognitionLevel = .accurate
recognitionRequest.recognitionLanguages = ["en-US"]
recognitionRequest.usesLanguageCorrection = true
do {
    try VNImageRequestHandler(url: screenshotFileURL, options: [:]).perform([recognitionRequest])
    let renderedText = (recognitionRequest.results ?? []).compactMap { $0.topCandidates(1).first?.string }.joined(separator: " ")
    let normalizedRenderedText = renderedText.lowercased().filter { $0.isLetter || $0.isNumber }
    let missingMarkers = expectedMarkers.filter { !normalizedRenderedText.contains($0) }
    guard missingMarkers.isEmpty else {
        fputs("\(screenshotName) does not show its expected knowledge content: \(missingMarkers.joined(separator: ", ")).\n", stderr)
        exit(1)
    }
    print("Verified rendered content: \(screenshotName)")
} catch {
    fputs("Cannot inspect \(screenshotName): \(error.localizedDescription)\n", stderr)
    exit(1)
}

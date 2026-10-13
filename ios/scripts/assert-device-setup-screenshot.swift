import Darwin
import Foundation
import Vision

// Inspect rendered content so a navigation fallback cannot count as a device setup screenshot.
guard CommandLine.arguments.count == 3 else {
    fputs("Provide the screenshot name and PNG path.\n", stderr)
    exit(1)
}
let screenshotName = CommandLine.arguments[1]
let screenshotFileURL = URL(fileURLWithPath: CommandLine.arguments[2])
let appearanceSuffixes = ["-largest-text", "-light", "-dark"]
var deviceSetupScreenName = screenshotName
if let appearanceSuffix = appearanceSuffixes.first(where: { deviceSetupScreenName.hasSuffix($0) }) {
    deviceSetupScreenName = String(deviceSetupScreenName.dropLast(appearanceSuffix.count))
}
let expectedMarkersByScreen: [String: [String]] = [
    "device-setup": ["choosewhattoadd", "mail"],
    "device-setup-mail": ["choosewhattoadd", "mailbox", "personal"],
    "device-setup-collections": ["choosewhattoadd", "calendar", "contacts"],
    "device-setup-profile": ["installtheprofile", "yoursetupprofileisready"],
    "device-setup-expired": ["setuplinkexpired", "thissetuplinkhasexpired"],
    "device-setup-receipt-unavailable": ["downloadprofile", "yoursetupprofileisready"],
    "device-setup-unavailable": ["noservicesavailable"],
]
guard let expectedMarkers = expectedMarkersByScreen[deviceSetupScreenName] else {
    fputs("No rendered-content assertion for \(deviceSetupScreenName).\n", stderr)
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
        fputs("\(screenshotName) does not show its expected device setup content: \(missingMarkers.joined(separator: ", ")).\n", stderr)
        exit(1)
    }
    if deviceSetupScreenName == "device-setup-expired",
        ["yoursetupprofileisready", "downloadprofile"].contains(where: { normalizedRenderedText.contains($0) })
    {
        fputs("Expired setup still shows a ready heading or download action.\n", stderr)
        exit(1)
    }
    print("Verified rendered content: \(screenshotName)")
} catch {
    fputs("Cannot inspect \(screenshotName): \(error.localizedDescription)\n", stderr)
    exit(1)
}

#if DEBUG
    import Foundation
    import TeaNodeKit
    import UIKit

    // The invented links that the screenshots of a message with a link photograph
    // (`-TeaNodeDemo`): a shop page with a picture, a guide with none, and a page
    // that cannot be read. Every address is made up, and the pictures are drawn
    // here since no server is behind the demo.

    extension DemoServer {
        // MARK: The messages

        /// A link as a shop writes it, with the parameters that say where the
        /// reader came from after the one that says which chair.
        static let linkShopAddress =
            "https://www.example.com/shop/balcony-chair?color=blue&gclid=EAIaIQobChMIexample&gbraid=0AAAAAexample&_gl=1*1abc2de*_ga*MTIzNDU2&utm_source=newsletter&utm_medium=email&utm_campaign=autumn"

        static let linkGuideAddress = "https://www.example.org/guides/care/outdoor-furniture-teak-and-aluminium?utm_source=chat"

        /// `-TeaNodeScreen conversation-link`: the main conversation, with a message
        /// of the person's and an answer of the agent's that each hold a link.
        static var linkConversation: String {
            #"""
            {"data":{
              "ReadAgentConversation":{
                "conversation":{"id":"main","kind":"main","title":null,"summary":null,"lastAt":"2026-10-10T09:05:00Z"},
                "todos":[],
                "messages":[
                  {"id":"1","createdAt":"2026-10-10T09:00:00Z","role":"user","content":"Is this chair any good for the balcony? \#(linkShopAddress)"},
                  {"id":"2","createdAt":"2026-10-10T09:00:20Z","role":"assistant","content":"It folds flat and is in stock, in blue. Teak and aluminium both last outside if you look after them: \#(linkGuideAddress). The [stackable one](https://www.example.net/stackable-chair?ref_src=twsrc) takes less room."}
                ],
                "total":2
              },
              "ListAgentInteractions":[]
            }}
            """#
        }

        /// The messages `-TeaNodeScreen chat-link` adds to the direct chat: one
        /// from Sam and one of Robin's own, each with a link.
        static var linkChatMessages: [String] {
            [
                chatMessageJSON("c07", daysAgo: 0, hour: 9, minute: 3, from: "sam", "This is the chair I meant: \(linkShopAddress)"),
                chatMessageJSON("c08", daysAgo: 0, hour: 9, minute: 6, from: "robin", "And how to look after it: \(linkGuideAddress)"),
            ]
        }

        // MARK: The answers

        /// The answer to `ReadLinkPreviews`, or nil for any other question.
        static func linkPreviewAnswering(query: String, body: Data?) -> String? {
            guard query.contains("ReadLinkPreviews") else { return nil }
            let addresses = requestVariables(body)["urls"] as? [String] ?? []
            return #"{"data":{"ReadLinkPreviews":[\#(addresses.map(linkPreviewJSON).joined(separator: ","))]}}"#
        }

        private static func linkPreviewJSON(_ address: String) -> String {
            let cleaned = jsonText(LinkClean.cleaned(address))
            let shown = jsonText(LinkClean.displayText(address))
            let asked = jsonText(address)
            let head = #""url":\#(asked),"cleanedUrl":\#(cleaned),"displayText":\#(shown)"#
            if address.contains("example.com/shop/") {
                return
                    #"{\#(head),"isAvailable":true,"title":"Balcony chair, folding, in blue","description":"A folding chair for a small balcony. Weatherproof aluminium frame, folds flat in two seconds and carries on one finger.","siteName":"Example Garden Shop","imageUrl":"/api/v1/link-preview/image/link-preview-demo","imageWidthPixels":600,"imageHeightPixels":340}"#
            }
            if address.contains("example.org/guides/") {
                return
                    #"{\#(head),"isAvailable":true,"title":"Looking after outdoor furniture","description":"How often to oil teak, wash aluminium and cover both for the winter.","siteName":"example.org","imageUrl":null,"imageWidthPixels":null,"imageHeightPixels":null}"#
            }
            return #"{\#(head),"isAvailable":false,"title":"","description":"","siteName":"","imageUrl":null,"imageWidthPixels":null,"imageHeightPixels":null}"#
        }

        private static func jsonText(_ text: String) -> String {
            guard let data = try? JSONSerialization.data(withJSONObject: text, options: [.fragmentsAllowed]) else { return "\"\"" }
            return String(decoding: data, as: UTF8.self)
        }

        /// The picture of the shop's page, for a read of an address under
        /// `/api/v1/link-preview/image/`: a chair on a wall, drawn here.
        static func linkPreviewPicture(for request: URLRequest) -> Data? {
            guard let path = request.url?.path, path.hasPrefix("/api/v1/link-preview/image/") else { return nil }
            let size = CGSize(width: 600, height: 340)
            let image = UIGraphicsImageRenderer(size: size).image { context in
                let drawing = context.cgContext
                drawing.setFillColor(UIColor(hue: 0.58, saturation: 0.18, brightness: 0.95, alpha: 1).cgColor)
                drawing.fill(CGRect(origin: .zero, size: size))
                drawing.setFillColor(UIColor(hue: 0.1, saturation: 0.2, brightness: 0.78, alpha: 1).cgColor)
                drawing.fill(CGRect(x: 0, y: size.height * 0.78, width: size.width, height: size.height * 0.22))
                drawing.setFillColor(UIColor(hue: 0.6, saturation: 0.55, brightness: 0.62, alpha: 1).cgColor)
                drawing.fill(CGRect(x: 220, y: 120, width: 160, height: 120))
                drawing.fill(CGRect(x: 210, y: 70, width: 20, height: 170))
                drawing.fill(CGRect(x: 210, y: 240, width: 14, height: 80))
                drawing.fill(CGRect(x: 366, y: 240, width: 14, height: 80))
            }
            return image.pngData()
        }
    }
#endif

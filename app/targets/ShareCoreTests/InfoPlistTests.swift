import Foundation
import Testing

/// Requirement: the extension is offered for one web URL, a text or one image only, with no App
/// Group and no exception to HTTPS beyond local networking (RG-01, ADR-0013, ADR-0014, ADR-0036,
/// design D2). Reads the versioned files of `share/`.
struct InfoPlistTests {
  private let shareFolder = URL(fileURLWithPath: #filePath)
    .deletingLastPathComponent()  // ShareCoreTests
    .deletingLastPathComponent()  // targets
    .appendingPathComponent("share", isDirectory: true)

  private func infoPlist() throws -> [String: Any] {
    let data = try Data(contentsOf: shareFolder.appendingPathComponent("Info.plist"))
    let plist = try PropertyListSerialization.propertyList(from: data, format: nil)
    return try #require(plist as? [String: Any])
  }

  /// A share as the activation rule sees it: extension items, their attachments, the types each
  /// attachment registers.
  private func share(_ items: [[String]]...) -> [String: Any] {
    [
      "extensionItems": items.map { attachments in
        ["attachments": attachments.map { types in ["registeredTypeIdentifiers": types] }]
      }
    ]
  }

  private func attachment(_ types: String...) -> [String] { types }

  @Test("Activation rules of the versioned Info.plist")
  func activationRulesOfTheVersionedInfoPlist() throws {
    let plist = try infoPlist()
    let fileText = try String(
      contentsOf: shareFolder.appendingPathComponent("Info.plist"), encoding: .utf8)

    let extensionInfo = try #require(plist["NSExtension"] as? [String: Any])
    let attributes = try #require(extensionInfo["NSExtensionAttributes"] as? [String: Any])
    // A predicate, not the dictionary form: the dictionary let two photos of Photos through
    // (QA Q9, 2026-11-18).
    let format = try #require(attributes["NSExtensionActivationRule"] as? String)
    #expect(!fileText.contains("TRUEPREDICATE"))
    #expect(!fileText.contains("NSExtensionActivationSupports"))
    #expect(extensionInfo["NSExtensionPointIdentifier"] as? String == "com.apple.share-services")
    #expect(
      extensionInfo["NSExtensionPrincipalClass"] as? String
        == "$(PRODUCT_MODULE_NAME).ShareViewController")

    let rule = NSPredicate(format: format)
    let url = attachment("public.url")
    let text = attachment("public.plain-text")
    let photo = attachment("public.heic", "public.jpeg")
    let livePhoto = attachment("com.apple.live-photo", "public.heic", "public.jpeg")
    let screenshot = attachment("public.png")

    // Accepted: Q2 to Q8 and Q21.
    #expect(rule.evaluate(with: share([url])))  // a Safari page
    #expect(rule.evaluate(with: share([url, text])))  // a page and a selection, Instagram
    #expect(rule.evaluate(with: share([text])))  // a text from Notes, a URL copied as text
    #expect(rule.evaluate(with: share([text, photo])))  // a note with a photo
    #expect(rule.evaluate(with: share([text], [photo])))  // the same, in two items
    #expect(rule.evaluate(with: share([photo])))  // a photo
    #expect(rule.evaluate(with: share([livePhoto])))  // a Live Photo
    #expect(rule.evaluate(with: share([screenshot])))  // a screenshot
    #expect(rule.evaluate(with: share([attachment("public.png", "public.file-url")])))

    // Refused: more than one image, across the whole share.
    #expect(!rule.evaluate(with: share([photo, photo])))
    #expect(!rule.evaluate(with: share([photo], [screenshot])))
    #expect(!rule.evaluate(with: share([text, photo], [photo])))
    // Refused: more than one web URL, a movie, a file that is not an image, nothing at all.
    #expect(!rule.evaluate(with: share([url, url])))
    #expect(!rule.evaluate(with: share([url], [url])))
    #expect(!rule.evaluate(with: share([attachment("com.apple.quicktime-movie")])))
    #expect(!rule.evaluate(with: share([photo, attachment("public.mpeg-4")])))
    #expect(!rule.evaluate(with: share([attachment("com.adobe.pdf", "public.file-url")])))
    #expect(!rule.evaluate(with: share([attachment("public.file-url")])))
    #expect(!rule.evaluate(with: share([])))
    #expect(!rule.evaluate(with: share()))
  }

  @Test("No exception to HTTPS beyond local networking")
  func noExceptionToHTTPSBeyondLocalNetworking() throws {
    let transport = try #require(try infoPlist()["NSAppTransportSecurity"] as? [String: Any])

    #expect(Set(transport.keys) == ["NSAllowsLocalNetworking"])
    #expect(transport["NSAllowsLocalNetworking"] as? Bool == true)
    #expect(transport["NSAllowsArbitraryLoads"] == nil)
  }

  /// `CFBundleDevelopmentRegion` would not do: the target generates its Info.plist too, and
  /// writes that key from `DEVELOPMENT_LANGUAGE` (`en`) over the versioned value.
  @Test("the system texts of the extension follow the language of the iPhone (ADR-0026)")
  func mixedLocalizations() throws {
    let plist = try infoPlist()

    #expect(plist["CFBundleAllowMixedLocalizations"] as? Bool == true)
    #expect(plist["CFBundleDevelopmentRegion"] == nil)
  }

  @Test("the extension grants itself no App Group and no shared keychain")
  func noEntitlements() throws {
    let files = try FileManager.default.contentsOfDirectory(atPath: shareFolder.path)
    let config = try String(
      contentsOf: shareFolder.appendingPathComponent("expo-target.config.js"), encoding: .utf8)

    #expect(!files.contains { $0.hasSuffix(".entitlements") })
    #expect(config.firstMatch(of: /entitlements\s*:/) == nil)
    #expect(!config.contains("application-groups"))
    #expect(!config.contains("keychain-access-groups"))
  }
}

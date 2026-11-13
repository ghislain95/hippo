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

  @Test("Activation rules of the versioned Info.plist")
  func activationRulesOfTheVersionedInfoPlist() throws {
    let plist = try infoPlist()
    let fileText = try String(
      contentsOf: shareFolder.appendingPathComponent("Info.plist"), encoding: .utf8)

    let extensionInfo = try #require(plist["NSExtension"] as? [String: Any])
    let attributes = try #require(extensionInfo["NSExtensionAttributes"] as? [String: Any])
    let rule = try #require(attributes["NSExtensionActivationRule"] as? [String: Any])

    #expect(
      Set(rule.keys) == [
        "NSExtensionActivationSupportsWebURLWithMaxCount", "NSExtensionActivationSupportsText",
        "NSExtensionActivationSupportsImageWithMaxCount",
      ])
    #expect(rule["NSExtensionActivationSupportsWebURLWithMaxCount"] as? Int == 1)
    #expect(rule["NSExtensionActivationSupportsText"] as? Bool == true)
    #expect(rule["NSExtensionActivationSupportsImageWithMaxCount"] as? Int == 1)
    #expect(!fileText.contains("TRUEPREDICATE"))
    #expect(extensionInfo["NSExtensionPointIdentifier"] as? String == "com.apple.share-services")
    #expect(
      extensionInfo["NSExtensionPrincipalClass"] as? String
        == "$(PRODUCT_MODULE_NAME).ShareViewController")
  }

  @Test("No exception to HTTPS beyond local networking")
  func noExceptionToHTTPSBeyondLocalNetworking() throws {
    let transport = try #require(try infoPlist()["NSAppTransportSecurity"] as? [String: Any])

    #expect(Set(transport.keys) == ["NSAllowsLocalNetworking"])
    #expect(transport["NSAllowsLocalNetworking"] as? Bool == true)
    #expect(transport["NSAllowsArbitraryLoads"] == nil)
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

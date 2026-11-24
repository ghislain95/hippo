import Foundation

/// The members the contract declares for a capture, read from the bundled contract
/// (`api/openapi.bundled.gen.json`), so that the extension never sends an undeclared member.
enum Contract {
  static func properties(of schema: String) throws -> Set<String> {
    let file = URL(fileURLWithPath: #filePath)
      .deletingLastPathComponent()  // ShareCoreTests
      .deletingLastPathComponent()  // targets
      .deletingLastPathComponent()  // app
      .deletingLastPathComponent()  // repository
      .appendingPathComponent("api/openapi.bundled.gen.json")
    let root = try JSONSerialization.jsonObject(with: Data(contentsOf: file))
    guard let document = root as? [String: Any],
      let components = document["components"] as? [String: Any],
      let schemas = components["schemas"] as? [String: Any],
      let definition = schemas[schema] as? [String: Any],
      let properties = definition["properties"] as? [String: Any]
    else { throw ContractError.missing(schema) }
    return Set(properties.keys)
  }
}

enum ContractError: Error {
  case missing(String)
}

import Foundation

/// The body of a capture, built once at the first send and sent again as it is by « Réessayer »
/// (RG-08, design D6).
enum RequestBody: Equatable, Sendable {
  /// A link or a note, in memory.
  case json(Data)
  /// An image, written to a file in the work directory and uploaded from it.
  case multipart(file: URL, boundary: String)

  var contentType: String {
    switch self {
    case .json: "application/json"
    case .multipart(_, let boundary): "multipart/form-data; boundary=\(boundary)"
    }
  }
}

/// `POST <server>/api/items` with the build-time token (ADR-0014, ADR-0016).
struct CaptureRequest: Equatable, Sendable {
  let body: RequestBody

  /// The request to send; a multipart body is uploaded from its file, not set here.
  func urlRequest(server: URL, token: String) -> URLRequest {
    var request = URLRequest(url: server.appendingPathComponent("api/items"))
    request.httpMethod = "POST"
    request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
    request.setValue("application/json", forHTTPHeaderField: "Accept")
    request.setValue(body.contentType, forHTTPHeaderField: "Content-Type")
    if case .json(let data) = body { request.httpBody = data }
    return request
  }
}

/// The JSON capture of a link or a note, with the contract's members only: `url` or `note`,
/// `user_note` when not blank, `is_private`, `client_id`, `source` (RG-01, RG-03, RG-07, RG-09,
/// RG-10, contract `CaptureRequest`).
enum CaptureBody {
  /// The largest JSON body and text part the server takes: 1 MiB.
  static let maximumTextByteCount: Int64 = 1_048_576

  enum TextForm: Equatable, Sendable {
    case link(String)
    case note(String)
  }

  enum Result: Equatable, Sendable {
    case body(RequestBody)
    /// The body, or the capture note of an image, weighs `byteCount` bytes, over 1 MiB.
    case tooLong(byteCount: Int64)
  }

  private struct Members: Encodable {
    var url: String?
    var note: String?
    var userNote: String?
    let isPrivate: Bool
    let clientId: String
    let source = "share"

    enum CodingKeys: String, CodingKey {
      case url
      case note
      case userNote = "user_note"
      case isPrivate = "is_private"
      case clientId = "client_id"
      case source
    }
  }

  /// - Parameter userNote: the capture note of a link; ignored for a note (RG-07).
  static func json(form: TextForm, userNote: String, isPrivate: Bool, clientId: Ulid) -> Result {
    var members = Members(isPrivate: isPrivate, clientId: clientId.value)
    switch form {
    case .link(let url):
      members.url = CaptureChoice.trimmed(url)
      members.userNote = nonBlank(userNote)
    case .note(let text):
      members.note = CaptureChoice.trimmed(text)
    }
    let encoder = JSONEncoder()
    encoder.outputFormatting = [.sortedKeys, .withoutEscapingSlashes]
    // Encoding strings and booleans cannot fail.
    let data = (try? encoder.encode(members)) ?? Data()
    guard Int64(data.count) <= maximumTextByteCount else {
      return .tooLong(byteCount: Int64(data.count))
    }
    return .body(.json(data))
  }

  /// The trimmed text, or nil when it is blank (RG-09).
  static func nonBlank(_ text: String) -> String? {
    let trimmed = CaptureChoice.trimmed(text)
    return trimmed.isEmpty ? nil : trimmed
  }
}

/// Writes the multipart capture of an image to a file, the image copied in 1 MiB chunks, so that
/// a 20 MB original is never held in memory (contract `CaptureImageRequest`, design D6).
enum MultipartWriter {
  private static let chunkByteCount = 1_048_576

  static func write(
    image: PreparedImage, userNote: String, isPrivate: Bool, clientId: Ulid, in directory: URL
  ) throws -> CaptureBody.Result {
    let note = CaptureBody.nonBlank(userNote)
    if let note, Int64(note.utf8.count) > CaptureBody.maximumTextByteCount {
      return .tooLong(byteCount: Int64(note.utf8.count))
    }
    let boundary = "hippo-\(clientId.value)"
    let file = directory.appendingPathComponent("\(clientId.value).multipart")
    let fileManager = FileManager.default
    try? fileManager.removeItem(at: file)
    guard fileManager.createFile(atPath: file.path, contents: nil) else {
      throw CocoaError(.fileWriteUnknown)
    }
    let output = try FileHandle(forWritingTo: file)
    defer { try? output.close() }
    let input = try FileHandle(forReadingFrom: image.file)
    defer { try? input.close() }

    try output.write(
      contentsOf: Data(
        ("--\(boundary)\r\n"
          + "Content-Disposition: form-data; name=\"image\"; filename=\"\(image.fileName)\"\r\n"
          + "Content-Type: \(image.format.mimeType)\r\n\r\n").utf8))
    while let chunk = try input.read(upToCount: chunkByteCount), !chunk.isEmpty {
      try output.write(contentsOf: chunk)
    }
    var fields: [(String, String)] = []
    if let note { fields.append(("user_note", note)) }
    fields.append(("is_private", isPrivate ? "true" : "false"))
    fields.append(("client_id", clientId.value))
    fields.append(("source", "share"))
    var tail = ""
    for (name, value) in fields {
      tail +=
        "\r\n--\(boundary)\r\nContent-Disposition: form-data; name=\"\(name)\"\r\n\r\n\(value)"
    }
    tail += "\r\n--\(boundary)--\r\n"
    try output.write(contentsOf: Data(tail.utf8))
    return .body(.multipart(file: file, boundary: boundary))
  }
}

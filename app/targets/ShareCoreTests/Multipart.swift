import Foundation

/// A minimal reader of the multipart bodies the extension writes, for the tests only.
struct MultipartPart: Equatable {
  let name: String
  let fileName: String?
  let contentType: String?
  let body: Data

  var text: String { String(decoding: body, as: UTF8.self) }
}

enum Multipart {
  static func parts(of data: Data, boundary: String) throws -> [MultipartPart] {
    let crlf = Data("\r\n".utf8)
    let delimiter = Data("--\(boundary)".utf8)
    guard data.starts(with: delimiter + crlf), data.hasSuffix(Data("\r\n--\(boundary)--\r\n".utf8))
    else { throw MultipartError.malformed("start or end") }
    var parts: [MultipartPart] = []
    var rest = data.dropFirst(delimiter.count + crlf.count)
    let separator = crlf + delimiter
    while let end = rest.firstRange(of: separator) {
      let raw = rest[rest.startIndex..<end.lowerBound]
      parts.append(try part(Data(raw)))
      rest = rest[end.upperBound...]
      if rest.starts(with: Data("--".utf8)) { break }
      rest = rest.dropFirst(crlf.count)
    }
    return parts
  }

  private static func part(_ raw: Data) throws -> MultipartPart {
    guard let split = raw.firstRange(of: Data("\r\n\r\n".utf8)) else {
      throw MultipartError.malformed("headers")
    }
    let headers = String(decoding: raw[raw.startIndex..<split.lowerBound], as: UTF8.self)
      .components(separatedBy: "\r\n")
    var name: String?
    var fileName: String?
    var contentType: String?
    for header in headers {
      if header.hasPrefix("Content-Disposition: form-data; ") {
        for item in header.dropFirst("Content-Disposition: form-data; ".count)
          .components(separatedBy: "; ")
        {
          let pair = item.split(separator: "=", maxSplits: 1).map(String.init)
          let value = pair.count == 2 ? pair[1].trimmingCharacters(in: ["\""]) : nil
          if pair.first == "name" { name = value }
          if pair.first == "filename" { fileName = value }
        }
      } else if header.hasPrefix("Content-Type: ") {
        contentType = String(header.dropFirst("Content-Type: ".count))
      }
    }
    guard let name else { throw MultipartError.malformed("name") }
    return MultipartPart(
      name: name, fileName: fileName, contentType: contentType,
      body: Data(raw[split.upperBound...]))
  }
}

enum MultipartError: Error {
  case malformed(String)
}

extension Data {
  fileprivate func hasSuffix(_ suffix: Data) -> Bool {
    count >= suffix.count && self.suffix(suffix.count) == suffix
  }
}

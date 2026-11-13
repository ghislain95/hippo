import Foundation
import Testing

@testable import ShareCore

/// Requirements: one share makes one item, by priority web URL, then image, then text; a text
/// shared with a URL or an image prefills the capture note; a shared text alone becomes the text
/// of a note; a share with nothing to capture is told as such (design D4).
struct CaptureChoiceTests {
  private let anImage = SharedImage(typeIdentifiers: ["public.jpeg"])

  private func url(_ text: String) -> URL {
    URL(string: text)!
  }

  @Test("Web URL alone")
  func webURLAlone() {
    let choice = CaptureChoice.choose(SharedContent(urls: [url("https://example.com/a")]))

    #expect(choice == CaptureChoice(form: .link("https://example.com/a"), prefilledNote: ""))
  }

  @Test("Web URL wins over an image")
  func webURLWinsOverAnImage() {
    let choice = CaptureChoice.choose(
      SharedContent(urls: [url("https://example.com/a")], images: [anImage]))

    #expect(choice?.form == .link("https://example.com/a"))
  }

  @Test("Image wins over a text")
  func imageWinsOverAText() {
    let choice = CaptureChoice.choose(
      SharedContent(texts: ["pour la salle de bain"], images: [anImage]))

    #expect(choice?.form == .image(anImage))
  }

  @Test("Text that is only a URL becomes a link")
  func textThatIsOnlyAURLBecomesALink() {
    let choice = CaptureChoice.choose(SharedContent(texts: ["  HTTPS://Example.com/a?x=1\n"]))

    #expect(choice == CaptureChoice(form: .link("HTTPS://Example.com/a?x=1"), prefilledNote: ""))
  }

  @Test("Text holding a URL among other words stays a note")
  func textHoldingAURLAmongOtherWordsStaysANote() {
    let choice = CaptureChoice.choose(
      SharedContent(texts: ["Regarde https://example.com/a ce soir"]))

    #expect(choice?.form == .note("Regarde https://example.com/a ce soir"))
  }

  @Test("URL of another scheme is ignored")
  func urlOfAnotherSchemeIsIgnored() {
    let choice = CaptureChoice.choose(
      SharedContent(urls: [url("mailto:a@example.com")], texts: ["Appeler Paul"]))

    #expect(choice?.form == .note("Appeler Paul"))
  }

  @Test("Safari selection with the page URL")
  func safariSelectionWithThePageURL() {
    let choice = CaptureChoice.choose(
      SharedContent(urls: [url("https://example.com/a")], texts: ["  Le paragraphe choisi\n"]))

    #expect(
      choice
        == CaptureChoice(
          form: .link("https://example.com/a"), prefilledNote: "Le paragraphe choisi")
    )
  }

  @Test("Text that repeats the URL")
  func textThatRepeatsTheURL() {
    let choice = CaptureChoice.choose(
      SharedContent(urls: [url("https://example.com/a")], texts: ["Un titre https://example.com/a"])
    )

    #expect(choice?.prefilledNote == "Un titre")
  }

  @Test("Text that is the URL itself")
  func textThatIsTheURLItself() {
    let choice = CaptureChoice.choose(
      SharedContent(urls: [url("https://example.com/a")], texts: ["https://example.com/a"]))

    #expect(choice?.prefilledNote == "")
  }

  @Test("Image shared with a text")
  func imageSharedWithAText() {
    let choice = CaptureChoice.choose(
      SharedContent(texts: ["recette de la story"], images: [anImage]))

    #expect(choice == CaptureChoice(form: .image(anImage), prefilledNote: "recette de la story"))
  }

  @Test("Text from Notes")
  func textFromNotes() {
    let choice = CaptureChoice.choose(SharedContent(texts: ["\n Idée : ranger les câbles \n"]))

    #expect(choice == CaptureChoice(form: .note("Idée : ranger les câbles"), prefilledNote: ""))
    #expect(choice?.form.hasCaptureNote == false)
  }

  @Test("Blank text")
  func blankText() {
    #expect(CaptureChoice.choose(SharedContent(texts: ["  \n "])) == nil)
  }

  @Test("a scheme in capitals is a web URL, a URL without a host is not")
  func schemeCaseAndHost() {
    #expect(
      CaptureChoice.choose(SharedContent(urls: [url("HTTP://example.com")]))?.form
        == .link("HTTP://example.com"))
    #expect(CaptureChoice.choose(SharedContent(texts: ["https://"]))?.form == .note("https://"))
  }

  @Test("an image without any image type is not captured")
  func imageWithoutImageType() {
    let content = SharedContent(images: [SharedImage(typeIdentifiers: ["public.plain-text"])])

    #expect(CaptureChoice.choose(content) == nil)
  }

  @Test("a text left blank once the URL is removed gives way to the next text")
  func blankAccompanyingTextSkipped() {
    let choice = CaptureChoice.choose(
      SharedContent(
        urls: [url("https://example.com/a")], texts: [" https://example.com/a ", "Le titre"]))

    #expect(choice?.prefilledNote == "Le titre")
  }

  @Test("a link made from a text takes the next text as its note")
  func linkFromTextTakesNextText() {
    let choice = CaptureChoice.choose(
      SharedContent(texts: ["https://example.com/a", "  À lire  "]))

    #expect(choice == CaptureChoice(form: .link("https://example.com/a"), prefilledNote: "À lire"))
  }
}

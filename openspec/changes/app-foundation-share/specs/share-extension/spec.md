# Spec Delta

## Purpose

Lets the user capture a web link, a text or an image from the iOS share sheet of any app: what the share extension accepts, the form it fills, the `POST /api/items` request it sends and the screen it shows for each answer. Every scenario is pure logic run by `swift test`; what only a device can show is in the QA checklist of the change's design.

## ADDED Requirements

### Requirement: The extension is offered for one web URL, a text or one image only
The extension's activation rules SHALL accept at most one web URL, a text and at most one image, and nothing else: no file, no movie, no attachment count beyond these. Its configuration SHALL grant no App Group and no shared keychain, and allow plain HTTP for local addresses only (RG-01, ADR-0013, ADR-0014, ADR-0036).

#### Scenario: Activation rules of the versioned Info.plist
- **GIVEN** the extension's versioned `Info.plist`
- **WHEN** its activation rule is read
- **THEN** it allows exactly one web URL, text, and exactly one image, and declares no file, attachment or movie key and no `TRUEPREDICATE`

#### Scenario: No exception to HTTPS beyond local networking
- **GIVEN** the extension's versioned `Info.plist`
- **WHEN** its transport security settings are read
- **THEN** the only key present is the one allowing local networking, and arbitrary loads are not allowed

### Requirement: One share makes one item, by priority web URL, then image, then text
From everything a share offers, the extension SHALL build exactly one capture: an `http` or `https` URL first (scheme in any case), else an image, else a text. A text that, once trimmed, is only an `http` or `https` URL counts as a web URL. A URL of another scheme is ignored (RG-01, RG-02, RG-09).

#### Scenario: Web URL alone
- **WHEN** a share offers the URL `https://example.com/a`
- **THEN** the capture is a link to `https://example.com/a`

#### Scenario: Web URL wins over an image
- **WHEN** a share offers the URL `https://example.com/a` and an image
- **THEN** the capture is a link to `https://example.com/a`

#### Scenario: Image wins over a text
- **WHEN** a share offers an image and the text `pour la salle de bain`
- **THEN** the capture is an image

#### Scenario: Text that is only a URL becomes a link
- **WHEN** a share offers only the text `  HTTPS://Example.com/a?x=1\n`
- **THEN** the capture is a link to `HTTPS://Example.com/a?x=1`

#### Scenario: Text holding a URL among other words stays a note
- **WHEN** a share offers only the text `Regarde https://example.com/a ce soir`
- **THEN** the capture is a note whose text is `Regarde https://example.com/a ce soir`

#### Scenario: URL of another scheme is ignored
- **WHEN** a share offers the URL `mailto:a@example.com` and the text `Appeler Paul`
- **THEN** the capture is a note whose text is `Appeler Paul`

### Requirement: A text shared with a URL or an image prefills the capture note
When the capture is a link or an image and the share also offers a text, the editable capture note SHALL be prefilled with that text, trimmed, without the exact characters of the captured URL; when nothing is left, the note SHALL start empty. Nothing shared is dropped silently (RG-01, RG-03, RG-09).

#### Scenario: Safari selection with the page URL
- **WHEN** a share offers the URL `https://example.com/a` and the text `  Le paragraphe choisi\n`
- **THEN** the capture is a link to `https://example.com/a` and the capture note is prefilled with `Le paragraphe choisi`

#### Scenario: Text that repeats the URL
- **WHEN** a share offers the URL `https://example.com/a` and the text `Un titre https://example.com/a`
- **THEN** the capture note is prefilled with `Un titre`

#### Scenario: Text that is the URL itself
- **WHEN** a share offers the URL `https://example.com/a` and the text `https://example.com/a`
- **THEN** the capture note starts empty

#### Scenario: Image shared with a text
- **WHEN** a share offers an image and the text `recette de la story`
- **THEN** the capture is an image and the capture note is prefilled with `recette de la story`

### Requirement: A shared text alone becomes the text of a note, without a capture note
A capture of the note form SHALL carry the shared text, trimmed, as its editable text, and SHALL never carry a capture note; a note whose text is blank once trimmed SHALL NOT be saved (RG-01, RG-02, RG-07, RG-09).

#### Scenario: Text from Notes
- **WHEN** a share offers only the text `\n Idée : ranger les câbles \n`
- **THEN** the capture is a note whose text is `Idée : ranger les câbles` and the form has no capture note field

#### Scenario: Blank note text cannot be saved
- **GIVEN** the form of a note whose text the user changed to `  \n`
- **WHEN** the user tries to save
- **THEN** « Enregistrer » is not offered, no request is built and the form stays shown

### Requirement: A share with nothing to capture is told as such
When a share offers no web URL, no readable image and no text left after trimming, the extension SHALL show the nothing-to-save state and send nothing (RG-02, RG-09).

#### Scenario: Blank text
- **WHEN** a share offers only the text `  \n `
- **THEN** the state is nothing-to-save and no request is built

#### Scenario: Unreadable image
- **WHEN** a share offers only an image whose data no image decoder can read and that is not of a kept type
- **THEN** the state is nothing-to-save and no request is built

### Requirement: An image is kept in its original format from a closed list, else converted to JPEG
Among the types an image is offered in, the extension SHALL pick the first of HEIC, PNG, WebP, JPEG, in this order, and send that file unchanged; an image offered in none of them SHALL be converted to JPEG. The form's caption SHALL read `Image · <format> · <size>` (RG-02, RG-38).

#### Scenario: HEIC preferred over JPEG
- **WHEN** an image is offered as `public.jpeg` and `public.heic`
- **THEN** the HEIC file is sent unchanged, declared `image/heic` with the file name `capture.heic`

#### Scenario: PNG screenshot
- **WHEN** an image is offered as `public.png` only
- **THEN** the PNG file is sent byte for byte and the caption starts with `Image · PNG`

#### Scenario: TIFF converted to JPEG
- **WHEN** an image is offered as `public.tiff` only
- **THEN** a JPEG converted from it is sent, declared `image/jpeg`, and the caption starts with `Image · JPEG`

#### Scenario: Other image types are never sent as they are
- **WHEN** an image is offered as `com.adobe.raw-image` and `public.jpeg`
- **THEN** the JPEG file is sent unchanged

### Requirement: An image over 20 MB is refused before any send
The size of the image to send SHALL be checked after any conversion, against 20 × 1 048 576 bytes: up to that size the form is shown, above it the too-large state is shown with the size and nothing is sent. Sizes SHALL read in megabytes of 1 048 576 bytes, one decimal rounded up, with a French decimal comma (RG-02).

#### Scenario: Image of exactly 20 MB
- **WHEN** the image to send weighs 20 971 520 bytes
- **THEN** the form is shown and the caption ends with `20,0 Mo`

#### Scenario: Image one byte over 20 MB
- **WHEN** the image to send weighs 20 971 521 bytes
- **THEN** the state is too-large with the text `20,1 Mo, la limite est de 20 Mo.` and no request is built

#### Scenario: Converted image over the limit
- **WHEN** an image offered as `public.tiff` converts to a JPEG of 22 000 000 bytes
- **THEN** the state is too-large and no request is built

#### Scenario: Size of a small image
- **WHEN** the image to send weighs 3 250 000 bytes
- **THEN** the caption ends with `3,1 Mo`

### Requirement: A text over 1 MB is refused before any send
The extension SHALL refuse to send a link or note whose JSON body would exceed 1 048 576 bytes, and an image capture whose capture note exceeds 1 048 576 bytes, and show the too-long state instead (RG-02).

#### Scenario: Note text over 1 MB
- **WHEN** the note text weighs 1 100 000 bytes in UTF-8
- **THEN** the state is too-long and no request is sent

#### Scenario: Note text just under the limit
- **WHEN** the JSON body of a note weighs 1 048 576 bytes
- **THEN** the request is sent

### Requirement: A link or a note is sent as the contract's JSON capture
The extension SHALL send `POST <server>/api/items` with the build-time bearer token, `Content-Type: application/json` and a body holding only the members of the contract's link or note capture: `url` or `note`, `user_note` when not blank, `is_private`, `client_id` and `source` `share`. Values are trimmed (RG-01, RG-03, RG-07, RG-09, RG-10).

#### Scenario: Link with a capture note
- **GIVEN** the server `https://hippo.example.ts.net` and a link to `https://example.com/a`
- **WHEN** the user writes ` à tester ` and saves
- **THEN** the request is `POST https://hippo.example.ts.net/api/items` with `Authorization: Bearer <token>` and the body `{"url": "https://example.com/a", "user_note": "à tester", "is_private": false, "client_id": "<capture id>", "source": "share"}`

#### Scenario: Blank capture note is left out
- **WHEN** the user saves a link with the capture note `  `
- **THEN** the body has no `user_note` member

#### Scenario: Private note
- **WHEN** the user turns Privé on and saves a note whose text is `Code du portail`
- **THEN** the body is `{"note": "Code du portail", "is_private": true, "client_id": "<capture id>", "source": "share"}`, without `user_note`

#### Scenario: Edited note text
- **WHEN** the user changes the prefilled text of a note to `Idée corrigée` and saves
- **THEN** the body's `note` is `Idée corrigée`

### Requirement: An image is sent as the contract's multipart capture
The extension SHALL send an image as `multipart/form-data` with one `image` file part declaring the picked type, and the text parts `is_private` (exactly `true` or `false`), `client_id`, `source` `share` and `user_note` when not blank, each once (RG-01, RG-02, RG-03, RG-09, RG-10).

#### Scenario: Image with a note, private
- **WHEN** the user saves a PNG image with the capture note `recette` and Privé on
- **THEN** the body has exactly the parts `image` (`capture.png`, `image/png`, the file's bytes unchanged), `user_note` `recette`, `is_private` `true`, `client_id` and `source` `share`, delimited by the boundary declared in `Content-Type`

#### Scenario: Image without a note
- **WHEN** the user saves an image with a blank capture note
- **THEN** the body has no `user_note` part and `is_private` is `false`

### Requirement: Each share carries its own capture id, kept by « Réessayer »
Each opening of the extension SHALL draw one capture id, a ULID of 26 characters from `0123456789ABCDEFGHJKMNPQRSTVWXYZ` whose first 48 bits are the current time in milliseconds. « Réessayer » SHALL send the same request again, byte for byte, with the same capture id (RG-08, ADR-0035).

#### Scenario: Shape of a capture id
- **WHEN** a capture id is drawn at 2026-11-13T10:00:00.000Z
- **THEN** it matches `^[0-7][0-9A-HJKMNP-TV-Z]{25}$` and its first 10 characters are `01M7A5K280` (1 794 564 000 000 milliseconds)

#### Scenario: Two shares, two ids
- **WHEN** two capture ids are drawn in the same millisecond
- **THEN** they differ

#### Scenario: « Réessayer » replays the same request
- **GIVEN** a send that ended unreachable
- **WHEN** the user taps « Réessayer »
- **THEN** the new request has the same body, byte for byte, and the same capture id

#### Scenario: The form is frozen after the first send
- **GIVEN** a send that ended unreachable
- **WHEN** the user tries to change the capture note or the Privé switch
- **THEN** the request sent by « Réessayer » is unchanged

### Requirement: Each answer of the server maps to one screen
The extension SHALL map an answer to a screen: no answer, `502`, `503`, `504` or any other `5xx` to unreachable, with « Réessayer »; `401` to token-refused; `413` with type `urn:hippo:problem:image-too-large` to too-large; any other `4xx` to refused, showing the problem's `detail` as is, without « Réessayer »; `201` to saved (RG-04, RG-06, RG-08).

#### Scenario: Server not reachable
- **WHEN** the request fails without an answer
- **THEN** the state is unreachable, with « Réessayer »

#### Scenario: Service down behind Tailscale
- **WHEN** the server answers `502`
- **THEN** the state is unreachable, with « Réessayer »

#### Scenario: Token refused
- **WHEN** the server answers `401`
- **THEN** the state is token-refused, without « Réessayer »

#### Scenario: Image refused as too large by the server
- **WHEN** the server answers `413` with type `urn:hippo:problem:image-too-large`
- **THEN** the state is too-large

#### Scenario: Image refused for its format
- **WHEN** the server answers `400` with type `urn:hippo:problem:unsupported-image` and the detail `Format d'image non pris en charge.`
- **THEN** the state is refused, showing `Format d'image non pris en charge.`, without « Réessayer »

#### Scenario: Refusal without a detail
- **WHEN** the server answers `415` with a problem that has no `detail`
- **THEN** the state is refused, showing the fallback text, without « Réessayer »

#### Scenario: Body over the server's limit
- **WHEN** the server answers `413` with type `about:blank`
- **THEN** the state is refused, without « Réessayer »

#### Scenario: New item
- **WHEN** the server answers `201` with an item whose `is_private` is false
- **THEN** the state is saved

### Requirement: « Déjà dans Hippo » is shown only for a link on its first send
A `200` SHALL show the duplicate screen only for a link on the first send of its capture id, in its variant with a note when a capture note was sent; any other `200`, and every `200` after « Réessayer », SHALL show the saved screen (RG-04, RG-08, RG-15).

#### Scenario: Duplicate link without a note
- **WHEN** the first send of a link without a capture note gets `200`
- **THEN** the state is duplicate with the text `Il était déjà dans ta bibliothèque.`

#### Scenario: Duplicate link with a note
- **WHEN** the first send of a link with the capture note `à tester` gets `200`
- **THEN** the state is duplicate with the text `Il était déjà dans ta bibliothèque ; ta note l'accompagne.`

#### Scenario: Replayed link after « Réessayer »
- **GIVEN** a first send of a link that ended unreachable
- **WHEN** « Réessayer » gets `200`
- **THEN** the state is saved

#### Scenario: Replayed image
- **WHEN** the send of an image gets `200`
- **THEN** the state is saved

### Requirement: A private item is confirmed as private
When the item answered by a `201`, or by a `200` that shows the saved screen, is private, the extension SHALL show the saved-private screen, which says the item waits in À trier and is never sent to the AI (RG-12, RG-13, RG-15).

#### Scenario: New private item
- **WHEN** the server answers `201` with an item whose `is_private` is true
- **THEN** the state is saved-private

#### Scenario: Replay of a private image
- **GIVEN** a first send of a private image that ended unreachable
- **WHEN** « Réessayer » gets `200` with an item whose `is_private` is true
- **THEN** the state is saved-private

### Requirement: The unreachable screen offers to copy what was shared
The unreachable screen SHALL offer « Copier le lien » with the URL for a link, « Copier le texte » with the note text for a note, and no copy action for an image (RG-06).

#### Scenario: Copy a link
- **GIVEN** a link to `https://example.com/a` that ended unreachable
- **WHEN** the copy action is read
- **THEN** it is « Copier le lien » and copies `https://example.com/a`

#### Scenario: Copy a text
- **GIVEN** a note whose text is `Idée` that ended unreachable
- **WHEN** the copy action is read
- **THEN** it is « Copier le texte » and copies `Idée`

#### Scenario: No copy for an image
- **GIVEN** an image that ended unreachable
- **WHEN** the copy action is read
- **THEN** there is none

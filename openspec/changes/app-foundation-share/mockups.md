# Mockups

Validated by Ghislain on 2026-11-13 (E5 rule: screens are mocked up and validated before they are coded). The user-facing texts are in French and are part of the validation; layout details are indicative.

| # | Screen | Content |
| --- | --- | --- |
| 1 | Home (app) | Title « Hippo »; server state pill « Serveur joignable » (success colour; otherwise the unreachable state); text « Partage un lien, un texte ou une image depuis n'importe quelle appli. Hippo le range dans la nuit. »; muted text « La bibliothèque arrive avec la prochaine version. » |
| 2 | Share a link | Navigation bar « Annuler » · « Hippo » · « Enregistrer »; preview card with the site (host) and the title or URL; field « Note (facultative) »; switch « Privé », off; caption « Un élément privé n'est jamais envoyé à l'IA. » |
| 3 | Share a text | Same bar; label « Note »; the shared text in an editable multi-line field; switch « Privé », off; no capture note field (RG-07) |
| 4 | Share an image | Same bar; thumbnail; caption with the kind, format and size (« Image · HEIC · 3,1 Mo »; the extension cannot tell a photo from a screenshot); field « Note (facultative) »; switch « Privé », off |
| 5 | Saved | Check icon (success); « Enregistré »; « Hippo le range dans la nuit. »; the sheet closes by itself after a short moment |
| 6 | Duplicate link | Only for a link, on the first send: icon (accent); « Déjà dans Hippo »; « Il était déjà dans ta bibliothèque. », or with a note « Il était déjà dans ta bibliothèque ; ta note l'accompagne. » A `200` after « Réessayer » shows screen 5 (decided on 2026-11-13) |
| 7 | Server unreachable | Wifi-off icon (danger); « Serveur injoignable »; « Vérifie que Tailscale est actif, puis réessaie. »; buttons « Réessayer » and « Copier le lien » (for a text: « Copier le texte »; for an image: no copy button) |
| 8 | Image too large | Icon (warning); « Image trop lourde »; « 20,3 Mo, la limite est de 20 Mo. » (one decimal, megabytes of 1 048 576 bytes, as the server); button « Fermer »; nothing is sent |
| 9 | Link with an accompanying text | Screen 2, the « Note (facultative) » field prefilled with the text shared with the URL (a Safari selection), editable (decided on 2026-11-13: URL > image > text, the accompanying text goes to the note) |
| 10 | Refused by the server | Ban icon (danger); « Hippo a refusé »; the problem's `detail` as the server wrote it (French, shown as is); caption « Message renvoyé par le serveur »; button « Fermer »; no « Réessayer » |
| 11 | Token refused (401) | Key-off icon (danger); « Accès refusé »; « Le token de l'appli ne correspond plus au serveur. Reconstruis l'appli. »; button « Fermer » |
| 12 | Saved, private | Lock icon (success); « Enregistré »; « Privé : il attend dans À trier et n'est jamais envoyé à l'IA. »; closes by itself like screen 5 |

Screens 9 to 12 and the corrected texts of screens 4, 6 and 8 were validated by Ghislain on 2026-11-13, after the adversarial pass.

While sending, « Enregistrer » shows a progress indicator and the form is not editable.

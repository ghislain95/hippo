# ADR-0008 — Extraction locale et conservation du contenu original

- **Statut** : Accepté
- **Date** : 2026-10-11

## Contexte

Envoyer une page web complète à l'IA coûte cher (le HTML est surtout du bruit) et donne de moins bons résultats. Par ailleurs, les pages disparaissent ou changent, et le prompt va s'améliorer : on voudra réanalyser d'anciens éléments.

## Décision

- L'**extraction** se fait sur le NAS, juste après la capture, sans IA :
  - pages web avec `go-readability` (contenu principal, titre, site) ;
  - vidéos YouTube : métadonnées et transcription quand elle existe ;
  - repli sur les métadonnées `og:*` en cas d'échec (RG-23).
- Le **contenu extrait est conservé** dans la base (jusqu'à environ 120 000 caractères), ainsi que les images capturées.
- Chaque fiche enregistre le **modèle**, la **version du prompt** et la **version du schéma** qui l'ont produite.
- La **réanalyse** repart du contenu conservé, sans retélécharger, et ne touche jamais aux corrections manuelles (RG-61).

## Conséquences

- Seul le texte utile est facturé.
- On peut réanalyser de façon ciblée (« toutes les recettes produites avec le schéma v1 ») après une amélioration.
- La base grossit plus vite (le texte des articles), ce qui reste négligeable : quelques dizaines à quelques centaines de Mo.
- Les contenus très longs sont coupés en V1 et marqués ; un découpage en sections arrive en V1.5.

## Alternatives écartées

- **Envoyer l'URL et laisser l'IA lire la page** : coût et résultat imprévisibles, dépend d'outils de navigation, et ne permet pas de réanalyser une page disparue.
- **Ne garder que la fiche** : toute amélioration du prompt resterait sans effet sur l'existant.

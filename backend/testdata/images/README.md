# Images de test

Images **synthétiques** lues par les tests de `internal/imagecopy` (copie de l'image envoyée à l'IA, change `submit-nightly-batch`, tâche 3.3). Aucune photo, aucun contenu personnel ([ADR-0027](../../../docs/adr/0027-depot-public.md)) : quatre quadrants de couleur unie (rouge en haut à gauche, vert en haut à droite, bleu en bas à gauche, jaune en bas à droite) et un léger dégradé, dessinés par [`synthetic.go`](synthetic.go).

Seuls les formats qu'aucun encodeur de Go ne sait écrire sont versionnés ici (HEIC, WebP). Les JPEG et PNG (EXIF, en-tête de plus de 50 Mpx, JPEG coupé, transparence) sont construits en code par `internal/capture/imagetest` ; les images de plusieurs dizaines de mégapixels des mesures ne sont jamais versionnées.

| Fichier | Contenu | Taille |
| --- | --- | --- |
| `rotated.heic` | 640 × 480 stockés, propriété `irot` d'angle 3 (un quart de tour dans le sens horaire pour la redresser : 480 × 640, le bleu en haut à gauche) ; item Exif réduit à la seule orientation 6 | 929 octets |
| `grid.heic` | 2048 × 1536 en image `grid` de 12 tuiles de 512 × 512, comme les photos d'iPhone | 3 600 octets |
| `quadrants.webp` | 640 × 480, WebP avec pertes (`VP8 `), sans métadonnées | 1 032 octets |

## Fabrication

Sur macOS (fait le 6 novembre 2026 avec `sips-316`, macOS 27, et `cwebp` 1.6.0 de Homebrew `webp`), depuis ce dossier :

```bash
tmp=$(mktemp -d)
go run synthetic.go 640 480 "$tmp/rotated.jpg" 6       # JPEG dont l'EXIF ne porte que l'orientation 6
sips -s format heic "$tmp/rotated.jpg" --out rotated.heic
go run synthetic.go 2048 1536 "$tmp/grid.png"
sips -s format heic "$tmp/grid.png" --out grid.heic
go run synthetic.go 640 480 "$tmp/quadrants.png"
cwebp -quiet -metadata none -q 80 "$tmp/quadrants.png" -o quadrants.webp
```

`sips` traduit l'orientation EXIF du JPEG en propriété `irot` et recopie l'EXIF (l'orientation seule) ; partant d'un PNG, il n'écrit aucun EXIF. Ni l'un ni l'autre n'ajoute de date, d'appareil ni de nom de machine : les chaînes du fichier se limitent aux noms de boîtes ISO BMFF (`strings -n 4 rotated.heic`). `cwebp -metadata none` n'écrit aucun bloc EXIF, XMP ni ICC.

Un test de `internal/imagecopy` relit les dimensions de chaque fichier, vérifie que `grid.heic` est bien une grille et que chaque fichier fait moins de 200 Ko.

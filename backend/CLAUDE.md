# backend/ — service Go

Binaire Go unique : API HTTP, stockage SQLite, extraction, batch nocturne ([ADR-0003](../docs/adr/0003-backend-go.md)). Conventions complètes : [`docs/dev/conventions-backend.md`](../docs/dev/conventions-backend.md). Tests : [`docs/dev/testing.md`](../docs/dev/testing.md).

## Règles critiques

- Librairie standard (`net/http`, `log/slog`) ; aucune dépendance nouvelle sans accord de Ghislain.
- Contrat d'abord : `api/openapi.yaml`, puis `make generate`. `*.gen.go` ne s'édite jamais.
- `user_id` dans chaque requête sur une table métier (RG-90) ; jamais d'élément privé vers l'IA (RG-12) ; budget vérifié avant chaque envoi (RG-81).
- Handlers sans logique métier ; seul `internal/analysis` importe le SDK Anthropic (règles `depguard` de `.golangci.yml`).
- Migrations : nouveau fichier `internal/store/migrations/NNNNN_<nom>.sql`, jamais la modification d'un fichier existant.
- Logs JSON sans secret ni contenu d'élément.
- Tests d'intégration sur vraie SQLite (`t.TempDir()`), sans réseau ; un scénario OpenSpec = un sous-test nommé d'après lui.

## Commandes (depuis la racine du dépôt)

```bash
make dev             # lance le backend sur 127.0.0.1:8080 (variables de infra/.env)
make check-backend   # lint + tests (-race) + build
make test-backend    # go test -race -cover ./...
make lint-backend    # golangci-lint run
make fmt             # formate le Go (gofumpt, goimports) et l'appli
make generate        # api.gen.go et schéma de fiches assemblé
```

---
name: verify-changes
description: Use when finishing any coding task in this repo — run format, vet, lint, unit and integration tests (DATABASE_TEST_DSN), race checks, and a live smoke test with the built binary before reporting done.
---

# Verifying changes

Run every gate that applies; report exact results. Do not claim success without output.

## Gates (in order)

```bash
gofmt -l .                       # must print nothing
go vet ./...
golangci-lint run ./...          # exit 0

# Integration tests (real Postgres; each package gets its own schema)
$env:DATABASE_TEST_DSN = "postgres://postgres:postgres@127.0.0.1:5432/ims_go_api_test?sslmode=disable"
go test -count=1 ./...
go test -count=1 -race -p 2 ./...   # -p 2 locally: race builds are memory-hungry
```

## Live smoke (for HTTP/CLI changes)

```bash
make migrate || go run ./cmd/app migrate
go build -o bin/ims.exe ./cmd/app
# start: $p = Start-Process .\bin\ims.exe serve -PassThru -NoNewWindow
# health: curl http://127.0.0.1:8080/api/health
# login:  POST /api/v1/login {"email":"superadmin@ims.com","password":"123456","device":"smoke"}
#        then use -H "Authorization: Bearer <token>"
#        impersonation: -H "X-Acting-Tenant: 1" -H "X-Acting-Branch: 1"
# stop: Stop-Process -Id $p.Id -Force
```

PowerShell quirk: pass JSON bodies via `--data-binary "@file.json"` — inline quoting with
`curl.exe` mangles double quotes.

## Also check

- New tables/columns: `migrate:status` shows goose + `River schema: vN (0 pending)`.
- Seeding: `db:seed` twice must report no duplicates (idempotent).
- Specs: tick the phase checklist and record implementation notes for anything you deviated on.
- Never commit; the user commits.

---
title: Extending the dashboard
description: Add a maintenance command, a dashboard endpoint, a panel or a palette entry.
---

## Add a maintenance command

The Commands panel runs an **allowlist** — nothing else can run from the browser. Add an entry to
`internal/app/dev_commands.go`:

```go title="internal/app/dev_commands.go"
{
	Name: "products:reindex", Label: "Reindex products", Group: "Maintenance",
	Description: "Rebuilds the product search index.",
	Confirm:    true,             // changes data → the UI asks first
	Background: true,             // slow → returns at once; watch the run
	Timeout:    10 * time.Minute,
	Options: []dev.CommandOption{
		{Name: "tenant", Label: "Tenant id", Type: "number", Description: "Empty = every tenant."},
		{Name: "dry_run", Label: "Dry run", Type: "bool", Default: "true"},
	},
	Run: func(ctx context.Context, opts map[string]string, out io.Writer) error {
		count, err := p.Products.Reindex(ctx, opts["tenant"], opts["dry_run"] == "true")
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "reindexed %d products\n", count)
		return nil
	},
},
```

- Option types: `bool`, `number`, `text`, `select` (with `Choices`).
- Write human-readable output to `out`; it is stored with the run and shown in the panel.
- Every run is audited (`command.run`) with its options, status and duration.
- Keep the CLI and the dashboard doing the **same** work: call the same service method from the
  Cobra command.

## Add a dashboard endpoint

1. A service method in `internal/modules/dev` (or call an existing module service).
2. A handler on `dev.Handler` — it embeds the same `httpx.Kit` as every module (`h.Bind`,
   `h.PathID`, `h.Data`).
3. The route in `internal/modules/dev/routes.go`; mutations add `r.With(h.manage)` and record an
   audit entry with `h.audit.Record(...)`.
4. Document it in `api/openapi.yaml`, run `make openapi` and `make openapi-check`.
5. Refresh the SPA's types: `npm --prefix web/dev run openapi:gen`.

## Add a panel

1. A route file `web/dev/src/routes/_app.<name>.tsx` (TanStack Router file routes).
2. Fetch with TanStack Query and the typed `api()` client (`src/api/client.ts`); export the
   response type from `src/api/types.ts`.
3. Use the shared components: `Card`, `Chip`, `StatCard`, `Drawer`, `Modal`, `Tabs` from
   `components/ui.tsx`, and **`Select`, `DatePicker`, `TimePicker`** from `components/inputs.tsx` —
   never the browser's native `<select>` or date input.
4. Add it to the sidebar in `components/AppShell.tsx`.
5. Deep-link state belongs in the URL (`validateSearch`), so links like `?tenant=7` work.

```bash
make dev-web             # Vite with hot reload on :8080/dev
make web-typecheck       # tsc
make web-build           # what the release embeds
```

## Add a palette entry

`web/dev/src/components/CommandPalette.tsx` lists pages, Inspector tabs and actions; add yours to
the matching array so **Ctrl+K** can reach it.

# Phase 10 — OpenAPI spec generator (code-aware, single-file)

**Status:** Done (2026-10-18)
**Roadmap:** PLAN.md §11 Phase 10
**Depends on:** Phase 7 (`ims gen module`), Phase 6 (embedded spec, `openapi:check`, kin-openapi)
**Started:** 2026-10-18

## Goal

Stop hand-writing the CRUD boilerplate in `api/openapi.yaml`. When a module is scaffolded
(`ims gen module`) or an existing module is analyzed (`ims gen openapi --module=X`), the tool
inserts that resource's paths and schemas into a **managed region of the one committed spec**,
deterministically and without ever reformatting the hand-written parts. The spec stays the single
source of truth; humans keep full control of everything outside the generated region.

## Reference (Laravel)

- `make:crud --with-fields --fields=...` — the declarative field spec our generator already mirrors;
  this phase extends it to also emit the API documentation block (Scribe-style output, but
  hand-tunable and region-scoped).
- Scribe — the DX bar: scaffolding a resource documents it automatically. We do **not** adopt
  annotation-driven full-spec generation (no swag/Huma; wrong ownership model, swag v2 is RC).

## Decisions (resolved at spec review)

1. **Single file, two managed regions.** No per-module spec files, no fragments, no bundler. The
   committed `api/openapi.yaml` gets exactly two marker-delimited regions: one inside `paths:` and
   one inside `components.schemas:`. Everything outside them is hand-written and never touched.
2. **Text splice, never a YAML round-trip.** The tool performs byte-level replacement between
   markers. Comments, anchors (`&`/`*`), ordering and flow style elsewhere are preserved exactly.
3. **Generated region = generated.** Anything between the markers is owned by the tool and is
   overwritten on regeneration; to customize, copy an entry into the hand-written section (or
   exclude the resource). This is documented on the marker comments themselves.
4. **Schema-name collisions are an error.** If a generated schema name (e.g. `Tenant`) already
   exists outside the region, the tool aborts (`--force` is required and still refuses, guiding the
   user to rename or use `--schema-prefix`). No silent clobbering.
5. **Analyzer is convention-bound and non-guessing.** It reads only declarative files (`routes.go`,
   `request.go`) with `go/ast`. If a route or struct is not found, it warns and emits nothing for it
   — it never reverse-engineers handler bodies.
6. **Scope now:** both flows ship — `ims gen module` emits the block (A/B) and
   `ims gen openapi --module=X --all` analyze existing modules (C).
7. **No new dependencies.** `kin-openapi` (validate), `text/template` (render), `go/ast`
   (analyze), `cobra` (CLI) — all already in the module graph.

## Detailed design

### Markers

```yaml
paths:
  /api/v1/system/info:            # hand-written — untouched
    …
  # ─── BEGIN GENERATED PATHS (ims gen openapi) — resources: product, widget — do not edit ───
  /api/v1/product:
    get: { … }
    post: { … }
  /api/v1/product/{id}:
    get: { … }
    patch: { … }
    delete: { … }
  # ─── END GENERATED PATHS ───

components:
  schemas:
    Problem: { … }                # hand-written — untouched
    # ─── BEGIN GENERATED SCHEMAS (ims gen openapi) — do not edit ───
    Product: { … }
    ProductUpsertRequest: { … }
    ProductEnvelope: { … }
    ProductPageEnvelope: { … }
    # ─── END GENERATED SCHEMAS ───
```

The `resources:` list in the paths marker is a machine-readable index used by `--all` (so we only
regenerate resources the tool owns, never hand-written endpoints).

### Generated content per resource

For `--name=product --fields="name:string,price:decimal,is_active:bool"`:

- **Schemas** (4): `Product`, `ProductUpsertRequest`, `ProductEnvelope`, `ProductPageEnvelope`
  (envelopes reuse the shared hand-written `PaginationLinks` / `PaginationMeta`).
- **Paths** (5): `GET`/`POST /api/v1/product`, `GET`/`PATCH`/`DELETE /api/v1/product/{id}` with our
  `{data}` / `{data,links,meta}` envelopes, standard `400/401/403/404/422/429` problem responses,
  `204` on delete, `BearerAuth`, and the two `X-Acting-*` headers.

### Field → schema mapping

| Field type | OpenAPI property |
|---|---|
| `string`, `text` | `{type: string, maxLength: 255}` (+ `nullable: true` when optional) |
| `int` | `{type: integer, format: int32}` (+ nullable) |
| `bigint`, `decimal` | `{type: integer, format: int64}` (+ nullable) |
| `bool` | `{type: boolean}` (+ nullable) |
| `date`, `datetime` | `{type: string, format: date-time, nullable: true}` |
| `json` | `{type: object, additionalProperties: true, nullable: true}` |

`required` is derived from `Optional` (optionals omitted). The resource schema additionally has
`id`, `tenant_id` (int64) and `created_at`, `updated_at` (date-time). Request schema = fields only.
Optional fields additionally carry an `example:` so Scalar prefills sensibly.

### Naming

- Resource: `pascal(name)` → `Product`.
- Request: `<Resource>UpsertRequest`.
- Envelopes: `<Resource>Envelope`, `<Resource>PageEnvelope`.
- Paths: `/<name>` and `/<name>/{id}` prefixed with the API base (`/api/v1`).
- Tags: `<Resource>s` (from the plural used by the generator).

### Determinism

- Resources sorted by name; within a resource, schemas before paths; path order
  `GET collection → POST collection → GET item → PATCH item → DELETE item`; property order
  `id, tenant_id, <fields in declared order>, created_at, updated_at`.
- Indentation fixed (2 spaces for path items, 4 for schema keys); LF newlines matching the file.
- Regenerating twice yields a byte-identical file.

### Splice algorithm (`internal/scaffold/openapi.go`)

1. Read `api/openapi.yaml` as bytes; detect newline style.
2. Locate the four marker lines. If missing → error telling the user to run the one-time
   `ims gen openapi --init` (which inserts empty regions at the fixed anchors: paths region directly
   before `components:`, schemas region at the end of the `schemas:` mapping).
3. Render the region bodies for all owned resources (from `--all` index or the single `--module`).
4. Replace the text strictly between the marker lines.
5. Parse the whole document with `kin-openapi` (`LoadFromData` + `Validate`); on failure abort and
   write nothing.
6. Write atomically (temp file + rename), preserving permissions.

### Analyzer (`internal/scaffold/openapi_analyze.go`)

- `routes.go`: `go/parser` walk for `r.<Get|Post|Patch|Put|Delete>("<path>", …)` → method + path;
  drop the leading `/`, prefix `/api/v1`. Reject `{id}`-less item routes (warn).
- `request.go`: find `InsertUpdateRequest` and `Response` structs; read each field's name,
  Go type (→ optional when pointer), `json:` tag and `validate:` tag (→ required + maxLength).
- Missing struct or no routes → warning, resource skipped.
- Output is a `ResourceSpec` struct consumed by the same renderer as `ims gen module` (single code
  path for both flows).

### Commands

- `ims gen module …` — after scaffolding, refresh the spec region (add `--no-openapi` to skip).
- `ims gen openapi --module=<name> [--dry-run] [--check]` — add/update one resource.
- `ims gen openapi --all [--dry-run] [--check]` — refresh every resource in the index.
- `ims gen openapi --init` — one-time: insert the empty marker regions.
- `--dry-run` prints the region diff; `--check` exits non-zero when the committed region is stale.
- Makefile: `make openapi-gen` (alias `ims gen openapi --all`); `make openapi-check` additionally
  runs `--check`.

### Guardrail

Extend `ims openapi:check` to run, in order: (1) existing spec validation + live-route drift
(unchanged), (2) `ims gen openapi --all --check` (region freshness). Step 2 is static (no DB), so CI
can also run it in an early, DB-less job.

## Scope checklist

Region infrastructure:

- [x] Markers added to `api/openapi.yaml` (+ one-time `--init`) and the two empty regions committed
- [x] `internal/scaffold/openapi.go`: marker locate, deterministic render, splice, kin-openapi
      validate, atomic write
- [x] `ResourceSpec` model + `fieldData` → schema mapping + envelope/page-envelope + path templates

Generator integration:

- [x] `ims gen module` calls the spec writer (with `--no-openapi` escape)
- [x] Analyzer (`routes.go` + `request.go` via `go/ast`) producing `ResourceSpec`
- [x] `ims gen openapi --module|--all|--init` with `--dry-run` / `--check`

Guardrails & docs:

- [x] `ims openapi:check` extended with the freshness check
- [x] `make openapi-gen`, `make openapi-check` updated; CI step added (DB-less)
- [x] README/runbook note; AGENTS.md "Adding a new module" step updated

## Implementation notes

- **Files:** `internal/scaffold/openapi.go` (ResourceSpec, field mapping, region render, marker
  locate/splice, `InitRegions`, `ParseIndex`, kin-openapi validate), 
  `internal/scaffold/openapi_analyze.go` (`go/ast` analyzer), `internal/scaffold/openapi_test.go`
  (mapping, determinism, splice idempotency, hand-written preservation, collision, init, analyzer),
  `internal/cli/openapi_gen.go` (`gen openapi` + `refreshOpenAPI`/`checkOpenAPIRegions` helpers),
  `internal/cli/gen.go` (`--no-openapi` + integration), `internal/cli/openapi.go` (freshness in
  `openapi:check`).
- **Markers:** detection is substring-based (`BEGIN/END GENERATED PATHS|SCHEMAS`); the
  `resources:` index on the paths marker drives `--all`.
- **Responses** in the generated region reference `#/components/responses/*` (not YAML aliases) so
  the block stays valid regardless of anchor placement; schemas reuse the hand-written
  `PaginationLinks` / `PaginationMeta`.
- **Idempotency:** resources are sorted by name; within a resource schemas precede paths and
  operations follow `GET collection → POST collection → GET/PATCH/DELETE item`. Regenerating twice
  is byte-identical (covered by tests and a live `gen openapi --all` twice run).

Decision resolutions (open questions):

1. **List params:** emit `page`, `per_page` and `search` (only when the resource has a string
   field). `order_by`/`order_direction` are *not* emitted — the scaffolded handler does not
   implement ordering, and documenting it would misrepresent the endpoint. Revisit if/when the
   generated handler gains whitelisted ordering.
2. **Envelopes:** emitted as `<Resource>Envelope` / `<Resource>PageEnvelope` schemas (as decided).
3. **`--all` discovery:** the `resources:` index comment (as decided).
4. **`make check`:** region freshness lives in `gen-check` (and thus `openapi-check`); CI runs
   `ims gen openapi --all --check` in the DB-less build-test job.

Deviations from the draft spec:

- **`--schema-prefix` not implemented.** A schema collision always aborts with a message guiding the
  user to rename the resource or move the hand-written schema; `--force` is likewise not needed
  because the tool never writes on collision. Add a prefix option later if a real need appears.
- **Analyzer tolerates plural module directories** (`resource "branch"` in `internal/modules/branches`)
  so hand-written modules can be documented too; the generated-module convention (dir == name) is
  preferred.
- **`--init` is idempotent** and validated via the contract test (`TestOpenAPISpecIsValid`) instead
  of a separate validation pass.
- **Make target is `openapi-gen`, not `openapi:gen`.** A colon in a make target name is not a legal
  identifier (it breaks the rule/`.PHONY` parse), and the repo already uses hyphenated targets
  (`gen-check`, `openapi-check`). The alias behaviour is unchanged.

## Out of scope

- Per-module spec files or a bundler.
- Generating docs for hand-written modules automatically (they require an explicit `--module`).
- Non-CRUD/derived endpoints, webhooks, or custom envelopes beyond `Envelope`/`PageEnvelope`.
- Swagger UI; the Scalar UI (`/docs`) already serves the resulting spec.
- Replacing the hand-tuned spec or adopting swag/Huma.

## Contracts touched

- **DB tables / migrations:** none.
- **API endpoints:** unchanged; the generator writes entries into `api/openapi.yaml`.
- **Env vars / config:** none.
- **CLI commands:** `ims gen openapi` (new); `ims gen module` gains `--no-openapi`;
  `ims openapi:check` extended.
- **Build:** `Makefile` (`openapi-gen`, `openapi-check`), CI workflow.

## Acceptance criteria

- `ims gen module --name=widget --fields=…` scaffolds the module **and** adds its paths + 4 schemas
  to the generated regions; `ims openapi:check` passes.
- `ims gen openapi --module=product` on an existing module adds the same block with no manual YAML.
- Running any generator twice produces a byte-identical `api/openapi.yaml` (idempotent).
- Hand-written regions are byte-identical after regeneration (verified by a test that splices and
  compares everything outside the markers).
- `ims gen openapi --all --check` exits non-zero when a region is stale and zero when current.
- A schema-name collision aborts with a clear message and writes nothing.
- `make check` and the kin-openapi contract tests stay green; no new dependencies added.
- The generator itself is covered by unit tests (schema mapping, splice idempotency, collision,
  analyzer parse, `--check`).

## Open questions

- List query params for generated endpoints: fixed set (`page`, `per_page`, `search`, `order_by`,
  `order_direction`) with a whitelisted `order_by`, or only `page`/`per_page`?
- Emit `<Resource>Envelope`/`PageEnvelope` schemas, or inline the envelope object in each path?
  (Decision: schemas, for consistency with the hand-written spec.)
- `--all` discovery: the `resources:` index comment (chosen) vs scanning `internal/modules/*`.
- Should `make check` include `gen openapi --all --check`, or keep it in `openapi-check` only?

## Effort & risks

| Step | Size |
|---|---|
| Marker splice + validate + atomic write | small |
| Schema/path templates + field mapping | small–medium |
| Analyzer (`go/ast`) | medium |
| CLI + `gen module` wiring + Makefile/CI | small |
| Tests | medium |

Risks: edits placed inside the generated region are overwritten (documented; region is small and
mechanical); the analyzer is convention-bound (mitigated by warn-and-skip, never guess); the
generator and `scaffold` templates must stay in sync (mitigated by a shared `ResourceSpec` and a
test asserting the generated module's routes match the emitted paths).

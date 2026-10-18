package scaffold

import (
	"strings"
	"testing"
)

func TestParseFields(t *testing.T) {
	fields, err := ParseFields("name:string,notes:text?,amount:decimal,active:bool,due:date,meta:json")
	if err != nil {
		t.Fatalf("ParseFields() error = %v", err)
	}
	if len(fields) != 6 {
		t.Fatalf("len = %d, want 6", len(fields))
	}
	if fields[0].Name != "name" || fields[0].Type != "string" || fields[0].Optional {
		t.Errorf("field 0 = %+v", fields[0])
	}
	if !fields[1].Optional || fields[1].Type != "text" {
		t.Errorf("field 1 = %+v", fields[1])
	}
}

func TestParseFieldsErrors(t *testing.T) {
	cases := map[string]string{
		"empty":     "",
		"reserved":  "id:string",
		"bad type":  "name:wizard",
		"no type":   "name",
		"duplicate": "name:string,name:string",
		"not snake": "Name:string",
	}
	for name, spec := range cases {
		if _, err := ParseFields(spec); err == nil {
			t.Errorf("%s: expected error for %q", name, spec)
		}
	}
}

func TestGenerateProducesExpectedFiles(t *testing.T) {
	fields, err := ParseFields("name:string,notes:text?,amount:decimal,active:bool,due:date,meta:json")
	if err != nil {
		t.Fatal(err)
	}
	files, err := Generate(Options{
		ModulePath: "example.com/app",
		Name:       "product",
		NextNumber: 13,
		Fields:     fields,
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	byPath := map[string]string{}
	for _, f := range files {
		byPath[f.Path] = string(f.Content)
	}

	want := []string{
		"migrations/00013_create_products.sql",
		"sql/queries/products.sql",
		"internal/modules/product/model.go",
		"internal/modules/product/repository.go",
		"internal/modules/product/service.go",
		"internal/modules/product/request.go",
		"internal/modules/product/handler.go",
		"internal/modules/product/routes.go",
		"internal/modules/product/service_test.go",
	}
	for _, path := range want {
		if _, ok := byPath[path]; !ok {
			t.Errorf("missing generated file %s", path)
		}
	}

	checks := map[string]string{
		"migrations/00013_create_products.sql":   "CREATE TABLE products",
		"sql/queries/products.sql":               "-- name: GetProduct :one",
		"internal/modules/product/model.go":      "type Product struct",
		"internal/modules/product/repository.go": "func NewRepository(queries *dbgen.Queries) Repository",
		"internal/modules/product/service.go":    "PermView",
		"internal/modules/product/handler.go":    "func (h *Handler) index",
		"internal/modules/product/routes.go":     "func (h *Handler) RegisterRoutes",
	}
	for path, needle := range checks {
		if !strings.Contains(byPath[path], needle) {
			t.Errorf("%s missing %q", path, needle)
		}
	}
}

func TestGenerateRejectsBadName(t *testing.T) {
	if _, err := Generate(Options{Name: "", Fields: []Field{{Name: "x", Type: "string"}}}); err == nil {
		t.Error("expected error for empty name")
	}
}

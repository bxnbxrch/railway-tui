package railwaycli

import (
	"encoding/json"
	"testing"

	"railway-tui/internal/model"
)

func TestParseDomainList(t *testing.T) {
	// Shape captured from `railway domain list --json`.
	const data = `{"domains":[
	  {"id":"d1","domain":"www.example.com","type":"custom","targetPort":8080,"syncStatus":"ACTIVE","createdAt":"2026-07-02T20:55:15.064+00:00"},
	  {"id":"d2","domain":"api-production.up.railway.app","type":"service","targetPort":3000,"syncStatus":"ACTIVE"}
	]}`
	var raw rawDomainList
	if err := json.Unmarshal([]byte(data), &raw); err != nil {
		t.Fatal(err)
	}
	if len(raw.Domains) != 2 {
		t.Fatalf("want 2 domains, got %d", len(raw.Domains))
	}
	d := raw.Domains[0].toModel()
	if d.Domain != "www.example.com" || d.Type != "custom" || d.TargetPort != 8080 {
		t.Fatalf("domain parsed wrong: %+v", d)
	}
	if d.URL() != "https://www.example.com" {
		t.Fatalf("url wrong: %q", d.URL())
	}
	if raw.Domains[1].toModel().Type != "service" {
		t.Fatalf("second domain type wrong")
	}
}

func TestStringifyVariableValues(t *testing.T) {
	// `variable list --json` values are strings, but decode defensively.
	cases := []struct {
		in   any
		want string
	}{
		{"https://x.com", "https://x.com"},
		{float64(42), "42"},
		{true, "true"},
		{nil, ""},
	}
	for _, c := range cases {
		if got := stringify(c.in); got != c.want {
			t.Errorf("stringify(%v): want %q got %q", c.in, c.want, got)
		}
	}
}

func TestSortVarsCaseInsensitive(t *testing.T) {
	got := []model.Variable{{Name: "Zebra"}, {Name: "apple"}, {Name: "Mango"}}
	sortVars(got)
	want := []string{"apple", "Mango", "Zebra"}
	for i, w := range want {
		if got[i].Name != w {
			t.Fatalf("pos %d: want %q got %q", i, w, got[i].Name)
		}
	}
}

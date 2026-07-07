package railwaycli

import (
	"encoding/json"
	"testing"

	"railway-tui/internal/model"
)

func TestParseServiceList(t *testing.T) {
	const data = `[
	  {"id":"svc1","name":"Database","isLinked":false,
	   "source":{"repo":null,"image":"ghcr.io/x/postgres:18"},
	   "status":"SUCCESS","deploymentStopped":false,"deploymentId":"d1",
	   "latestDeployment":{"id":"d1","status":"SUCCESS","createdAt":"2026-07-02T10:01:54.654Z","deploymentStopped":false},
	   "url":null,
	   "volumes":[{"name":"v","mountPath":"/data","currentSizeMb":935.87,"sizeMb":50000,"state":"READY"}],
	   "regions":[{"name":"eu","location":"EU West","configured":1}],
	   "replicas":{"configured":1,"running":1,"crashed":0,"exited":0,"total":1}}
	]`
	var raw []rawService
	if err := json.Unmarshal([]byte(data), &raw); err != nil {
		t.Fatal(err)
	}
	s := raw[0].toModel()
	if s.Name != "Database" || s.Status != model.StatusSuccess {
		t.Fatalf("bad service: %+v", s)
	}
	if s.Image != "ghcr.io/x/postgres:18" {
		t.Fatalf("image not parsed: %q", s.Image)
	}
	if s.Replicas.Running != 1 || len(s.Volumes) != 1 {
		t.Fatalf("replicas/volumes wrong: %+v", s.Replicas)
	}
	if s.LatestDeploy == nil || s.LatestDeploy.CreatedAt.IsZero() {
		t.Fatal("latest deploy time not parsed")
	}
}

func TestParseStatusTopology(t *testing.T) {
	const data = `{
	  "id":"proj1","name":"unity","workspace":{"name":"Unity"},
	  "environments":{"edges":[
	    {"node":{"id":"e1","name":"production","canAccess":true,"serviceInstances":{"edges":[
	      {"node":{"serviceId":"s1","serviceName":"backend","environmentId":"e1","numReplicas":2,
	        "latestDeployment":{"id":"d1","status":"SUCCESS","createdAt":"2026-07-07T14:10:55.820Z"},
	        "domains":{"customDomains":[],"serviceDomains":[{"domain":"backend.up.railway.app"}]},
	        "source":{"repo":"org/backend","image":null}}}
	    ]}}}
	  ]}
	}`
	var raw rawStatus
	if err := json.Unmarshal([]byte(data), &raw); err != nil {
		t.Fatal(err)
	}
	p := raw.toModel()
	if p.Name != "unity" || p.Workspace != "Unity" {
		t.Fatalf("project meta wrong: %+v", p)
	}
	if len(p.Environments) != 1 || p.Environments[0].Name != "production" {
		t.Fatalf("env wrong: %+v", p.Environments)
	}
	svc := p.Environments[0].Services[0]
	if svc.Name != "backend" || svc.Status != model.StatusSuccess {
		t.Fatalf("svc wrong: %+v", svc)
	}
	if svc.URL != "https://backend.up.railway.app" {
		t.Fatalf("url wrong: %q", svc.URL)
	}
	if svc.Replicas.Configured != 2 {
		t.Fatalf("replicas wrong: %d", svc.Replicas.Configured)
	}
}

func TestParseMetrics(t *testing.T) {
	const data = `{"environment":"dev","measurements":{
	  "CPU_USAGE":[{"ts":"2026-07-07T13:27:30+00:00","value":0.5},{"ts":"2026-07-07T13:28:00+00:00","value":0.7}],
	  "MEMORY_USAGE_GB":[{"ts":"2026-07-07T13:27:30+00:00","value":0.25}]
	}}`
	var raw rawMetrics
	if err := json.Unmarshal([]byte(data), &raw); err != nil {
		t.Fatal(err)
	}
	m := raw.toModel("backend")
	if m.Environment != "dev" {
		t.Fatalf("env wrong: %q", m.Environment)
	}
	cpu := m.Series["CPU_USAGE"]
	if len(cpu.Points) != 2 || cpu.Last() != 0.7 {
		t.Fatalf("cpu series wrong: %+v", cpu)
	}
	if cpu.Points[0].TS.IsZero() {
		t.Fatal("metric timestamp not parsed")
	}
}

func TestParseProjectRefs(t *testing.T) {
	const data = `[{"workspace":{"name":"Unity"},"id":"p1","name":"unity",
	  "environments":{"edges":[
	    {"node":{"id":"e1","name":"production","canAccess":true}},
	    {"node":{"id":"e2","name":"dev","canAccess":true}}
	  ]}}]`
	var raw []rawProjectRef
	if err := json.Unmarshal([]byte(data), &raw); err != nil {
		t.Fatal(err)
	}
	ref := model.ProjectRef{ID: raw[0].ID, Name: raw[0].Name, Workspace: raw[0].Workspace.Name}
	for _, e := range raw[0].Environments.Edges {
		ref.Envs = append(ref.Envs, model.EnvRef{ID: e.Node.ID, Name: e.Node.Name})
	}
	if ref.Name != "unity" || len(ref.Envs) != 2 || ref.Envs[1].Name != "dev" {
		t.Fatalf("project ref wrong: %+v", ref)
	}
}

func TestDecodeLogLineHTTP(t *testing.T) {
	src := model.Source{ServiceName: "api", Kind: model.LogHTTP}
	lls := decodeLogLines(`{"timestamp":"2026-07-07T14:27:13.879Z","method":"GET","path":"/api/x","httpStatus":200,"totalDuration":42}`, src)
	if len(lls) != 1 {
		t.Fatalf("want 1 line, got %d", len(lls))
	}
	if lls[0].Message == "" || lls[0].Timestamp.IsZero() {
		t.Fatalf("http line not summarized: %+v", lls[0])
	}
}

// TestDecodeLogLineMultiline verifies that a build-style message with a
// trailing newline and embedded newlines is split into one LogLine per row,
// each with a distinct timestamp (so replay dedup can't swallow rows).
func TestDecodeLogLineMultiline(t *testing.T) {
	src := model.Source{ServiceName: "api", Kind: model.LogBuild}
	lls := decodeLogLines(`{"timestamp":"2026-07-07T14:27:13.879Z","level":"info","message":"warn:\n- first\n- second\n"}`, src)
	if len(lls) != 3 {
		t.Fatalf("want 3 split lines, got %d: %+v", len(lls), lls)
	}
	want := []string{"warn:", "- first", "- second"}
	for i, w := range want {
		if lls[i].Message != w {
			t.Errorf("row %d: want %q got %q", i, w, lls[i].Message)
		}
	}
	if !lls[1].Timestamp.After(lls[0].Timestamp) || !lls[2].Timestamp.After(lls[1].Timestamp) {
		t.Errorf("split rows must have strictly increasing timestamps")
	}
	if lls[0].Level != "info" || lls[2].Level != "info" {
		t.Errorf("split rows must inherit level")
	}
}

// TestDecodeLogLineTrailingNewline verifies single-row messages lose their
// trailing newline (which previously rendered as blank rows in the pane).
func TestDecodeLogLineTrailingNewline(t *testing.T) {
	src := model.Source{ServiceName: "api", Kind: model.LogBuild}
	lls := decodeLogLines(`{"timestamp":"2026-07-07T14:27:13.879Z","message":"built in 3.37s\n"}`, src)
	if len(lls) != 1 || lls[0].Message != "built in 3.37s" {
		t.Fatalf("trailing newline not trimmed: %+v", lls)
	}
}

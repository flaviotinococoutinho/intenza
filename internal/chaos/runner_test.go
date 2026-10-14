package chaos

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// fake serves Toxiproxy, Prometheus and the admin/health endpoints.
type fake struct {
	mu      sync.Mutex
	calls   []string
	healthy bool
	metric  string
}

func (f *fake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, r.Method+" "+r.URL.Path)
	switch {
	case r.URL.Path == "/healthz":
		if f.healthy {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	case r.URL.Path == "/api/v1/query":
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"result": []any{map[string]any{"value": []any{1, f.metric}}}}})
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func runner(t *testing.T, f *fake) *Runner {
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	r, err := NewRunner(context.Background(), Env{API: srv.URL, Prometheus: srv.URL, Toxiproxy: srv.URL,
		Admin: map[string]string{"psp": srv.URL}, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func f64(v float64) *float64 { return &v }

func TestVerdicts(t *testing.T) {
	off := false
	exp := func(max float64) Experiment {
		return Experiment{
			ID: "x", Hypothesis: "h", Risk: "r",
			Steady:   []Probe{{Name: "vivo", HTTP: &HTTPProbe{URL: "", Status: 204}}},
			Method:   []Step{{Toxiproxy: &ToxiproxyAction{Proxy: "kafka", Enabled: &off}}, {Probe: &Probe{Name: "atraso", PromQL: &PromQLProbe{Query: "q"}, Max: f64(max)}}},
			Rollback: []Step{{Toxiproxy: &ToxiproxyAction{Reset: true}}},
		}
	}
	f := &fake{healthy: true, metric: "5"}
	r := runner(t, f)
	e := exp(10)
	e.Steady[0].HTTP.URL = r.Env.API + "/healthz"
	if j := r.Run(context.Background(), e); j.Verdict != "held" {
		t.Fatalf("held expected: %+v", j.Records)
	}
	e = exp(1)
	e.Steady[0].HTTP.URL = r.Env.API + "/healthz"
	j := r.Run(context.Background(), e)
	if j.Verdict != "falsified" {
		t.Fatalf("falsified expected: %s", j.Verdict)
	}
	resetSeen := false
	for _, c := range f.calls {
		resetSeen = resetSeen || c == "POST /reset"
	}
	if !resetSeen {
		t.Fatal("rollback must run even when the hypothesis is falsified")
	}
	f.healthy = false
	if j := r.Run(context.Background(), e); j.Verdict != "aborted" {
		t.Fatalf("not steady before the fault: aborted, got %s", j.Verdict)
	}
}

func TestJournalFiles(t *testing.T) {
	dir := t.TempDir()
	j := Journal{Experiment: Experiment{ID: "demo", Title: "Demo", Hypothesis: "h", Risk: "r"}, Verdict: "held",
		Records: []Record{{Phase: "verify", What: "p", OK: true, Value: f64(0.2)}}}
	if err := Write(dir, j); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "demo.json"))
	var back struct {
		Verdict    string `json:"verdict"`
		FinishedAt string `json:"finished_at"`
	}
	if err := json.Unmarshal(raw, &back); err != nil || back.Verdict != "held" {
		t.Fatalf("journal must carry the fields the knowledge base reads: %s", raw)
	}
	md, _ := os.ReadFile(filepath.Join(dir, "demo.md"))
	if !strings.Contains(string(md), "hipótese sustentada") {
		t.Fatalf("markdown: %s", md)
	}
}

func TestEveryExperimentFileParses(t *testing.T) {
	files, _ := filepath.Glob(filepath.Join("..", "..", "chaos", "experiments", "*.yaml"))
	if len(files) < 9 {
		t.Fatalf("experiments: %d", len(files))
	}
	for _, f := range files {
		if _, err := LoadExperiment(f); err != nil {
			t.Error(err)
		}
	}
}

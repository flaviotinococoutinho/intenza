package chaos

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/flaviotinococoutinho/intenza/internal/platform/buildinfo"
)

// fake serves Toxiproxy, Prometheus and the admin/health endpoints. Each
// query answers from its list in turn (the last one repeats): a number, ""
// for no series, "!" for an error.
type fake struct {
	mu      sync.Mutex
	calls   []string
	healthy bool
	series  map[string][]string
}

func (f *fake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, r.Method+" "+r.URL.Path)
	switch r.URL.Path {
	case "/healthz":
		if f.healthy {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	case "/api/v1/query":
		q := r.URL.Query().Get("query")
		answers := f.series[q]
		answer := ""
		if len(answers) > 0 {
			answer = answers[0]
			if len(answers) > 1 {
				f.series[q] = answers[1:]
			}
		}
		switch answer {
		case "!":
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "error", "error": "parse error"})
		case "":
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": map[string]any{"result": []any{}}})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": map[string]any{"result": []any{map[string]any{"value": []any{1, answer}}}}})
		}
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func runner(t *testing.T, f *fake) *Runner {
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	r, err := NewRunner(context.Background(), Env{
		API: srv.URL, Prometheus: srv.URL, Toxiproxy: srv.URL,
		Admin: map[string]string{"psp": srv.URL}, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
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
	f := &fake{healthy: true, series: map[string][]string{"q": {"5"}}}
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

// minimal builds a healthy experiment whose verdict depends on verify alone.
func minimal(r *Runner, verify ...Probe) Experiment {
	return Experiment{
		ID: "x", Hypothesis: "h", Risk: "r",
		Steady: []Probe{{Name: "vivo", HTTP: &HTTPProbe{URL: r.Env.API + "/healthz", Status: 204}}},
		Method: []Step{{Note: "falha"}},
		Verify: verify,
	}
}

func TestProbesThatMeasureNothingAbortInsteadOfRefuting(t *testing.T) {
	for name, answer := range map[string]string{"série ausente": "", "erro do prometheus": "!"} {
		t.Run(name, func(t *testing.T) {
			r := runner(t, &fake{healthy: true, series: map[string][]string{"q": {answer}}})
			// read as zero, the missing series would have "held" under max: 10
			j := r.Run(context.Background(), minimal(r, Probe{Name: "atraso", PromQL: &PromQLProbe{Query: "q"}, Max: f64(10)}))
			last := j.Records[len(j.Records)-1]
			if j.Verdict != "aborted" || !last.Inconclusive || last.OK {
				t.Fatalf("verdict %s, record %+v", j.Verdict, last)
			}
		})
	}
}

func TestCountersAreReadAsGrowthDuringTheRun(t *testing.T) {
	for _, c := range []struct {
		name    string
		answers []string // baseline, then verify
		want    string
	}{
		{"série nasce com o primeiro evento", []string{"", "1"}, "held"},
		{"contador herdado, nada novo", []string{"7", "7"}, "falsified"},
		{"contador herdado, um evento novo", []string{"7", "8"}, "held"},
		{"processo reiniciou depois de contar", []string{"7", "1"}, "held"},
		{"sem linha de base", []string{"!", "8"}, "aborted"},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := runner(t, &fake{healthy: true, series: map[string][]string{"dlq": c.answers}})
			j := r.Run(context.Background(), minimal(r, Probe{Name: "foi para a dlq", PromQL: &PromQLProbe{Query: "dlq", Delta: true}, Min: f64(1)}))
			if j.Verdict != c.want {
				t.Fatalf("verdict %s, want %s: %+v", j.Verdict, c.want, j.Records)
			}
		})
	}
}

func TestAMeasuredCounterexampleOutweighsABlindProbe(t *testing.T) {
	r := runner(t, &fake{healthy: true, series: map[string][]string{"lat": {"9"}, "nada": {""}}})
	j := r.Run(context.Background(), minimal(r,
		Probe{Name: "cega", PromQL: &PromQLProbe{Query: "nada"}, Max: f64(1)},
		Probe{Name: "atraso", PromQL: &PromQLProbe{Query: "lat"}, Max: f64(1)},
	))
	if j.Verdict != "falsified" {
		t.Fatalf("verdict %s", j.Verdict)
	}
}

func TestPauseAndUnpauseAreIdempotent(t *testing.T) {
	dir, err := os.MkdirTemp("", "dk") // short: unix socket paths cap near 100 bytes
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := filepath.Join(dir, "d.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{ReadHeaderTimeout: time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		switch {
		case strings.HasSuffix(r.URL.Path, "/unpause"):
			_, _ = io.WriteString(w, `{"message":"Container abc is not paused"}`)
		case strings.HasSuffix(r.URL.Path, "/pause"):
			_, _ = io.WriteString(w, `{"message":"Container abc is already paused"}`)
		default:
			_, _ = io.WriteString(w, `{"message":"boom"}`)
		}
	})}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	r := &Runner{Env: Env{DockerHost: "unix://" + sock, Project: "intenza"}}
	for _, action := range []string{"pause", "unpause"} {
		if err := r.docker(context.Background(), DockerAction{Service: "mongo", Action: action}); err != nil {
			t.Errorf("%s twice must be fine: %v", action, err)
		}
	}
	if err := r.docker(context.Background(), DockerAction{Service: "mongo", Action: "restart"}); err == nil {
		t.Error("a real failure must stay a failure")
	}
}

func TestDeltaProbesNeedUniqueNames(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.yaml")
	if err := os.WriteFile(path, []byte(`id: x
hypothesis: h
risk: r
method: [{wait: 1s}]
verify:
  - {name: dlq, promql: {query: a, delta: true}, min: 1}
  - {name: dlq, promql: {query: b, delta: true}, min: 1}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadExperiment(path); err == nil || !strings.Contains(err.Error(), "nomes únicos") {
		t.Fatalf("want a duplicate-name error, got %v", err)
	}
}

func TestJournalFiles(t *testing.T) {
	dir := t.TempDir()
	j := Journal{
		Experiment: Experiment{ID: "demo", Title: "Demo", Hypothesis: "h", Risk: "r"}, Verdict: "held",
		Records: []Record{{Phase: "verify", What: "p", OK: true, Value: f64(0.2)}},
	}
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

func TestEvidenceNamesTheCommitItCameFrom(t *testing.T) {
	if buildinfo.Read().Commit != "" {
		t.Skip("binary already stamped by the toolchain")
	}
	t.Setenv("INTENZA_COMMIT", "d4b494c")
	t.Setenv("GITHUB_SHA", "merge-ref")
	if got := evidenceCommit(); got != "d4b494c" {
		t.Fatalf("commit %q: the PR head must win over GitHub's merge commit", got)
	}
}

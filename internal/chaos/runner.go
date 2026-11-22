package chaos

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/flaviotinococoutinho/intenza/internal/chaos/load"
	"github.com/flaviotinococoutinho/intenza/internal/platform/buildinfo"
)

// Env is where the system under test lives.
type Env struct {
	API        string
	Prometheus string
	Toxiproxy  string
	MongoURI   string
	DockerHost string            // unix socket path
	Admin      map[string]string // service → admin base URL
	Project    string            // compose project name
	Log        *slog.Logger
}

// Record is one line of the journal.
type Record struct {
	At    time.Time `json:"at"`
	Phase string    `json:"phase"` // steady | method | rollback | verify
	What  string    `json:"what"`
	OK    bool      `json:"ok"`
	// Inconclusive marks a probe that measured nothing (failed query, missing
	// series, broken precondition): it can abort a run, never refute it.
	Inconclusive bool     `json:"inconclusive,omitempty"`
	Value        *float64 `json:"value,omitempty"`
	Detail       string   `json:"detail,omitempty"`
}

// outcome of one probe.
type outcome int

const (
	passed     outcome = iota
	violated           // a measured counterexample
	unmeasured         // nothing was measured: no conclusion either way
)

// errInconclusive marks a probe that could not measure.
var errInconclusive = errors.New("sem medição")

func inconclusive(format string, args ...any) error {
	return fmt.Errorf("%w: %s", errInconclusive, fmt.Sprintf(format, args...))
}

// Journal is the full account of a run, the evidence a "measured" claim points to.
type Journal struct {
	Experiment  Experiment              `json:"experiment"`
	Verdict     string                  `json:"verdict"` // held | falsified | aborted
	StartedAt   time.Time               `json:"started_at"`
	FinishedAt  time.Time               `json:"finished_at"`
	Records     []Record                `json:"records"`
	Load        map[string]load.Summary `json:"load,omitempty"`
	Environment map[string]string       `json:"environment"`
}

// Runner executes experiments.
type Runner struct {
	Env   Env
	http  *http.Client
	mongo *mongo.Client
}

// NewRunner connects the probes that need clients.
func NewRunner(ctx context.Context, env Env) (*Runner, error) {
	r := &Runner{Env: env, http: &http.Client{Timeout: 10 * time.Second}}
	if env.MongoURI != "" {
		c, err := mongo.Connect(options.Client().ApplyURI(env.MongoURI).SetTimeout(5 * time.Second))
		if err != nil {
			return nil, err
		}
		r.mongo = c
	}
	return r, nil
}

// Close releases clients.
func (r *Runner) Close(ctx context.Context) {
	if r.mongo != nil {
		_ = r.mongo.Disconnect(ctx)
	}
}

// Run executes one experiment and returns its journal.
//
// The verdict follows what was observed: aborted when the experiment itself
// broke (no steady state, an action or the load failed), falsified when a
// probe measured a counterexample, aborted again when some probe measured
// nothing and none refuted, held otherwise.
func (r *Runner) Run(ctx context.Context, e Experiment) Journal {
	j := Journal{Experiment: e, StartedAt: time.Now().UTC(), Verdict: "held", Environment: map[string]string{
		"go": runtime.Version(), "os": runtime.GOOS + "/" + runtime.GOARCH, "commit": evidenceCommit(),
		"ci": os.Getenv("CI"), "runner": os.Getenv("RUNNER_NAME"), "run_id": os.Getenv("GITHUB_RUN_ID"),
	}}
	log := r.Env.Log.With("experiment", e.ID)
	var broken, refuted, blind bool
	note := func(phase, what string, o outcome, v *float64, detail string) {
		j.Records = append(j.Records, Record{
			At: time.Now().UTC(), Phase: phase, What: what, OK: o == passed, Inconclusive: o == unmeasured, Value: v, Detail: detail,
		})
		log.Info(phase, "what", what, "ok", o == passed, "detail", detail)
	}
	judge := func(phase, what string, o outcome, v *float64, detail string) {
		note(phase, what, o, v, detail)
		refuted = refuted || o == violated
		blind = blind || o == unmeasured
	}
	finish := func() Journal {
		switch {
		case broken:
			j.Verdict = "aborted"
		case refuted:
			j.Verdict = "falsified"
		case blind:
			j.Verdict = "aborted"
		default:
			j.Verdict = "held"
		}
		j.FinishedAt = time.Now().UTC()
		return j
	}

	// 1. steady state before the fault: otherwise no conclusion can be drawn
	for _, p := range e.Steady {
		v, o, detail := r.probe(ctx, p, nil, nil)
		note("steady", p.Name, o, v, detail)
		broken = broken || o != passed
	}
	if broken {
		return finish()
	}

	// 2. baselines: counters are read as growth during this run, so a series
	// born with its first event, or left over by an earlier run, reads right
	base := map[string]float64{}
	for _, p := range e.deltaProbes() {
		v, _, err := r.promql(ctx, p.PromQL.Query)
		if err != nil {
			note("baseline", p.Name, unmeasured, nil, err.Error())
			continue
		}
		base[p.Name] = v
		note("baseline", p.Name, passed, &v, "linha de base")
	}

	// 3. background load
	var (
		stats   *load.Stats
		loadErr error
		wg      sync.WaitGroup
	)
	if e.Load != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			stats, loadErr = load.Run(ctx, load.Config{
				API: r.Env.API, Rate: e.Load.Rate, Sellers: e.Load.Sellers, ProposeP: e.Load.ProposeP, Window: e.Load.Window,
				Snipe: time.Duration(e.Load.Snipe), Duration: time.Duration(e.Load.Duration), Settlement: e.Load.Settlement,
				Seed: e.Load.Seed, Log: log,
			})
		}()
	}

	// 4. method, then 5. rollbacks that always run
	func() {
		defer func() {
			for _, s := range e.Rollback {
				err := r.act(context.WithoutCancel(ctx), s)
				note("rollback", describe(s), outcomeOf(err == nil), nil, errText(err))
			}
		}()
		for _, s := range e.Method {
			if s.Probe != nil {
				v, o, detail := r.probe(ctx, *s.Probe, nil, base)
				judge("method", "probe "+s.Probe.Name, o, v, detail)
				continue
			}
			err := r.act(ctx, s)
			note("method", describe(s), outcomeOf(err == nil), nil, errText(err))
			if err != nil {
				broken = true
				return
			}
		}
	}()

	// 6. let the system settle, wait for the load, verify
	wg.Wait()
	if loadErr != nil {
		note("verify", "carga", violated, nil, loadErr.Error())
		broken = true
	}
	if stats != nil {
		j.Load = stats.Summaries()
	}
	if e.Settle > 0 {
		time.Sleep(time.Duration(e.Settle))
	}
	for _, p := range e.Verify {
		v, o, detail := r.probe(ctx, p, j.Load, base)
		judge("verify", p.Name, o, v, detail)
	}
	return finish()
}

// evidenceCommit names the code a journal is evidence about: the binary's
// own stamp or, under `go run` in CI, the commit the workflow checked out.
func evidenceCommit() string {
	for _, c := range []string{buildinfo.Read().Commit, os.Getenv("INTENZA_COMMIT"), os.Getenv("GITHUB_SHA")} {
		if c != "" {
			return c
		}
	}
	return ""
}

func outcomeOf(ok bool) outcome {
	if ok {
		return passed
	}
	return violated
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func describe(s Step) string {
	switch {
	case s.Wait > 0:
		return "espera " + time.Duration(s.Wait).String()
	case s.Toxiproxy != nil:
		t := s.Toxiproxy
		switch {
		case t.Reset:
			return "toxiproxy reset"
		case t.Toxic != nil:
			return fmt.Sprintf("toxiproxy %s +%s %v", t.Proxy, t.Toxic.Type, t.Toxic.Attributes)
		case t.Enabled != nil:
			return fmt.Sprintf("toxiproxy %s enabled=%v", t.Proxy, *t.Enabled)
		}
	case s.Docker != nil:
		return "docker " + s.Docker.Action + " " + s.Docker.Service
	case s.Fault != nil:
		return fmt.Sprintf("falha %s %v", s.Fault.Service, s.Fault.Set)
	case s.Insert != nil:
		return "insere em " + s.Insert.Collection
	}
	return s.Note
}

// act performs an action.
func (r *Runner) act(ctx context.Context, s Step) error {
	switch {
	case s.Wait > 0:
		select {
		case <-time.After(time.Duration(s.Wait)):
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	case s.Toxiproxy != nil:
		return r.toxiproxy(ctx, *s.Toxiproxy)
	case s.Docker != nil:
		return r.docker(ctx, *s.Docker)
	case s.Fault != nil:
		base, ok := r.Env.Admin[s.Fault.Service]
		if !ok {
			return fmt.Errorf("serviço sem admin: %s", s.Fault.Service)
		}
		return r.post(ctx, base+"/debug/faults", s.Fault.Set)
	case s.Insert != nil:
		if r.mongo == nil {
			return errors.New("sem mongo")
		}
		var doc bson.D
		if err := bson.UnmarshalExtJSON([]byte(s.Insert.Document), false, &doc); err != nil {
			return err
		}
		_, err := r.mongo.Database("intenza").Collection(s.Insert.Collection).InsertOne(ctx, doc)
		return err
	}
	return nil
}

func (r *Runner) post(ctx context.Context, u string, body any) error {
	b, _ := json.Marshal(body)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	res, err := r.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		msg, _ := io.ReadAll(res.Body)
		return fmt.Errorf("%s: %d %s", u, res.StatusCode, msg)
	}
	return nil
}

func (r *Runner) toxiproxy(ctx context.Context, t ToxiproxyAction) error {
	switch {
	case t.Reset:
		return r.post(ctx, r.Env.Toxiproxy+"/reset", map[string]any{})
	case t.Toxic != nil:
		tx := *t.Toxic
		if tx.Stream == "" {
			tx.Stream = "downstream"
		}
		if tx.Toxicity == 0 {
			tx.Toxicity = 1
		}
		return r.post(ctx, r.Env.Toxiproxy+"/proxies/"+t.Proxy+"/toxics", tx)
	case t.Enabled != nil:
		return r.post(ctx, r.Env.Toxiproxy+"/proxies/"+t.Proxy, map[string]any{"enabled": *t.Enabled})
	}
	return errors.New("ação do toxiproxy vazia")
}

// docker talks to the Engine API over the unix socket: no SDK needed.
func (r *Runner) docker(ctx context.Context, d DockerAction) error {
	sock := strings.TrimPrefix(r.Env.DockerHost, "unix://")
	c := &http.Client{Timeout: 60 * time.Second, Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var dl net.Dialer
			return dl.DialContext(ctx, "unix", sock)
		},
	}}
	name := r.Env.Project + "-" + d.Service + "-1"
	u := "http://docker/containers/" + url.PathEscape(name) + "/" + d.Action
	if d.Action == "stop" || d.Action == "restart" {
		u += "?t=2"
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, u, nil)
	res, err := c.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 && res.StatusCode != http.StatusNotModified {
		msg, _ := io.ReadAll(res.Body)
		// already in the asked state: a rollback after the method undid the
		// fault itself is not a failure (start/stop answer 304 for the same)
		if (d.Action == "unpause" && bytes.Contains(msg, []byte("is not paused"))) ||
			(d.Action == "pause" && bytes.Contains(msg, []byte("is already paused"))) {
			return nil
		}
		return fmt.Errorf("docker %s %s: %d %s", d.Action, name, res.StatusCode, msg)
	}
	return nil
}

// probe evaluates a probe; loadStats is only available after the load and
// base holds the baselines of the delta probes.
func (r *Runner) probe(ctx context.Context, p Probe, loadStats map[string]load.Summary, base map[string]float64) (*float64, outcome, string) {
	if p.HTTP != nil {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, p.HTTP.URL, nil)
		res, err := r.http.Do(req)
		if err != nil {
			return nil, violated, err.Error() // no answer is the observation
		}
		_ = res.Body.Close()
		want := p.HTTP.Status
		if want == 0 {
			want = http.StatusOK
		}
		v := float64(res.StatusCode)
		return &v, outcomeOf(res.StatusCode == want), "status " + strconv.Itoa(res.StatusCode)
	}
	v, err := r.measure(ctx, p, loadStats, base)
	switch {
	case errors.Is(err, errInconclusive):
		return nil, unmeasured, err.Error()
	case err != nil:
		return nil, violated, err.Error()
	}
	return &v, outcomeOf((p.Min == nil || v >= *p.Min) && (p.Max == nil || v <= *p.Max)), bounds(p)
}

func (r *Runner) measure(ctx context.Context, p Probe, loadStats map[string]load.Summary, base map[string]float64) (float64, error) {
	switch {
	case p.PromQL != nil:
		v, present, err := r.promql(ctx, p.PromQL.Query)
		if err != nil {
			return 0, inconclusive("prometheus: %v", err)
		}
		if p.PromQL.Delta {
			b, ok := base[p.Name]
			switch {
			case !ok:
				return 0, inconclusive("sem linha de base")
			case v < b: // the process restarted and its counter with it
				return v, nil
			}
			return v - b, nil
		}
		if !present {
			return 0, inconclusive("série ausente: nada a comparar")
		}
		return v, nil
	case p.Mongo != nil:
		v, err := r.mongoCount(ctx, *p.Mongo)
		if err != nil {
			return 0, inconclusive("mongo: %v", err)
		}
		return v, nil
	case p.LoadStat != nil:
		s, ok := loadStats[p.LoadStat.Op]
		if !ok {
			if p.LoadStat.Stat == "count" {
				return 0, nil // nothing of that kind happened: a real zero
			}
			return 0, inconclusive("sem medições para %s", p.LoadStat.Op)
		}
		v, ok := map[string]float64{
			"p50": s.P50, "p95": s.P95, "p99": s.P99, "error_ratio": s.ErrorRatio,
			"count": float64(s.Count), "server_errors": float64(s.ServerErrors),
		}[p.LoadStat.Stat]
		if !ok {
			return 0, inconclusive("estatística desconhecida %q", p.LoadStat.Stat)
		}
		return v, nil
	case p.Scenario != "":
		return r.scenario(ctx, p.Scenario)
	}
	return 0, inconclusive("sonda vazia")
}

func bounds(p Probe) string {
	var parts []string
	if p.Min != nil {
		parts = append(parts, "≥ "+strconv.FormatFloat(*p.Min, 'f', -1, 64))
	}
	if p.Max != nil {
		parts = append(parts, "≤ "+strconv.FormatFloat(*p.Max, 'f', -1, 64))
	}
	return strings.Join(parts, " e ")
}

// promql runs an instant query. A missing series is not a zero: present
// tells them apart (NaN, as histogram_quantile gives over no data, counts as
// missing), and an error answer is an error, not an empty result.
func (r *Runner) promql(ctx context.Context, q string) (v float64, present bool, err error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, r.Env.Prometheus+"/api/v1/query?query="+url.QueryEscape(q), nil)
	res, err := r.http.Do(req)
	if err != nil {
		return 0, false, err
	}
	defer res.Body.Close()
	var out struct {
		Status string `json:"status"`
		Error  string `json:"error"`
		Data   struct {
			Result []struct {
				Value [2]any `json:"value"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return 0, false, err
	}
	if res.StatusCode != http.StatusOK || out.Status != "success" {
		return 0, false, fmt.Errorf("%d %s", res.StatusCode, out.Error)
	}
	if len(out.Data.Result) == 0 {
		return 0, false, nil
	}
	raw, _ := out.Data.Result[0].Value[1].(string)
	if v, err = strconv.ParseFloat(raw, 64); err != nil {
		return 0, false, err
	}
	if math.IsNaN(v) {
		return 0, false, nil
	}
	return v, true, nil
}

func (r *Runner) mongoCount(ctx context.Context, p MongoProbe) (float64, error) {
	if r.mongo == nil {
		return 0, errors.New("sem conexão com o mongo")
	}
	coll := r.mongo.Database("intenza").Collection(p.Collection)
	if p.Pipeline != "" {
		var pipeline []bson.D
		if err := bson.UnmarshalExtJSON([]byte(`{"p":`+p.Pipeline+`}`), false, &struct {
			P *[]bson.D `bson:"p"`
		}{&pipeline}); err != nil {
			return 0, err
		}
		cur, err := coll.Aggregate(ctx, pipeline)
		if err != nil {
			return 0, err
		}
		var rows []struct {
			N float64 `bson:"n"`
		}
		if err := cur.All(ctx, &rows); err != nil || len(rows) == 0 {
			return 0, err
		}
		return rows[0].N, nil
	}
	var filter bson.D
	if p.Filter != "" {
		if err := bson.UnmarshalExtJSON([]byte(p.Filter), false, &filter); err != nil {
			return 0, err
		}
	}
	n, err := coll.CountDocuments(ctx, filter)
	return float64(n), err
}

// Write stores the journal as JSON (evidence) and Markdown (for people).
func Write(dir string, j Journal) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(struct {
		Journal
		ID      string `json:"id"`
		Verdict string `json:"verdict"`
	}{j, j.Experiment.ID, j.Verdict}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, j.Experiment.ID+".json"), raw, 0o600); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, j.Experiment.ID+".md"), []byte(Markdown(j)), 0o600)
}

// Markdown renders the journal as a short report: the causal chain, the
// verdict and the measurements behind it.
func Markdown(j Journal) string {
	var b strings.Builder
	e := j.Experiment
	verdict := map[string]string{"held": "hipótese sustentada", "falsified": "hipótese refutada", "aborted": "abortado (sem estado estável, ação falhou ou sonda sem medição)"}[j.Verdict]
	fmt.Fprintf(&b, "# %s\n\n**Veredito: %s** · risco `%s` · %s → %s\n\n", e.Title, verdict, e.Risk,
		j.StartedAt.Format(time.RFC3339), j.FinishedAt.Format(time.RFC3339))
	fmt.Fprintf(&b, "> %s\n\n", e.Hypothesis)
	fmt.Fprintf(&b, "| causa | mecanismo | efeito esperado |\n|---|---|---|\n| %s | %s | %s |\n\n", e.Cause, e.Mechanism, e.Effect)
	b.WriteString("| fase | o quê | ok | valor | detalhe |\n|---|---|---|---:|---|\n")
	for _, r := range j.Records {
		val := ""
		if r.Value != nil {
			val = strconv.FormatFloat(*r.Value, 'g', 4, 64)
		}
		ok := "sim"
		switch {
		case r.Inconclusive:
			ok = "sem medição"
		case !r.OK:
			ok = "**não**"
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s |\n", r.Phase, r.What, ok, val, strings.ReplaceAll(r.Detail, "|", "/"))
	}
	if len(j.Load) > 0 {
		b.WriteString("\n### Carga, do lado de quem usa\n\n| operação | n | p50 | p95 | p99 | 5xx | 4xx | erro de rede |\n|---|---:|---:|---:|---:|---:|---:|---:|\n")
		for _, op := range []string{"publish", "propose", "rank", "award", "ship", "deliver", "accept", "released", "all"} {
			s, ok := j.Load[op]
			if !ok {
				continue
			}
			fmt.Fprintf(&b, "| %s | %d | %.3f | %.3f | %.3f | %d | %d | %d |\n", op, s.Count, s.P50, s.P95, s.P99, s.ServerErrors, s.ClientErrors, s.Transport)
		}
	}
	return b.String()
}

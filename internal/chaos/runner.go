package chaos

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
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
	At     time.Time `json:"at"`
	Phase  string    `json:"phase"` // steady | method | rollback | verify
	What   string    `json:"what"`
	OK     bool      `json:"ok"`
	Value  *float64  `json:"value,omitempty"`
	Detail string    `json:"detail,omitempty"`
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
func (r *Runner) Run(ctx context.Context, e Experiment) Journal {
	j := Journal{Experiment: e, StartedAt: time.Now().UTC(), Verdict: "held", Environment: map[string]string{
		"go": runtime.Version(), "os": runtime.GOOS + "/" + runtime.GOARCH, "commit": buildinfo.Read().Commit,
		"ci": os.Getenv("CI"), "runner": os.Getenv("RUNNER_NAME"),
	}}
	log := r.Env.Log.With("experiment", e.ID)
	note := func(phase, what string, ok bool, v *float64, detail string) {
		j.Records = append(j.Records, Record{At: time.Now().UTC(), Phase: phase, What: what, OK: ok, Value: v, Detail: detail})
		log.Info(phase, "what", what, "ok", ok, "detail", detail)
	}

	// 1. steady state before the fault: otherwise no conclusion can be drawn
	for _, p := range e.Steady {
		v, ok, detail := r.probe(ctx, p, nil)
		note("steady", p.Name, ok, v, detail)
		if !ok {
			j.Verdict = "aborted"
		}
	}
	if j.Verdict == "aborted" {
		j.FinishedAt = time.Now().UTC()
		return j
	}

	// 2. background load
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

	// 3. method, then 4. rollbacks that always run
	func() {
		defer func() {
			for _, s := range e.Rollback {
				err := r.act(context.WithoutCancel(ctx), s)
				note("rollback", describe(s), err == nil, nil, errText(err))
			}
		}()
		for _, s := range e.Method {
			if s.Probe != nil {
				v, ok, detail := r.probe(ctx, *s.Probe, nil)
				note("method", "probe "+s.Probe.Name, ok, v, detail)
				if !ok {
					j.Verdict = "falsified"
				}
				continue
			}
			err := r.act(ctx, s)
			note("method", describe(s), err == nil, nil, errText(err))
			if err != nil {
				j.Verdict = "aborted"
				return
			}
		}
	}()

	// 5. let the system settle, wait for the load, verify
	wg.Wait()
	if loadErr != nil {
		note("verify", "carga", false, nil, loadErr.Error())
		j.Verdict = "aborted"
	}
	if stats != nil {
		j.Load = stats.Summaries()
	}
	if e.Settle > 0 {
		time.Sleep(time.Duration(e.Settle))
	}
	for _, p := range e.Verify {
		v, ok, detail := r.probe(ctx, p, j.Load)
		note("verify", p.Name, ok, v, detail)
		if !ok && j.Verdict == "held" {
			j.Verdict = "falsified"
		}
	}
	j.FinishedAt = time.Now().UTC()
	return j
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
		return fmt.Errorf("docker %s %s: %d %s", d.Action, name, res.StatusCode, msg)
	}
	return nil
}

// probe evaluates a probe; loadStats is only available after the load.
func (r *Runner) probe(ctx context.Context, p Probe, loadStats map[string]load.Summary) (*float64, bool, string) {
	var (
		v   float64
		err error
	)
	switch {
	case p.HTTP != nil:
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, p.HTTP.URL, nil)
		res, herr := r.http.Do(req)
		if herr != nil {
			return nil, false, herr.Error()
		}
		_ = res.Body.Close()
		want := p.HTTP.Status
		if want == 0 {
			want = http.StatusOK
		}
		v = float64(res.StatusCode)
		return &v, res.StatusCode == want, "status " + strconv.Itoa(res.StatusCode)
	case p.PromQL != nil:
		v, err = r.promql(ctx, p.PromQL.Query)
	case p.Mongo != nil:
		v, err = r.mongoCount(ctx, *p.Mongo)
	case p.LoadStat != nil:
		s, ok := loadStats[p.LoadStat.Op]
		if !ok {
			return nil, false, "sem medições para " + p.LoadStat.Op
		}
		v = map[string]float64{
			"p50": s.P50, "p95": s.P95, "p99": s.P99, "error_ratio": s.ErrorRatio,
			"count": float64(s.Count), "server_errors": float64(s.ServerErrors),
		}[p.LoadStat.Stat]
	case p.Scenario != "":
		v, err = r.scenario(ctx, p.Scenario)
	default:
		return nil, false, "sonda vazia"
	}
	if err != nil {
		return nil, false, err.Error()
	}
	ok := (p.Min == nil || v >= *p.Min) && (p.Max == nil || v <= *p.Max)
	return &v, ok, bounds(p)
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

func (r *Runner) promql(ctx context.Context, q string) (float64, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, r.Env.Prometheus+"/api/v1/query?query="+url.QueryEscape(q), nil)
	res, err := r.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	var out struct {
		Data struct {
			Result []struct {
				Value [2]any `json:"value"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return 0, err
	}
	if len(out.Data.Result) == 0 {
		return 0, nil // no series: treated as zero (no events of that kind)
	}
	s, _ := out.Data.Result[0].Value[1].(string)
	return strconv.ParseFloat(s, 64)
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
	verdict := map[string]string{"held": "hipótese sustentada", "falsified": "hipótese refutada", "aborted": "abortado (sistema fora do estado estável ou ação falhou)"}[j.Verdict]
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
		if !r.OK {
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

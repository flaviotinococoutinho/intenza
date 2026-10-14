//intenza:paradigm imperative-shell

// Package chaos runs experiments in the sense of the principles of chaos
// engineering: a hypothesis about the steady state, a fault introduced on
// purpose, measurements before, during and after, and rollbacks that always
// run. An experiment cannot prove resilience; it can only fail to falsify the
// hypothesis, which is what its verdict records ("held" or "falsified").
// When the system is not steady before the fault, the run is "aborted": no
// conclusion is drawn from it.
package chaos

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"go.yaml.in/yaml/v3"
)

// Duration reads Go durations ("30s") from YAML.
type Duration time.Duration

func (d *Duration) UnmarshalYAML(n *yaml.Node) error {
	v, err := time.ParseDuration(n.Value)
	*d = Duration(v)
	return err
}

// MarshalJSON prints the Go form.
func (d Duration) MarshalJSON() ([]byte, error) { return json.Marshal(time.Duration(d).String()) }

// Experiment is one YAML file under chaos/experiments.
type Experiment struct {
	ID         string   `yaml:"id" json:"id"`
	Title      string   `yaml:"title" json:"title"`
	Risk       string   `yaml:"risk" json:"risk"`
	Claims     []string `yaml:"claims" json:"claims"`
	Hypothesis string   `yaml:"hypothesis" json:"hypothesis"`
	Cause      string   `yaml:"cause" json:"cause"`
	Mechanism  string   `yaml:"mechanism" json:"mechanism"`
	Effect     string   `yaml:"effect" json:"expected_effect"`
	Load       *Load    `yaml:"load,omitempty" json:"load,omitempty"`
	Steady     []Probe  `yaml:"steady_state" json:"steady_state"`
	Method     []Step   `yaml:"method" json:"method"`
	Rollback   []Step   `yaml:"rollback" json:"rollback"`
	Verify     []Probe  `yaml:"verify" json:"verify"`
	Settle     Duration `yaml:"settle,omitempty" json:"settle,omitempty"`
}

// Load is the background traffic of an experiment.
type Load struct {
	Rate       float64  `yaml:"rate" json:"rate"`             // intents per second (Poisson)
	Sellers    int      `yaml:"sellers" json:"sellers"`       // registered sellers
	ProposeP   float64  `yaml:"propose_p" json:"propose_p"`   // chance a seller answers an intent
	Window     string   `yaml:"window" json:"window"`         // bidding window (ISO 8601)
	Snipe      Duration `yaml:"snipe" json:"snipe"`           // mean lead before the deadline
	Duration   Duration `yaml:"duration" json:"duration"`     // how long to generate
	Settlement bool     `yaml:"settlement" json:"settlement"` // drive agreements to release
	Seed       uint64   `yaml:"seed" json:"seed"`
}

// Step is either an action or a probe evaluated in the middle of the method.
type Step struct {
	Wait      Duration         `yaml:"wait,omitempty" json:"wait,omitempty"`
	Toxiproxy *ToxiproxyAction `yaml:"toxiproxy,omitempty" json:"toxiproxy,omitempty"`
	Docker    *DockerAction    `yaml:"docker,omitempty" json:"docker,omitempty"`
	Fault     *FaultAction     `yaml:"fault,omitempty" json:"fault,omitempty"`
	Probe     *Probe           `yaml:"probe,omitempty" json:"probe,omitempty"`
	Insert    *MongoInsert     `yaml:"insert,omitempty" json:"insert,omitempty"`
	Note      string           `yaml:"note,omitempty" json:"note,omitempty"`
}

// ToxiproxyAction changes a proxy: enable/disable, add a toxic, or reset all.
type ToxiproxyAction struct {
	Proxy   string `yaml:"proxy,omitempty" json:"proxy,omitempty"`
	Enabled *bool  `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Toxic   *Toxic `yaml:"toxic,omitempty" json:"toxic,omitempty"`
	Reset   bool   `yaml:"reset,omitempty" json:"reset,omitempty"`
}

// Toxic is a Toxiproxy toxic.
type Toxic struct {
	Name       string         `yaml:"name" json:"name"`
	Type       string         `yaml:"type" json:"type"`
	Stream     string         `yaml:"stream" json:"stream"`
	Toxicity   float64        `yaml:"toxicity" json:"toxicity"`
	Attributes map[string]int `yaml:"attributes" json:"attributes"`
}

// MongoInsert writes a document directly (for example a poison event in the outbox).
type MongoInsert struct {
	Collection string `yaml:"collection" json:"collection"`
	Document   string `yaml:"document" json:"document"` // extended JSON
}

// DockerAction pauses, stops or restarts a compose service.
type DockerAction struct {
	Service string `yaml:"service" json:"service"`
	Action  string `yaml:"action" json:"action"` // pause | unpause | stop | start | restart | kill
}

// FaultAction turns knobs on a service's admin /debug/faults.
type FaultAction struct {
	Service string            `yaml:"service" json:"service"`
	Set     map[string]string `yaml:"set" json:"set"`
}

// Probe measures something and compares it with bounds.
type Probe struct {
	Name     string         `yaml:"name" json:"name"`
	HTTP     *HTTPProbe     `yaml:"http,omitempty" json:"http,omitempty"`
	PromQL   *PromQLProbe   `yaml:"promql,omitempty" json:"promql,omitempty"`
	Mongo    *MongoProbe    `yaml:"mongo,omitempty" json:"mongo,omitempty"`
	LoadStat *LoadStatProbe `yaml:"load,omitempty" json:"load,omitempty"`
	Scenario string         `yaml:"scenario,omitempty" json:"scenario,omitempty"` // built-in multi-step probe
	Min      *float64       `yaml:"min,omitempty" json:"min,omitempty"`
	Max      *float64       `yaml:"max,omitempty" json:"max,omitempty"`
}

// HTTPProbe expects a status from a GET.
type HTTPProbe struct {
	URL    string `yaml:"url" json:"url"`
	Status int    `yaml:"status" json:"status"`
}

// PromQLProbe evaluates an instant query; the first sample is the value.
type PromQLProbe struct {
	Query string `yaml:"query" json:"query"`
}

// MongoProbe counts documents (or runs an aggregation returning {n}).
type MongoProbe struct {
	Collection string `yaml:"collection" json:"collection"`
	Filter     string `yaml:"filter" json:"filter"`               // extended JSON
	Pipeline   string `yaml:"pipeline,omitempty" json:"pipeline"` // extended JSON array ending in {$count: "n"}
}

// LoadStatProbe reads a client-side statistic of the load generator.
type LoadStatProbe struct {
	Op   string `yaml:"op" json:"op"`     // publish | propose | rank | award | ship | deliver | accept | all
	Stat string `yaml:"stat" json:"stat"` // p50 | p95 | p99 | error_ratio | count | server_errors
}

// LoadExperiment reads and checks one file.
func LoadExperiment(path string) (Experiment, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Experiment{}, err
	}
	var e Experiment
	if err := yaml.Unmarshal(raw, &e); err != nil {
		return Experiment{}, fmt.Errorf("%s: %w", path, err)
	}
	if e.ID == "" || e.Hypothesis == "" || e.Risk == "" || len(e.Method) == 0 {
		return Experiment{}, fmt.Errorf("%s: id, hypothesis, risk e method são obrigatórios", path)
	}
	return e, nil
}

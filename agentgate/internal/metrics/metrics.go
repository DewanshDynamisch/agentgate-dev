// Package metrics provides a lightweight, dependency-free Prometheus text
// exposition format 0.0.4 metrics registry and collectors.
package metrics

import (
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// DefaultBuckets are the fixed histogram latency buckets per the AgentGate specification:
// 0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5
var DefaultBuckets = []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5}

// Collector is an interface for any metric that can expose Prometheus text lines.
type Collector interface {
	Name() string
	Help() string
	Type() string
	Write(w io.Writer) error
}

type label struct {
	name  string
	value string
}

func escapeLabelValue(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

func escapeHelp(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

func formatLabels(labels []label) string {
	if len(labels) == 0 {
		return ""
	}
	sorted := make([]label, len(labels))
	copy(sorted, labels)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].name < sorted[j].name
	})
	var b strings.Builder
	b.WriteByte('{')
	for i, l := range sorted {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(l.name)
		b.WriteString(`="`)
		b.WriteString(escapeLabelValue(l.value))
		b.WriteByte('"')
	}
	b.WriteByte('}')
	return b.String()
}

func formatFloat(v float64) string {
	if math.IsNaN(v) {
		return "NaN"
	}
	if math.IsInf(v, 1) {
		return "+Inf"
	}
	if math.IsInf(v, -1) {
		return "-Inf"
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}

func makeLabels(names, values []string) []label {
	labels := make([]label, 0, len(names))
	for i, name := range names {
		val := ""
		if i < len(values) {
			val = values[i]
		}
		labels = append(labels, label{name: name, value: val})
	}
	return labels
}

func labelsKey(labels []label) string {
	return formatLabels(labels)
}

// -------------------------------------------------------------------------
// Counter (unlabelled)
// -------------------------------------------------------------------------

// Counter is a simple unlabelled monotonic counter.
type Counter struct {
	name string
	help string
	mu   sync.Mutex
	val  float64
}

// NewCounter creates an unlabelled counter.
func NewCounter(name, help string) *Counter {
	return &Counter{name: name, help: help}
}

func (c *Counter) Name() string { return c.name }
func (c *Counter) Help() string { return c.help }
func (c *Counter) Type() string { return "counter" }

func (c *Counter) Inc() {
	c.Add(1)
}

func (c *Counter) Add(delta float64) {
	if delta < 0 {
		return
	}
	c.mu.Lock()
	c.val += delta
	c.mu.Unlock()
}

func (c *Counter) Value() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.val
}

func (c *Counter) Write(w io.Writer) error {
	c.mu.Lock()
	v := c.val
	c.mu.Unlock()
	_, err := fmt.Fprintf(w, "%s %s\n", c.name, formatFloat(v))
	return err
}

// -------------------------------------------------------------------------
// CounterVec (labelled)
// -------------------------------------------------------------------------

type counterSeries struct {
	labels []label
	mu     sync.Mutex
	val    float64
}

// CounterVec manages partitioned counters by label dimensions.
type CounterVec struct {
	name       string
	help       string
	labelNames []string
	mu         sync.RWMutex
	series     map[string]*counterSeries
}

// NewCounterVec creates a labelled counter vector.
func NewCounterVec(name, help string, labelNames []string) *CounterVec {
	return &CounterVec{
		name:       name,
		help:       help,
		labelNames: labelNames,
		series:     make(map[string]*counterSeries),
	}
}

func (cv *CounterVec) Name() string { return cv.name }
func (cv *CounterVec) Help() string { return cv.help }
func (cv *CounterVec) Type() string { return "counter" }

func (cv *CounterVec) getSeries(labelValues []string) *counterSeries {
	lbls := makeLabels(cv.labelNames, labelValues)
	key := labelsKey(lbls)

	cv.mu.RLock()
	s, ok := cv.series[key]
	cv.mu.RUnlock()
	if ok {
		return s
	}

	cv.mu.Lock()
	defer cv.mu.Unlock()
	s, ok = cv.series[key]
	if !ok {
		s = &counterSeries{labels: lbls}
		cv.series[key] = s
	}
	return s
}

func (cv *CounterVec) Inc(labelValues ...string) {
	cv.Add(1, labelValues...)
}

func (cv *CounterVec) Add(delta float64, labelValues ...string) {
	if delta < 0 {
		return
	}
	s := cv.getSeries(labelValues)
	s.mu.Lock()
	s.val += delta
	s.mu.Unlock()
}

func (cv *CounterVec) Write(w io.Writer) error {
	cv.mu.RLock()
	keys := make([]string, 0, len(cv.series))
	for k := range cv.series {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	seriesList := make([]*counterSeries, len(keys))
	for i, k := range keys {
		seriesList[i] = cv.series[k]
	}
	cv.mu.RUnlock()

	for _, s := range seriesList {
		s.mu.Lock()
		val := s.val
		s.mu.Unlock()
		lblStr := formatLabels(s.labels)
		if _, err := fmt.Fprintf(w, "%s%s %s\n", cv.name, lblStr, formatFloat(val)); err != nil {
			return err
		}
	}
	return nil
}

// -------------------------------------------------------------------------
// HistogramVec
// -------------------------------------------------------------------------

type histogramSeries struct {
	labels  []label
	buckets []float64
	mu      sync.Mutex
	counts  []uint64
	count   uint64
	sum     float64
}

// HistogramVec samples observations across fixed buckets and label sets.
type HistogramVec struct {
	name       string
	help       string
	labelNames []string
	buckets    []float64
	mu         sync.RWMutex
	series     map[string]*histogramSeries
}

// NewHistogramVec creates a labelled histogram vector with specified or default buckets.
func NewHistogramVec(name, help string, labelNames []string, buckets []float64) *HistogramVec {
	if len(buckets) == 0 {
		buckets = DefaultBuckets
	}
	sortedBuckets := make([]float64, len(buckets))
	copy(sortedBuckets, buckets)
	sort.Float64s(sortedBuckets)

	return &HistogramVec{
		name:       name,
		help:       help,
		labelNames: labelNames,
		buckets:    sortedBuckets,
		series:     make(map[string]*histogramSeries),
	}
}

func (hv *HistogramVec) Name() string { return hv.name }
func (hv *HistogramVec) Help() string { return hv.help }
func (hv *HistogramVec) Type() string { return "histogram" }

func (hv *HistogramVec) getSeries(labelValues []string) *histogramSeries {
	lbls := makeLabels(hv.labelNames, labelValues)
	key := labelsKey(lbls)

	hv.mu.RLock()
	s, ok := hv.series[key]
	hv.mu.RUnlock()
	if ok {
		return s
	}

	hv.mu.Lock()
	defer hv.mu.Unlock()
	s, ok = hv.series[key]
	if !ok {
		s = &histogramSeries{
			labels:  lbls,
			buckets: hv.buckets,
			counts:  make([]uint64, len(hv.buckets)),
		}
		hv.series[key] = s
	}
	return s
}

func (hv *HistogramVec) Observe(seconds float64, labelValues ...string) {
	s := hv.getSeries(labelValues)
	s.mu.Lock()
	defer s.mu.Unlock()

	s.count++
	s.sum += seconds
	for i, b := range s.buckets {
		if seconds <= b {
			s.counts[i]++
		}
	}
}

func (hv *HistogramVec) Write(w io.Writer) error {
	hv.mu.RLock()
	keys := make([]string, 0, len(hv.series))
	for k := range hv.series {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	seriesList := make([]*histogramSeries, len(keys))
	for i, k := range keys {
		seriesList[i] = hv.series[k]
	}
	hv.mu.RUnlock()

	for _, s := range seriesList {
		s.mu.Lock()
		snapCount := s.count
		snapSum := s.sum
		snapCounts := make([]uint64, len(s.counts))
		copy(snapCounts, s.counts)
		s.mu.Unlock()

		// 1. Emit each bucket
		for i, b := range s.buckets {
			bucketLabels := make([]label, len(s.labels)+1)
			copy(bucketLabels, s.labels)
			bucketLabels[len(s.labels)] = label{name: "le", value: formatFloat(b)}

			lblStr := formatLabels(bucketLabels)
			if _, err := fmt.Fprintf(w, "%s_bucket%s %d\n", hv.name, lblStr, snapCounts[i]); err != nil {
				return err
			}
		}

		// 2. Emit +Inf bucket
		infLabels := make([]label, len(s.labels)+1)
		copy(infLabels, s.labels)
		infLabels[len(s.labels)] = label{name: "le", value: "+Inf"}
		if _, err := fmt.Fprintf(w, "%s_bucket%s %d\n", hv.name, formatLabels(infLabels), snapCount); err != nil {
			return err
		}

		// 3. Emit _sum
		lblStr := formatLabels(s.labels)
		if _, err := fmt.Fprintf(w, "%s_sum%s %s\n", hv.name, lblStr, formatFloat(snapSum)); err != nil {
			return err
		}

		// 4. Emit _count
		if _, err := fmt.Fprintf(w, "%s_count%s %d\n", hv.name, lblStr, snapCount); err != nil {
			return err
		}
	}
	return nil
}

// -------------------------------------------------------------------------
// Gauge (optionally labelled)
// -------------------------------------------------------------------------

type gaugeSeries struct {
	labels []label
	mu     sync.Mutex
	val    float64
}

// Gauge represents a single numerical value that can arbitrarily go up and down.
type Gauge struct {
	name       string
	help       string
	labelNames []string
	mu         sync.RWMutex
	series     map[string]*gaugeSeries
}

// NewGauge creates a Gauge collector.
func NewGauge(name, help string, labelNames []string) *Gauge {
	return &Gauge{
		name:       name,
		help:       help,
		labelNames: labelNames,
		series:     make(map[string]*gaugeSeries),
	}
}

func (g *Gauge) Name() string { return g.name }
func (g *Gauge) Help() string { return g.help }
func (g *Gauge) Type() string { return "gauge" }

func (g *Gauge) getSeries(labelValues []string) *gaugeSeries {
	lbls := makeLabels(g.labelNames, labelValues)
	key := labelsKey(lbls)

	g.mu.RLock()
	s, ok := g.series[key]
	g.mu.RUnlock()
	if ok {
		return s
	}

	g.mu.Lock()
	defer g.mu.Unlock()
	s, ok = g.series[key]
	if !ok {
		s = &gaugeSeries{labels: lbls}
		g.series[key] = s
	}
	return s
}

func (g *Gauge) Set(val float64, labelValues ...string) {
	s := g.getSeries(labelValues)
	s.mu.Lock()
	s.val = val
	s.mu.Unlock()
}

func (g *Gauge) Value(labelValues ...string) float64 {
	s := g.getSeries(labelValues)
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.val
}

func (g *Gauge) Write(w io.Writer) error {
	g.mu.RLock()
	keys := make([]string, 0, len(g.series))
	for k := range g.series {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	seriesList := make([]*gaugeSeries, len(keys))
	for i, k := range keys {
		seriesList[i] = g.series[k]
	}
	g.mu.RUnlock()

	// If unlabelled and no series set yet, write 0
	if len(g.labelNames) == 0 && len(seriesList) == 0 {
		_, err := fmt.Fprintf(w, "%s 0\n", g.name)
		return err
	}

	for _, s := range seriesList {
		s.mu.Lock()
		val := s.val
		s.mu.Unlock()
		lblStr := formatLabels(s.labels)
		if _, err := fmt.Fprintf(w, "%s%s %s\n", g.name, lblStr, formatFloat(val)); err != nil {
			return err
		}
	}
	return nil
}

// -------------------------------------------------------------------------
// Registry
// -------------------------------------------------------------------------

// Registry holds a set of collectors and formats them in Prometheus 0.0.4 text format.
type Registry struct {
	mu         sync.RWMutex
	collectors []Collector
	byName     map[string]Collector
}

// NewRegistry constructs an empty metrics registry.
func NewRegistry() *Registry {
	return &Registry{
		byName: make(map[string]Collector),
	}
}

// Register registers a new collector. It returns an error if a collector with the same name exists.
func (r *Registry) Register(c Collector) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.byName[c.Name()]; exists {
		return fmt.Errorf("metric with name %q already registered", c.Name())
	}
	r.byName[c.Name()] = c
	r.collectors = append(r.collectors, c)
	return nil
}

// MustRegister registers a collector and panics on error.
func (r *Registry) MustRegister(c Collector) {
	if err := r.Register(c); err != nil {
		panic(err)
	}
}

// NewCounter creates and registers an unlabelled counter.
func (r *Registry) NewCounter(name, help string) *Counter {
	c := NewCounter(name, help)
	r.MustRegister(c)
	return c
}

// NewCounterVec creates and registers a labelled counter vector.
func (r *Registry) NewCounterVec(name, help string, labelNames []string) *CounterVec {
	cv := NewCounterVec(name, help, labelNames)
	r.MustRegister(cv)
	return cv
}

// NewHistogramVec creates and registers a histogram vector.
func (r *Registry) NewHistogramVec(name, help string, labelNames []string, buckets []float64) *HistogramVec {
	hv := NewHistogramVec(name, help, labelNames, buckets)
	r.MustRegister(hv)
	return hv
}

// NewGauge creates and registers a gauge.
func (r *Registry) NewGauge(name, help string, labelNames []string) *Gauge {
	g := NewGauge(name, help, labelNames)
	r.MustRegister(g)
	return g
}

// WriteText serializes all registered collectors in deterministic Prometheus 0.0.4 text format.
func (r *Registry) WriteText(w io.Writer) error {
	r.mu.RLock()
	collectors := make([]Collector, len(r.collectors))
	copy(collectors, r.collectors)
	r.mu.RUnlock()

	sort.Slice(collectors, func(i, j int) bool {
		return collectors[i].Name() < collectors[j].Name()
	})

	for _, c := range collectors {
		if _, err := fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s %s\n", c.Name(), escapeHelp(c.Help()), c.Name(), c.Type()); err != nil {
			return err
		}
		if err := c.Write(w); err != nil {
			return err
		}
	}
	return nil
}

// Handler returns an http.Handler serving GET /metrics in Prometheus 0.0.4 text format.
func (r *Registry) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodGet && req.Method != http.MethodHead {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		if req.Method == http.MethodHead {
			return
		}
		_ = r.WriteText(w)
	})
}

// -------------------------------------------------------------------------
// Metrics Bundle
// -------------------------------------------------------------------------

// Metrics groups the exact AgentGate production metrics.
type Metrics struct {
	Registry                   *Registry
	DecisionsTotal             *CounterVec
	DecisionDurationSeconds    *HistogramVec
	AuditAppendFailuresTotal   *Counter
	CredentialIssuanceFailures *Counter
	AdminRequestsTotal         *CounterVec
	BuildInfo                  *Gauge
}

// NewMetrics initializes and registers all 6 AgentGate production metrics into a new Registry.
func NewMetrics(version string) *Metrics {
	reg := NewRegistry()
	m := &Metrics{
		Registry: reg,
		DecisionsTotal: reg.NewCounterVec(
			"agentgate_decisions_total",
			"Total number of authorization decisions evaluated.",
			[]string{"decision", "reason"},
		),
		DecisionDurationSeconds: reg.NewHistogramVec(
			"agentgate_decision_duration_seconds",
			"Duration of authorization decisions in seconds.",
			[]string{"decision"},
			DefaultBuckets,
		),
		AuditAppendFailuresTotal: reg.NewCounter(
			"agentgate_audit_append_failures_total",
			"Total number of audit append failures.",
		),
		CredentialIssuanceFailures: reg.NewCounter(
			"agentgate_credential_issuance_failures_total",
			"Total number of downstream credential issuance failures.",
		),
		AdminRequestsTotal: reg.NewCounterVec(
			"agentgate_admin_requests_total",
			"Total number of admin API HTTP requests.",
			[]string{"route", "status"},
		),
		BuildInfo: reg.NewGauge(
			"agentgate_build_info",
			"AgentGate build and version information.",
			[]string{"version"},
		),
	}
	if version == "" {
		version = "dev"
	}
	m.BuildInfo.Set(1, version)
	return m
}

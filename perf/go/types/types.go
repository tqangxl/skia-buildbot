package types

import (
	"encoding/gob"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"go.skia.org/infra/go/util"
	"go.skia.org/infra/perf/go/config"
)

// FillType is how filling in of missing values should be done in Trace.Grow().
type FillType int

const (
	FILL_BEFORE FillType = iota
	FILL_AFTER
)

// Trace represents a single series of measurements. The actual values it
// stores per Commit is defined by implementations of Trace.
type Trace interface {
	// Returns the parameters that describe this trace.
	Params() map[string]string

	// Merge this trace with the given trace. The given trace is expected to come
	// after this trace.
	Merge(Trace) Trace

	DeepCopy() Trace

	// Grow the measurements, filling in with sentinel values either before or
	// after based on FillType.
	Grow(int, FillType)

	// The number of samples in the series.
	Len() int

	// IsMissing returns true if the measurement at index i is a sentinel value,
	// for example, config.MISSING_DATA_SENTINEL.
	IsMissing(i int) bool

	// Trim trims the measurements to just the range from [begin, end).
	//
	// Just like a Go [:] slice this is inclusive of begin and exclusive of end.
	// The length on the Trace will then become end-begin.
	Trim(begin, end int) error
}

// Matches returns true if the given Trace matches the given query.
func Matches(tr Trace, query url.Values) bool {
	for k, values := range query {
		if _, ok := tr.Params()[k]; !ok || !util.In(tr.Params()[k], values) {
			return false
		}
	}
	return true
}

// MatchesWithIgnores returns true if the given Trace matches the given query
// and none of the ignore queries.
func MatchesWithIgnores(tr Trace, query url.Values, ignores ...url.Values) bool {
	if !Matches(tr, query) {
		return false
	}
	for _, i := range ignores {
		if Matches(tr, i) {
			return false
		}
	}
	return true
}

// PerfTrace represents all the values of a single floating point measurement.
// *PerfTrace implements Trace.
type PerfTrace struct {
	Values  []float64         `json:"values"`
	Params_ map[string]string `json:"params"`
}

func (t *PerfTrace) Params() map[string]string {
	return t.Params_
}

func (t *PerfTrace) Len() int {
	return len(t.Values)
}

func (t *PerfTrace) IsMissing(i int) bool {
	return t.Values[i] == config.MISSING_DATA_SENTINEL
}

func (t *PerfTrace) DeepCopy() Trace {
	n := len(t.Values)
	cp := &PerfTrace{
		Values:  make([]float64, n, n),
		Params_: make(map[string]string),
	}
	copy(cp.Values, t.Values)
	for k, v := range t.Params_ {
		cp.Params_[k] = v
	}
	return cp
}

func (t *PerfTrace) Merge(next Trace) Trace {
	nextPerf := next.(*PerfTrace)
	n := len(t.Values) + len(nextPerf.Values)
	n1 := len(t.Values)

	merged := NewPerfTraceN(n)
	merged.Params_ = t.Params_
	for k, v := range nextPerf.Params_ {
		merged.Params_[k] = v
	}
	for i, v := range t.Values {
		merged.Values[i] = v
	}
	for i, v := range nextPerf.Values {
		merged.Values[n1+i] = v
	}
	return merged
}

func (t *PerfTrace) Grow(n int, fill FillType) {
	if n < len(t.Values) {
		panic(fmt.Sprintf("Grow must take a value (%d) larger than the current Trace size: %d", n, len(t.Values)))
	}
	delta := n - len(t.Values)
	newValues := make([]float64, n)

	if fill == FILL_AFTER {
		copy(newValues, t.Values)
		for i := 0; i < delta; i++ {
			newValues[i+len(t.Values)] = config.MISSING_DATA_SENTINEL
		}
	} else {
		for i := 0; i < delta; i++ {
			newValues[i] = config.MISSING_DATA_SENTINEL
		}
		copy(newValues[delta:], t.Values)
	}
	t.Values = newValues
}

func (g *PerfTrace) Trim(begin, end int) error {
	if end < begin || end > g.Len() || begin < 0 {
		return fmt.Errorf("Invalid Trim range [%d, %d) of [0, %d]", begin, end, g.Len())
	}
	n := end - begin
	newValues := make([]float64, n)

	for i := 0; i < n; i++ {
		newValues[i] = g.Values[i+begin]
	}
	g.Values = newValues
	return nil
}

// NewPerfTrace allocates a new Trace set up for the given number of samples.
//
// The Trace Values are pre-filled in with the missing data sentinel since not
// all tests will be run on all commits.
func NewPerfTrace() *PerfTrace {
	return NewPerfTraceN(config.TILE_SIZE)
}

// NewPerfTraceN allocates a new Trace set up for the given number of samples.
//
// The Trace Values are pre-filled in with the missing data sentinel since not
// all tests will be run on all commits.
func NewPerfTraceN(n int) *PerfTrace {
	t := &PerfTrace{
		Values:  make([]float64, n, n),
		Params_: make(map[string]string),
	}
	for i, _ := range t.Values {
		t.Values[i] = config.MISSING_DATA_SENTINEL
	}
	return t
}

func init() {
	// Register *PerfTrace and *GoldenTrace in gob so that it can be used as a
	// concrete type for Trace when writing and reading Tiles in gobs.
	gob.Register(&PerfTrace{})
	gob.Register(&GoldenTrace{})
}

type TryBotResults struct {
	// Map from Trace key to value.
	Values map[string]float64
}

func NewTryBotResults() *TryBotResults {
	return &TryBotResults{
		Values: map[string]float64{},
	}
}

func AsCalculatedID(id string) string {
	if strings.HasPrefix(id, "!") {
		return id
	}
	return "!" + id
}

func IsCalculatedID(id string) bool {
	return strings.HasPrefix(id, "!")
}

func AsFormulaID(id string) string {
	if strings.HasPrefix(id, "@") {
		return id
	}
	return "@" + id
}

func IsFormulaID(id string) bool {
	return strings.HasPrefix(id, "@")
}

func FormulaFromID(id string) string {
	return id[1:]
}

// Commit is information about each Git commit.
type Commit struct {
	CommitTime int64  `json:"commit_time" bq:"timestamp" db:"ts"`
	Hash       string `json:"hash"        bq:"gitHash"   db:"githash"`
	Author     string `json:"author"                     db:"author"`
}

// Tile is a config.TILE_SIZE commit slice of data.
//
// The length of the Commits array is the same length as all of the Values
// arrays in all of the Traces.
type Tile struct {
	Traces   map[string]Trace    `json:"traces"`
	ParamSet map[string][]string `json:"param_set"`
	Commits  []*Commit           `json:"commits"`

	// What is the scale of this Tile, i.e. it contains every Nth point, where
	// N=const.TILE_SCALE^Scale.
	Scale     int `json:"scale"`
	TileIndex int `json:"tileIndex"`
}

// NewTile returns an new Tile object.
func NewTile() *Tile {
	t := &Tile{
		Traces:   map[string]Trace{},
		ParamSet: map[string][]string{},
		Commits:  make([]*Commit, config.TILE_SIZE, config.TILE_SIZE),
	}
	for i := range t.Commits {
		t.Commits[i] = &Commit{}
	}
	return t
}

// LastCommitIndex returns the index of the last valid Commit.
func (t Tile) LastCommitIndex() int {
	for i := len(t.Commits) - 1; i > 0; i-- {
		if t.Commits[i].CommitTime != 0 {
			return i
		}
	}
	return 0
}

// Returns the hashes of the first and last commits in the Tile.
func (t Tile) CommitRange() (string, string) {
	return t.Commits[0].Hash, t.Commits[t.LastCommitIndex()].Hash
}

// Makes a copy of the tile where the Traces and Commits are deep copies and
// all the rest of the data is a shallow copy.
func (t Tile) Copy() *Tile {
	ret := &Tile{
		Traces:    map[string]Trace{},
		ParamSet:  t.ParamSet,
		Scale:     t.Scale,
		TileIndex: t.TileIndex,
		Commits:   make([]*Commit, len(t.Commits)),
	}
	for i, c := range t.Commits {
		cp := *c
		ret.Commits[i] = &cp
	}
	for k, v := range t.Traces {
		ret.Traces[k] = v.DeepCopy()
	}
	return ret
}

// Trim trims the measurements to just the range from [begin, end).
//
// Just like a Go [:] slice this is inclusive of begin and exclusive of end.
// The length on the Traces will then become end-begin.
func (t Tile) Trim(begin, end int) (*Tile, error) {
	length := end - begin
	if end < begin || end > len(t.Commits) || begin < 0 {
		return nil, fmt.Errorf("Invalid Trim range [%d, %d) of [0, %d]", begin, end, length)
	}
	ret := &Tile{
		Traces:    map[string]Trace{},
		ParamSet:  t.ParamSet,
		Scale:     t.Scale,
		TileIndex: t.TileIndex,
		Commits:   make([]*Commit, length),
	}

	for i := 0; i < length; i++ {
		cp := *t.Commits[i+begin]
		ret.Commits[i] = &cp
	}
	for k, v := range t.Traces {
		t := v.DeepCopy()
		if err := t.Trim(begin, end); err != nil {
			return nil, fmt.Errorf("Failed to Trim trace: %s", err)
		}
		ret.Traces[k] = t
	}
	return ret, nil
}

// Same as Tile but instead of Traces we preserve the raw JSON. This is a
// utitlity struct that is used to parse a tile where we don't know the
// Trace type upfront.
type TileWithRawTraces struct {
	Traces    map[string]json.RawMessage `json:"traces"`
	ParamSet  map[string][]string        `json:"param_set"`
	Commits   []*Commit                  `json:"commits"`
	Scale     int                        `json:"scale"`
	TileIndex int                        `json:"tileIndex"`
}

// TileFromJson parses a tile that has been serialized to JSON.
// traceExample has to be an instance of the Trace implementation
// that needs to be deserialized.
// Note: Instead of the type switch below we could use reflection
// to be truely generic, but it makes the code harder to read and
// currently we only have two types.
func TileFromJson(r io.Reader, traceExample Trace) (*Tile, error) {
	// Figure out the type of trace.
	var factory func() Trace
	switch traceExample.(type) {
	case *GoldenTrace:
		factory = func() Trace { return NewGoldenTrace() }
	case *PerfTrace:
		factory = func() Trace { return NewPerfTrace() }
	}

	// Decode everything, but the traces.
	dec := json.NewDecoder(r)
	var rawTile TileWithRawTraces
	err := dec.Decode(&rawTile)
	if err != nil {
		return nil, err
	}

	// Parse the traces.
	traces := map[string]Trace{}
	for k, rawJson := range rawTile.Traces {
		newTrace := factory()
		if err = json.Unmarshal(rawJson, newTrace); err != nil {
			return nil, err
		}
		traces[k] = newTrace.(Trace)
	}

	return &Tile{
		Traces:    traces,
		ParamSet:  rawTile.ParamSet,
		Commits:   rawTile.Commits,
		Scale:     rawTile.Scale,
		TileIndex: rawTile.Scale,
	}, nil
}

// TraceGUI is used in TileGUI.
type TraceGUI struct {
	Data   [][2]float64      `json:"data"`
	Label  string            `json:"label"`
	Params map[string]string `json:"_params"`
}

// TileGUI is the JSON the server serves for tile requests.
type TileGUI struct {
	ParamSet map[string][]string `json:"paramset,omitempty"`
	Commits  []*Commit           `json:"commits,omitempty"`
	Scale    int                 `json:"scale"`
	Tiles    []int               `json:"tiles"`
	Ticks    []interface{}       `json:"ticks"` // The x-axis tick marks.
	Skps     []int               `json:"skps"`  // The x values where SKPs were regenerated.
}

func NewTileGUI(scale int, tileIndex int) *TileGUI {
	return &TileGUI{
		ParamSet: make(map[string][]string, 0),
		Commits:  make([]*Commit, 0),
		Scale:    scale,
		Tiles:    []int{tileIndex},
	}
}

// TileStore is an interface representing the ability to save and restore Tiles.
type TileStore interface {
	Put(scale, index int, tile *Tile) error

	// Get returns the Tile for a given scale and index. Pass in -1 for index to
	// get the last tile for a given scale. Each tile contains its tile index and
	// scale. Get returns (nil, nil) if there is no data in the store yet for that
	// scale and index. The implementation of TileStore can assume that the
	// caller will not modify the tile it returns.
	Get(scale, index int) (*Tile, error)

	// GetModifiable behaves identically to Get, except it always returns a
	// copy that can be modified.
	GetModifiable(scale, index int) (*Tile, error)
}

// ValueWeight is a weight proportional to the number of times the parameter
// Value appears in a cluster. Used in ClusterSummary.
type ValueWeight struct {
	Value  string
	Weight int
}

// StepFit stores information on the best Step Function fit on a trace.
//
// Used in ClusterSummary.
type StepFit struct {
	// LeastSquares is the Least Squares error for a step function curve fit to the trace.
	LeastSquares float64

	// TurningPoint is the index where the Step Function changes value.
	TurningPoint int

	// StepSize is the size of the step in the step function. Negative values
	// indicate a step up, i.e. they look like a performance regression in the
	// trace, as opposed to positive values which look like performance
	// improvements.
	StepSize float64

	// The "Regression" value is calculated as Step Size / Least Squares Error.
	//
	// The better the fit the larger the number returned, because LSE
	// gets smaller with a better fit. The higher the Step Size the
	// larger the number returned.
	Regression float64

	// Status of the cluster.
	//
	// Values can be "High", "Low", and "Uninteresting"
	Status string
}

// ClusterSummary is a summary of a single cluster of traces.
type ClusterSummary struct {
	// Traces contains at most config.MAX_SAMPLE_TRACES_PER_CLUSTER sample
	// traces, the first is the centroid.
	Traces [][][]float64

	// Keys of all the members of the Cluster.
	Keys []string

	// ParamSummaries is a summary of all the parameters in the cluster.
	ParamSummaries [][]ValueWeight

	// StepFit is info on the fit of the centroid to a step function.
	StepFit *StepFit

	// Hash is the Git hash at the step point.
	Hash string

	// Timestamp is when this hash was committed.
	Timestamp int64

	// Status is the status, "New", "Ingore" or "Bug".
	Status string

	// A note about the Status.
	Message string

	// ID is the identifier for this summary in the datastore.
	ID int64

	// Bugs is a list of IDs of bugs in the codesite issue tracker.
	Bugs []int64
}

// ValidStatusValues are the valid values of ClusterSummary.Status when the
// ClusterSummary is used as an alert.
var ValidStatusValues = []string{"New", "Ignore", "Bug"}

func NewClusterSummary(numKeys, numTraces int) *ClusterSummary {
	return &ClusterSummary{
		Keys:           make([]string, numKeys),
		Traces:         make([][][]float64, numTraces),
		ParamSummaries: [][]ValueWeight{},
		StepFit:        &StepFit{},
		Hash:           "",
		Timestamp:      0,
		Status:         "",
		Message:        "",
		ID:             -1,
	}
}

// Merge adds in new info from the passed in ClusterSummary.
func (c *ClusterSummary) Merge(from *ClusterSummary) {
	for _, k := range from.Keys {
		if !util.In(k, c.Keys) {
			c.Keys = append(c.Keys, k)
		}
	}
}

// Finds the paramSet for the given slice of traces.
func GetParamSet(traces map[string]Trace, paramSet map[string][]string) {
	for _, trace := range traces {
		for k, v := range trace.Params() {
			if _, ok := paramSet[k]; !ok {
				paramSet[k] = []string{v}
			} else if !util.In(v, paramSet[k]) {
				paramSet[k] = append(paramSet[k], v)
			}
		}
	}
}

// Merge the two Tiles, presuming tile1 comes before tile2.
func Merge(tile1, tile2 *Tile) *Tile {
	n := len(tile1.Commits) + len(tile2.Commits)
	n1 := len(tile1.Commits)
	t := &Tile{
		Traces:   make(map[string]Trace),
		ParamSet: make(map[string][]string),
		Commits:  make([]*Commit, n, n),
	}
	for i := range t.Commits {
		t.Commits[i] = &Commit{}
	}

	// Merge the Commits.
	for i, c := range tile1.Commits {
		t.Commits[i] = c
	}
	for i, c := range tile2.Commits {
		t.Commits[n1+i] = c
	}

	// Merge the Traces.
	seen := map[string]bool{}
	for key, trace := range tile1.Traces {
		seen[key] = true
		if trace2, ok := tile2.Traces[key]; ok {
			t.Traces[key] = trace.Merge(trace2)
		} else {
			cp := trace.DeepCopy()
			cp.Grow(n, FILL_AFTER)
			t.Traces[key] = cp
		}
	}
	// Now add in the traces that are only in tile2.
	for key, trace := range tile2.Traces {
		if _, ok := seen[key]; ok {
			continue
		}
		cp := trace.DeepCopy()
		cp.Grow(n, FILL_BEFORE)
		t.Traces[key] = cp
	}

	// Recreate the ParamSet.
	GetParamSet(t.Traces, t.ParamSet)

	t.Scale = tile1.Scale
	t.TileIndex = tile1.TileIndex

	return t
}

const (
	// No digest available.
	MISSING_DIGEST = ""
)

// GoldenTrace represents all the Digests of a single test across a series
// of Commits. GoldenTrace implements the Trace interface.
type GoldenTrace struct {
	Params_ map[string]string
	Values  []string
}

func (g *GoldenTrace) Params() map[string]string {
	return g.Params_
}

func (g *GoldenTrace) Len() int {
	return len(g.Values)
}

func (g *GoldenTrace) IsMissing(i int) bool {
	return g.Values[i] == MISSING_DIGEST
}

func (g *GoldenTrace) DeepCopy() Trace {
	n := len(g.Values)
	cp := &GoldenTrace{
		Values:  make([]string, n, n),
		Params_: make(map[string]string),
	}
	copy(cp.Values, g.Values)
	for k, v := range g.Params_ {
		cp.Params_[k] = v
	}
	return cp
}

func (g *GoldenTrace) Merge(next Trace) Trace {
	nextGold := next.(*GoldenTrace)
	n := len(g.Values) + len(nextGold.Values)
	n1 := len(g.Values)

	merged := NewGoldenTraceN(n)
	merged.Params_ = g.Params_
	for k, v := range nextGold.Params_ {
		merged.Params_[k] = v
	}
	for i, v := range g.Values {
		merged.Values[i] = v
	}
	for i, v := range nextGold.Values {
		merged.Values[n1+i] = v
	}
	return merged
}

func (g *GoldenTrace) Grow(n int, fill FillType) {
	if n < len(g.Values) {
		panic(fmt.Sprintf("Grow must take a value (%d) larger than the current Trace size: %d", n, len(g.Values)))
	}
	delta := n - len(g.Values)
	newValues := make([]string, n)

	if fill == FILL_AFTER {
		copy(newValues, g.Values)
		for i := 0; i < delta; i++ {
			newValues[i+len(g.Values)] = MISSING_DIGEST
		}
	} else {
		for i := 0; i < delta; i++ {
			newValues[i] = MISSING_DIGEST
		}
		copy(newValues[delta:], g.Values)
	}
	g.Values = newValues
}

func (g *GoldenTrace) Trim(begin, end int) error {
	if end < begin || end > g.Len() || begin < 0 {
		return fmt.Errorf("Invalid Trim range [%d, %d) of [0, %d]", begin, end, g.Len())
	}
	n := end - begin
	newValues := make([]string, n)

	for i := 0; i < n; i++ {
		newValues[i] = g.Values[i+begin]
	}
	g.Values = newValues
	return nil
}

// NewGoldenTrace allocates a new Trace set up for the given number of samples.
//
// The Trace Values are pre-filled in with the missing data sentinel since not
// all tests will be run on all commits.
func NewGoldenTrace() *GoldenTrace {
	return NewGoldenTraceN(config.TILE_SIZE)
}

// NewGoldenTraceN allocates a new Trace set up for the given number of samples.
//
// The Trace Values are pre-filled in with the missing data sentinel since not
// all tests will be run on all commits.
func NewGoldenTraceN(n int) *GoldenTrace {
	g := &GoldenTrace{
		Values:  make([]string, n, n),
		Params_: make(map[string]string),
	}
	for i, _ := range g.Values {
		g.Values[i] = MISSING_DIGEST
	}
	return g
}

// Activity stores information on one user action activity. This corresponds to
// one record in the activity database table. See DESIGN.md for details.
type Activity struct {
	ID     int
	TS     int64
	UserID string
	Action string
	URL    string
}

// Date returns an RFC3339 string for the Activity's TS.
func (a *Activity) Date() string {
	return time.Unix(a.TS, 0).Format(time.RFC3339)
}

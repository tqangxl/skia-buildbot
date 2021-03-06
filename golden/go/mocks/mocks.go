package mocks

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	assert "github.com/stretchr/testify/require"
	"go.skia.org/infra/golden/go/diff"
	pconfig "go.skia.org/infra/perf/go/config"
	"go.skia.org/infra/perf/go/filetilestore"
	ptypes "go.skia.org/infra/perf/go/types"
)

// Mock the url generator function.
func MockUrlGenerator(path string) string {
	return path
}

// Mock the diffstore.
type MockDiffStore struct{}

func (m MockDiffStore) Get(dMain string, dRest []string) (map[string]*diff.DiffMetrics, error) {
	result := map[string]*diff.DiffMetrics{}
	for _, d := range dRest {
		result[d] = &diff.DiffMetrics{
			NumDiffPixels:     10,
			PixelDiffPercent:  1.0,
			PixelDiffFilePath: fmt.Sprintf("diffpath/%s-%s", dMain, d),
			MaxRGBADiffs:      []int{5, 3, 4, 0},
			DimDiffer:         false,
		}
	}
	return result, nil
}

func (m MockDiffStore) AbsPath(digest []string) map[string]string {
	result := map[string]string{}
	for _, d := range digest {
		result[d] = "abspath/" + d
	}
	return result
}

func (m MockDiffStore) UnavailableDigests() map[string]bool {
	return nil
}

func (m MockDiffStore) CalculateDiffs([]string) {}

func NewMockDiffStore() diff.DiffStore {
	return MockDiffStore{}
}

// Mock the tilestore for GoldenTraces
func NewMockTileStore(t *testing.T, digests [][]string, params []map[string]string, commits []*ptypes.Commit) ptypes.TileStore {
	// Build the tile from the digests, params and commits.
	traces := map[string]ptypes.Trace{}

	for idx, traceDigests := range digests {
		traces[TraceKey(params[idx])] = &ptypes.GoldenTrace{
			Params_: params[idx],
			Values:  traceDigests,
		}
	}

	tile := ptypes.NewTile()
	tile.Traces = traces
	tile.Commits = commits

	return &MockTileStore{
		t:    t,
		tile: tile,
	}
}

// TraceKey returns the trace key used in MockTileStore generated from the
// params map.
func TraceKey(params map[string]string) string {
	traceParts := make([]string, 0, len(params))
	for _, v := range params {
		traceParts = append(traceParts, v)
	}
	sort.Strings(traceParts)
	return strings.Join(traceParts, ":")
}

// NewMockTileStoreFromJson reads a tile that has been serialized to JSON
// and wraps an instance of MockTileStore around it.
func NewMockTileStoreFromJson(t *testing.T, fname string) ptypes.TileStore {
	f, err := os.Open(fname)
	assert.Nil(t, err)

	tile, err := ptypes.TileFromJson(f, &ptypes.GoldenTrace{})
	assert.Nil(t, err)

	return &MockTileStore{
		t:    t,
		tile: tile,
	}
}

type MockTileStore struct {
	t    *testing.T
	tile *ptypes.Tile
}

func (m *MockTileStore) Get(scale, index int) (*ptypes.Tile, error) {
	return m.tile, nil
}

func (m *MockTileStore) Put(scale, index int, tile *ptypes.Tile) error {
	assert.FailNow(m.t, "Should not be called.")
	return nil
}

func (m *MockTileStore) GetModifiable(scale, index int) (*ptypes.Tile, error) {
	return m.Get(scale, index)
}

// GetTileStoreFromEnv looks at the TEST_TILE_DIR environement variable for the
// name of directory that contains tiles. If it's defined it will return a
// TileStore instance. If the not the calling test will fail.
func GetTileStoreFromEnv(t assert.TestingT) ptypes.TileStore {
	// Get the TEST_TILE environment variable that points to the
	// tile to read.
	tileDir := os.Getenv("TEST_TILE_DIR")
	assert.NotEqual(t, "", tileDir, "Please define the TEST_TILE_DIR environment variable to point to a live tile store.")
	return filetilestore.NewFileTileStore(tileDir, pconfig.DATASET_GOLD, 2*time.Minute)
}

// tally returns counts of digests for various views on a Tile.
package tally

import (
	"fmt"
	"net/url"
	"sync"
	"time"

	"github.com/skia-dev/glog"
	"go.skia.org/infra/go/timer"
	"go.skia.org/infra/golden/go/storage"
	gtypes "go.skia.org/infra/golden/go/types"
	"go.skia.org/infra/perf/go/types"
)

type OnChangeCallback func()

// Tally maps a digest to a count.
type Tally map[string]int

// TraceTally maps each trace id to its Tally.
type TraceTally map[string]*Tally

// TestTally maps each test name to its Tally.
type TestTally map[string]*Tally

// Tallies allows querying for digest counts in different ways.
type Tallies struct {
	mutex      sync.Mutex
	storages   *storage.Storage
	traceTally TraceTally
	testTally  TestTally
	callbacks  []OnChangeCallback
}

// New creates a new Tallies for the given storage object.
func New(storages *storage.Storage) (*Tallies, error) {
	tile, err := storages.GetLastTileTrimmed()
	if err != nil {
		return nil, fmt.Errorf("Couldn't retrieve tile: %s", err)
	}

	trace, test := tallyTile(tile)
	t := &Tallies{
		traceTally: trace,
		testTally:  test,
		storages:   storages,
		callbacks:  []OnChangeCallback{},
	}
	go func() {
		for _ = range time.Tick(2 * time.Minute) {
			tile, err := storages.GetLastTileTrimmed()
			if err != nil {
				glog.Errorf("Couldn't retrieve tile: %s", err)
				continue
			}

			trace, test := tallyTile(tile)
			t.mutex.Lock()
			t.traceTally = trace
			t.testTally = test
			t.mutex.Unlock()
			for _, cb := range t.callbacks {
				go cb()
			}
		}
	}()
	return t, nil
}

func (t *Tallies) OnChange(f OnChangeCallback) {
	t.callbacks = append(t.callbacks, f)
}

func (t *Tallies) ByTest() TestTally {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	return t.testTally
}

func (t *Tallies) ByTrace() TraceTally {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	return t.traceTally
}

// ByQuery returns a Tally of all the digests that match the given query.
func (t *Tallies) ByQuery(query url.Values, ignores ...url.Values) (Tally, error) {
	tile, err := t.storages.GetLastTileTrimmed()
	if err != nil {
		return nil, fmt.Errorf("Couldn't retrieve tile: %s", err)
	}
	return tallyBy(tile, t.traceTally, query, ignores...), nil

}

// tallyBy does the actual work of ByQuery.
func tallyBy(tile *types.Tile, traceTally TraceTally, query url.Values, ignores ...url.Values) Tally {
	ret := Tally{}
	for k, tr := range tile.Traces {
		if types.MatchesWithIgnores(tr, query, ignores...) {
			if _, ok := traceTally[k]; !ok {
				continue
			}
			for digest, n := range *traceTally[k] {
				if _, ok := ret[digest]; ok {
					ret[digest] += n
				} else {
					ret[digest] = n
				}
			}
		}
	}
	return ret
}

// tallyTile computes a TraceTally and TestTally from the given Tile.
func tallyTile(tile *types.Tile) (TraceTally, TestTally) {
	defer timer.New("tally").Stop()
	traceTally := TraceTally{}
	testTally := TestTally{}
	for k, tr := range tile.Traces {
		gtr := tr.(*types.GoldenTrace)
		tally := Tally{}
		for _, s := range gtr.Values {
			if s == types.MISSING_DIGEST {
				continue
			}
			if n, ok := tally[s]; ok {
				tally[s] = n + 1
			} else {
				tally[s] = 1
			}
		}
		traceTally[k] = &tally
		testName := tr.Params()[gtypes.PRIMARY_KEY_FIELD]
		if t, ok := testTally[testName]; ok {
			for digest, n := range tally {
				if _, ok := (*t)[digest]; ok {
					(*t)[digest] += n
				} else {
					(*t)[digest] = n
				}
			}
		} else {
			cp := Tally{}
			for k, v := range tally {
				cp[k] = v
			}
			testTally[testName] = &cp
		}
	}
	return traceTally, testTally
}

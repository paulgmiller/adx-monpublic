package counter

import (
	"sync"
	"sync/atomic"
)

// MultiEstimator is a cardinality estimator that can track cardinality of multiple keys over time without big drops in
// counts when counting windows roll over.
type MultiEstimator struct {
	i uint64

	mu         sync.RWMutex
	estimators [2]map[string]*Estimator
}

func NewMultiEstimator() *MultiEstimator {
	est := &MultiEstimator{}
	est.Reset()
	return est
}

// Keys returns the current keys being counted.
func (e *MultiEstimator) Keys() []string {
	idx := atomic.LoadUint64(&e.i)
	est := e.estimators[idx%2]
	e.mu.RLock()
	keys := make([]string, 0, len(est))
	for k := range est {
		keys = append(keys, k)
	}
	e.mu.RUnlock()
	return keys
}

// Count returns the current count of items.
func (e *MultiEstimator) Count(key string) uint64 {
	idx := atomic.LoadUint64(&e.i)
	e.mu.RLock()
	est := e.estimators[idx%2][key]
	e.mu.RUnlock()
	if est == nil {
		return 0
	}
	return est.Count()
}

// Add adds an item to the estimator.
func (e *MultiEstimator) Add(key string, i uint64) {
	idx := atomic.LoadUint64(&e.i)

	e.mu.Lock()
	est := e.estimators[idx%2][key]
	if est == nil {
		est = NewEstimator()
		e.estimators[idx%2][key] = est
	}
	e.mu.Unlock()
	est.Add(i)

	e.mu.Lock()
	est = e.estimators[(idx+1)%2][key]
	if est == nil {
		est = NewEstimator()
		e.estimators[(idx+1)%2][key] = est
	}
	e.mu.Unlock()
	est.Add(i)
}

// Reset resets the estimator.
func (e *MultiEstimator) Reset() {
	e.estimators[0] = map[string]*Estimator{}
	e.estimators[1] = map[string]*Estimator{}
}

// Roll switches the active counter.  This should be called on some cadence to periodically expire items no longer
// being counted.
func (e *MultiEstimator) Roll() {
	idx := atomic.AddUint64(&e.i, 1)
	e.estimators[(idx-1)%2] = map[string]*Estimator{}
}

// Package stats holds the run's counters and latency histograms. Everything is lock-free: tens of
// thousands of cabbers record into the same values.
package stats

import (
	"math"
	"sync/atomic"
	"time"
)

const (
	buckets   = 64
	minBucket = time.Millisecond
	maxBucket = 30 * time.Second
)

// growth is the ratio of two neighbouring bucket bounds, so that 62 steps span 1 ms … 30 s.
var growth = math.Pow(float64(maxBucket)/float64(minBucket), 1.0/(buckets-2))

// Histogram counts durations in 64 logarithmic buckets: the last one holds everything above 30 s.
// A percentile is the upper bound of its bucket, so it is exact to within one bucket (≈ 18%).
type Histogram struct {
	counts [buckets]atomic.Int64
	total  atomic.Int64
	max    atomic.Int64
}

func bucketOf(d time.Duration) int {
	if d <= minBucket {
		return 0
	}
	i := int(math.Ceil(math.Log(float64(d)/float64(minBucket)) / math.Log(growth)))
	if i >= buckets {
		return buckets - 1
	}
	return i
}

// bound is the upper bound of bucket i; the overflow bucket has none.
func bound(i int) time.Duration {
	return time.Duration(float64(minBucket) * math.Pow(growth, float64(i)))
}

// Record adds one sample.
func (h *Histogram) Record(d time.Duration) {
	if d < 0 {
		d = 0
	}
	h.counts[bucketOf(d)].Add(1)
	h.total.Add(1)
	for {
		current := h.max.Load()
		if int64(d) <= current || h.max.CompareAndSwap(current, int64(d)) {
			return
		}
	}
}

// Summary is a consistent view of a histogram at one moment.
type Summary struct {
	Count         int64
	P50, P95, P99 time.Duration
	Max           time.Duration
}

// Summary reads the histogram. Samples recorded while it runs may or may not be included, but
// the percentiles always come from one copy of the counts.
func (h *Histogram) Summary() Summary {
	var counts [buckets]int64
	var total int64
	for i := range counts {
		counts[i] = h.counts[i].Load()
		total += counts[i]
	}
	max := time.Duration(h.max.Load())
	return Summary{
		Count: total,
		P50:   percentile(&counts, total, 0.50, max),
		P95:   percentile(&counts, total, 0.95, max),
		P99:   percentile(&counts, total, 0.99, max),
		Max:   max,
	}
}

func percentile(counts *[buckets]int64, total int64, q float64, max time.Duration) time.Duration {
	if total == 0 {
		return 0
	}
	rank := int64(math.Ceil(q * float64(total)))
	var seen int64
	for i, n := range counts {
		seen += n
		if seen >= rank {
			if i == buckets-1 {
				return max
			}
			if upper := bound(i); upper < max {
				return upper
			}
			return max
		}
	}
	return max
}

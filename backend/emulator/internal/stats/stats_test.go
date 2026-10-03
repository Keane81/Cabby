package stats

import (
	"sync"
	"testing"
	"time"

	"github.com/Keane81/Cabby/backend/emulator/internal/gateway"
)

// within reports whether got is no more than one bucket (the growth ratio) away from want.
func within(got, want time.Duration) bool {
	lo := time.Duration(float64(want) / growth)
	hi := time.Duration(float64(want) * growth)
	return got >= lo && got <= hi
}

func TestPercentilesOnKnownSamples(t *testing.T) {
	var h Histogram
	for i := 1; i <= 100; i++ {
		h.Record(time.Duration(i) * 10 * time.Millisecond) // 10 ms … 1 s
	}
	s := h.Summary()
	if s.Count != 100 {
		t.Fatalf("count = %d", s.Count)
	}
	if !within(s.P50, 500*time.Millisecond) || !within(s.P95, 950*time.Millisecond) || !within(s.P99, 990*time.Millisecond) {
		t.Errorf("p50/p95/p99 = %s/%s/%s", s.P50, s.P95, s.P99)
	}
	if s.Max != time.Second {
		t.Errorf("max = %s, want exact 1s", s.Max)
	}
}

func TestPercentileNeverExceedsMax(t *testing.T) {
	var h Histogram
	h.Record(7 * time.Millisecond)
	s := h.Summary()
	if s.P99 > s.Max {
		t.Errorf("p99 %s above max %s", s.P99, s.Max)
	}
}

func TestOverflowBucketReportsMax(t *testing.T) {
	var h Histogram
	h.Record(2 * time.Minute)
	s := h.Summary()
	if s.P50 != 2*time.Minute || s.Max != 2*time.Minute {
		t.Errorf("overflow p50/max = %s/%s, want 2m", s.P50, s.Max)
	}
}

func TestSubMillisecondAndNegativeSamples(t *testing.T) {
	var h Histogram
	h.Record(0)
	h.Record(-time.Second)
	h.Record(300 * time.Microsecond)
	if s := h.Summary(); s.Count != 3 || s.P99 > time.Millisecond {
		t.Errorf("summary = %+v", s)
	}
}

func TestEmptyHistogram(t *testing.T) {
	var h Histogram
	if s := h.Summary(); s != (Summary{}) {
		t.Errorf("summary = %+v, want zero", s)
	}
}

func TestConcurrentRecording(t *testing.T) {
	var h Histogram
	var wg sync.WaitGroup
	const workers, each = 16, 5000
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < each; i++ {
				h.Record(time.Duration(1+(i+w)%200) * time.Millisecond)
			}
		}(w)
	}
	var reader sync.WaitGroup
	reader.Add(1)
	stop := make(chan struct{})
	go func() {
		defer reader.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_ = h.Summary()
			}
		}
	}()
	wg.Wait()
	close(stop)
	reader.Wait()
	if s := h.Summary(); s.Count != workers*each {
		t.Errorf("count = %d, want %d", s.Count, workers*each)
	}
}

func TestSnapshotListsOnlyNonZeroFailures(t *testing.T) {
	var c Counters
	c.SentOK.Add(5)
	c.SendFailed(gateway.Unavailable)
	c.SendFailed(gateway.Unavailable)
	c.SendFailed(gateway.Timeout)
	c.SendFailed(gateway.Canceled) // the run stopping is not a failure
	c.SendFailed(gateway.OK)
	s := c.Snapshot()
	if s.SentOK != 5 || s.SendFailedTotal != 3 {
		t.Fatalf("snapshot = %+v", s)
	}
	if len(s.SendFailed) != 2 || s.SendFailed["unavailable"] != 2 || s.SendFailed["timeout"] != 1 {
		t.Errorf("failed = %v", s.SendFailed)
	}
}

func TestSnapshotIsConsistentUnderLoad(t *testing.T) {
	var c Counters
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 2000; j++ {
				c.SendFailed(gateway.Network)
				c.SentOK.Add(1)
			}
		}()
	}
	for i := 0; i < 50; i++ {
		s := c.Snapshot()
		var sum int64
		for _, n := range s.SendFailed {
			sum += n
		}
		if sum != s.SendFailedTotal {
			t.Fatalf("total %d differs from the sum %d", s.SendFailedTotal, sum)
		}
	}
	wg.Wait()
	if s := c.Snapshot(); s.SendFailed["network"] != 16000 || s.SentOK != 16000 {
		t.Errorf("final = %+v", s)
	}
}

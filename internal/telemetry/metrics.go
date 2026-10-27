// Package telemetry keeps bounded, process-local diagnostics with no user labels.
package telemetry

import (
	"sync/atomic"
	"time"
)

type Metric uint8

const (
	VoiceAnswer Metric = iota
	StreamAnswer
	WSQueue
	WSWrite
	ACLRefresh
	VoiceRTP
	StreamRTP
	metricCount
)

var names = [...]string{"voice_answer", "stream_answer", "ws_queue", "ws_write", "acl_refresh", "voice_rtp", "stream_rtp"}
var enabled atomic.Bool
var values [metricCount]recorder
var bounds = [...]time.Duration{time.Microsecond, 10 * time.Microsecond, 100 * time.Microsecond, time.Millisecond, 10 * time.Millisecond, 100 * time.Millisecond, time.Second, 10 * time.Second}

type recorder struct {
	count, errors, samples, nanos, max atomic.Uint64
	buckets                            [len(bounds) + 1]atomic.Uint64
}
type Value struct {
	Count, Errors, TimingSamples, TotalNanoseconds, MaxNanoseconds uint64
	Buckets                                                        []uint64
}

func Enable()       { enabled.Store(true) }
func Enabled() bool { return enabled.Load() }

func Observe(metric Metric, duration time.Duration, failed bool) {
	if !Enabled() {
		return
	}
	r := &values[metric]
	r.count.Add(1)
	if failed {
		r.errors.Add(1)
	}
	recordDuration(r, duration)
}

func recordDuration(r *recorder, duration time.Duration) {
	r.samples.Add(1)
	ns := uint64(max(duration, 0))
	r.nanos.Add(ns)
	for old := r.max.Load(); ns > old; old = r.max.Load() {
		if r.max.CompareAndSwap(old, ns) {
			break
		}
	}
	index := len(bounds)
	for i, bound := range bounds {
		if duration <= bound {
			index = i
			break
		}
	}
	r.buckets[index].Add(1)
}

// Forward samples timings for one in 128 packets, but counts every write/error.
// The writer and payload are never retained by the recorder.
func Forward(metric Metric, write func() error) error {
	if !Enabled() {
		return write()
	}
	r := &values[metric]
	n := r.count.Add(1)
	if n%128 != 0 {
		err := write()
		if err != nil {
			r.errors.Add(1)
		}
		return err
	}
	start := time.Now()
	err := write()
	elapsed := time.Since(start)
	if err != nil {
		r.errors.Add(1)
	}
	recordDuration(r, elapsed)
	return err
}

func Snapshot() map[string]Value {
	result := make(map[string]Value, len(names))
	for i, name := range names {
		r := &values[i]
		v := Value{Count: r.count.Load(), Errors: r.errors.Load(), TimingSamples: r.samples.Load(), TotalNanoseconds: r.nanos.Load(), MaxNanoseconds: r.max.Load(), Buckets: make([]uint64, len(bounds)+1)}
		for j := range v.Buckets {
			v.Buckets[j] = r.buckets[j].Load()
		}
		result[name] = v
	}
	return result
}

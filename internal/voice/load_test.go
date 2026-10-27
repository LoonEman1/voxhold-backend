package voice

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pion/rtp"
	"voxhold-backend/internal/medialoadstats"
	"voxhold-backend/internal/telemetry"
)

type loadSignalSink struct {
	*testSignalSink
	queues  map[string]chan string
	stopped <-chan struct{}
	errors  chan error
	offers  atomic.Uint64
}

func (s *loadSignalSink) SendOffer(id, sdp string) bool {
	queue := s.queues[id]
	select {
	case <-s.stopped:
		return false
	case queue <- sdp:
		s.offers.Add(1)
		return true
	default:
		return false
	}
}

// Opt-in, real Pion/UDP forwarding smoke. Payloads are synthetic and do not
// validate an Opus decoder. Process memory includes the load peers themselves.
func TestVoiceRoomLoad(t *testing.T) {
	if os.Getenv("VOXHOLD_MEDIA_LOAD") != "1" {
		t.Skip("set VOXHOLD_MEDIA_LOAD=1")
	}
	if os.Getenv("VOXHOLD_LOAD_METRICS") == "1" {
		telemetry.Enable()
	}
	for _, n := range []int{2, 5, 10} {
		for _, kbps := range []int{64, 128} {
			for _, burst := range []bool{false, true} {
				t.Run(fmt.Sprintf("peers=%d/kbps=%d/burst=%v", n, kbps, burst), func(t *testing.T) {
					stop := make(chan struct{})
					sink := &loadSignalSink{testSignalSink: newTestSignalSink(), queues: make(map[string]chan string), stopped: stop, errors: make(chan error, 128)}
					m := newTestManager(t, sink)
					sink.manager = m
					var baseline runtime.MemStats
					runtime.ReadMemStats(&baseline)
					peers := make([]*testPeer, n)
					seen := make([]map[uint32]bool, n)
					joined := make([]time.Time, n)
					readyMs := make([]float64, n)
					var mu sync.Mutex
					var packets, bytes atomic.Uint64
					var workers sync.WaitGroup
					defer func() {
						close(stop)
						for _, p := range peers {
							if p != nil {
								p.close()
								m.Leave(p.connectionID)
							}
						}
						workers.Wait()
						if stats := m.Stats(); stats["peers"] != 0 || stats["rooms"] != 0 {
							t.Error("media resources retained:", stats)
						}
					}()
					for i := range peers {
						peers[i] = newTestPeer(t, m, fmt.Sprintf("load-%d", i))
						sink.addPeer(peers[i])
						queue := make(chan string, 128)
						sink.queues[peers[i].connectionID] = queue
						workers.Add(1)
						go func(peer *testPeer) {
							defer workers.Done()
							for {
								select {
								case <-stop:
									return
								case offer := <-queue:
									if err := peer.acceptOffer(offer); err != nil {
										select {
										case sink.errors <- err:
										default:
										}
									}
								}
							}
						}(peers[i])
						seen[i] = make(map[uint32]bool)
						workers.Add(1)
						go func(i int) {
							defer workers.Done()
							for {
								select {
								case <-stop:
									return
								case p := <-peers[i].incomingRTP:
									mu.Lock()
									seen[i][p.SSRC] = true
									if len(seen[i]) == n-1 && readyMs[i] == 0 {
										readyMs[i] = float64(time.Since(joined[i]).Microseconds()) / 1000
									}
									mu.Unlock()
									packets.Add(1)
									bytes.Add(uint64(len(p.Payload)))
								}
							}
						}(i)
					}
					joins := make([]float64, n)
					join := func(i int) {
						start := time.Now()
						mu.Lock()
						joined[i] = start
						mu.Unlock()
						if err := m.Join(peers[i].connectionID, int64(i+1), 1, 1, false, false); err != nil {
							t.Error(err)
						}
						joins[i] = float64(time.Since(start).Microseconds()) / 1000
					}
					if burst {
						var joinsWG sync.WaitGroup
						for i := range peers {
							joinsWG.Add(1)
							go func(i int) { defer joinsWG.Done(); join(i) }(i)
						}
						joinsWG.Wait()
					} else {
						for i := range peers {
							join(i)
						}
					}
					for i, p := range peers {
						workers.Add(1)
						go func(i int, p *testPeer) {
							defer workers.Done()
							ticker := time.NewTicker(20 * time.Millisecond)
							defer ticker.Stop()
							payload := make([]byte, kbps*1000/8/50)
							var seq uint16
							for {
								select {
								case <-stop:
									return
								case <-ticker.C:
									binary.BigEndian.PutUint64(payload, uint64(time.Now().UnixNano()))
									_ = p.localTrack.WriteRTP(&rtp.Packet{Header: rtp.Header{Version: 2, SequenceNumber: seq, Timestamp: uint32(seq) * 960, SSRC: uint32(i + 1)}, Payload: payload})
									seq++
								}
							}
						}(i, p)
					}
					deadline := time.Now().Add(15 * time.Second)
					for {
						mu.Lock()
						complete := true
						for _, tracks := range seen {
							complete = complete && len(tracks) == n-1
						}
						mu.Unlock()
						if complete {
							break
						}
						if time.Now().After(deadline) {
							mu.Lock()
							t.Log("seen tracks:", seen)
							mu.Unlock()
							select {
							case err := <-sink.errors:
								t.Fatal(err)
							default:
								t.Fatal("not every speaker reached every listener")
							}
						}
						time.Sleep(10 * time.Millisecond)
					}
					cpuBefore := medialoadstats.CPUSeconds()
					started := time.Now()
					beforePackets, beforeBytes := packets.Load(), bytes.Load()
					time.Sleep(2 * time.Second)
					elapsed := time.Since(started).Seconds()
					delivered := packets.Load() - beforePackets
					if delivered < uint64(float64(n*(n-1)*50)*elapsed*0.8) {
						t.Fatalf("insufficient steady RTP: %d", delivered)
					}
					if stats := m.Stats(); stats["senders"] != n*(n-1) {
						t.Fatalf("unexpected fanout: %v", stats)
					}
					var after runtime.MemStats
					runtime.ReadMemStats(&after)
					slices.Sort(joins)
					slices.Sort(readyMs)
					report := map[string]any{"offers": sink.offers.Load(), "peers": n, "kbps": kbps, "simultaneous_join": burst, "join_enqueue_p95_ms": joins[len(joins)-1], "all_tracks_ready_p95_ms": readyMs[len(readyMs)-1], "rtp_packets": delivered, "payload_egress_mbps": float64(bytes.Load()-beforeBytes) * 8 / elapsed / 1e6, "combined_process_heap_bytes": after.HeapAlloc, "combined_heap_delta_bytes": int64(after.HeapAlloc) - int64(baseline.HeapAlloc), "gc_cycles": after.NumGC - baseline.NumGC, "stats": m.Stats(), "combined_cpu_cores": (medialoadstats.CPUSeconds() - cpuBefore) / elapsed, "combined_rss_bytes": medialoadstats.RSSBytes(), "metrics_enabled": telemetry.Enabled(), "telemetry": telemetry.Snapshot()}
					encoded, _ := json.Marshal(report)
					t.Log(string(encoded))
				})
			}
		}
	}
}

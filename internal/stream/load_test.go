package stream

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
	"voxhold-backend/internal/medialoadstats"
	"voxhold-backend/internal/telemetry"
)

type streamLoadPeer struct {
	peer           *webrtc.PeerConnection
	mu             sync.Mutex
	pending        []ICECandidate
	offers         chan string
	packets, bytes atomic.Uint64
}
type streamLoadSink struct {
	manager *Manager
	peers   map[string]*streamLoadPeer // immutable before any manager operations
	stop    <-chan struct{}
	errors  chan error
	offers  atomic.Uint64
}

func (s *streamLoadSink) SendOffer(id, sdp string) bool {
	select {
	case <-s.stop:
		return false
	case s.peers[id].offers <- sdp:
		s.offers.Add(1)
		return true
	default:
		return false
	}
}
func (s *streamLoadSink) SendICECandidate(id string, c ICECandidate) bool {
	p := s.peers[id]
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.peer.RemoteDescription() == nil {
		p.pending = append(p.pending, c)
		return true
	}
	return p.peer.AddICECandidate(webrtc.ICECandidateInit{Candidate: c.Candidate, SDPMid: c.SDPMid, SDPMLineIndex: c.SDPMLineIndex, UsernameFragment: c.UsernameFragment}) == nil
}
func (s *streamLoadSink) CloseStream(id, reason string) {
	select {
	case s.errors <- fmt.Errorf("%s: %s", id, reason):
	default:
	}
}

func TestStreamRoomLoad(t *testing.T) {
	if os.Getenv("VOXHOLD_MEDIA_LOAD") != "1" {
		t.Skip("set VOXHOLD_MEDIA_LOAD=1")
	}
	if os.Getenv("VOXHOLD_LOAD_METRICS") == "1" {
		telemetry.Enable()
	}
	for _, viewers := range []int{1, 4, 9} {
		for _, kbps := range []int{6000, 16000} {
			t.Run(fmt.Sprintf("viewers=%d/kbps=%d", viewers, kbps), func(t *testing.T) {
				stop := make(chan struct{})
				sink := &streamLoadSink{peers: make(map[string]*streamLoadPeer), stop: stop, errors: make(chan error, 128)}
				m, err := NewManager(Config{MaxViewers: 32, MaxVideoBitrateKbps: 16000}, sink)
				if err != nil {
					t.Fatal(err)
				}
				sink.manager = m
				var workers sync.WaitGroup
				defer func() {
					close(stop)
					_ = m.Close()
					for _, p := range sink.peers {
						_ = p.peer.Close()
					}
					workers.Wait()
					if stats := m.Stats(); stats["peers"] != 0 || stats["rooms"] != 0 {
						t.Error("resources retained", stats)
					}
				}()
				var track *webrtc.TrackLocalStaticRTP
				for i := 0; i <= viewers; i++ {
					id := fmt.Sprintf("peer-%d", i)
					peer, err := webrtc.NewPeerConnection(webrtc.Configuration{})
					if err != nil {
						t.Fatal(err)
					}
					p := &streamLoadPeer{peer: peer, offers: make(chan string, 128)}
					sink.peers[id] = p
					if i == 0 {
						track, err = webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8, ClockRate: 90000}, "screen", "stream")
						if err != nil {
							t.Fatal(err)
						}
						sender, err := peer.AddTrack(track)
						if err != nil {
							t.Fatal(err)
						}
						workers.Add(1)
						go func() {
							defer workers.Done()
							buffer := make([]byte, 1500)
							for {
								if _, _, err := sender.Read(buffer); err != nil {
									return
								}
							}
						}()
					}
					peer.OnICECandidate(func(c *webrtc.ICECandidate) {
						if c == nil {
							return
						}
						v := c.ToJSON()
						_ = m.AddICECandidate(id, ICECandidate{Candidate: v.Candidate, SDPMid: v.SDPMid, SDPMLineIndex: v.SDPMLineIndex, UsernameFragment: v.UsernameFragment})
					})
					peer.OnTrack(func(remote *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
						for {
							packet, _, err := remote.ReadRTP()
							if err != nil {
								return
							}
							p.packets.Add(1)
							p.bytes.Add(uint64(len(packet.Payload)))
						}
					})
					workers.Add(1)
					go func() {
						defer workers.Done()
						for {
							select {
							case <-stop:
								return
							case offer := <-p.offers:
								p.mu.Lock()
								err := peer.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: offer})
								if err == nil {
									for _, c := range p.pending {
										if err = peer.AddICECandidate(webrtc.ICECandidateInit{Candidate: c.Candidate, SDPMid: c.SDPMid, SDPMLineIndex: c.SDPMLineIndex, UsernameFragment: c.UsernameFragment}); err != nil {
											break
										}
									}
									p.pending = nil
								}
								if err == nil {
									var answer webrtc.SessionDescription
									answer, err = peer.CreateAnswer(nil)
									if err == nil {
										err = peer.SetLocalDescription(answer)
									}
									if err == nil {
										err = m.AcceptAnswer(id, answer.SDP)
									}
								}
								p.mu.Unlock()
								if err != nil {
									select {
									case sink.errors <- err:
									default:
									}
								}
							}
						}
					}()
				}
				if err = m.Start("peer-0", 1, 1, 1, CodecVP8, false); err != nil {
					t.Fatal(err)
				}
				var sent atomic.Uint64
				workers.Add(1)
				go func() {
					defer workers.Done()
					ticker := time.NewTicker(10 * time.Millisecond)
					defer ticker.Stop()
					payload := make([]byte, 1200)
					var budget, sequence uint64
					for {
						select {
						case <-stop:
							return
						case <-ticker.C:
							budget += uint64(kbps) * 1000 / 8 / 100
							for budget >= 1200 {
								budget -= 1200
								err := track.WriteRTP(&rtp.Packet{Header: rtp.Header{Version: 2, SequenceNumber: uint16(sequence), Timestamp: uint32(sequence * 1500)}, Payload: payload})
								if err == nil {
									sent.Add(1)
								}
								sequence++
							}
						}
					}
				}()
				// Viewers join after publisher traffic has begun: exercise late offers.
				time.Sleep(100 * time.Millisecond)
				startJoin := time.Now()
				for i := 1; i <= viewers; i++ {
					if err = m.Watch(fmt.Sprintf("peer-%d", i), int64(i+1), 1, 1); err != nil {
						t.Fatal(err)
					}
				}
				deadline := time.Now().Add(10 * time.Second)
				for {
					ready := true
					for i := 1; i <= viewers; i++ {
						ready = ready && sink.peers[fmt.Sprintf("peer-%d", i)].packets.Load() > 0
					}
					if ready {
						break
					}
					select {
					case err := <-sink.errors:
						t.Fatal(err)
					default:
					}
					if time.Now().After(deadline) {
						t.Fatal("viewer received no RTP")
					}
					time.Sleep(10 * time.Millisecond)
				}
				joinMs := float64(time.Since(startJoin).Microseconds()) / 1000
				time.Sleep(time.Second)
				var beforeBytes, beforePackets uint64
				for _, p := range sink.peers {
					beforeBytes += p.bytes.Load()
					beforePackets += p.packets.Load()
				}
				beforeSent := sent.Load()
				cpuBefore := medialoadstats.CPUSeconds()
				started := time.Now()
				time.Sleep(2 * time.Second)
				elapsed := time.Since(started).Seconds()
				var bytes, packets uint64
				for _, p := range sink.peers {
					bytes += p.bytes.Load()
					packets += p.packets.Load()
				}
				bytes -= beforeBytes
				packets -= beforePackets
				if packets < uint64(float64((sent.Load()-beforeSent)*uint64(viewers))*0.8) {
					t.Fatal("insufficient RTP fanout")
				}
				var memory runtime.MemStats
				runtime.ReadMemStats(&memory)
				report := map[string]any{"viewers": viewers, "target_kbps": kbps, "join_to_first_rtp_all_ms": joinMs, "sent_packets": sent.Load() - beforeSent, "received_packets": packets, "payload_egress_mbps": float64(bytes) * 8 / elapsed / 1e6, "combined_process_heap_bytes": memory.HeapAlloc, "offers": sink.offers.Load(), "stats": m.Stats(), "combined_cpu_cores": (medialoadstats.CPUSeconds() - cpuBefore) / elapsed, "combined_rss_bytes": medialoadstats.RSSBytes(), "metrics_enabled": telemetry.Enabled(), "telemetry": telemetry.Snapshot()}
				data, _ := json.Marshal(report)
				t.Log(string(data))
			})
		}
	}
}

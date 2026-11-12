package realtimehttp

import (
	"context"
	"errors"
	"github.com/coder/websocket"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"voxhold-backend/internal/account"
	"voxhold-backend/internal/realtime"
)

type heartbeatAuth func(context.Context, string) (int64, error)

func (f heartbeatAuth) Authenticate(ctx context.Context, token string) (int64, error) {
	return f(ctx, token)
}

func TestApplicationPingEchoesRequestID(t *testing.T) {
	client := realtime.NewClient(2, "test", []int64{1})
	if err := (&Handler{}).handleIncomingEvent(context.Background(), client, realtime.IncomingEvent{RequestID: "ping-1", Type: realtime.EventPing}); err != nil {
		t.Fatal(err)
	}
	event, ok := client.PopOutgoing()
	if !ok || event.Type != realtime.EventPong || event.RequestID != "ping-1" {
		t.Fatal(event, ok)
	}
}

func TestPeriodicAuthToleratesTransientFailureAndResetsCounter(t *testing.T) {
	testHeartbeatWriter(t, []error{context.DeadlineExceeded, nil, context.DeadlineExceeded, nil}, 0)
}
func TestPeriodicAuthClosesAfterThreeConsecutiveFailures(t *testing.T) {
	testHeartbeatWriter(t, []error{context.DeadlineExceeded, context.DeadlineExceeded, context.DeadlineExceeded}, websocket.StatusTryAgainLater)
}
func TestPeriodicAuthRejectsRevokedSessionImmediately(t *testing.T) {
	testHeartbeatWriter(t, []error{account.ErrUnauthorized}, websocket.StatusPolicyViolation)
}

func TestVoiceRetentionDependsOnWebSocketClose(t *testing.T) {
	for _, test := range []struct {
		name   string
		status websocket.StatusCode
		retain bool
	}{
		{"normal close", websocket.StatusNormalClosure, false},
		{"client detected network loss", 4000, true},
		{"transport lost", 0, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			hub := realtime.NewHub()
			t.Cleanup(hub.Close)
			client := realtime.NewClient(2, "test", []int64{1})
			hub.Register(client)
			hub.JoinVoice(client, 1, 1, false, false)
			handler := &Handler{hub: hub}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			finished := make(chan error, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := websocket.Accept(w, r, nil)
				if err == nil {
					defer conn.CloseNow()
					handler.serveConnection(ctx, conn, client, "test")
					hub.Unregister(client)
				}
				finished <- err
			}))
			defer server.Close()
			conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.CloseNow()
			if test.status == 0 {
				err = conn.CloseNow()
			} else {
				err = conn.Close(test.status, "")
			}
			if err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-finished:
				if err != nil {
					t.Fatal(err)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			participant, exists := hub.CurrentVoice(client)
			if exists != test.retain || participant.Reconnecting != test.retain {
				t.Fatalf("voice after close: exists=%t reconnecting=%t, want both %t", exists, participant.Reconnecting, test.retain)
			}
		})
	}
}

func testHeartbeatWriter(t *testing.T, outcomes []error, want websocket.StatusCode) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ticks := make(chan time.Time)
	calls := make(chan int, 10)
	finished := make(chan error, 1)
	count := 0
	h := &Handler{authenticator: heartbeatAuth(func(context.Context, string) (int64, error) {
		i := count
		count++
		calls <- count
		if i >= len(outcomes) {
			return 2, nil
		}
		return 2, outcomes[i]
	})}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			finished <- err
			return
		}
		defer conn.CloseNow()
		conn.CloseRead(ctx)
		finished <- h.writeLoopWithPings(ctx, conn, realtime.NewClient(2, "test", []int64{1}), "test", ticks)
	}))
	defer server.Close()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	received := make(chan error, 1)
	readerDone := make(chan struct{})
	go func() { defer close(readerDone); _, _, err := conn.Read(ctx); received <- err }()
	defer func() { cancel(); conn.CloseNow(); <-readerDone }()
	for range outcomes {
		select {
		case ticks <- time.Now():
		case err := <-finished:
			t.Fatalf("closed before tick: %v", err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		select {
		case <-calls:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	if want != 0 {
		select {
		case err := <-received:
			if websocket.CloseStatus(err) != want {
				t.Fatalf("close: %v want %d", err, want)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		select {
		case err := <-finished:
			if err != nil {
				t.Fatal(err)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	} else {
		if err := conn.Ping(ctx); err != nil {
			t.Fatalf("transient authentication failure closed socket: %v", err)
		}
		cancel()
		select {
		case err := <-finished:
			if !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
		case <-time.After(time.Second):
			t.Fatal("writer did not stop")
		}
	}
}

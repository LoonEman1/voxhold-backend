package telemetry

import (
	"context"
	"net/http"
	"testing"
)

func TestDiagnosticsRejectsPublicBinding(t *testing.T) {
	for _, address := range []string{":9000", "0.0.0.0:9000", "[::]:9000", "localhost:9000", "192.0.2.1:9000"} {
		if _, _, err := Listen(address, func() any { return nil }); err == nil {
			t.Fatal("accepted", address)
		}
	}
	server, listener, err := Listen("127.0.0.1:0", func() any { return Snapshot() })
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { _ = server.Serve(listener); close(done) }()
	defer func() { _ = server.Shutdown(context.Background()); <-done }()
	response, err := http.Get("http://" + listener.Addr().String() + "/debug/vars")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 200 || response.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("diagnostics response", response.StatusCode)
	}
}

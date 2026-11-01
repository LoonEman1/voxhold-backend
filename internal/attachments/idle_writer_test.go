package attachments

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func slowRead(t *testing.T, url string) (int, error) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	total := 0
	buf := make([]byte, 64<<10)
	for {
		time.Sleep(50 * time.Millisecond)
		n, err := resp.Body.Read(buf)
		total += n
		if err == io.EOF {
			return total, nil
		}
		if err != nil {
			return total, err
		}
	}
}

func TestIdleDeadlineWriterLetsSlowClientFinish(t *testing.T) {
	const size = 4 << 20
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = idleDeadlineWriter{ResponseWriter: w, idle: 2 * time.Second}.Write(make([]byte, size))
	}))
	srv.Config.WriteTimeout = 300 * time.Millisecond
	srv.Start()
	defer srv.Close()
	total, err := slowRead(t, srv.URL)
	if err != nil || total != size {
		t.Fatalf("got %d bytes, err %v; want %d", total, err, size)
	}
}


func TestIdleDeadlineWriterFailsOnStalledClient(t *testing.T) {
	done := make(chan error, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := idleDeadlineWriter{ResponseWriter: w, idle: 300 * time.Millisecond}.Write(make([]byte, 64<<20))
		done <- err
	}))
	defer srv.Close()
	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() // never read: the client stalls
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected write error after idle timeout")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("write never failed")
	}
}

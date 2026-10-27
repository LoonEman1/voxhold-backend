package mediahandshake

import (
	"testing"
	"testing/synctest"
	"time"
)

func TestHandshakeStages(t *testing.T) {
	for _, scenario := range []string{"no-answer", "no-connect", "connected", "closed", "waiting-for-track", "late-answer"} {
		t.Run(scenario, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				failures := make(chan string, 2)
				d := New(func(reason string) { failures <- reason })
				defer d.Stop()
				if scenario != "waiting-for-track" {
					d.Offer()
				}
				switch scenario {
				case "no-connect":
					d.Answer()
				case "connected":
					d.Answer()
					d.Stop()
					d.Offer()
				case "closed":
					d.Stop()
				}
				time.Sleep(AnswerTimeout + ConnectTimeout + time.Second)
				synctest.Wait()
				if scenario == "late-answer" {
					d.Answer()
				}
				want := scenario == "no-answer" || scenario == "no-connect" || scenario == "late-answer"
				if (len(failures) == 1) != want {
					t.Fatalf("unexpected timeouts: %d", len(failures))
				}
			})
		})
	}
}

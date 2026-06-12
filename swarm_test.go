package swarm_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/taybart/log"
	"github.com/taybart/swarm"
)

func TestWork(t *testing.T) {
	log.SetLevel(log.TEST)
	log.Test("starting test")

	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
	defer srv.Close()
	url := srv.URL

	// run for a few seconds so the live reporter ticks, dumping rps and
	// latency every second
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	wp := swarm.NewWorkerPool()
	wp.Interval = time.Second

	err := wp.Swarm(ctx, 4, []swarm.Job{
		{
			Fn: func() error {
				req, err := http.NewRequest("GET", url+"/get", nil)
				if err != nil {
					return err
				}
				return wp.Request(swarm.Request{Req: req})
			},
		},
		{
			Fn: func() error {
				req, err := http.NewRequest("POST", url+"/post",
					strings.NewReader(`{"hello":"world"}`))
				if err != nil {
					return err
				}
				err = wp.Request(swarm.Request{Req: req})
				if err != nil {
					return err
				}
				req, err = http.NewRequest("PUT", url+"/put", nil)
				if err != nil {
					return err
				}
				return wp.Request(swarm.Request{Req: req})
			},
		},
	})
	if err != nil {
		t.Fatalf("swarm returned error: %v", err)
	}

	if len(wp.Results) == 0 {
		t.Fatal("expected requests to have been recorded, got none")
	}
	if wp.Report.TotalRequests != len(wp.Results) {
		t.Fatalf("report total %d != results %d",
			wp.Report.TotalRequests, len(wp.Results))
	}
	log.Test("recorded", len(wp.Results), "requests")
}

// TestScenario exercises the VU model: a per-VU setup acquires a token that
// later steps must echo back, proving state flows through one user's journey.
func TestScenario(t *testing.T) {
	log.SetLevel(log.TEST)
	log.Test("starting scenario test")

	var issued int64
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/login":
				// each VU's setup gets a distinct token
				n := atomic.AddInt64(&issued, 1)
				fmt.Fprintf(w, `{"token":"tok-%d"}`, n)
			case "/action":
				// later steps must carry the token from setup
				if r.Header.Get("Authorization") == "" {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				w.WriteHeader(http.StatusOK)
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
	defer srv.Close()
	url := srv.URL

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	wp := swarm.NewWorkerPool()
	wp.Interval = time.Second
	wp.RampUp = 500 * time.Millisecond // stagger the 5 VUs over half a second
	wp.WarmUp = 200 * time.Millisecond // discard the first 200ms of results

	scenario := swarm.Scenario{
		Name: "navigate",
		Setup: func(ctx context.Context, st *swarm.State) error {
			var login struct {
				Token string `json:"token"`
			}
			req, err := http.NewRequestWithContext(ctx, "POST", url+"/login", nil)
			if err != nil {
				return err
			}
			if err := wp.RequestWithResponse(swarm.Request{Req: req}, &login); err != nil {
				return err
			}
			st.Set("token", login.Token)
			return nil
		},
		Steps: []swarm.Step{
			{
				Name:  "action",
				Think: 10 * time.Millisecond,
				Fn: func(ctx context.Context, st *swarm.State) error {
					req, err := http.NewRequestWithContext(ctx, "POST", url+"/action", nil)
					if err != nil {
						return err
					}
					req.Header.Set("Authorization", "Bearer "+st.String("token"))
					return wp.Request(swarm.Request{Req: req, Expect: http.StatusOK})
				},
			},
		},
	}

	if err := wp.Run(ctx, 5, []swarm.Scenario{scenario}); err != nil {
		t.Fatalf("run returned error: %v", err)
	}

	if len(wp.Results) == 0 {
		t.Fatal("expected recorded action requests, got none")
	}
	for _, res := range wp.Results {
		if res.Error != nil {
			t.Fatalf("step failed (token did not flow through?): %v", res.Error)
		}
	}
	log.Test("recorded", len(wp.Results), "scenario steps")
}

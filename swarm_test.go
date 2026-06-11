package swarm_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
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

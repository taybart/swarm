package swarm

import (
	"errors"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/taybart/log"
)

var (
	ErrBadStatus = errors.New("bad status returned")
)

type Time struct {
	Timestamp time.Time
	Latency   time.Duration
}

type Result struct {
	Method     string
	Path       string
	Error      error
	Count      int
	Time       Time
	StatusCode int
}

type Stat struct {
	Path         string
	Method       string
	Count        int
	AverageTime  time.Duration
	RequestTimes []Time
}

func (s Stat) String() string {
	return fmt.Sprintf("%s%s%s %s%s (%d)%s avg %s",
		log.Green, s.Method, log.Blue, s.Path, log.Yellow,
		s.Count, log.Reset, s.AverageTime)
}
func (s *Stat) CalcTimes() {
	if len(s.RequestTimes) == 0 {
		return
	}
	var sum time.Duration
	for _, t := range s.RequestTimes {
		sum += t.Latency
	}
	s.AverageTime = sum / time.Duration(len(s.RequestTimes))
}

func (wp *WorkerPool) recordResult(start time.Time, req Request, res *http.Response) {

	result := Result{
		Path:       req.Req.URL.Path,
		Method:     req.Req.Method,
		Count:      1,
		StatusCode: res.StatusCode,
		Time: Time{
			Latency:   time.Since(start),
			Timestamp: time.Now(),
		},
	}
	if req.Expect != 0 && res.StatusCode != req.Expect {
		result.Error = ErrBadStatus
	}

	// lock-free counters for the live reporter
	atomic.AddInt64(&wp.count, 1)
	atomic.AddInt64(&wp.latency, int64(result.Time.Latency))

	// recordResult is called concurrently from every worker goroutine
	wp.mu.Lock()
	wp.Results = append(wp.Results, result)
	wp.mu.Unlock()
}

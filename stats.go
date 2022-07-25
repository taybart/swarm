package swarm

import (
	"errors"
	"fmt"
	"net/http"
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
	avg := int64(0)
	for _, t := range s.RequestTimes {
		avg += int64(t.Latency)
	}
	avg /= int64(len(s.RequestTimes))

	s.AverageTime = time.Duration(avg) * time.Nanosecond
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

	wp.Results = append(wp.Results, result)
}

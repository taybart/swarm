package swarm

import (
	"context"
	"errors"
	"math/rand"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/taybart/log"
)

var ErrNoJobs = errors.New("swarm: no jobs provided")

type Job struct {
	ID     int // TODO: unused
	Weight int
	Fn     func() error
}

type WorkerPool struct {
	// 64-bit atomic counters kept first for alignment on 32-bit platforms.
	// Updated by every worker via recordResult, read by the live reporter,
	// so the reporter never contends with workers on the Results lock.
	count   int64 // total requests recorded
	latency int64 // total latency in ns

	Results  []Result
	Timeout  time.Duration
	Interval time.Duration // live progress interval; 0 disables
	Report   Report
	jobsch   chan Job
	cancel   context.CancelFunc
	mu       sync.RWMutex
}

func NewWorkerPool() *WorkerPool {
	wp := WorkerPool{
		Results:  []Result{},
		jobsch:   make(chan Job),
		Interval: time.Second, // live rps/latency cadence; set 0 to disable
	}
	return &wp
}

// Swarm start work
func (wp *WorkerPool) Swarm(ctx context.Context, workers int, jobs []Job) error {
	if len(jobs) == 0 {
		return ErrNoJobs
	}

	// derive a cancellable context so Cancel(), the timeout, and signals
	// all funnel through one shutdown path
	ctx, cancel := context.WithCancel(ctx)
	if wp.Timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, wp.Timeout)
	}
	wp.cancel = cancel
	defer cancel()

	// check for SIGINT/TERM
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigs)

	jobIDs := wp.calculateWeights(jobs)

	wp.Report.StartTime = time.Now()
	log.Info("Creating work queue")
	if wp.Interval > 0 {
		go wp.reportProgress(ctx)
	}
	go func() {
		defer func() {
			close(wp.jobsch)
			log.Info("Work Finalized")
		}()
		for {
			jobID := jobIDs[rand.Intn(len(jobIDs))]
			// keep the send inside the select so we can be cancelled
			// even while every worker is busy
			select {
			case <-sigs:
				return
			case <-ctx.Done():
				return
			case wp.jobsch <- jobs[jobID]:
			}
		}
	}()
	return wp.doWork(workers)
}

// doWork spin up workers
func (wp *WorkerPool) doWork(workers int) error {
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go wp.listenForWork(&wg)
	}
	log.Info(workers, "workers listening for jobs...")
	wg.Wait()

	// finish up
	return wp.Report.Generate(wp.Results)
}

// reportProgress logs live throughput/latency every wp.Interval until ctx is
// done. It reads atomic counters only, so it never blocks workers.
func (wp *WorkerPool) reportProgress(ctx context.Context) {
	ticker := time.NewTicker(wp.Interval)
	defer ticker.Stop()
	var last int64
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n := atomic.LoadInt64(&wp.count)
			totLatency := atomic.LoadInt64(&wp.latency)

			avg := time.Duration(0)
			if n > 0 {
				avg = time.Duration(totLatency / n)
			}
			rps := float64(n-last) / wp.Interval.Seconds()
			last = n

			log.Infof("requests %d | %.1f req/s | avg %s\n", n, rps, avg)
		}
	}
}

func (wp *WorkerPool) calculateWeights(jobs []Job) []int {
	weights := []int{}
	for id, job := range jobs {
		if job.Weight == 0 {
			weights = append(weights, id)
			continue
		}
		for i := 0; i < job.Weight; i++ {
			weights = append(weights, id)
		}
	}
	return weights
}

// Cancel stops the worker pool. Safe to call before Swarm (no-op) and
// safe to call more than once.
func (wp *WorkerPool) Cancel() {
	if wp.cancel != nil {
		wp.cancel()
	}
}

func (wp *WorkerPool) listenForWork(wg *sync.WaitGroup) {
	defer wg.Done()
	for job := range wp.jobsch {
		if err := job.Fn(); err != nil {
			log.Error(err)
		}
	}
}

package swarm

import (
	"context"
	"errors"
	"math/rand"
	"net/http"
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
	RampUp   time.Duration // stagger worker/VU startup over this window; 0 = all at once
	WarmUp   time.Duration // discard results recorded in this window after start; 0 = keep all
	Report   Report

	// Client is used by Request/RequestWithResponse. NewWorkerPool installs one
	// with a tuned transport; replace it to customize (proxy, TLS, etc.).
	Client    *http.Client
	transport *http.Transport // owned default transport; per-host cap tuned to concurrency

	jobsch chan Job
	cancel context.CancelFunc
	mu     sync.RWMutex
}

func NewWorkerPool() *WorkerPool {
	// One shared keep-alive pool. The default MaxIdleConnsPerHost is 2, which
	// serializes a swarm onto two connections and measures the client instead
	// of the target; begin() raises the per-host cap to the concurrency level.
	tr := &http.Transport{
		MaxIdleConns:        256,
		MaxIdleConnsPerHost: 64,
		IdleConnTimeout:     90 * time.Second,
	}
	wp := WorkerPool{
		Results:   []Result{},
		jobsch:    make(chan Job),
		Interval:  time.Second, // live rps/latency cadence; set 0 to disable
		transport: tr,
		Client:    &http.Client{Transport: tr},
	}
	return &wp
}

// begin derives the run context (timeout + signal cancellation), tunes the
// connection pool to the concurrency level, and starts the live reporter.
// Shared by Swarm (jobs) and Run (scenarios).
func (wp *WorkerPool) begin(ctx context.Context, concurrency int) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(ctx)
	if wp.Timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, wp.Timeout)
	}
	wp.cancel = cancel

	// SIGINT/TERM funnels through the same cancel as timeout and Cancel().
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		select {
		case <-sigs:
			cancel()
		case <-ctx.Done():
		}
		signal.Stop(sigs)
	}()

	if wp.transport != nil && wp.transport.MaxIdleConnsPerHost < concurrency {
		wp.transport.MaxIdleConnsPerHost = concurrency
	}

	wp.Report.StartTime = time.Now()
	wp.Report.WarmUp = wp.WarmUp
	if wp.Interval > 0 {
		go wp.reportProgress(ctx)
	}
	return ctx, cancel
}

// stagger sleeps for one ramp-up slice between startups, returning early if the
// run is cancelled mid-ramp (remaining workers then spawn immediately and exit).
func (wp *WorkerPool) stagger(ctx context.Context, total int) {
	if wp.RampUp <= 0 || total <= 1 {
		return
	}
	select {
	case <-ctx.Done():
	case <-time.After(wp.RampUp / time.Duration(total)):
	}
}

// Swarm start work
func (wp *WorkerPool) Swarm(ctx context.Context, workers int, jobs []Job) error {
	if len(jobs) == 0 {
		return ErrNoJobs
	}

	ctx, cancel := wp.begin(ctx, workers)
	defer cancel()

	jobIDs := wp.calculateWeights(jobs)

	log.Info("Creating work queue")
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
			case <-ctx.Done():
				return
			case wp.jobsch <- jobs[jobID]:
			}
		}
	}()
	return wp.doWork(ctx, workers)
}

// doWork spin up workers
func (wp *WorkerPool) doWork(ctx context.Context, workers int) error {
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go wp.listenForWork(&wg)
		wp.stagger(ctx, workers)
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

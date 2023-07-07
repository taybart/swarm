package swarm

import (
	"context"
	"math/rand"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/taybart/log"
)

type Job struct {
	ID     int // TODO: unused
	Weight int
	Fn     func() error
}

type WorkerPool struct {
	Results []Result
	Timeout time.Duration
	Report  Report
	jobsch  chan Job
	mu      sync.RWMutex
}

func NewWorkerPool() *WorkerPool {
	wp := WorkerPool{
		Results: []Result{},
		jobsch:  make(chan Job),
	}
	return &wp
}

// Swarm: start work
func (wp *WorkerPool) Swarm(ctx context.Context, workers int, jobs []Job) {
	// check for SIGINT/TERM
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	jobIDs := wp.calculateWeights(jobs)

	wp.Report.StartTime = time.Now()
	log.Info("Creating work queue")
	go func() {
		defer func() {
			close(wp.jobsch)
			log.Info("Work Finalized")
		}()
		for {
			select {
			case <-sigs:
				return
			case <-ctx.Done():
				return
			default:
				jobID := jobIDs[rand.Intn(len(jobIDs))]
				wp.jobsch <- jobs[jobID]
			}
		}
	}()
	wp.doWork(workers)
}

// doWork: spin up workers
func (wp *WorkerPool) doWork(workers int) {
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go wp.ListenForWork(&wg)
	}
	log.Info(workers, "workers listening for jobs...")
	wg.Wait()

	// finish up
	wp.Report.Generate(wp.Results)
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

// Swarm: start work
func (wp *WorkerPool) Wait() {
	// wq.
}

// Swarm: start work
func (wp *WorkerPool) Cancel() {
	// wq.
}

func (wp *WorkerPool) ListenForWork(wg *sync.WaitGroup) {
	for job := range wp.jobsch {
		err := job.Fn()
		if err != nil {
			log.Error(err)
		}
	}
	wg.Done()
}

package swarm

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/taybart/log"
)

var ErrNoScenarios = errors.New("swarm: no scenarios provided")

// State carries data across the steps of a single virtual user's journey
// (e.g. a session/exec token from step one used by later steps). Each VU gets
// its own State, so values never leak between simulated users.
type State struct {
	Vars map[string]any
}

func (s *State) Set(key string, v any) { s.Vars[key] = v }

func (s *State) Get(key string) (any, bool) {
	v, ok := s.Vars[key]
	return v, ok
}

// String returns Vars[key] as a string, or "" if absent/not a string.
// Convenient for the common token case: st.String("token").
func (s *State) String(key string) string {
	v, _ := s.Vars[key].(string)
	return v
}

// Step is one action in a scenario. Fn shares the VU's State and should make
// its requests via wp.Request so they land in the report. Think pauses after
// the step to mimic a user reading the screen before acting again.
type Step struct {
	Name  string
	Fn    func(ctx context.Context, st *State) error
	Think time.Duration
}

// Scenario is an ordered user journey run by a virtual user (VU). Setup runs
// once per VU before the loop — put one-time, reusable work there (acquire a
// token that survives across iterations, bootstrap). Requests Setup makes with
// a raw http client stay out of the report; only wp.Request/RequestWithResponse
// calls are recorded, so use a raw client in Setup to keep warm-up traffic out
// of the stats. If a token is single-use, acquire it as the first Step instead
// so every iteration gets a fresh one. Steps run top-to-bottom, aborting the
// iteration on the first error, then repeat until the run ends.
type Scenario struct {
	Name   string
	Weight int
	Setup  func(ctx context.Context, st *State) error
	Steps  []Step
}

// Run drives vus virtual users through the weighted scenarios until ctx is
// cancelled, Timeout elapses, or a signal arrives. Unlike Swarm (which fires
// independent weighted jobs), each VU runs a full scenario in order on its own
// goroutine, so per-user state like a session token flows naturally between
// steps.
func (wp *WorkerPool) Run(ctx context.Context, vus int, scenarios []Scenario) error {
	if len(scenarios) == 0 {
		return ErrNoScenarios
	}

	ctx, cancel := wp.begin(ctx, vus)
	defer cancel()

	weights := scenarioWeights(scenarios)
	log.Info("Starting", vus, "virtual users...")

	var wg sync.WaitGroup
	for i := 0; i < vus; i++ {
		wg.Add(1)
		sc := scenarios[weights[rand.Intn(len(weights))]]
		go wp.runVU(ctx, sc, &wg)
		wp.stagger(ctx, vus)
	}
	wg.Wait()

	return wp.Report.Generate(wp.Results)
}

// runVU is one virtual user: build State, run Setup once, then loop the
// scenario's steps until the run is cancelled.
func (wp *WorkerPool) runVU(ctx context.Context, sc Scenario, wg *sync.WaitGroup) {
	defer wg.Done()

	st := &State{Vars: map[string]any{}}
	if sc.Setup != nil {
		if err := sc.Setup(ctx, st); err != nil {
			log.Error(fmt.Errorf("scenario %q setup: %w", sc.Name, err))
			return
		}
	}

	for {
		if ctx.Err() != nil {
			return
		}
		for _, step := range sc.Steps {
			if ctx.Err() != nil {
				return
			}
			if err := step.Fn(ctx, st); err != nil {
				// the run ending cancels in-flight requests; that's shutdown,
				// not a step failure, so don't report it as an error
				if ctx.Err() != nil {
					return
				}
				log.Error(fmt.Errorf("scenario %q step %q: %w", sc.Name, step.Name, err))
				break // abort this iteration; next loop starts the journey over
			}
			if step.Think > 0 {
				select {
				case <-ctx.Done():
					return
				case <-time.After(step.Think):
				}
			}
		}
	}
}

// scenarioWeights expands scenarios into a selection slice; a 0/negative weight
// counts as 1 (mirrors calculateWeights for jobs).
func scenarioWeights(scenarios []Scenario) []int {
	weights := []int{}
	for id, sc := range scenarios {
		w := sc.Weight
		if w <= 0 {
			w = 1
		}
		for i := 0; i < w; i++ {
			weights = append(weights, id)
		}
	}
	return weights
}

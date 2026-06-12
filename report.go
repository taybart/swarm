package swarm

import (
	"encoding/csv"
	"fmt"
	"os"
	"time"

	"github.com/taybart/log"
)

type Report struct {
	StartTime     time.Time
	WarmUp        time.Duration // results before StartTime+WarmUp are excluded
	Stats         map[string]Stat
	TotalRequests int
}

func (r *Report) Generate(results []Result) error {
	// Drop warm-up results so cold caches / connection establishment / JIT
	// don't skew steady-state latency and throughput.
	measureStart := r.StartTime
	if r.WarmUp > 0 {
		measureStart = r.StartTime.Add(r.WarmUp)
		kept := results[:0:0]
		for _, res := range results {
			if res.Time.Timestamp.Before(measureStart) {
				continue
			}
			kept = append(kept, res)
		}
		log.Infof("warm-up: discarded %d of %d requests\n", len(results)-len(kept), len(results))
		results = kept
	}

	r.TotalRequests = len(results)
	r.Stats = make(map[string]Stat)
	for _, res := range results {
		key := fmt.Sprintf("%s:%s", res.Method, res.Path)
		s, ok := r.Stats[key]
		if !ok {
			s = Stat{
				Method: res.Method,
				Path:   res.Path,
			}
		}
		s.Count += 1
		s.RequestTimes = append(s.RequestTimes, res.Time)
		r.Stats[key] = s
	}
	for k, stat := range r.Stats {
		stat.CalcTimes()
		r.Stats[k] = stat
	}
	elapsed := time.Since(measureStart)
	rps := 0.0
	if elapsed > 0 {
		rps = float64(r.TotalRequests) / elapsed.Seconds()
	}
	log.SetPlain()
	log.Infof("\nTotal requests %s%d%s in %s%s%s req/s %s%.1f%s\n",
		log.Green, r.TotalRequests, log.Reset,
		log.Blue, elapsed, log.Reset,
		log.Yellow, rps, log.Reset)
	log.Info("request stats:")
	for _, s := range r.Stats {
		log.Info(s)
	}
	log.SetFancy()
	return r.toCSV()
}

// TODO: stream so there are no mem issues
func (r *Report) toCSV() error {
	file, err := os.Create("result.csv")
	if err != nil {
		return err
	}
	defer file.Close()
	w := csv.NewWriter(file)

	biggest := -1
	for _, stat := range r.Stats {
		if biggest < len(stat.RequestTimes) {
			biggest = len(stat.RequestTimes)
		}
	}
	body := make([][]string, biggest+1)
	for i := range body {
		body[i] = make([]string, len(r.Stats)*2)
	}
	col := 0
	for k, stat := range r.Stats {
		body[0][col] = fmt.Sprintf("%s_timestamp", k)
		body[0][col+1] = fmt.Sprintf("%s_latency", k)
		for i, t := range stat.RequestTimes {
			// body[i+1][col] = t.Timestamp.Format(time.RFC3339)
			body[i+1][col] = fmt.Sprintf("%d", t.Timestamp.Unix())
			body[i+1][col+1] = fmt.Sprintf("%d", t.Latency.Milliseconds())
		}
		col += 2
	}

	for _, row := range body {
		if err := w.Write(row); err != nil {
			return fmt.Errorf("error writing record to csv: %w", err)
		}
	}

	// Write any buffered data to the underlying writer.
	w.Flush()

	return w.Error()
}

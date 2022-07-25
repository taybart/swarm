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
	Stats         map[string]Stat
	TotalRequests int
}

func (r *Report) Generate(results []Result) {
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
	t := float64(r.TotalRequests) / float64(time.Since(r.StartTime).Milliseconds())
	log.SetPlain()
	log.Infof("Total requests %d in %s req/s %.1f\n", r.TotalRequests, time.Since(r.StartTime), t*1000)
	log.Info("requests")
	for _, s := range r.Stats {
		log.Info(s)
	}
	log.SetFancy()
	r.toCSV()
}

// TODO: stream so there are no mem issues
func (r *Report) toCSV() {
	file, err := os.Create("result.csv")
	if err != nil {
		log.Fatal(err)
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
			log.Fatalln("error writing record to csv:", err)
		}
	}

	// Write any buffered data to the underlying writer (standard output).
	w.Flush()

	if err := w.Error(); err != nil {
		log.Fatal(err)
	}
}

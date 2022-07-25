# Swarm

Load test tool


```go
package main

import (
  "fmt"

  "github.com/taybart/swarm"
)

func main() {
  // create worker pool
  wp := swarm.NewWorkerPool()

  // start 100 workers executing job.Fn
  wp.Swarm(100, []swarm.Job{
          {
            Fn: func() error {
              req, err := http.NewRequest("GET", fmt.Sprintf("%s/get", url), nil)
              if err != nil {
                return err
              }
              return wp.Request(swarm.Request{Req: req})
            },
          },
  })
}
```

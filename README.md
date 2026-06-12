# Swarm

Load test tool. Two models, share the same pool, stats, reporter, ramp-up, and warm-up:

- **Jobs** (`Swarm`) — workers fire independent, weighted, random requests. Best for hammering endpoints.
- **Scenarios** (`Run`) — each virtual user (VU) runs an ordered journey, carrying state (e.g. a session token) between steps. Best for simulating users navigating an app.

## Jobs: weighted random requests

```go
wp := swarm.NewWorkerPool()

// 100 workers firing weighted-random jobs until ctx ends
wp.Swarm(ctx, 100, []swarm.Job{
    {
        Weight: 3, // hit 3x as often as a weight-1 job
        Fn: func() error {
            req, err := http.NewRequest("GET", url+"/get", nil)
            if err != nil {
                return err
            }
            return wp.Request(swarm.Request{Req: req})
        },
    },
})
```

## Scenarios: sequential journeys with shared state

Each VU runs `Setup` once (acquire a token, bootstrap), then loops `Steps` in
order, passing `State` between them. State never leaks between VUs.

```go
wp := swarm.NewWorkerPool()
wp.RampUp = 5 * time.Second        // stagger VU startup over 5s
wp.WarmUp = 10 * time.Second       // discard the first 10s of results

// 50 virtual users running the journey until ctx ends
wp.Run(ctx, 50, []swarm.Scenario{
    {
        Name: "credit-card-payment",
        Setup: func(ctx context.Context, st *swarm.State) error {
            tok, err := getToken()      // raw http client -> not recorded
            if err != nil {
                return err
            }
            st.Set("token", tok)
            return nil
        },
        Steps: []swarm.Step{
            {
                Name:  "initiate",
                Think: 500 * time.Millisecond, // pause like a real user
                Fn: func(ctx context.Context, st *swarm.State) error {
                    req, _ := http.NewRequestWithContext(ctx, "POST",
                        url+"/client/stages/initiate", body())
                    req.Header.Set("Authorization", "Bearer "+st.String("token"))
                    return wp.Request(swarm.Request{Req: req, Expect: 200})
                },
            },
            // more steps share the same st.String("token")...
        },
    },
})
```

Reusable token → acquire it in `Setup`. Single-use token → make acquisition the
first `Step` so each iteration gets a fresh one.

## Pool options

| Field      | Effect                                                              |
|------------|--------------------------------------------------------------------|
| `Timeout`  | Stop the run after this duration.                                  |
| `Interval` | Live rps/latency log cadence (default 1s; 0 disables).            |
| `RampUp`   | Stagger worker/VU startup over this window (0 = all at once).     |
| `WarmUp`   | Discard results recorded in this window after start.              |
| `Client`   | HTTP client (NewWorkerPool installs one with a tuned transport).  |

A run also stops on `ctx` cancel, `SIGINT`/`SIGTERM`, or `wp.Cancel()`. Results
are written to `result.csv`.

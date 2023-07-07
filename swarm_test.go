package swarm_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/taybart/log"
	"github.com/taybart/rest/server"
	"github.com/taybart/swarm"
)

func TestWork(t *testing.T) {
	log.SetLevel(log.TEST)
	log.Test("starting test")
	url := "http://127.0.0.1:8080"

	serv := server.New(server.Config{Addr: url})
	go serv.ListenAndServe()

	ctx := context.Background()

	wp := swarm.NewWorkerPool()
	go wp.Swarm(ctx, 4, []swarm.Job{
		{
			Fn: func() error {
				req, err := http.NewRequest("GET",
					fmt.Sprintf("%s/get", url), nil)
				if err != nil {
					return err
				}
				return wp.Request(swarm.Request{Req: req})
			},
		},
		{
			Fn: func() error {
				req, err := http.NewRequest("POST",
					fmt.Sprintf("%s/post", url),
					strings.NewReader(`{"hello":"world"}`))
				if err != nil {
					return err
				}
				err = wp.Request(swarm.Request{Req: req})
				if err != nil {
					return err
				}
				req, err = http.NewRequest("PUT",
					fmt.Sprintf("%s/put", url), nil)
				if err != nil {
					return err
				}
				return wp.Request(swarm.Request{Req: req})
			},
		},
	})
	log.Test("Canceling context")
	ctx.Done()
}

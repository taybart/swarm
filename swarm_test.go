package swarm_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/taybart/swarm"
)

func TestWork(t *testing.T) {
	url := "http://127.0.0.1:8080"

	wp := swarm.NewWorkerPool()
	wp.Swarm(4, []swarm.Job{
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
}

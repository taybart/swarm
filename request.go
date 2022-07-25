package swarm

import (
	"encoding/json"
	"io/ioutil"
	"net/http"
	"time"
)

type Request struct {
	Req    *http.Request
	Expect int
}

func (wp *WorkerPool) Request(req Request) error {
	start := time.Now()
	res, err := http.DefaultClient.Do(req.Req)
	if err != nil {
		return err
	}
	wp.recordResult(start, req, res)
	return nil
}

func (wp *WorkerPool) RequestWithResponse(req Request, response interface{}) error {
	start := time.Now()
	res, err := http.DefaultClient.Do(req.Req)
	if err != nil {
		return err
	}
	wp.recordResult(start, req, res)

	defer res.Body.Close()
	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, response)
}

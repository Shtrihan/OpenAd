// Command loadtest drives POST /v1/ad with concurrent clients and checks
// latency and error budgets. Each request uses a fresh installation id, so
// frequency caps do not interfere.
//
//	go run ./cmd/loadtest -url http://localhost:8080 -c 64 -d 30s -max-p95 30ms -min-rps 1000
package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"
)

func main() {
	var (
		base      = flag.String("url", "http://localhost:8080", "ad server base URL")
		conc      = flag.Int("c", 32, "concurrent clients")
		dur       = flag.Duration("d", 20*time.Second, "test duration")
		placement = flag.String("placement", "00000000-0000-4000-8000-000000000021", "placement id")
		app       = flag.String("app", "00000000-0000-4000-8000-000000000011", "app id")
		maxP95    = flag.Duration("max-p95", 30*time.Millisecond, "fail if p95 latency is above this")
		minRPS    = flag.Float64("min-rps", 0, "fail if throughput is below this")
	)
	flag.Parse()

	client := &http.Client{
		Timeout:   5 * time.Second,
		Transport: &http.Transport{MaxIdleConnsPerHost: *conc, MaxConnsPerHost: *conc},
	}
	ctx, cancel := context.WithTimeout(context.Background(), *dur)
	defer cancel()

	type result struct {
		lat    []time.Duration
		filled int
		errs   map[string]int
	}
	results := make([]result, *conc)
	var wg sync.WaitGroup
	start := time.Now()
	for w := 0; w < *conc; w++ {
		wg.Add(1)
		go func(r *result) {
			defer wg.Done()
			r.errs = map[string]int{}
			for ctx.Err() == nil {
				body := fmt.Sprintf(`{"id":%q,"placement_id":%q,"app":{"id":%q},`+
					`"device":{"platform":"android","language":"ru"},"user":{"installation_id":%q},`+
					`"sdk":{"name":"openad-android","version":"0.1.0"}}`,
					uuid.NewString(), *placement, *app, uuid.NewString())
				t0 := time.Now()
				resp, err := client.Post(*base+"/v1/ad", "application/json", bytes.NewBufferString(body))
				if err != nil {
					if ctx.Err() == nil {
						r.errs[err.Error()]++
					}
					continue
				}
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				r.lat = append(r.lat, time.Since(t0))
				switch resp.StatusCode {
				case http.StatusOK:
					r.filled++
				case http.StatusNoContent:
				default:
					r.errs[fmt.Sprintf("HTTP %d", resp.StatusCode)]++
				}
			}
		}(&results[w])
	}
	wg.Wait()
	elapsed := time.Since(start)

	var all []time.Duration
	filled, errors := 0, map[string]int{}
	for _, r := range results {
		all = append(all, r.lat...)
		filled += r.filled
		for k, v := range r.errs {
			errors[k] += v
		}
	}
	if len(all) == 0 {
		fmt.Println("no requests completed", errors)
		os.Exit(1)
	}
	slices.Sort(all)
	pct := func(p float64) time.Duration { return all[min(int(float64(len(all))*p), len(all)-1)] }
	rps := float64(len(all)) / elapsed.Seconds()
	nErr := 0
	for _, v := range errors {
		nErr += v
	}

	fmt.Printf("requests  %d in %s (%.0f req/s), concurrency %d\n", len(all), elapsed.Round(time.Millisecond), rps, *conc)
	fmt.Printf("filled    %.1f%%\n", 100*float64(filled)/float64(len(all)))
	fmt.Printf("latency   p50 %s  p95 %s  p99 %s  max %s\n", pct(.50), pct(.95), pct(.99), all[len(all)-1])
	fmt.Printf("errors    %d %v\n", nErr, errors)

	ok := true
	if nErr > 0 {
		fmt.Println("FAIL: errors occurred")
		ok = false
	}
	if p := pct(.95); p > *maxP95 {
		fmt.Printf("FAIL: p95 %s > %s\n", p, *maxP95)
		ok = false
	}
	if rps < *minRPS {
		fmt.Printf("FAIL: %.0f req/s < %.0f\n", rps, *minRPS)
		ok = false
	}
	if filled == 0 {
		fmt.Println("FAIL: no ads filled")
		ok = false
	}
	if !ok {
		os.Exit(1)
	}
}

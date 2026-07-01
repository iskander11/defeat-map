package maptiles

import (
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// Fetcher downloads a single tile at a time, on demand, over HTTPS, with a
// polite identifying User-Agent as required by the OSM tile usage policy.
// It never runs an unattended bulk crawl — it only fetches tiles the map
// widget actually asks for because the user is looking at them.
type Fetcher struct {
	client    *http.Client
	userAgent string
	sem       chan struct{}
	mu        sync.Mutex
	lastReq   time.Time
	minGap    time.Duration
}

func NewFetcher(userAgent string) *Fetcher {
	return &Fetcher{
		client:    &http.Client{Timeout: 15 * time.Second},
		userAgent: userAgent,
		sem:       make(chan struct{}, 4), // small concurrency, interactive use only
		minGap:    60 * time.Millisecond,
	}
}

func (f *Fetcher) throttle() {
	f.mu.Lock()
	wait := f.minGap - time.Since(f.lastReq)
	if wait > 0 {
		f.mu.Unlock()
		time.Sleep(wait)
		f.mu.Lock()
	}
	f.lastReq = time.Now()
	f.mu.Unlock()
}

// Fetch downloads one tile PNG from urlTemplate (with {z}/{x}/{y} placeholders).
func (f *Fetcher) Fetch(urlTemplate string, z, x, y int) ([]byte, error) {
	f.sem <- struct{}{}
	defer func() { <-f.sem }()
	f.throttle()

	url := expandTileURL(urlTemplate, z, x, y)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", f.userAgent)

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tile fetch %s: status %d", url, resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func expandTileURL(tmpl string, z, x, y int) string {
	return fmt.Sprintf(tmpl, z, x, y)
}

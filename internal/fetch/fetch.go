// Package fetch is the HTTP side of wit-stitch: a small client plus a concurrent,
// resumable tile downloader.
package fetch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const maxBody = 64 << 20

// StatusError is returned for non-2xx responses.
type StatusError struct {
	URL  string
	Code int
}

func (e *StatusError) Error() string {
	msg := fmt.Sprintf("%s: HTTP %d", e.URL, e.Code)
	if e.Code == http.StatusForbidden || e.Code == http.StatusUnauthorized {
		msg += " (the server refused access)"
	}
	return msg
}

// Client wraps http.Client with a User-Agent and body limits.
type Client struct {
	HTTP      *http.Client
	UserAgent string
}

// New returns a Client whose connection pool is sized for `workers` parallel requests.
func New(workers int) *Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.MaxIdleConnsPerHost = workers
	tr.MaxConnsPerHost = workers
	return &Client{
		HTTP:      &http.Client{Transport: tr, Timeout: 60 * time.Second},
		UserAgent: "wit-stitch/0.2 (tiled image stitcher)",
	}
}

// Get fetches url and returns its body.
func (c *Client) Get(ctx context.Context, url string) ([]byte, error) {
	body, _, err := c.get(ctx, url)
	return body, err
}

func (c *Client) get(ctx context.Context, url string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
		return nil, "", &StatusError{URL: url, Code: resp.StatusCode}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	return body, resp.Header.Get("Content-Type"), err
}

// Status is the state of one tile download.
type Status uint8

const (
	Pending Status = iota
	Active
	Retrying
	Done
	Cached // already on disk from an earlier run
	Failed
)

// Job is one tile to download: URLs are tried in order, falling through on 404.
type Job struct {
	URLs []string
	Path string
}

// Reporter receives progress updates. It must be safe for concurrent use.
type Reporter interface {
	Update(i int, s Status, bytes int64)
}

// DownloadAll fetches every job with `workers` goroutines, skipping files that are
// already on disk. It returns one error slot per job (nil on success).
func (c *Client) DownloadAll(ctx context.Context, jobs []Job, workers, retries int, rep Reporter) []error {
	errs := make([]error, len(jobs))
	idx := make(chan int)
	var wg sync.WaitGroup
	for range max(workers, 1) {
		wg.Go(func() {
			for i := range idx {
				errs[i] = c.downloadOne(ctx, i, jobs[i], retries, rep)
			}
		})
	}
feed:
	for i := range jobs {
		select {
		case idx <- i:
		case <-ctx.Done():
			for j := i; j < len(jobs); j++ {
				errs[j] = ctx.Err()
			}
			break feed
		}
	}
	close(idx)
	wg.Wait()
	return errs
}

func (c *Client) downloadOne(ctx context.Context, i int, job Job, retries int, rep Reporter) error {
	if st, err := os.Stat(job.Path); err == nil && st.Size() > 0 {
		rep.Update(i, Cached, 0)
		return nil
	}
	rep.Update(i, Active, 0)
	var err error
	for attempt := 0; attempt <= retries; attempt++ {
		if attempt > 0 {
			rep.Update(i, Retrying, 0)
			backoff := time.Duration(1<<(attempt-1))*500*time.Millisecond + rand.N(250*time.Millisecond)
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				rep.Update(i, Failed, 0)
				return ctx.Err()
			}
			rep.Update(i, Active, 0)
		}
		var n int64
		n, err = c.tryURLs(ctx, job)
		if err == nil {
			rep.Update(i, Done, n)
			return nil
		}
		if !retryable(err) || ctx.Err() != nil {
			break
		}
	}
	rep.Update(i, Failed, 0)
	return err
}

// tryURLs fetches the first of job.URLs that exists and writes it to job.Path.
func (c *Client) tryURLs(ctx context.Context, job Job) (int64, error) {
	var first error
	for _, u := range job.URLs {
		body, ctype, err := c.get(ctx, u)
		if err != nil {
			if first == nil {
				first = err
			}
			var se *StatusError
			if errors.As(err, &se) && se.Code == http.StatusNotFound {
				continue // try the next fallback URL
			}
			return 0, err
		}
		if strings.HasPrefix(ctype, "text/html") {
			return 0, &nonImageError{u}
		}
		return int64(len(body)), writeAtomic(job.Path, body)
	}
	return 0, first
}

type nonImageError struct{ url string }

func (e *nonImageError) Error() string {
	return e.url + ": server returned an HTML page instead of an image"
}

func retryable(err error) bool {
	var se *StatusError
	if errors.As(err, &se) {
		return se.Code == http.StatusTooManyRequests || se.Code >= 500
	}
	var ni *nonImageError
	if errors.As(err, &ni) || errors.Is(err, context.Canceled) {
		return false
	}
	var ne net.Error
	return errors.As(err, &ne) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, context.DeadlineExceeded)
}

func writeAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".part"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

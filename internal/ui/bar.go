package ui

import (
	"fmt"
	"os"
	"sync/atomic"
	"time"

	"golang.org/x/term"
)

// Bar is a single-line progress bar redrawn in place.
type Bar struct {
	out   *os.File
	label string
	total int64
	n     atomic.Int64
	live  bool
	start time.Time

	stop, done chan struct{}
}

func NewBar(out *os.File, label string, total int, plain bool) *Bar {
	return &Bar{
		out: out, label: label, total: int64(total),
		live: !plain && term.IsTerminal(int(out.Fd())),
		stop: make(chan struct{}), done: make(chan struct{}),
	}
}

func (b *Bar) Add(n int) { b.n.Add(int64(n)) }

func (b *Bar) Start() {
	b.start = time.Now()
	go func() {
		defer close(b.done)
		if !b.live {
			return
		}
		t := time.NewTicker(100 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-b.stop:
				return
			case <-t.C:
				b.draw(false)
			}
		}
	}()
}

func (b *Bar) Stop() {
	close(b.stop)
	<-b.done
	b.draw(true)
}

func (b *Bar) draw(final bool) {
	n := b.n.Load()
	line := fmt.Sprintf("%s %s %d/%d", b.label, bar(int(n), int(b.total), 24), n, b.total)
	if final {
		line += fmt.Sprintf("  (%s)", time.Since(b.start).Round(10*time.Millisecond))
	}
	if b.live {
		fmt.Fprintf(b.out, "\r\x1b[2K%s", line)
		if final {
			fmt.Fprintln(b.out)
		}
	} else if final {
		fmt.Fprintln(b.out, line)
	}
}

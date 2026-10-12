// Package ui draws live terminal progress: a colour map of the tile grid while
// downloading, and a simple bar while stitching.
package ui

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"

	"wit-stitch/internal/fetch"
)

// 256-colour palette indices for each tile state.
var stateColor = map[fetch.Status]int{
	fetch.Pending:  238,
	fetch.Active:   220,
	fetch.Retrying: 208,
	fetch.Done:     42,
	fetch.Cached:   33,
	fetch.Failed:   196,
}

const partialColor = 29 // a block of tiles that is partly done

// Grid is a live map of tile download states. Each tile is one "pixel" drawn with
// half-block characters, so one terminal cell shows two tile rows; large grids are
// downsampled so the map always fits the terminal.
type Grid struct {
	out        *os.File
	title      string
	cols, rows int
	live       bool

	mu     sync.Mutex
	states []fetch.Status
	counts [Failed + 1]int
	bytes  int64
	start  time.Time
	lines  int // lines drawn by the previous frame

	stop, done chan struct{}
}

// Failed mirrors fetch.Failed for array sizing.
const Failed = fetch.Failed

// NewGrid creates a grid for cols×rows tiles. If out is not a terminal (or plain is
// set) it falls back to periodic one-line progress messages.
func NewGrid(out *os.File, title string, cols, rows int, plain bool) *Grid {
	g := &Grid{
		out: out, title: title, cols: cols, rows: rows,
		live:   !plain && term.IsTerminal(int(out.Fd())),
		states: make([]fetch.Status, cols*rows),
		stop:   make(chan struct{}),
		done:   make(chan struct{}),
	}
	g.counts[fetch.Pending] = cols * rows
	return g
}

// Update implements fetch.Reporter.
func (g *Grid) Update(i int, s fetch.Status, n int64) {
	g.mu.Lock()
	g.counts[g.states[i]]--
	g.states[i] = s
	g.counts[s]++
	g.bytes += n
	g.mu.Unlock()
}

// Start begins redrawing in the background.
func (g *Grid) Start() {
	g.start = time.Now()
	interval := 100 * time.Millisecond
	if !g.live {
		interval = 2 * time.Second
	} else {
		fmt.Fprint(g.out, "\x1b[?25l") // hide cursor
	}
	go func() {
		defer close(g.done)
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-g.stop:
				return
			case <-t.C:
				g.draw()
			}
		}
	}()
}

// Stop draws the final frame and restores the cursor.
func (g *Grid) Stop() {
	close(g.stop)
	<-g.done
	g.draw()
	if g.live {
		fmt.Fprint(g.out, "\x1b[?25h")
	}
}

// Counts returns how many tiles are done (fresh or cached) and failed.
func (g *Grid) Counts() (done, failed int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.counts[fetch.Done] + g.counts[fetch.Cached], g.counts[fetch.Failed]
}

func (g *Grid) draw() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.live {
		fmt.Fprintln(g.out, g.statsLine())
		return
	}
	var b strings.Builder
	if g.lines > 0 {
		fmt.Fprintf(&b, "\x1b[%dF", g.lines) // back to the top of the previous frame
	}
	lines := g.frame()
	for _, l := range lines {
		b.WriteString("\x1b[2K")
		b.WriteString(l)
		b.WriteByte('\n')
	}
	b.WriteString("\x1b[J")
	g.lines = len(lines)
	io.WriteString(g.out, b.String())
}

func (g *Grid) frame() []string {
	tw, th, err := term.GetSize(int(g.out.Fd()))
	if err != nil || tw <= 0 {
		tw, th = 80, 24
	}
	maxW, maxH := max(tw-1, 10), max(th-6, 4)*2 // pixels available; 2 pixel rows per line

	k := 1 // tiles per pixel side
	for (g.cols+k-1)/k > maxW || (g.rows+k-1)/k > maxH {
		k++
	}
	pw, ph := (g.cols+k-1)/k, (g.rows+k-1)/k

	scale := ""
	if k > 1 {
		scale = fmt.Sprintf("  (1 cell = %d×%d tiles)", k, k)
	}
	lines := []string{fmt.Sprintf("\x1b[1m%s\x1b[0m  %d×%d tiles%s", g.title, g.cols, g.rows, scale)}
	for py := 0; py < ph; py += 2 {
		var b strings.Builder
		lastFg, lastBg := -1, -1
		for px := 0; px < pw; px++ {
			fg := g.blockColor(px, py, k)
			bg := -1
			if py+1 < ph {
				bg = g.blockColor(px, py+1, k)
			}
			if fg != lastFg {
				fmt.Fprintf(&b, "\x1b[38;5;%dm", fg)
				lastFg = fg
			}
			if bg != lastBg {
				if bg < 0 {
					b.WriteString("\x1b[49m")
				} else {
					fmt.Fprintf(&b, "\x1b[48;5;%dm", bg)
				}
				lastBg = bg
			}
			b.WriteString("▀")
		}
		b.WriteString("\x1b[0m")
		lines = append(lines, b.String())
	}
	lines = append(lines, g.legend(), g.statsLine())
	return lines
}

// blockColor summarises the k×k block of tiles at pixel (px, py).
func (g *Grid) blockColor(px, py, k int) int {
	var n, done, cached int
	var active, retrying, failed bool
	for r := py * k; r < min((py+1)*k, g.rows); r++ {
		for c := px * k; c < min((px+1)*k, g.cols); c++ {
			n++
			switch g.states[r*g.cols+c] {
			case fetch.Done:
				done++
			case fetch.Cached:
				cached++
			case fetch.Active:
				active = true
			case fetch.Retrying:
				retrying = true
			case fetch.Failed:
				failed = true
			}
		}
	}
	switch {
	case failed:
		return stateColor[fetch.Failed]
	case retrying:
		return stateColor[fetch.Retrying]
	case active:
		return stateColor[fetch.Active]
	case cached == n:
		return stateColor[fetch.Cached]
	case done+cached == n:
		return stateColor[fetch.Done]
	case done+cached > 0:
		return partialColor
	}
	return stateColor[fetch.Pending]
}

func (g *Grid) legend() string {
	item := func(s fetch.Status, name string) string {
		return fmt.Sprintf("\x1b[38;5;%dm■\x1b[0m %s", stateColor[s], name)
	}
	return strings.Join([]string{
		item(fetch.Done, "done"), item(fetch.Cached, "cached"), item(fetch.Active, "fetching"),
		item(fetch.Retrying, "retrying"), item(fetch.Failed, "failed"), item(fetch.Pending, "pending"),
	}, "  ")
}

func (g *Grid) statsLine() string {
	total := len(g.states)
	fresh, cached, failed := g.counts[fetch.Done], g.counts[fetch.Cached], g.counts[fetch.Failed]
	finished := fresh + cached + failed
	elapsed := time.Since(g.start).Seconds()
	rate := float64(fresh) / max(elapsed, 0.001)
	eta := "--"
	if rate > 0 && finished < total {
		eta = (time.Duration(float64(total-finished)/rate) * time.Second).Round(time.Second).String()
	}
	s := fmt.Sprintf("%s %d/%d  %3.0f%%  %.1f tiles/s  %s  ETA %s",
		bar(finished, total, 24), fresh+cached, total, 100*float64(finished)/float64(max(total, 1)),
		rate, humanBytes(g.bytes), eta)
	if cached > 0 {
		s += fmt.Sprintf("  (%d cached)", cached)
	}
	if failed > 0 {
		s += fmt.Sprintf("  \x1b[31m%d failed\x1b[0m", failed)
		if !g.live {
			s = strings.NewReplacer("\x1b[31m", "", "\x1b[0m", "").Replace(s)
		}
	}
	return s
}

func bar(n, total, width int) string {
	filled := 0
	if total > 0 {
		filled = n * width / total
	}
	return "[" + strings.Repeat("█", filled) + strings.Repeat("·", width-filled) + "]"
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.2f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	default:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	}
}

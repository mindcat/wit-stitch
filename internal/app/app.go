// Package app ties the pieces together: resolve a URL to a tile pyramid, download
// one level of it, and stitch the result.
package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"wit-stitch/internal/fetch"
	"wit-stitch/internal/source"
	"wit-stitch/internal/stitch"
	"wit-stitch/internal/ui"
)

type Options struct {
	URL       string
	Output    string // default: <image name>.jpg
	TilesDir  string // default: <output>.tiles
	Level     int    // <0 counts down from the highest level (-1 = full resolution)
	Jobs      int
	Retries   int
	Quality   int
	KeepTiles bool
	InfoOnly  bool
	Plain     bool
	Out       *os.File // progress/log output; default os.Stderr
}

func Run(ctx context.Context, o Options) error {
	out := o.Out
	if out == nil {
		out = os.Stderr
	}
	client := fetch.New(o.Jobs)

	fmt.Fprintf(out, "Resolving %s\n", o.URL)
	img, err := source.Resolve(ctx, client, o.URL)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Found %s image %q: %d×%d px, %d levels, tile size %d\n",
		img.Kind, img.Name, img.Width, img.Height, len(img.Levels), img.TileSize)

	if o.InfoOnly {
		printLevels(out, img)
		return nil
	}

	li := o.Level
	if li < 0 {
		li += len(img.Levels)
	}
	if li < 0 || li >= len(img.Levels) {
		return fmt.Errorf("level %d out of range (0-%d)", o.Level, len(img.Levels)-1)
	}
	lv := img.Levels[li]

	output := o.Output
	if output == "" {
		output = img.Name + ".jpg"
	}
	tilesDir := o.TilesDir
	if tilesDir == "" {
		tilesDir = strings.TrimSuffix(output, filepath.Ext(output)) + ".tiles"
	}
	if gb := float64(lv.Width) * float64(lv.Height) * 4 / (1 << 30); gb > 2 {
		fmt.Fprintf(out, "Warning: the stitched canvas needs about %.1f GB of memory\n", gb)
	}
	fmt.Fprintf(out, "Level %d: %d×%d px, %d×%d = %d tiles → %s\n\n",
		li, lv.Width, lv.Height, lv.Cols, lv.Rows, len(lv.Tiles), output)

	// Download.
	jobs := make([]fetch.Job, len(lv.Tiles))
	pieces := make([]stitch.Piece, len(lv.Tiles))
	for i, t := range lv.Tiles {
		ext := path.Ext(t.URLs[0])
		if len(ext) > 5 || ext == "" {
			ext = ".img"
		}
		p := filepath.Join(tilesDir, fmt.Sprintf("%d", li), fmt.Sprintf("%d-%d%s", t.Col, t.Row, ext))
		jobs[i] = fetch.Job{URLs: t.URLs, Path: p}
		pieces[i] = stitch.Piece{Path: p, Dst: t.Dst, Offset: t.Offset}
	}
	grid := ui.NewGrid(out, "Downloading "+img.Name, lv.Cols, lv.Rows, o.Plain)
	grid.Start()
	errs := client.DownloadAll(ctx, jobs, o.Jobs, o.Retries, grid)
	grid.Stop()

	if ctx.Err() != nil {
		return fmt.Errorf("interrupted; downloaded tiles are kept in %s, run the same command again to resume", tilesDir)
	}
	if failed := summarizeFailures(out, errs); failed == len(jobs) {
		return fmt.Errorf("every tile failed to download; tiles are kept in %s", tilesDir)
	}

	// Stitch.
	fmt.Fprintln(out)
	bar := ui.NewBar(out, "Stitching", len(pieces), o.Plain)
	bar.Start()
	canvas, missing, err := stitch.Stitch(ctx, lv.Width, lv.Height, pieces, runtime.NumCPU(), func() { bar.Add(1) })
	bar.Stop()
	if err != nil {
		return err
	}

	fmt.Fprintf(out, "Encoding %s… ", output)
	start := time.Now()
	if err := stitch.Encode(canvas, output, o.Quality); err != nil {
		fmt.Fprintln(out)
		return err
	}
	st, _ := os.Stat(output)
	fmt.Fprintf(out, "done (%s, %.1f MB)\n", time.Since(start).Round(10*time.Millisecond), float64(st.Size())/(1<<20))

	switch {
	case missing > 0:
		fmt.Fprintf(out, "%d tiles were missing and drawn as placeholders. Tiles kept in %s; rerun to retry them.\n", missing, tilesDir)
	case o.KeepTiles:
		fmt.Fprintf(out, "Tiles kept in %s\n", tilesDir)
	default:
		if err := os.RemoveAll(tilesDir); err != nil {
			fmt.Fprintf(out, "Could not remove %s: %v\n", tilesDir, err)
		}
	}
	fmt.Fprintf(out, "Saved %s (%d×%d)\n", output, lv.Width, lv.Height)
	return nil
}

func printLevels(out *os.File, img *source.Image) {
	fmt.Fprintf(out, "Metadata: %s\n\n%5s  %13s  %11s  %7s\n", img.MetaURL, "level", "size (px)", "grid", "tiles")
	for i, lv := range img.Levels {
		fmt.Fprintf(out, "%5d  %13s  %11s  %7d\n", i,
			fmt.Sprintf("%d×%d", lv.Width, lv.Height), fmt.Sprintf("%d×%d", lv.Cols, lv.Rows), len(lv.Tiles))
	}
}

// summarizeFailures prints the distinct download errors and returns the failure count.
func summarizeFailures(out *os.File, errs []error) int {
	var failed int
	kinds := map[string]int{}
	var order []string
	for _, err := range errs {
		if err == nil {
			continue
		}
		failed++
		msg := err.Error()
		var se *fetch.StatusError
		if errors.As(err, &se) {
			msg = fmt.Sprintf("HTTP %d", se.Code)
			if se.Code == 401 || se.Code == 403 {
				msg += " (access refused by server)"
			}
		}
		if kinds[msg] == 0 {
			order = append(order, msg)
		}
		kinds[msg]++
	}
	if failed > 0 {
		fmt.Fprintf(out, "%d tiles failed:\n", failed)
		for _, k := range order {
			fmt.Fprintf(out, "  %4d × %s\n", kinds[k], k)
		}
	}
	return failed
}

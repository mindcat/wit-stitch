// Package source understands tiled-image pyramid formats (Zoomify, Deep Zoom,
// IIIF) and turns a URL into the list of tiles needed to rebuild the image.
package source

import (
	"image"
	"net/url"
	"path"
	"regexp"
	"strings"
)

// Tile is one HTTP request needed to rebuild a level of the image.
type Tile struct {
	Col, Row int
	// URLs are tried in order; later entries are fallbacks used when earlier ones 404.
	URLs []string
	// Dst is the region of the level canvas this tile covers.
	Dst image.Rectangle
	// Offset is where Dst.Min falls inside the decoded tile (non-zero when tiles overlap, as in DZI).
	Offset image.Point
}

// Level is one zoom level of the pyramid.
type Level struct {
	Width, Height int
	Cols, Rows    int
	Tiles         []Tile // row-major
}

// Image describes a tiled image pyramid.
type Image struct {
	Kind          string // "zoomify", "dzi" or "iiif"
	Name          string // suggested output name, filesystem-safe
	MetaURL       string
	Width, Height int // full resolution
	TileSize      int
	Levels        []Level // smallest first; the last level is full resolution
}

func ceilDiv(a, b int) int { return (a + b - 1) / b }

// gridTiles builds the row-major tile list for a level whose tiles are laid out
// on a regular ts×ts grid, using urlsFor to name each tile.
func gridTiles(w, h, ts int, urlsFor func(col, row int) []string) Level {
	lv := Level{Width: w, Height: h, Cols: ceilDiv(w, ts), Rows: ceilDiv(h, ts)}
	lv.Tiles = make([]Tile, 0, lv.Cols*lv.Rows)
	for r := 0; r < lv.Rows; r++ {
		for c := 0; c < lv.Cols; c++ {
			lv.Tiles = append(lv.Tiles, Tile{
				Col: c, Row: r,
				URLs: urlsFor(c, r),
				Dst:  image.Rect(c*ts, r*ts, min((c+1)*ts, w), min((r+1)*ts, h)),
			})
		}
	}
	return lv
}

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// nameFrom derives a filesystem-safe name from the last meaningful path segment of a URL.
func nameFrom(raw string, trimExt bool) string {
	u, err := url.Parse(raw)
	p := raw
	if err == nil {
		p = u.Path
	}
	base := path.Base(strings.TrimRight(p, "/"))
	if trimExt {
		base = strings.TrimSuffix(base, path.Ext(base))
	}
	base = strings.Trim(unsafeName.ReplaceAllString(base, "_"), "_.")
	if base == "" {
		return "stitched"
	}
	return base
}

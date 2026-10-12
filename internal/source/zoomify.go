package source

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Zoomify layout: <base>/ImageProperties.xml describes the image, and tiles live at
// <base>/TileGroup{g}/{tier}-{col}-{row}.jpg. Tiles are numbered row-major across
// all tiers starting from the smallest, and each TileGroup folder holds 256 of them.

var (
	zoomifyTileRe  = regexp.MustCompile(`(?i)^(.*)/TileGroup\d+/\d+-\d+-\d+\.(?:jpe?g|png)$`)
	zoomifyPropsRe = regexp.MustCompile(`(?i)^(.*)/ImageProperties\.xml$`)
	xmlAttrRe      = regexp.MustCompile(`(\w+)\s*=\s*"([^"]*)"`)
)

// zoomifyBase returns the Zoomify base folder if u is a tile or ImageProperties.xml URL.
func zoomifyBase(u string) (string, bool) {
	for _, re := range []*regexp.Regexp{zoomifyTileRe, zoomifyPropsRe} {
		if m := re.FindStringSubmatch(u); m != nil {
			return m[1], true
		}
	}
	return "", false
}

func isZoomifyProps(body []byte) bool {
	return bytes.Contains(bytes.ToUpper(body), []byte("<IMAGE_PROPERTIES"))
}

type zoomifyTier struct{ w, h, cols, rows int }

func parseZoomify(base string, body []byte) (*Image, error) {
	if !isZoomifyProps(body) {
		return nil, errors.New("not a Zoomify ImageProperties.xml")
	}
	attrs := map[string]int{}
	for _, m := range xmlAttrRe.FindAllSubmatch(body, -1) {
		if n, err := strconv.Atoi(string(m[2])); err == nil {
			attrs[strings.ToUpper(string(m[1]))] = n
		}
	}
	w, h, ts := attrs["WIDTH"], attrs["HEIGHT"], attrs["TILESIZE"]
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("zoomify: bad image size %dx%d", w, h)
	}
	if ts <= 0 {
		ts = 256
	}

	tiers := zoomifyTiers(w, h, ts, attrs["NUMTILES"])
	img := &Image{
		Kind: "zoomify", Name: nameFrom(base, false), MetaURL: base + "/ImageProperties.xml",
		Width: w, Height: h, TileSize: ts,
	}
	offset := 0 // index of the first tile of the current tier across the whole pyramid
	for z, t := range tiers {
		lv := gridTiles(t.w, t.h, ts, func(c, r int) []string {
			g := (offset + r*t.cols + c) / 256
			urls := []string{zoomifyURL(base, g, z, c, r)}
			// Fallbacks in case the tier math disagrees with the server's converter.
			urls = append(urls, zoomifyURL(base, g+1, z, c, r))
			if g > 0 {
				urls = append(urls, zoomifyURL(base, g-1, z, c, r))
			}
			return urls
		})
		img.Levels = append(img.Levels, lv)
		offset += t.cols * t.rows
	}
	return img, nil
}

func zoomifyURL(base string, group, tier, col, row int) string {
	return fmt.Sprintf("%s/TileGroup%d/%d-%d-%d.jpg", base, group, tier, col, row)
}

// zoomifyTiers computes the tier sizes, smallest first. Converters disagree slightly
// on rounding, so both common schemes are tried and the one matching NUMTILES wins.
func zoomifyTiers(w, h, ts, numTiles int) []zoomifyTier {
	// Halve the image (rounding down) until it fits in one tile.
	var halving []zoomifyTier
	for cw, ch := w, h; ; cw, ch = cw/2, ch/2 {
		halving = append(halving, zoomifyTier{cw, ch, ceilDiv(cw, ts), ceilDiv(ch, ts)})
		if cw <= ts && ch <= ts {
			break
		}
	}
	slices.Reverse(halving)

	// OpenLayers-style: double the tile footprint instead (rounding up).
	var doubling []zoomifyTier
	for f := 1; ; f *= 2 {
		cw, ch := ceilDiv(w, f), ceilDiv(h, f)
		doubling = append(doubling, zoomifyTier{cw, ch, ceilDiv(cw, ts), ceilDiv(ch, ts)})
		if cw <= ts && ch <= ts {
			break
		}
	}
	slices.Reverse(doubling)

	count := func(ts []zoomifyTier) (n int) {
		for _, t := range ts {
			n += t.cols * t.rows
		}
		return n
	}
	if numTiles > 0 && count(halving) != numTiles && count(doubling) == numTiles {
		return doubling
	}
	return halving
}

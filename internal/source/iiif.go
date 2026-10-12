package source

import (
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"image"
	"regexp"
	"slices"
	"strings"
)

// IIIF Image API: {id}/info.json describes the image, and any region can be requested
// as {id}/{x},{y},{w},{h}/{size}/0/default.jpg. The server advertises a preferred tile
// size and the scale factors it pre-renders.

var (
	iiifInfoRe  = regexp.MustCompile(`^(.*)/info\.json$`)
	iiifImageRe = regexp.MustCompile(`^(.*)/(?:full|square|\d+,\d+,\d+,\d+|pct:[^/]+)/[^/]+/!?[\d.]+/(?:default|color|gray|bitonal|native)\.\w+$`)
)

// iiifInfoURL returns the info.json URL if u is an info.json or image request URL.
func iiifInfoURL(u string) (string, bool) {
	if iiifInfoRe.MatchString(u) {
		return u, true
	}
	if m := iiifImageRe.FindStringSubmatch(u); m != nil {
		return m[1] + "/info.json", true
	}
	return "", false
}

func isIIIF(body []byte) bool {
	return bytes.Contains(body, []byte("iiif.io/api/image"))
}

type iiifInfo struct {
	Context any    `json:"@context"`
	ID2     string `json:"@id"`
	ID3     string `json:"id"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
	Tiles   []struct {
		Width        int   `json:"width"`
		Height       int   `json:"height"`
		ScaleFactors []int `json:"scaleFactors"`
	} `json:"tiles"`
}

func parseIIIF(infoURL string, body []byte) (*Image, error) {
	var info iiifInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return nil, fmt.Errorf("iiif: %w", err)
	}
	if info.Width <= 0 || info.Height <= 0 {
		return nil, fmt.Errorf("iiif: bad image size %dx%d", info.Width, info.Height)
	}
	id := cmp.Or(info.ID3, info.ID2, strings.TrimSuffix(infoURL, "/info.json"))
	id = strings.TrimRight(id, "/")
	ctx, _ := json.Marshal(info.Context)
	v3 := bytes.Contains(ctx, []byte("/image/3"))

	tw, th, scales := 1024, 1024, []int{1}
	if len(info.Tiles) > 0 {
		t := info.Tiles[0]
		tw = t.Width
		th = cmp.Or(t.Height, t.Width)
		if len(t.ScaleFactors) > 0 {
			scales = slices.Clone(t.ScaleFactors)
		}
	}
	if tw <= 0 || th <= 0 {
		return nil, fmt.Errorf("iiif: bad tile size %dx%d", tw, th)
	}
	slices.Sort(scales)
	slices.Reverse(scales) // biggest scale factor = smallest level first

	w, h := info.Width, info.Height
	img := &Image{
		Kind: "iiif", Name: nameFrom(id, true), MetaURL: infoURL,
		Width: w, Height: h, TileSize: tw,
	}
	for _, s := range scales {
		lw, lh := ceilDiv(w, s), ceilDiv(h, s)
		lv := Level{Width: lw, Height: lh, Cols: ceilDiv(lw, tw), Rows: ceilDiv(lh, th)}
		for r := 0; r < lv.Rows; r++ {
			for c := 0; c < lv.Cols; c++ {
				// Region in full-resolution pixels, and the size we ask it to be scaled to.
				rx, ry := c*tw*s, r*th*s
				rw, rh := min(tw*s, w-rx), min(th*s, h-ry)
				sw, sh := ceilDiv(rw, s), ceilDiv(rh, s)
				size := fmt.Sprintf("%d,", sw)
				if v3 {
					size = fmt.Sprintf("%d,%d", sw, sh)
				}
				lv.Tiles = append(lv.Tiles, Tile{
					Col: c, Row: r,
					URLs: []string{fmt.Sprintf("%s/%d,%d,%d,%d/%s/0/default.jpg", id, rx, ry, rw, rh, size)},
					Dst:  image.Rect(c*tw, r*th, min(c*tw+sw, lw), min(r*th+sh, lh)),
				})
			}
		}
		img.Levels = append(img.Levels, lv)
	}
	return img, nil
}

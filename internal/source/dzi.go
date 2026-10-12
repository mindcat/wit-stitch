package source

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"math"
	"regexp"
	"strings"
)

// Deep Zoom (DZI, used by OpenSeadragon): <name>.dzi describes the image, and tiles
// live at <name>_files/{level}/{col}_{row}.{format}. Level L is the image scaled by
// 2^(L-max), where max = ceil(log2(max(w, h))). Neighbouring tiles overlap by Overlap px.

var (
	dziMetaRe = regexp.MustCompile(`(?i)^(.*)\.dzi$`)
	dziTileRe = regexp.MustCompile(`(?i)^(.*)_files/\d+/\d+_\d+\.\w+$`)
)

// dziMetaCandidates returns likely descriptor URLs if u looks like a DZI descriptor or tile.
func dziMetaCandidates(u string) []string {
	if dziMetaRe.MatchString(u) {
		return []string{u}
	}
	if m := dziTileRe.FindStringSubmatch(u); m != nil {
		return []string{m[1] + ".dzi", m[1] + ".xml"}
	}
	return nil
}

func isDZI(body []byte) bool {
	return bytes.Contains(body, []byte("schemas.microsoft.com/deepzoom")) ||
		(bytes.Contains(body, []byte("<Image")) && bytes.Contains(body, []byte("TileSize")))
}

type dziDoc struct {
	TileSize int    `xml:"TileSize,attr"`
	Overlap  int    `xml:"Overlap,attr"`
	Format   string `xml:"Format,attr"`
	Size     struct {
		Width  int `xml:"Width,attr"`
		Height int `xml:"Height,attr"`
	} `xml:"Size"`
}

func parseDZI(metaURL string, body []byte) (*Image, error) {
	var d dziDoc
	if err := xml.Unmarshal(body, &d); err != nil {
		return nil, fmt.Errorf("dzi: %w", err)
	}
	w, h, ts, ov := d.Size.Width, d.Size.Height, d.TileSize, d.Overlap
	if w <= 0 || h <= 0 || ts <= 0 {
		return nil, fmt.Errorf("dzi: bad descriptor (size %dx%d, tile %d)", w, h, ts)
	}
	format := strings.ToLower(d.Format)
	if format == "" {
		format = "jpg"
	}
	base := metaURL[:len(metaURL)-len(extOf(metaURL))] + "_files"

	img := &Image{
		Kind: "dzi", Name: nameFrom(metaURL, true), MetaURL: metaURL,
		Width: w, Height: h, TileSize: ts,
	}
	maxLevel := int(math.Ceil(math.Log2(float64(max(w, h)))))
	for l := 0; l <= maxLevel; l++ {
		scale := 1 << (maxLevel - l)
		lw, lh := ceilDiv(w, scale), ceilDiv(h, scale)
		lv := gridTiles(lw, lh, ts, func(c, r int) []string {
			return []string{fmt.Sprintf("%s/%d/%d_%d.%s", base, l, c, r, format)}
		})
		// Every tile except the first in each row/column starts Overlap px early.
		for i := range lv.Tiles {
			t := &lv.Tiles[i]
			if t.Col > 0 {
				t.Offset.X = ov
			}
			if t.Row > 0 {
				t.Offset.Y = ov
			}
		}
		img.Levels = append(img.Levels, lv)
	}
	return img, nil
}

func extOf(u string) string {
	if i := strings.LastIndexByte(u, '.'); i > strings.LastIndexByte(u, '/') {
		return u[i:]
	}
	return ""
}

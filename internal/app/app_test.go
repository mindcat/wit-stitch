package app

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"sync/atomic"
	"testing"
)

// The fake servers below render tiles from this source image as lossless PNGs, so a
// correct stitch reproduces it exactly.
func sourceImage(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{uint8(x * 7), uint8(y * 13), uint8((x*y)>>3 ^ x), 255})
		}
	}
	return img
}

// scaled returns src resized to w×h (nearest neighbour), standing in for a lower zoom level.
func scaled(src *image.RGBA, w, h int) *image.RGBA {
	if w == src.Bounds().Dx() && h == src.Bounds().Dy() {
		return src
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	sw, sh := src.Bounds().Dx(), src.Bounds().Dy()
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dst.Set(x, y, src.At(x*sw/w, y*sh/h))
		}
	}
	return dst
}

func writePNG(w http.ResponseWriter, img image.Image, r image.Rectangle) {
	sub := image.NewRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	draw.Draw(sub, sub.Bounds(), img, r.Min, draw.Src)
	var buf bytes.Buffer
	png.Encode(&buf, sub)
	w.Header().Set("Content-Type", "image/jpeg") // Zoomify always claims .jpg; decoding sniffs the real format
	w.Write(buf.Bytes())
}

// zoomifyServer serves a Zoomify pyramid at /zoomify/TestMap.
func zoomifyServer(t *testing.T, src *image.RGBA, ts int, flaky func(path string) bool) *httptest.Server {
	W, H := src.Bounds().Dx(), src.Bounds().Dy()
	type tier struct{ w, h, cols, rows, offset int }
	var tiers []tier
	for w, h := W, H; ; w, h = w/2, h/2 {
		tiers = append([]tier{{w: w, h: h, cols: (w + ts - 1) / ts, rows: (h + ts - 1) / ts}}, tiers...)
		if w <= ts && h <= ts {
			break
		}
	}
	total := 0
	for i := range tiers {
		tiers[i].offset = total
		total += tiers[i].cols * tiers[i].rows
	}
	tileRe := regexp.MustCompile(`^/zoomify/TestMap/TileGroup(\d+)/(\d+)-(\d+)-(\d+)\.jpg$`)
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/zoomify/TestMap/ImageProperties.xml":
			fmt.Fprintf(w, `<IMAGE_PROPERTIES WIDTH="%d" HEIGHT="%d" NUMTILES="%d" NUMIMAGES="1" VERSION="1.8" TILESIZE="%d" />`, W, H, total, ts)
		case r.URL.Path == "/viewer.html":
			fmt.Fprint(w, `<html><script>Z.showImage("viewer", "/zoomify/TestMap", "zSkinPath=x");</script></html>`)
		default:
			m := tileRe.FindStringSubmatch(r.URL.Path)
			if m == nil {
				http.NotFound(w, r)
				return
			}
			g, z, c, row := atoi(m[1]), atoi(m[2]), atoi(m[3]), atoi(m[4])
			if z >= len(tiers) {
				http.NotFound(w, r)
				return
			}
			tr := tiers[z]
			if c >= tr.cols || row >= tr.rows || (tr.offset+row*tr.cols+c)/256 != g {
				http.NotFound(w, r)
				return
			}
			if flaky != nil && flaky(r.URL.Path) {
				http.Error(w, "boom", http.StatusInternalServerError)
				return
			}
			lvl := scaled(src, tr.w, tr.h)
			writePNG(w, lvl, image.Rect(c*ts, row*ts, min((c+1)*ts, tr.w), min((row+1)*ts, tr.h)))
		}
	}))
}

// dziServer serves a Deep Zoom pyramid at /img/photo.dzi.
func dziServer(t *testing.T, src *image.RGBA, ts, overlap int) *httptest.Server {
	W, H := src.Bounds().Dx(), src.Bounds().Dy()
	maxLevel := 0
	for 1<<maxLevel < max(W, H) {
		maxLevel++
	}
	tileRe := regexp.MustCompile(`^/img/photo_files/(\d+)/(\d+)_(\d+)\.png$`)
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/img/photo.dzi" {
			fmt.Fprintf(w, `<?xml version="1.0"?><Image xmlns="http://schemas.microsoft.com/deepzoom/2008" TileSize="%d" Overlap="%d" Format="png"><Size Width="%d" Height="%d"/></Image>`, ts, overlap, W, H)
			return
		}
		m := tileRe.FindStringSubmatch(r.URL.Path)
		if m == nil {
			http.NotFound(w, r)
			return
		}
		l, c, row := atoi(m[1]), atoi(m[2]), atoi(m[3])
		s := 1 << (maxLevel - l)
		lw, lh := (W+s-1)/s, (H+s-1)/s
		x0, y0 := c*ts, row*ts
		if x0 >= lw || y0 >= lh {
			http.NotFound(w, r)
			return
		}
		rect := image.Rect(x0-overlap, y0-overlap, x0+ts+overlap, y0+ts+overlap).Intersect(image.Rect(0, 0, lw, lh))
		writePNG(w, scaled(src, lw, lh), rect)
	}))
}

// iiifServer serves an IIIF Image API v3 endpoint at /iiif/scan.
func iiifServer(t *testing.T, src *image.RGBA, ts int) *httptest.Server {
	W, H := src.Bounds().Dx(), src.Bounds().Dy()
	var srv *httptest.Server
	reqRe := regexp.MustCompile(`^/iiif/scan/(\d+),(\d+),(\d+),(\d+)/(\d+),(\d+)/0/default\.jpg$`)
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/iiif/scan/info.json" {
			fmt.Fprintf(w, `{"@context":"http://iiif.io/api/image/3/context.json","id":"%s/iiif/scan","type":"ImageService3","protocol":"http://iiif.io/api/image","width":%d,"height":%d,"tiles":[{"width":%d,"scaleFactors":[1,2,4]}]}`, srv.URL, W, H, ts)
			return
		}
		m := reqRe.FindStringSubmatch(r.URL.Path)
		if m == nil {
			http.NotFound(w, r)
			return
		}
		x, y, rw, rh, sw, sh := atoi(m[1]), atoi(m[2]), atoi(m[3]), atoi(m[4]), atoi(m[5]), atoi(m[6])
		region := image.NewRGBA(image.Rect(0, 0, rw, rh))
		draw.Draw(region, region.Bounds(), src, image.Pt(x, y), draw.Src)
		out := scaled(region, sw, sh)
		writePNG(w, out, out.Bounds())
	}))
	return srv
}

func atoi(s string) int { n, _ := strconv.Atoi(s); return n }

func run(t *testing.T, url string, level int) (*image.RGBA, string) {
	t.Helper()
	dir := t.TempDir()
	out := filepath.Join(dir, "out.png")
	devnull, _ := os.Open(os.DevNull)
	defer devnull.Close()
	err := Run(context.Background(), Options{
		URL: url, Output: out, Level: level, Jobs: 8, Retries: 2, Quality: 90, Plain: true, Out: devnull,
	})
	if err != nil {
		t.Fatalf("Run(%s): %v", url, err)
	}
	return decodePNG(t, out), dir
}

func decodePNG(t *testing.T, path string) *image.RGBA {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	rgba := image.NewRGBA(img.Bounds())
	draw.Draw(rgba, rgba.Bounds(), img, img.Bounds().Min, draw.Src)
	return rgba
}

func assertSame(t *testing.T, got, want *image.RGBA) {
	t.Helper()
	if got.Bounds() != want.Bounds() {
		t.Fatalf("size: got %v, want %v", got.Bounds(), want.Bounds())
	}
	if !bytes.Equal(got.Pix, want.Pix) {
		for y := 0; y < want.Bounds().Dy(); y++ {
			for x := 0; x < want.Bounds().Dx(); x++ {
				if got.RGBAAt(x, y) != want.RGBAAt(x, y) {
					t.Fatalf("pixel (%d,%d): got %v, want %v", x, y, got.RGBAAt(x, y), want.RGBAAt(x, y))
				}
			}
		}
	}
}

func TestZoomify(t *testing.T) {
	src := sourceImage(1100, 700) // 64px tiles → 276 tiles total, so TileGroup1 is exercised
	srv := zoomifyServer(t, src, 64, nil)
	defer srv.Close()

	for _, u := range []string{
		srv.URL + "/zoomify/TestMap/TileGroup0/2-1-1.jpg", // any tile
		srv.URL + "/zoomify/TestMap/ImageProperties.xml",
		srv.URL + "/zoomify/TestMap/", // the folder
		srv.URL + "/viewer.html",      // a page embedding the viewer
	} {
		t.Run(u[len(srv.URL):], func(t *testing.T) {
			got, _ := run(t, u, -1)
			assertSame(t, got, src)
		})
	}

	t.Run("lower level", func(t *testing.T) {
		got, _ := run(t, srv.URL+"/zoomify/TestMap/ImageProperties.xml", -2)
		assertSame(t, got, scaled(src, 550, 350))
	})
}

func TestDZI(t *testing.T) {
	src := sourceImage(1000, 650)
	srv := dziServer(t, src, 126, 1)
	defer srv.Close()
	for _, u := range []string{srv.URL + "/img/photo.dzi", srv.URL + "/img/photo_files/10/3_2.png"} {
		got, _ := run(t, u, -1)
		assertSame(t, got, src)
	}
}

func TestIIIF(t *testing.T) {
	src := sourceImage(900, 1300)
	srv := iiifServer(t, src, 256)
	defer srv.Close()
	got, _ := run(t, srv.URL+"/iiif/scan/info.json", -1)
	assertSame(t, got, src)
	got, _ = run(t, srv.URL+"/iiif/scan/full/max/0/default.jpg", -1)
	assertSame(t, got, src)
}

// A tile that keeps failing becomes a placeholder; rerunning with the same tile
// directory reuses the cached tiles and fetches only the missing one.
func TestResumeAfterFailure(t *testing.T) {
	src := sourceImage(600, 400)
	var broken atomic.Bool
	broken.Store(true)
	var requests atomic.Int32
	srv := zoomifyServer(t, src, 128, func(p string) bool {
		requests.Add(1)
		return broken.Load() && p == "/zoomify/TestMap/TileGroup0/3-2-1.jpg"
	})
	defer srv.Close()

	dir := t.TempDir()
	devnull, _ := os.Open(os.DevNull)
	defer devnull.Close()
	opts := Options{
		URL: srv.URL + "/zoomify/TestMap/ImageProperties.xml", Output: filepath.Join(dir, "m.png"),
		Level: -1, Jobs: 4, Retries: 1, Quality: 90, Plain: true, Out: devnull,
	}
	if err := Run(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	if got := decodePNG(t, opts.Output); bytes.Equal(got.Pix, src.Pix) {
		t.Fatal("expected a placeholder for the broken tile")
	}
	if _, err := os.Stat(filepath.Join(dir, "m.tiles")); err != nil {
		t.Fatal("tiles dir should be kept when tiles are missing")
	}

	broken.Store(false)
	requests.Store(0)
	if err := Run(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	assertSame(t, decodePNG(t, opts.Output), src)
	if n := requests.Load(); n != 1 {
		t.Fatalf("resume fetched %d tiles, want 1", n)
	}
	if _, err := os.Stat(filepath.Join(dir, "m.tiles")); !os.IsNotExist(err) {
		t.Fatal("tiles dir should be cleaned up after a complete stitch")
	}
}

// Package stitch assembles downloaded tiles into one image.
package stitch

import (
	"bufio"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"

	_ "golang.org/x/image/webp"
)

// Piece is one tile file and where it goes on the canvas.
type Piece struct {
	Path   string
	Dst    image.Rectangle
	Offset image.Point // where Dst.Min falls inside the tile image
}

// Stitch decodes pieces with `workers` goroutines and draws them onto a w×h canvas.
// Tiles that are missing or fail to decode are drawn as placeholders and counted
// in `missing`. progress is called once per piece.
func Stitch(ctx context.Context, w, h int, pieces []Piece, workers int, progress func()) (img *image.RGBA, missing int, err error) {
	canvas := image.NewRGBA(image.Rect(0, 0, w, h))
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		next = make(chan Piece)
	)
	for range max(workers, 1) {
		wg.Go(func() {
			for p := range next {
				// Each piece owns a disjoint Dst rectangle, so concurrent draws don't overlap.
				if !drawPiece(canvas, p) {
					placeholder(canvas, p.Dst)
					mu.Lock()
					missing++
					mu.Unlock()
				}
				progress()
			}
		})
	}
	for _, p := range pieces {
		select {
		case next <- p:
		case <-ctx.Done():
			close(next)
			wg.Wait()
			return nil, missing, ctx.Err()
		}
	}
	close(next)
	wg.Wait()
	return canvas, missing, nil
}

func drawPiece(canvas *image.RGBA, p Piece) bool {
	f, err := os.Open(p.Path)
	if err != nil {
		return false
	}
	defer f.Close()
	tile, _, err := image.Decode(bufio.NewReader(f))
	if err != nil {
		return false
	}
	draw.Draw(canvas, p.Dst, tile, tile.Bounds().Min.Add(p.Offset), draw.Src)
	return true
}

var (
	phFill   = image.NewUniform(color.RGBA{0x1c, 0x1c, 0x1c, 0xff})
	phStroke = color.RGBA{0x80, 0x30, 0x30, 0xff}
)

// placeholder marks a missing tile: dark fill with a red border and diagonal cross.
func placeholder(canvas *image.RGBA, r image.Rectangle) {
	draw.Draw(canvas, r, phFill, image.Point{}, draw.Src)
	w, h := r.Dx(), r.Dy()
	for x := 0; x < w; x++ {
		canvas.SetRGBA(r.Min.X+x, r.Min.Y, phStroke)
		canvas.SetRGBA(r.Min.X+x, r.Max.Y-1, phStroke)
	}
	for y := 0; y < h; y++ {
		canvas.SetRGBA(r.Min.X, r.Min.Y+y, phStroke)
		canvas.SetRGBA(r.Max.X-1, r.Min.Y+y, phStroke)
	}
	for i := 0; i < max(w, h); i++ {
		x, y := i*w/max(w, h), i*h/max(w, h)
		canvas.SetRGBA(r.Min.X+x, r.Min.Y+y, phStroke)
		canvas.SetRGBA(r.Max.X-1-x, r.Min.Y+y, phStroke)
	}
}

// Encode writes img to path, choosing JPEG or PNG from the file extension.
func Encode(img image.Image, path string, quality int) error {
	ext := strings.ToLower(filepath.Ext(path))
	b := img.Bounds()
	if (ext == ".jpg" || ext == ".jpeg") && (b.Dx() > 65535 || b.Dy() > 65535) {
		return fmt.Errorf("image is %dx%d; JPEG is limited to 65535px per side, use a .png output", b.Dx(), b.Dy())
	}
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	bw := bufio.NewWriterSize(f, 1<<20)
	switch ext {
	case ".jpg", ".jpeg":
		err = jpeg.Encode(bw, img, &jpeg.Options{Quality: quality})
	case ".png":
		err = (&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(bw, img)
	default:
		err = fmt.Errorf("unsupported output format %q (use .jpg or .png)", ext)
	}
	if err == nil {
		err = bw.Flush()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(path)
	}
	return err
}

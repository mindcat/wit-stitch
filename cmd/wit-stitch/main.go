// Command wit-stitch downloads the tiles of a zoomable image (Zoomify, Deep Zoom or
// IIIF) and stitches them into a single high-resolution image.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/urfave/cli/v3"

	"wit-stitch/internal/app"
)

func main() {
	cmd := &cli.Command{
		Name:      "wit-stitch",
		Usage:     "rebuild a full-resolution image from a zoomable tile viewer",
		ArgsUsage: "<url>",
		Description: "The URL can be a single tile, a metadata file (ImageProperties.xml, .dzi, info.json),\n" +
			"a Zoomify image folder, or a web page that embeds one of those. The highest zoom level\n" +
			"is found automatically and every tile is downloaded in parallel.\n\n" +
			"Examples:\n" +
			"  wit-stitch https://example.com/zoomify/MyMap/TileGroup0/0-0-0.jpg\n" +
			"  wit-stitch --info https://example.com/images/photo.dzi\n" +
			"  wit-stitch -o out/map.png -j 16 https://example.com/iiif/abc/info.json",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "output", Aliases: []string{"o"}, Usage: "output file, .jpg or .png (default: <image name>.jpg)"},
			&cli.IntFlag{Name: "level", Aliases: []string{"l"}, Value: -1, Usage: "zoom level to fetch; negative counts down from the most detailed (-1)"},
			&cli.IntFlag{Name: "jobs", Aliases: []string{"j"}, Value: 8, Usage: "parallel downloads"},
			&cli.IntFlag{Name: "retries", Value: 3, Usage: "retries per tile for transient errors"},
			&cli.IntFlag{Name: "quality", Aliases: []string{"q"}, Value: 92, Usage: "JPEG quality (1-100)"},
			&cli.StringFlag{Name: "tiles-dir", Usage: "where tiles are stored while downloading (default: <output>.tiles)"},
			&cli.BoolFlag{Name: "keep-tiles", Aliases: []string{"k"}, Usage: "keep the downloaded tiles after stitching"},
			&cli.BoolFlag{Name: "info", Aliases: []string{"i"}, Usage: "show the image's zoom levels and exit"},
			&cli.BoolFlag{Name: "plain", Usage: "plain log output instead of the live tile map"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if cmd.NArg() != 1 {
				return cli.Exit("expected exactly one URL (see --help)", 2)
			}
			if q := cmd.Int("quality"); q < 1 || q > 100 {
				return cli.Exit("--quality must be between 1 and 100", 2)
			}
			return app.Run(ctx, app.Options{
				URL:       cmd.Args().First(),
				Output:    cmd.String("output"),
				TilesDir:  cmd.String("tiles-dir"),
				Level:     int(cmd.Int("level")),
				Jobs:      max(int(cmd.Int("jobs")), 1),
				Retries:   max(int(cmd.Int("retries")), 0),
				Quality:   int(cmd.Int("quality")),
				KeepTiles: cmd.Bool("keep-tiles"),
				InfoOnly:  cmd.Bool("info"),
				Plain:     cmd.Bool("plain"),
			})
		},
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := cmd.Run(ctx, os.Args); err != nil {
		fmt.Fprintln(os.Stderr, "wit-stitch:", err)
		os.Exit(1)
	}
}

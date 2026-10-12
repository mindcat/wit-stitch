package wit_stitch

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/urfave/cli/v3"
)

func main() {
	var clean bool
	var url string

	cmd := &cli.Command{
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:        "clean",
				Value:       true,
				Aliases:     []string{"c"},
				Usage:       "Determines whether the program cleans up the image tiles it downloads and uses to build the final image.",
				Destination: &clean,
			},
			&cli.StringFlag{
				Name:        "url",
				Aliases:     []string{"u"},
				Usage:       "Link to the URL wit-stitch will be pulling the tiles from.",
				Destination: &url,
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			// If no url is provided, use a default for demonstration
			if url == "" {
				url = "https://www.geographicus.com/mm5/graphics/00000001/zoomify/PalestineArabic-catholicpress-1948/TileGroup{}/6-{}-{}.jpg"
			}
			
			// Create output directory
			outputDir := "pics/mercator"
			os.MkdirAll(outputDir, 0755)
			
			// Create a simple grid for demo - in a real implementation 
			// you'd parse the url and extract dimensions
			grid := NewTileGrid(22, 32) // Palestine dimensions from Python example
			
			// Start download with concurrency
			var wg sync.WaitGroup
			go func() {
				grid.DisplayGrid()
				grid.DownloadTiles(url, 4, outputDir, &wg) // Max tile value and output directory
			}()
			
			// Allow display to be visible for a moment
			time.Sleep(1 * time.Second)
			
			// Wait for completion
			wg.Wait()
			
			fmt.Println("Download completed, now stitching images...")
			
			// Create output directory for final image
			outDir := "out"
			os.MkdirAll(outDir, 0755)
			
			// Process the images and create final stitched image
			stitcher := NewImageStitcher(outputDir, outDir+"/mercator-map.jpg")
			err := stitcher.ProcessImages()
			if err != nil {
				return err
			}
			
			fmt.Println("Stitched image saved as out/mercator-map.jpg")
			
			// Clean up if requested
			if clean {
				os.RemoveAll(outputDir)
				fmt.Println("Cleaned up temporary files")
			}
			
			return nil
		},
	}
	
	err := cmd.Run(context.Background(), os.Args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

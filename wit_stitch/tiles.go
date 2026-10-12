package wit_stitch

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
)

// TileGrid represents the grid of downloaded tiles
type TileGrid struct {
	Grid   [][]string
	Width  int
	Height int
	mu     sync.Mutex
}

// NewTileGrid creates a new tile grid with specified dimensions
func NewTileGrid(width, height int) *TileGrid {
	grid := make([][]string, height+1)
	for i := range grid {
		grid[i] = make([]string, width+1)
		for j := range grid[i] {
			grid[i][j] = "_"
		}
	}
	
	return &TileGrid{
		Grid:   grid,
		Width:  width,
		Height: height,
	}
}

// UpdateGrid updates the display grid with download status
func (tg *TileGrid) UpdateGrid(i, j int, success bool) {
	tg.mu.Lock()
	defer tg.mu.Unlock()
	
	if success {
		tg.Grid[j][i] = "✓"
	} else {
		tg.Grid[j][i] = "✖"
	}
}

// DisplayGrid prints the current grid state to stdout
func (tg *TileGrid) DisplayGrid() {
	fmt.Print("\033[2J\033[H") // Clear screen and move cursor to home
	fmt.Println("Downloading tiles...")
	for _, row := range tg.Grid {
		for _, cell := range row {
			fmt.Printf("%s ", cell)
		}
		fmt.Println()
	}
}

// DownloadTile downloads a single tile from the given URL 
func (tg *TileGrid) DownloadTile(urlTemplate string, i, j, maxTile int, outputDir string) bool {
	var success bool
	t := 0
	
	for t <= maxTile {
		url := fmt.Sprintf(urlTemplate, t, i, j)
		
		success = tg.attemptDownload(url, i, j, outputDir)
		if success {
			break
		}
		t++
	}
	
	tg.UpdateGrid(i, j, success)
	return success
}

// attemptDownload attempts to download a single tile
func (tg *TileGrid) attemptDownload(url string, i, j int, outputDir string) bool {
	resp, err := http.Get(url)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return false
	}
	
	filename := fmt.Sprintf("6-%d-%d.jpg", i, j)
	filePath := filepath.Join(outputDir, filename)
	
	out, err := os.Create(filePath)
	if err != nil {
		return false
	}
	defer out.Close()
	
	_, err = io.Copy(out, resp.Body)
	return err == nil
}

// DownloadTiles downloads all tiles concurrently using goroutines
func (tg *TileGrid) DownloadTiles(urlTemplate string, maxTile int, outputDir string, wg *sync.WaitGroup) {
	defer wg.Done()
	
	for i := 0; i <= tg.Width; i++ {
		for j := 0; j <= tg.Height; j++ {
			go func(i, j int) {
				tg.DownloadTile(urlTemplate, i, j, maxTile, outputDir)
			}(i, j)
		}
	}
}
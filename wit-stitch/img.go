package wit_stitch

import (
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"sync"
)

// ImageStitcher handles the image stitching process
type ImageStitcher struct {
	InputDir  string
	OutputFile string
}

// NewImageStitcher creates a new image stitcher
func NewImageStitcher(inputDir, outputFile string) *ImageStitcher {
	return &ImageStitcher{
		InputDir:  inputDir,
		OutputFile: outputFile,
	}
}

// ProcessImages processes and stiches all images in the input directory
func (is *ImageStitcher) ProcessImages() error {
	// Parse filenames and sort images by position using regex
	filenamePattern := regexp.MustCompile(`5-(\d+)-(\d+)\.jpg`)
	
	images := make(map[[2]int]string)
	var imagePositions [][3]int // [horizontal, vertical, filename]
	
	// Read directory and process files
	files, err := os.ReadDir(is.InputDir)
	if err != nil {
		return err
	}
	
	for _, file := range files {
		if file.IsDir() {
			continue
		}
		
		name := file.Name()
		matches := filenamePattern.FindStringSubmatch(name)
		
		if len(matches) == 3 {
			hPos, _ := strconv.Atoi(matches[1])
			vPos, _ := strconv.Atoi(matches[2])
			
			images[[2]int{hPos, vPos}] = name
			imagePositions = append(imagePositions, [3]int{hPos, vPos, 0}) // third element unused at this point
		}
	}
	
	// Sort by vertical position first, then horizontal
	for i := 0; i < len(imagePositions)-1; i++ {
		for j := 0; j < len(imagePositions)-1-i; j++ {
			if imagePositions[j][1] > imagePositions[j+1][1] || 
			   (imagePositions[j][1] == imagePositions[j+1][1] && imagePositions[j][0] > imagePositions[j+1][0]) {
				imagePositions[j], imagePositions[j+1] = imagePositions[j+1], imagePositions[j]
			}
		}
	}
	
	maxH := 0
	maxV := 0
	
	for _, pos := range imagePositions {
		if pos[0] > maxH {
			maxH = pos[0]
		}
		if pos[1] > maxV {
			maxV = pos[1]
		}
	}
	
	// Calculate total size
	var imgWidth, imgHeight int
	
	// Get dimensions from first image (assuming all same size)
	if len(imagePositions) > 0 {
		firstFile := imagePositions[0]
		imgPath := filepath.Join(is.InputDir, images[[2]int{firstFile[0], firstFile[1]}])
		
		imgFile, err := os.Open(imgPath)
		if err != nil {
			return err
		}
		defer imgFile.Close()
		
		img, _, err := image.Decode(imgFile)
		if err != nil {
			return err
		}
		
		bounds := img.Bounds()
		imgWidth = bounds.Dx()
		imgHeight = bounds.Dy()
	}
	
	totalWidth := (maxH + 1) * imgWidth
	totalHeight := (maxV + 1) * imgHeight
	
	// Create canvas
	stitchedImage := image.NewRGBA(image.Rect(0, 0, totalWidth, totalHeight))
	
	// Prepare worker pool for concurrent image processing
	var wg sync.WaitGroup
	numWorkers := 4 // Adjust as needed
	
	// Create channel to send processing tasks
	taskChan := make(chan [3]int, len(imagePositions))
	
	// Start workers
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for pos := range taskChan {
				hPos := pos[0]
				vPos := pos[1]
				
				imgPath := filepath.Join(is.InputDir, images[[2]int{hPos, vPos}])
				imgFile, err := os.Open(imgPath)
				if err != nil {
					// Create placeholder image for missing tile
					placeholder := is.createPlaceholderImage(
						fmt.Sprintf("6-%d-%d.jpg", hPos, vPos), 
						imgWidth, 
						imgHeight,
					)
					is.pasteImage(stitchedImage, placeholder, hPos*imgWidth, vPos*imgHeight)
				} else {
					defer imgFile.Close()
					
					img, _, err := image.Decode(imgFile)
					if err != nil {
						// Create placeholder for errors
						placeholder := is.createPlaceholderImage(
							fmt.Sprintf("6-%d-%d.jpg", hPos, vPos), 
							imgWidth, 
							imgHeight,
						)
						is.pasteImage(stitchedImage, placeholder, hPos*imgWidth, vPos*imgHeight)
					} else {
						is.pasteImage(stitchedImage, img, hPos*imgWidth, vPos*imgHeight)
					}
				}
			}
		}()
	}
	
	// Send tasks to channel
	for _, pos := range imagePositions {
		taskChan <- [3]int{pos[0], pos[1], 0}
	}
	close(taskChan)
	
	// Wait for all workers to finish
	wg.Wait()
	
	// Save the stitched image
	outputFile, err := os.Create(is.OutputFile)
	if err != nil {
		return err
	}
	defer outputFile.Close()
	
	return jpeg.Encode(outputFile, stitchedImage, &jpeg.Options{Quality: 90})
}

// pasteImage pastes an image onto the canvas at the specified position
func (is *ImageStitcher) pasteImage(dst *image.RGBA, src image.Image, x, y int) {
	bounds := src.Bounds()
	for j := 0; j < bounds.Dy(); j++ {
		for i := 0; i < bounds.Dx(); i++ {
			if i+x >= 0 && i+x < dst.Rect.Max.X && j+y >= 0 && j+y < dst.Rect.Max.Y {
				dst.Set(i+x, j+y, src.At(bounds.Min.X+i, bounds.Min.Y+j))
			}
		}
	}
}

// createPlaceholderImage creates a placeholder image for missing tiles
func (is *ImageStitcher) createPlaceholderImage(text string, width, height int) *image.RGBA {
	// Create a black background
	placeholder := image.NewRGBA(image.Rect(0, 0, width, height))
	
	// Fill with black
	for i := 0; i < width; i++ {
		for j := 0; j < height; j++ {
			placeholder.Set(i, j, color.RGBA{0, 0, 0, 255})
		}
	}
	
	// Set text to white at center (simplified approach)
	textWidth := len(text) * 8 // Approximate width
	textHeight := 16
	
	x := (width - textWidth) / 2
	y := (height - textHeight) / 2
	
	// Draw some placeholder text using basic pixels
	for i := 0; i < textWidth && x+i < width; i++ {
		for j := 0; j < textHeight && y+j < height; j++ {
			if j == 0 || j == textHeight-1 || i == 0 || i == textWidth-1 {
				placeholder.Set(x+i, y+j, color.RGBA{255, 255, 255, 255})
			}
		}
	}
	
	return placeholder
}
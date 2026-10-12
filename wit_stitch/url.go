package wit_stitch

import (
	"regexp"
)

// CleanURL cleans a URL by removing specific TileGroup patterns and normalizing parameters
func CleanURL(url string) string {
	// Pattern to match the URL structure and extract parts for replacement
	pattern := regexp.MustCompile(`(https://.*/TileGroup)\d+(/)\d+-\d+-\d+(\.jpg)`)
	
	// The replacement pattern that preserves the base URL but removes TileGroup number
	replacement := `$1{}$2{}-{}-{}$3`
	
	// Replace using regex
	cleaned := pattern.ReplaceAllString(url, replacement)
	return cleaned
}
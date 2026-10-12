package source

import "testing"

func TestZoomifyTiers(t *testing.T) {
	// Real values from the OpenLayers Zoomify demo image.
	tiers := zoomifyTiers(4000, 3000, 256, 257)
	want := [][2]int{{1, 1}, {2, 2}, {4, 3}, {8, 6}, {16, 12}}
	if len(tiers) != len(want) {
		t.Fatalf("got %d tiers, want %d", len(tiers), len(want))
	}
	for i, w := range want {
		if tiers[i].cols != w[0] || tiers[i].rows != w[1] {
			t.Errorf("tier %d: got %dx%d tiles, want %dx%d", i, tiers[i].cols, tiers[i].rows, w[0], w[1])
		}
	}

	img, err := parseZoomify("https://x/z/Map", []byte(`<IMAGE_PROPERTIES WIDTH="4000" HEIGHT="3000" NUMTILES="257" TILESIZE="256" />`))
	if err != nil {
		t.Fatal(err)
	}
	// Tile 256 (the last tile overall) is the first one in TileGroup1.
	last := img.Levels[4].Tiles[len(img.Levels[4].Tiles)-1]
	if got := last.URLs[0]; got != "https://x/z/Map/TileGroup1/4-15-11.jpg" {
		t.Errorf("last tile URL = %s", got)
	}
}

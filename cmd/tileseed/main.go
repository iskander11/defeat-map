// Command tileseed does a one-time, rate-limited download of the low-zoom
// OSM overview tiles for Crimea (zoom 6-12) so the app ships with an instant
// offline overview. It is intentionally NOT a bulk scraper: the zoom range
// is capped specifically to stay in the low hundreds/thousands of tiles and
// it runs with a polite delay + identifying User-Agent, per the OSM tile
// usage policy. Deeper zooms are fetched on demand by the app itself as the
// user actually browses, and cached forever from then on.
package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"defeatmap/internal/geo"
	"defeatmap/internal/maptiles"
)

func main() {
	outDir := "assets/tiles/crimea"
	if len(os.Args) > 1 {
		outDir = os.Args[1]
	}
	minZoom, maxZoom := 6, 12

	f := maptiles.NewFetcher(maptiles.DefaultUserAgent)
	total, done := 0, 0
	type job struct{ z, x, y int }
	var jobs []job
	for z := minZoom; z <= maxZoom; z++ {
		minX, minY, maxX, maxY := geo.CrimeaBBox.TileRange(z)
		for x := minX; x <= maxX; x++ {
			for y := minY; y <= maxY; y++ {
				jobs = append(jobs, job{z, x, y})
			}
		}
	}
	total = len(jobs)
	fmt.Printf("Всего тайлов к загрузке (z%d-%d): %d\n", minZoom, maxZoom, total)

	start := time.Now()
	for _, j := range jobs {
		p := filepath.Join(outDir, fmt.Sprint(j.z), fmt.Sprint(j.x), fmt.Sprintf("%d.png", j.y))
		if _, err := os.Stat(p); err == nil {
			done++
			continue
		}
		data, err := f.Fetch(maptiles.DefaultTileURL, j.z, j.x, j.y)
		if err != nil {
			log.Printf("WARN: не удалось загрузить z%d/%d/%d: %v", j.z, j.x, j.y, err)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			log.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0o644); err != nil {
			log.Fatal(err)
		}
		done++
		if done%50 == 0 || done == total {
			fmt.Printf("[%d/%d] прошло %s\n", done, total, time.Since(start).Round(time.Second))
		}
	}
	fmt.Println("Готово.")
}

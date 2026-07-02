// Package geo has slippy-map tile math and the built-in settlement gazetteer.
package geo

import "math"

// LonLatToTile converts geographic coordinates to fractional tile coordinates
// at the given zoom level (standard Web Mercator / OSM slippy map scheme).
func LonLatToTile(lon, lat float64, zoom int) (x, y float64) {
	n := math.Exp2(float64(zoom))
	x = (lon + 180.0) / 360.0 * n
	latRad := lat * math.Pi / 180.0
	y = (1.0 - math.Log(math.Tan(latRad)+1.0/math.Cos(latRad))/math.Pi) / 2.0 * n
	return
}

// TileToLonLat converts fractional tile coordinates back to lon/lat (the
// top-left corner of the tile at integer x,y).
func TileToLonLat(x, y float64, zoom int) (lon, lat float64) {
	n := math.Exp2(float64(zoom))
	lon = x/n*360.0 - 180.0
	latRad := math.Atan(math.Sinh(math.Pi * (1.0 - 2.0*y/n)))
	lat = latRad * 180.0 / math.Pi
	return
}

// DistanceKm returns the great-circle distance between two WGS84 points in
// kilometres (haversine formula).
func DistanceKm(lat1, lon1, lat2, lon2 float64) float64 {
	const earthRadiusKm = 6371.0
	rad := math.Pi / 180
	dLat := (lat2 - lat1) * rad
	dLon := (lon2 - lon1) * rad
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*rad)*math.Cos(lat2*rad)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return earthRadiusKm * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

// BBox is a geographic bounding box.
type BBox struct {
	MinLat, MaxLat, MinLon, MaxLon float64
}

// TileRange returns the inclusive integer tile range covering the bbox at zoom.
func (b BBox) TileRange(zoom int) (minX, minY, maxX, maxY int) {
	x1, y1 := LonLatToTile(b.MinLon, b.MaxLat, zoom) // top-left
	x2, y2 := LonLatToTile(b.MaxLon, b.MinLat, zoom) // bottom-right
	minX, maxX = clampOrder(int(math.Floor(x1)), int(math.Floor(x2)), zoom)
	minY, maxY = clampOrder(int(math.Floor(y1)), int(math.Floor(y2)), zoom)
	return
}

func clampOrder(a, b, zoom int) (int, int) {
	if a > b {
		a, b = b, a
	}
	max := int(math.Exp2(float64(zoom))) - 1
	if a < 0 {
		a = 0
	}
	if b > max {
		b = max
	}
	return a, b
}

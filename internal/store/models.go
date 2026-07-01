package store

import "time"

// Region is a named map area the user tracks (e.g. "Крым").
type Region struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	CenterLat   float64 `json:"centerLat"`
	CenterLon   float64 `json:"centerLon"`
	DefaultZoom int     `json:"defaultZoom"`
	MinLat      float64 `json:"minLat"`
	MaxLat      float64 `json:"maxLat"`
	MinLon      float64 `json:"minLon"`
	MaxLon      float64 `json:"maxLon"`
	// BundleDir is the name of the folder under assets/tiles that ships a
	// pre-seeded low-zoom offline overview for this region, if any.
	BundleDir string `json:"bundleDir,omitempty"`
}

// City is a searchable settlement inside a Region.
type City struct {
	ID       string  `json:"id"`
	RegionID string  `json:"regionId"`
	Name     string  `json:"name"`
	Lat      float64 `json:"lat"`
	Lon      float64 `json:"lon"`
}

// Incident is a single logged instance of damage to an object.
type Incident struct {
	ID          string    `json:"id"`
	RegionID    string    `json:"regionId"`
	City        string    `json:"city"`
	ObjectName  string    `json:"objectName"`
	Date        string    `json:"date"` // YYYY-MM-DD
	Description string    `json:"description,omitempty"`
	Lat         float64   `json:"lat"`
	Lon         float64   `json:"lon"`
	CreatedAt   time.Time `json:"createdAt"`
}

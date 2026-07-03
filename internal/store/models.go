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
	ID          string  `json:"id"`
	RegionID    string  `json:"regionId"`
	City        string  `json:"city"`
	ObjectName  string  `json:"objectName"`
	Date        string  `json:"date"` // YYYY-MM-DD
	Description string  `json:"description,omitempty"`
	Lat         float64 `json:"lat"`
	Lon         float64 `json:"lon"`
	// Color is the marker/card accent color as "#rrggbb"; empty means the
	// default accent color (see parseHexColor in internal/ui).
	Color string `json:"color,omitempty"`
	// HasCallout/CalloutDX/CalloutDY persist a pinned map callout card (see
	// MapWidget's callouts): whether one is currently placed for this
	// incident, and its position as a screen-pixel offset from the marker
	// at the time it was last moved. Restored on the next app launch so
	// placed callouts survive a restart.
	HasCallout bool      `json:"hasCallout,omitempty"`
	CalloutDX  float64   `json:"calloutDx,omitempty"`
	CalloutDY  float64   `json:"calloutDy,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
}

// LayerPoint is one reference point imported from a KML/KMZ file.
type LayerPoint struct {
	Name        string  `json:"name"`
	Description string  `json:"description,omitempty"`
	Lat         float64 `json:"lat"`
	Lon         float64 `json:"lon"`
}

// Layer is a set of imported reference points shown as their own
// toggleable overlay on the map — separate from the user's own incidents,
// and not individually editable; the whole layer is removed at once.
type Layer struct {
	ID       string       `json:"id"`
	RegionID string       `json:"regionId"`
	Name     string       `json:"name"`
	Visible  bool         `json:"visible"`
	Points   []LayerPoint `json:"points"`
}

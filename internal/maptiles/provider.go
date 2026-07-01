package maptiles

import (
	"os"
	"path/filepath"
	"strconv"
)

const (
	// DefaultSource is the standard OSM raster tile layer (includes street
	// labels at all zoom levels where they are meaningful).
	DefaultSource    = "osm"
	DefaultTileURL   = "https://tile.openstreetmap.org/%d/%d/%d.png"
	DefaultUserAgent = "DefeatMapApp/1.0 (+contact: iskandercapital@gmail.com)"
)

// TileSource is one selectable map provider.
type TileSource struct {
	ID      string // used as the on-disk cache folder name
	Name    string // shown in the UI
	URLTmpl string // %d,%d,%d -> z,x,y
	Note    string // shown as a caveat in the UI, if any
}

// TileSources lists the map providers the user can pick between. Only
// OpenStreetMap's tile server has terms that explicitly allow this kind of
// direct, unauthenticated use; Google's and Yandex's raster tile endpoints
// below are the commonly-used "unofficial" URLs seen across many hobby GIS
// tools — they work today for light personal use but are not sanctioned by
// either company's terms of service (no API key/attribution flow) and can
// change or stop working without notice. Prefer OpenStreetMap unless you
// specifically need one of the others.
var TileSources = []TileSource{
	{
		ID:      DefaultSource,
		Name:    "OpenStreetMap",
		URLTmpl: DefaultTileURL,
	},
	{
		ID:      "google",
		Name:    "Google Карты",
		URLTmpl: "https://mt1.google.com/vt/lyrs=m&x=%[2]d&y=%[3]d&z=%[1]d",
		Note:    "Неофициальный доступ к тайлам Google (не по ToS/API) — может перестать работать без предупреждения",
	},
	{
		ID:      "google-sat",
		Name:    "Google Спутник",
		URLTmpl: "https://mt1.google.com/vt/lyrs=s&x=%[2]d&y=%[3]d&z=%[1]d",
		Note:    "Неофициальный доступ к тайлам Google (не по ToS/API) — может перестать работать без предупреждения",
	},
	{
		ID:      "yandex",
		Name:    "Яндекс Карты",
		URLTmpl: "https://vec01.maps.yandex.net/tiles?l=map&v=24.06.15-0&x=%[2]d&y=%[3]d&z=%[1]d&scale=1&lang=ru_RU",
		Note:    "Неофициальный доступ к тайлам Яндекса (не по ToS/API) — версия слоя может устареть, тогда тайлы перестанут грузиться",
	},
}

// FindTileSource looks up a source by ID, falling back to OpenStreetMap.
func FindTileSource(id string) TileSource {
	for _, s := range TileSources {
		if s.ID == id {
			return s
		}
	}
	return TileSources[0]
}

// Provider resolves a tile from, in order: a read-only bundled seed
// directory shipped with the app, the permanent on-disk cache, or the
// network (which is then written to the permanent cache so it never needs
// to be fetched again).
type Provider struct {
	cache      *DiskCache
	fetcher    *Fetcher
	bundleRoot string // optional read-only pre-seeded tiles, e.g. next to the exe
	source     string
	urlTmpl    string
}

func NewProvider(cache *DiskCache, fetcher *Fetcher, bundleRoot, source, urlTmpl string) *Provider {
	return &Provider{cache: cache, fetcher: fetcher, bundleRoot: bundleRoot, source: source, urlTmpl: urlTmpl}
}

// SetBundleRoot switches the read-only pre-seeded tile directory (used when
// the user switches between regions — only some regions ship a bundle).
func (p *Provider) SetBundleRoot(root string) { p.bundleRoot = root }

// SetSource switches the active map provider. Each source has its own disk
// cache folder (by ID) so switching providers never mixes tiles from two
// different maps, and no re-fetching is needed when switching back.
func (p *Provider) SetSource(s TileSource) {
	p.source = s.ID
	p.urlTmpl = s.URLTmpl
}

func (p *Provider) SourceID() string { return p.source }

// GetCached returns a tile immediately if it is already available locally
// (bundled or previously cached), without touching the network.
func (p *Provider) GetCached(z, x, y int) ([]byte, bool) {
	if p.bundleRoot != "" {
		bp := filepath.Join(p.bundleRoot, strconv.Itoa(z), strconv.Itoa(x), strconv.Itoa(y)+".png")
		if b, err := os.ReadFile(bp); err == nil {
			return b, true
		}
	}
	return p.cache.Get(p.source, z, x, y)
}

// RequestAsync fetches a tile over the network in a background goroutine and
// invokes cb on completion (from a background goroutine — the caller is
// responsible for hopping back onto the UI thread). The result is cached to
// disk permanently before cb is invoked.
func (p *Provider) RequestAsync(z, x, y int, cb func(data []byte, err error)) {
	go func() {
		data, err := p.fetcher.Fetch(p.urlTmpl, z, x, y)
		if err != nil {
			cb(nil, err)
			return
		}
		_ = p.cache.Put(p.source, z, x, y, data)
		cb(data, nil)
	}()
}

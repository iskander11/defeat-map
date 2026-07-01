package maptiles

import (
	"os"
	"path/filepath"
	"strconv"
)

const (
	// DefaultSource is the standard OSM raster tile layer (includes street
	// labels at all zoom levels where they are meaningful).
	DefaultSource   = "osm"
	DefaultTileURL  = "https://tile.openstreetmap.org/%d/%d/%d.png"
	DefaultUserAgent = "DefeatMapApp/1.0 (+contact: iskandercapital@gmail.com)"
)

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

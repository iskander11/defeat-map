// Package maptiles provides an on-disk, permanent tile cache with an
// on-demand fetcher. Tiles are only ever requested for the area the user is
// actually looking at (normal interactive use, same as any map app) — there
// is no bulk pre-scraping of the OSM tile server, in line with its usage
// policy (https://operations.osmfoundation.org/policies/tiles/).
package maptiles

import (
	"fmt"
	"os"
	"path/filepath"
)

// DiskCache stores tiles as plain PNG files under root/z/x/y.png. Once a
// tile is written it is never deleted or re-fetched, satisfying "downloads
// once and stays available offline forever".
type DiskCache struct {
	root string
}

func NewDiskCache(root string) *DiskCache {
	return &DiskCache{root: root}
}

func (c *DiskCache) path(source string, z, x, y int) string {
	return filepath.Join(c.root, source, fmt.Sprint(z), fmt.Sprint(x), fmt.Sprintf("%d.png", y))
}

func (c *DiskCache) Get(source string, z, x, y int) ([]byte, bool) {
	b, err := os.ReadFile(c.path(source, z, x, y))
	if err != nil {
		return nil, false
	}
	return b, true
}

func (c *DiskCache) Put(source string, z, x, y int, data []byte) error {
	p := c.path(source, z, x, y)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

func (c *DiskCache) Has(source string, z, x, y int) bool {
	_, err := os.Stat(c.path(source, z, x, y))
	return err == nil
}

package ui

import (
	"fmt"
	"math"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"

	"defeatmap/internal/geo"
	"defeatmap/internal/maptiles"
	"defeatmap/internal/store"
)

const tilePx float32 = 256

type tileKey struct{ z, x, y int }

// MapWidget is a custom interactive slippy map: drag to pan, scroll to zoom
// (anchored under the cursor), tap to drop a pin, colored markers for
// logged incidents, and an OSM attribution badge as required by ODbL.
type MapWidget struct {
	widget.BaseWidget

	mu        sync.Mutex
	provider  *maptiles.Provider
	zoom      int
	minZoom   int
	maxZoom   int
	centerLat float64
	centerLon float64
	size      fyne.Size

	bounds        *geo.BBox
	onOutOfBounds func(lat, lon float64)
	outOfBoundsFired bool

	addPinMode bool
	onPick     func(lat, lon float64)
	onMarkerTap func(id string)

	incidents []store.Incident

	imgCache map[tileKey]*canvas.Image
	loading  map[tileKey]bool
}

func NewMapWidget(provider *maptiles.Provider) *MapWidget {
	m := &MapWidget{
		provider:  provider,
		zoom:      11,
		minZoom:   5,
		maxZoom:   19,
		centerLat: 45.05,
		centerLon: 34.2,
		imgCache:  map[tileKey]*canvas.Image{},
		loading:   map[tileKey]bool{},
	}
	m.ExtendBaseWidget(m)
	return m
}

func (m *MapWidget) CreateRenderer() fyne.WidgetRenderer {
	r := &mapRenderer{widget: m}
	r.rebuild()
	return r
}

// ---- public API ----

func (m *MapWidget) SetView(lat, lon float64, zoom int) {
	m.mu.Lock()
	m.centerLat, m.centerLon, m.zoom = lat, lon, clampInt(zoom, m.minZoom, m.maxZoom)
	m.outOfBoundsFired = false
	m.mu.Unlock()
	m.Refresh()
}

func (m *MapWidget) SetProvider(p *maptiles.Provider) {
	m.mu.Lock()
	m.provider = p
	m.imgCache = map[tileKey]*canvas.Image{}
	m.loading = map[tileKey]bool{}
	m.mu.Unlock()
	m.Refresh()
}

func (m *MapWidget) SetBounds(b *geo.BBox) {
	m.mu.Lock()
	m.bounds = b
	m.outOfBoundsFired = false
	m.mu.Unlock()
}

func (m *MapWidget) SetOnOutOfBounds(cb func(lat, lon float64)) { m.onOutOfBounds = cb }
func (m *MapWidget) SetOnPick(cb func(lat, lon float64))        { m.onPick = cb }
func (m *MapWidget) SetOnMarkerTap(cb func(id string))          { m.onMarkerTap = cb }

func (m *MapWidget) SetAddPinMode(v bool) { m.addPinMode = v }
func (m *MapWidget) AddPinMode() bool     { return m.addPinMode }

func (m *MapWidget) SetIncidents(list []store.Incident) {
	m.mu.Lock()
	m.incidents = list
	m.mu.Unlock()
	m.Refresh()
}

func (m *MapWidget) Zoom() int { return m.zoom }

func (m *MapWidget) ZoomIn()  { m.zoomAt(fyne.NewPos(m.size.Width/2, m.size.Height/2), 1) }
func (m *MapWidget) ZoomOut() { m.zoomAt(fyne.NewPos(m.size.Width/2, m.size.Height/2), -1) }

// ---- geometry helpers ----

func (m *MapWidget) pixelToLonLat(pos fyne.Position) (lon, lat float64) {
	cx, cy := geo.LonLatToTile(m.centerLon, m.centerLat, m.zoom)
	px := cx + float64(pos.X-m.size.Width/2)/float64(tilePx)
	py := cy + float64(pos.Y-m.size.Height/2)/float64(tilePx)
	return geo.TileToLonLat(px, py, m.zoom)
}

func (m *MapWidget) lonLatToPixel(lon, lat float64) fyne.Position {
	cx, cy := geo.LonLatToTile(m.centerLon, m.centerLat, m.zoom)
	tx, ty := geo.LonLatToTile(lon, lat, m.zoom)
	x := float32(tx-cx)*tilePx + m.size.Width/2
	y := float32(ty-cy)*tilePx + m.size.Height/2
	return fyne.NewPos(x, y)
}

func (m *MapWidget) zoomAt(anchor fyne.Position, delta int) {
	m.mu.Lock()
	newZoom := clampInt(m.zoom+delta, m.minZoom, m.maxZoom)
	if newZoom == m.zoom {
		m.mu.Unlock()
		return
	}
	lon, lat := m.pixelToLonLat(anchor)
	nx, ny := geo.LonLatToTile(lon, lat, newZoom)
	ctx := nx - float64(anchor.X-m.size.Width/2)/float64(tilePx)
	cty := ny - float64(anchor.Y-m.size.Height/2)/float64(tilePx)
	m.zoom = newZoom
	m.centerLon, m.centerLat = geo.TileToLonLat(ctx, cty, newZoom)
	m.mu.Unlock()
	m.Refresh()
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// ---- input handling ----

func (m *MapWidget) Dragged(ev *fyne.DragEvent) {
	m.mu.Lock()
	x0, y0 := geo.LonLatToTile(m.centerLon, m.centerLat, m.zoom)
	x0 -= float64(ev.Dragged.DX) / float64(tilePx)
	y0 -= float64(ev.Dragged.DY) / float64(tilePx)
	m.centerLon, m.centerLat = geo.TileToLonLat(x0, y0, m.zoom)
	m.mu.Unlock()
	m.Refresh()
}

func (m *MapWidget) DragEnd() {
	m.checkBounds()
}

func (m *MapWidget) checkBounds() {
	if m.bounds == nil || m.onOutOfBounds == nil {
		return
	}
	if m.centerLat < m.bounds.MinLat || m.centerLat > m.bounds.MaxLat ||
		m.centerLon < m.bounds.MinLon || m.centerLon > m.bounds.MaxLon {
		if !m.outOfBoundsFired {
			m.outOfBoundsFired = true
			m.onOutOfBounds(m.centerLat, m.centerLon)
		}
	} else {
		m.outOfBoundsFired = false
	}
}

func (m *MapWidget) Scrolled(ev *fyne.ScrollEvent) {
	if ev.Scrolled.DY > 0 {
		m.zoomAt(ev.Position, 1)
	} else if ev.Scrolled.DY < 0 {
		m.zoomAt(ev.Position, -1)
	}
}

func (m *MapWidget) Tapped(ev *fyne.PointEvent) {
	if m.addPinMode {
		lon, lat := m.pixelToLonLat(ev.Position)
		if m.onPick != nil {
			m.onPick(lat, lon)
		}
		return
	}
	// hit-test markers (closest within 10px)
	const hit = 10
	var bestID string
	var bestDist float32 = hit + 1
	for _, in := range m.incidents {
		p := m.lonLatToPixel(in.Lon, in.Lat)
		dx := p.X - ev.Position.X
		dy := p.Y - ev.Position.Y
		d := float32(math.Hypot(float64(dx), float64(dy)))
		if d < bestDist {
			bestDist = d
			bestID = in.ID
		}
	}
	if bestID != "" && bestDist <= hit && m.onMarkerTap != nil {
		m.onMarkerTap(bestID)
	}
	m.checkBounds()
}

// ---- renderer ----

type mapRenderer struct {
	widget  *MapWidget
	objects []fyne.CanvasObject
}

func (r *mapRenderer) Layout(size fyne.Size) {
	r.widget.mu.Lock()
	r.widget.size = size
	r.widget.mu.Unlock()
	r.rebuild()
}

func (r *mapRenderer) MinSize() fyne.Size { return fyne.NewSize(320, 240) }
func (r *mapRenderer) Refresh()           { r.rebuild() }
func (r *mapRenderer) Objects() []fyne.CanvasObject { return r.objects }
func (r *mapRenderer) Destroy()           {}

func (r *mapRenderer) rebuild() {
	w := r.widget
	w.mu.Lock()
	size := w.size
	zoom := w.zoom
	centerLon, centerLat := w.centerLon, w.centerLat
	provider := w.provider
	incidents := append([]store.Incident{}, w.incidents...)
	w.mu.Unlock()

	if size.Width <= 0 || size.Height <= 0 || provider == nil {
		r.objects = nil
		return
	}

	bg := canvas.NewRectangle(colBackground)
	bg.Resize(size)
	objects := []fyne.CanvasObject{bg}

	cx, cy := geo.LonLatToTile(centerLon, centerLat, zoom)
	n := int(math.Exp2(float64(zoom)))

	tilesAcross := int(size.Width/tilePx) + 3
	tilesDown := int(size.Height/tilePx) + 3
	startTX := int(math.Floor(cx)) - tilesAcross/2
	startTY := int(math.Floor(cy)) - tilesDown/2

	for dx := 0; dx <= tilesAcross; dx++ {
		for dy := 0; dy <= tilesDown; dy++ {
			tx := startTX + dx
			ty := startTY + dy
			if ty < 0 || ty >= n {
				continue
			}
			wrappedX := ((tx % n) + n) % n

			screenX := (float32(tx)-float32(cx))*tilePx + size.Width/2
			screenY := (float32(ty)-float32(cy))*tilePx + size.Height/2

			obj := r.tileObject(zoom, wrappedX, ty)
			obj.Resize(fyne.NewSize(tilePx+1, tilePx+1))
			obj.Move(fyne.NewPos(screenX, screenY))
			objects = append(objects, obj)
		}
	}

	// markers
	for _, in := range incidents {
		tx, ty := geo.LonLatToTile(in.Lon, in.Lat, zoom)
		screenX := (float32(tx)-float32(cx))*tilePx + size.Width/2
		screenY := (float32(ty)-float32(cy))*tilePx + size.Height/2
		if screenX < -20 || screenX > size.Width+20 || screenY < -20 || screenY > size.Height+20 {
			continue
		}
		pin := canvas.NewCircle(colPrimary)
		pin.StrokeColor = colForeground
		pin.StrokeWidth = 1.5
		const r2 = 7
		pin.Resize(fyne.NewSize(r2*2, r2*2))
		pin.Move(fyne.NewPos(screenX-r2, screenY-r2))
		objects = append(objects, pin)
	}

	// attribution (required by OpenStreetMap / ODbL)
	attrBg := canvas.NewRectangle(colButton)
	attrBg.Resize(fyne.NewSize(190, 18))
	attrBg.Move(fyne.NewPos(4, size.Height-22))
	attrTxt := canvas.NewText("© OpenStreetMap contributors", colForeground)
	attrTxt.TextSize = 10
	attrTxt.Move(fyne.NewPos(8, size.Height-21))
	objects = append(objects, attrBg, attrTxt)

	r.objects = objects
}

// tileObject returns the cached tile image if already decoded, otherwise a
// placeholder rectangle while a background fetch is kicked off (at most
// once per tile key).
func (r *mapRenderer) tileObject(z, x, y int) fyne.CanvasObject {
	w := r.widget
	key := tileKey{z, x, y}

	w.mu.Lock()
	if img, ok := w.imgCache[key]; ok {
		w.mu.Unlock()
		return img
	}
	w.mu.Unlock()

	if data, ok := w.provider.GetCached(z, x, y); ok {
		img := canvas.NewImageFromResource(fyne.NewStaticResource(fmt.Sprintf("tile-%d-%d-%d.png", z, x, y), data))
		img.FillMode = canvas.ImageFillStretch
		img.ScaleMode = canvas.ImageScaleFastest
		w.mu.Lock()
		w.imgCache[key] = img
		w.mu.Unlock()
		return img
	}

	w.mu.Lock()
	alreadyLoading := w.loading[key]
	w.loading[key] = true
	w.mu.Unlock()

	if !alreadyLoading {
		provider := w.provider
		provider.RequestAsync(z, x, y, func(data []byte, err error) {
			fyne.Do(func() {
				w.mu.Lock()
				delete(w.loading, key)
				if err == nil {
					img := canvas.NewImageFromResource(fyne.NewStaticResource(fmt.Sprintf("tile-%d-%d-%d.png", z, x, y), data))
					img.FillMode = canvas.ImageFillStretch
					img.ScaleMode = canvas.ImageScaleFastest
					w.imgCache[key] = img
				}
				w.mu.Unlock()
				if err == nil {
					w.Refresh()
				}
			})
		})
	}

	placeholder := canvas.NewRectangle(colButton)
	placeholder.StrokeColor = colSeparator
	placeholder.StrokeWidth = 0.5
	return placeholder
}

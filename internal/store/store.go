// Package store provides simple JSON-file persistence for regions, cities
// and incidents. No database engine is used so the app builds and runs
// without any CGO dependency beyond what Fyne itself already requires.
package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

type data struct {
	Regions   []Region   `json:"regions"`
	Cities    []City     `json:"cities"`
	Incidents []Incident `json:"incidents"`
	Layers    []Layer    `json:"layers,omitempty"`
}

// Store is a thread-safe, disk-backed store for the app's data.
type Store struct {
	mu   sync.RWMutex
	path string
	d    data
}

// AppDataDir returns the per-user directory where all mutable app state
// (database file + tile cache) lives, e.g. %APPDATA%\DefeatMap.
func AppDataDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "DefeatMap")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// Open loads the store from disk, creating an empty one if it doesn't exist yet.
func Open(path string) (*Store, error) {
	s := &Store{path: path}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}
	if len(b) == 0 {
		return s, nil
	}
	if err := json.Unmarshal(b, &s.d); err != nil {
		return nil, fmt.Errorf("parse store: %w", err)
	}
	return s, nil
}

// save persists the store atomically (write to temp file, then rename).
func (s *Store) save() error {
	b, err := json.MarshalIndent(s.d, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// ---- Regions ----

func (s *Store) Regions() []Region {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Region, len(s.d.Regions))
	copy(out, s.d.Regions)
	return out
}

func (s *Store) AddRegion(r Region) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.d.Regions {
		if existing.ID == r.ID {
			return fmt.Errorf("регион с таким id уже существует")
		}
	}
	s.d.Regions = append(s.d.Regions, r)
	return s.save()
}

// ---- Cities ----

func (s *Store) CitiesByRegion(regionID string) []City {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []City
	for _, c := range s.d.Cities {
		if c.RegionID == regionID {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (s *Store) AddCity(c City) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.d.Cities = append(s.d.Cities, c)
	return s.save()
}

// EnsureCity adds a city if a city with the same (case-insensitive) name
// doesn't already exist in the region, returning its record either way.
func (s *Store) EnsureCity(regionID, name string, lat, lon float64) (City, error) {
	s.mu.Lock()
	for _, c := range s.d.Cities {
		if c.RegionID == regionID && equalFold(c.Name, name) {
			s.mu.Unlock()
			return c, nil
		}
	}
	c := City{ID: newID(), RegionID: regionID, Name: name, Lat: lat, Lon: lon}
	s.d.Cities = append(s.d.Cities, c)
	err := s.save()
	s.mu.Unlock()
	return c, err
}

// ---- Incidents ----

func (s *Store) Incidents() []Incident {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Incident, len(s.d.Incidents))
	copy(out, s.d.Incidents)
	sort.Slice(out, func(i, j int) bool { return out[i].Date > out[j].Date })
	return out
}

func (s *Store) IncidentsByRegion(regionID string) []Incident {
	all := s.Incidents()
	var out []Incident
	for _, in := range all {
		if in.RegionID == regionID {
			out = append(out, in)
		}
	}
	return out
}

func (s *Store) AddIncident(in Incident) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if in.ID == "" {
		in.ID = newID()
	}
	s.d.Incidents = append(s.d.Incidents, in)
	return s.save()
}

func (s *Store) UpdateIncident(in Incident) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, existing := range s.d.Incidents {
		if existing.ID == in.ID {
			s.d.Incidents[i] = in
			return s.save()
		}
	}
	return fmt.Errorf("происшествие не найдено")
}

// SetIncidentCallout persists a pinned map callout card's position for an
// incident (hasCallout=true) or clears it (false), independent of editing
// the incident's other fields — called whenever the user drags a callout
// into place or removes it, so it survives an app restart.
func (s *Store) SetIncidentCallout(id string, hasCallout bool, dx, dy float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, existing := range s.d.Incidents {
		if existing.ID == id {
			s.d.Incidents[i].HasCallout = hasCallout
			s.d.Incidents[i].CalloutDX = dx
			s.d.Incidents[i].CalloutDY = dy
			return s.save()
		}
	}
	return nil
}

func (s *Store) DeleteIncident(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, existing := range s.d.Incidents {
		if existing.ID == id {
			s.d.Incidents = append(s.d.Incidents[:i], s.d.Incidents[i+1:]...)
			return s.save()
		}
	}
	return nil
}

// ---- Layers (imported KML/KMZ reference points) ----

func (s *Store) LayersByRegion(regionID string) []Layer {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []Layer
	for _, l := range s.d.Layers {
		if l.RegionID == regionID {
			out = append(out, l)
		}
	}
	return out
}

// AddLayer stores a newly-imported layer, assigning it an ID if it doesn't
// have one yet.
func (s *Store) AddLayer(l Layer) (Layer, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if l.ID == "" {
		l.ID = newID()
	}
	s.d.Layers = append(s.d.Layers, l)
	return l, s.save()
}

func (s *Store) SetLayerVisible(id string, visible bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, existing := range s.d.Layers {
		if existing.ID == id {
			s.d.Layers[i].Visible = visible
			return s.save()
		}
	}
	return fmt.Errorf("слой не найден")
}

func (s *Store) DeleteLayer(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, existing := range s.d.Layers {
		if existing.ID == id {
			s.d.Layers = append(s.d.Layers[:i], s.d.Layers[i+1:]...)
			return s.save()
		}
	}
	return nil
}

func equalFold(a, b string) bool {
	if len(a) != len(b) {
		return foldLower(a) == foldLower(b)
	}
	return foldLower(a) == foldLower(b)
}

func foldLower(s string) string {
	r := []rune(s)
	for i, c := range r {
		if c >= 'A' && c <= 'Z' {
			r[i] = c + 32
		} else if c >= 'А' && c <= 'Я' {
			r[i] = c + 32
		} else if c == 'Ё' {
			r[i] = 'ё'
		}
	}
	return string(r)
}

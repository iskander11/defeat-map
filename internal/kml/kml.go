// Package kml has a minimal KML/KMZ reader and writer — just enough to
// import point Placemarks from a KML/KMZ file as a map layer, and export
// incidents back out as one. It intentionally does not attempt to handle
// the full KML spec (LineStrings, Polygons, styles, ground overlays, etc.)
// since this app only ever deals in point locations.
package kml

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
)

// Point is one Placemark with Point geometry, parsed from or destined for
// a KML/KMZ file.
type Point struct {
	Name        string
	Description string
	Lat, Lon    float64
}

type kmlRoot struct {
	Document kmlFolder `xml:"Document"`
}

// kmlFolder covers both <Document> and <Folder>, which share the same
// possible children — Placemarks and further nested Folders.
type kmlFolder struct {
	Placemarks []kmlPlacemark `xml:"Placemark"`
	Folders    []kmlFolder    `xml:"Folder"`
}

type kmlPlacemark struct {
	Name        string    `xml:"name"`
	Description string    `xml:"description"`
	Point       *kmlPoint `xml:"Point"`
}

type kmlPoint struct {
	Coordinates string `xml:"coordinates"`
}

// ParseKML parses raw KML XML and returns every Placemark that has Point
// geometry (Placemarks with other geometry types, e.g. LineString or
// Polygon, are skipped), walking nested <Folder> elements recursively.
func ParseKML(data []byte) ([]Point, error) {
	var root kmlRoot
	if err := xml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("разбор KML: %w", err)
	}
	var points []Point
	collectPoints(root.Document, &points)
	return points, nil
}

func collectPoints(f kmlFolder, out *[]Point) {
	for _, pm := range f.Placemarks {
		if pm.Point == nil {
			continue
		}
		lat, lon, ok := parseCoordinates(pm.Point.Coordinates)
		if !ok {
			continue
		}
		*out = append(*out, Point{
			Name:        strings.TrimSpace(pm.Name),
			Description: strings.TrimSpace(pm.Description),
			Lat:         lat,
			Lon:         lon,
		})
	}
	for _, sub := range f.Folders {
		collectPoints(sub, out)
	}
}

// parseCoordinates parses a KML "lon,lat[,alt]" coordinate tuple (KML
// lists longitude first). Only the first tuple is used if there are
// several (e.g. a stray multi-point Placemark) since this app only
// tracks single-point locations.
func parseCoordinates(s string) (lat, lon float64, ok bool) {
	s = strings.TrimSpace(s)
	if fields := strings.Fields(s); len(fields) > 0 {
		s = fields[0]
	}
	parts := strings.Split(s, ",")
	if len(parts) < 2 {
		return 0, 0, false
	}
	lonF, err1 := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	latF, err2 := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return latF, lonF, true
}

// ParseKMZ opens a KMZ (a zip archive) and parses the first ".kml" entry
// found inside it.
func ParseKMZ(data []byte) ([]Point, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("разбор KMZ (zip): %w", err)
	}
	for _, f := range zr.File {
		if !strings.EqualFold(path.Ext(f.Name), ".kml") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("чтение %s из KMZ: %w", f.Name, err)
		}
		raw, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, fmt.Errorf("чтение %s из KMZ: %w", f.Name, err)
		}
		return ParseKML(raw)
	}
	return nil, fmt.Errorf("в KMZ-архиве не найден файл .kml")
}

// BuildKML generates a minimal, valid KML document with one Placemark per
// point.
func BuildKML(points []Point) []byte {
	var b bytes.Buffer
	b.WriteString(xml.Header)
	b.WriteString("<kml xmlns=\"http://www.opengis.net/kml/2.2\">\n<Document>\n")
	for _, p := range points {
		b.WriteString("  <Placemark>\n    <name>")
		xml.EscapeText(&b, []byte(p.Name))
		b.WriteString("</name>\n    <description>")
		xml.EscapeText(&b, []byte(p.Description))
		fmt.Fprintf(&b, "</description>\n    <Point><coordinates>%f,%f,0</coordinates></Point>\n  </Placemark>\n", p.Lon, p.Lat)
	}
	b.WriteString("</Document>\n</kml>\n")
	return b.Bytes()
}

// BuildKMZ zips a generated KML document as "doc.kml", the conventional
// name most KMZ readers (including Google Earth) look for first.
func BuildKMZ(points []Point) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("doc.kml")
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(BuildKML(points)); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

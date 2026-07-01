package geo

import (
	"fmt"
	"math"
)

// FormatDecimal renders WGS84 decimal degrees, e.g. "45.123456, 34.123456".
func FormatDecimal(lat, lon float64) string {
	return fmt.Sprintf("%.6f, %.6f", lat, lon)
}

// FormatDMS renders WGS84 degrees-minutes-seconds, e.g.
// `45°07'24.4"N 34°07'24.4"E`.
func FormatDMS(lat, lon float64) string {
	return fmt.Sprintf("%s %s", dms(lat, "N", "S"), dms(lon, "E", "W"))
}

func dms(v float64, pos, neg string) string {
	hemi := pos
	if v < 0 {
		hemi = neg
		v = -v
	}
	deg := math.Floor(v)
	minFull := (v - deg) * 60
	min := math.Floor(minFull)
	sec := (minFull - min) * 60
	return fmt.Sprintf("%d°%02d'%04.1f\"%s", int(deg), int(min), sec, hemi)
}

// UTMZone returns the standard UTM zone number for a longitude.
func UTMZone(lon float64) int {
	return int(math.Floor((lon+180)/6)) + 1
}

// ToUTM converts WGS84 lat/lon to UTM easting/northing (meters) using the
// standard Snyder forward-projection formulas, the same ones used by GPS
// receivers and civilian GIS software worldwide.
func ToUTM(lat, lon float64) (zone int, easting, northing float64, north bool) {
	const a = 6378137.0
	const f = 1 / 298.257223563
	e2 := 2*f - f*f
	e2p := e2 / (1 - e2)
	const k0 = 0.9996

	zone = UTMZone(lon)
	lon0 := float64(zone-1)*6 - 180 + 3

	latR := lat * math.Pi / 180
	lonR := lon * math.Pi / 180
	lon0R := lon0 * math.Pi / 180

	N := a / math.Sqrt(1-e2*math.Sin(latR)*math.Sin(latR))
	T := math.Tan(latR) * math.Tan(latR)
	C := e2p * math.Cos(latR) * math.Cos(latR)
	Adiff := math.Cos(latR) * (lonR - lon0R)

	M := a * ((1-e2/4-3*e2*e2/64-5*e2*e2*e2/256)*latR -
		(3*e2/8+3*e2*e2/32+45*e2*e2*e2/1024)*math.Sin(2*latR) +
		(15*e2*e2/256+45*e2*e2*e2/1024)*math.Sin(4*latR) -
		(35*e2*e2*e2/3072)*math.Sin(6*latR))

	easting = k0*N*(Adiff+(1-T+C)*math.Pow(Adiff, 3)/6+
		(5-18*T+T*T+72*C-58*e2p)*math.Pow(Adiff, 5)/120) + 500000

	northing = k0 * (M + N*math.Tan(latR)*(math.Pow(Adiff, 2)/2+
		(5-T+9*C+4*C*C)*math.Pow(Adiff, 4)/24+
		(61-58*T+T*T+600*C-330*e2p)*math.Pow(Adiff, 6)/720))

	north = lat >= 0
	if !north {
		northing += 10000000
	}
	return
}

// FormatUTM renders a UTM coordinate, e.g. "36N 512345E 4988123N".
func FormatUTM(lat, lon float64) string {
	zone, e, n, north := ToUTM(lat, lon)
	hemi := "N"
	if !north {
		hemi = "S"
	}
	return fmt.Sprintf("%d%s  %.0fE %.0fN", zone, hemi, e, n)
}

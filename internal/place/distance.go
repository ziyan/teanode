package place

// DistanceMeters is the great circle distance between two points in meters.
// A place's radius, how far a search result is from where the person is, and
// the order of a list of results are all measured with it, so that they agree.
func DistanceMeters(firstLatitude, firstLongitude, secondLatitude, secondLongitude float64) float64 {
	return distanceKilometers(firstLatitude, firstLongitude, secondLatitude, secondLongitude) * 1000
}

// IsOnEarth reports whether the coordinates are ones a position can have. A
// value that is not a number is not on the earth: every comparison with it is
// false.
func IsOnEarth(latitudeDegrees, longitudeDegrees float64) bool {
	return latitudeDegrees >= -90 && latitudeDegrees <= 90 && longitudeDegrees >= -180 && longitudeDegrees <= 180
}

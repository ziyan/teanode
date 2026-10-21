//go:build race

package db_test

// The race detector slows every Go loop several times over, so a timing test
// would measure the detector.
const isRaceDetectorOn = true

//go:build race

package picture

// The race detector slows pixel loops by an order of magnitude, so the timing
// test would measure the detector.
const isRaceDetectorOn = true

//go:build race

package review

// raceEnabled reports that the race detector is instrumenting this build, which
// multiplies every timing by enough to make a wall-clock budget meaningless.
const raceEnabled = true

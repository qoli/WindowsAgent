package actionosd

import (
	"errors"
	"fmt"
)

// StartupCursor selects the explicit lower bound for bounded OSD reconstruction.
func StartupCursor(last, minimum uint64, replayLimit int) (uint64, error) {
	if replayLimit <= 0 {
		return 0, errors.New("OSD startup replay limit must be positive")
	}
	if minimum > last {
		return 0, fmt.Errorf("minimum event cursor %d is after current last sequence %d", minimum, last)
	}
	after := uint64(0)
	limit := uint64(replayLimit)
	if last > limit {
		after = last - limit
	}
	if minimum > after {
		after = minimum
	}
	return after, nil
}

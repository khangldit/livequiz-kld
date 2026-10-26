package game

import (
	"math"
	"time"
)

const MaxPoints = 1000

// Score tính điểm cho 1 câu trả lời:
// đúng ngay lập tức = 1000, đúng ở giây cuối = 500, sai = 0.
func Score(correct bool, elapsed, limit time.Duration) int {
	if !correct || limit <= 0 {
		return 0
	}
	elapsed = min(max(elapsed, 0), limit) // kẹp elapsed vào [0, limit]
	ratio := float64(elapsed) / float64(limit)
	return int(math.Round(MaxPoints * (1 - ratio/2)))
}
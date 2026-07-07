package ui

import "strings"

var sparkRunes = []rune("▁▂▃▄▅▆▇█")

// sparkline renders values as a unicode braille-ish sparkline of given width.
// It samples/aggregates to fit width and scales to the value range.
func sparkline(values []float64, width int) string {
	if width <= 0 || len(values) == 0 {
		return strings.Repeat(" ", max(0, width))
	}
	// Downsample to width buckets (average).
	buckets := make([]float64, width)
	if len(values) <= width {
		// Right-align actual values, pad left with the first value.
		offset := width - len(values)
		for i := 0; i < offset; i++ {
			buckets[i] = values[0]
		}
		copy(buckets[offset:], values)
	} else {
		per := float64(len(values)) / float64(width)
		for i := 0; i < width; i++ {
			start := int(float64(i) * per)
			end := int(float64(i+1) * per)
			if end > len(values) {
				end = len(values)
			}
			if end <= start {
				end = start + 1
			}
			var sum float64
			for j := start; j < end && j < len(values); j++ {
				sum += values[j]
			}
			buckets[i] = sum / float64(end-start)
		}
	}
	mn, mx := buckets[0], buckets[0]
	for _, v := range buckets {
		if v < mn {
			mn = v
		}
		if v > mx {
			mx = v
		}
	}
	span := mx - mn
	var b strings.Builder
	for _, v := range buckets {
		idx := 0
		if span > 0 {
			idx = int((v - mn) / span * float64(len(sparkRunes)-1))
		}
		if idx < 0 {
			idx = 0
		}
		if idx >= len(sparkRunes) {
			idx = len(sparkRunes) - 1
		}
		b.WriteRune(sparkRunes[idx])
	}
	return b.String()
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

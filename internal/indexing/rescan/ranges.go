package rescan

import (
	"fmt"
	"sort"
)

// Range represents a block range.
type Range struct {
	Start uint64
	End   uint64
}

// String returns the range in "start-end" format.
func (r Range) String() string {
	return fmt.Sprintf("%d-%d", r.Start, r.End)
}

// Size returns the number of blocks in the range.
func (r Range) Size() uint64 {
	return r.End - r.Start + 1
}

// Split splits the range into chunks of maxSize.
func (r Range) Split(maxSize uint64) []Range {
	if r.Size() <= maxSize {
		return []Range{r}
	}

	var chunks []Range
	current := r.Start

	for current <= r.End {
		chunkEnd := min(current+maxSize-1, r.End)
		chunks = append(chunks, Range{Start: current, End: chunkEnd})
		current = chunkEnd + 1
	}

	return chunks
}

// Overlaps checks if two ranges overlap or are adjacent.
func (r Range) Overlaps(other Range) bool {
	// Adjacent or overlapping
	return r.Start <= other.End+1 && other.Start <= r.End+1
}

// Merge merges two overlapping/adjacent ranges.
func (r Range) Merge(other Range) Range {
	start := r.Start
	if other.Start < start {
		start = other.Start
	}
	end := r.End
	if other.End > end {
		end = other.End
	}
	return Range{Start: start, End: end}
}

// MergeRanges merges overlapping and adjacent ranges.
func MergeRanges(ranges []Range) []Range {
	if len(ranges) <= 1 {
		return ranges
	}

	// Sort by start
	sort.Slice(ranges, func(i, j int) bool {
		return ranges[i].Start < ranges[j].Start
	})

	merged := []Range{ranges[0]}

	for i := 1; i < len(ranges); i++ {
		last := &merged[len(merged)-1]
		current := ranges[i]

		if last.Overlaps(current) {
			*last = last.Merge(current)
		} else {
			merged = append(merged, current)
		}
	}

	return merged
}

// ParseRange parses a "start-end" string into a Range.
func ParseRange(s string) (Range, error) {
	var start, end uint64
	_, err := fmt.Sscanf(s, "%d-%d", &start, &end)
	if err != nil {
		return Range{}, fmt.Errorf("invalid range format: %s", s)
	}
	if start > end {
		return Range{}, fmt.Errorf("start > end: %d > %d", start, end)
	}
	return Range{Start: start, End: end}, nil
}

// RangesFromStrings parses multiple range strings.
func RangesFromStrings(strs []string) ([]Range, error) {
	ranges := make([]Range, 0, len(strs))
	for _, s := range strs {
		r, err := ParseRange(s)
		if err != nil {
			return nil, err
		}
		ranges = append(ranges, r)
	}
	return ranges, nil
}

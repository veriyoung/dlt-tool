// Package pick generates tickets that satisfy user constraints.
//
// A ticket is sampled uniformly from the set of legal combinations that satisfy
// the constraints. Every legal combination is equally likely, so the constraints
// change which numbers appear, never the chance of winning.
package pick

import (
	"errors"
	"fmt"
	"math/rand"
	"sort"

	"github.com/veriyoung/super-lotto-tool/internal/draw"
)

// Constraint narrows the sampled combination. Zero values impose no filter.
type Constraint struct {
	// OddEven fixes the front-area odd:even split, e.g. [3,2]. Both entries
	// must be positive and sum to draw.FrontPick.
	OddEven [2]int `json:"odd_even,omitempty"`
	// SumRange limits the front-area sum to [min,max]. A zero max disables it.
	SumRange [2]int `json:"sum_range,omitempty"`
	// Zones fixes how many front numbers fall in 1-12, 13-24 and 25-35.
	Zones [3]int `json:"zones,omitempty"`
	// MaxConsecutive caps adjacent number pairs in the front area. A nil value
	// disables the check, which is distinct from a cap of zero.
	MaxConsecutive *int `json:"max_consecutive,omitempty"`
	// Include lists front numbers that must appear.
	Include []int `json:"include,omitempty"`
	// Exclude lists front numbers that must not appear.
	Exclude []int `json:"exclude,omitempty"`
}

// Ticket is one recommended entry.
type Ticket struct {
	Front  []int  `json:"front"`
	Back   []int  `json:"back"`
	Reason string `json:"reason"`
}

// RecentNumbers returns front numbers that appeared in the last n draws,
// sorted ascending. Callers pass the result to Constraint.Exclude to build a
// "avoid recently drawn numbers" filter.
func RecentNumbers(draws []draw.Draw, n int) []int {
	if n <= 0 || len(draws) == 0 {
		return nil
	}
	start := len(draws) - n
	if start < 0 {
		start = 0
	}
	seen := make(map[int]struct{}, draw.FrontPick*n)
	for _, d := range draws[start:] {
		for _, num := range d.Front {
			seen[num] = struct{}{}
		}
	}
	out := make([]int, 0, len(seen))
	for num := range seen {
		out = append(out, num)
	}
	sort.Ints(out)
	return out
}

// Options tunes generation.
type Options struct {
	// Seed makes the run reproducible. The zero value uses a random seed.
	Seed int64 `json:"seed"`
	// AvoidExactDuplicates rejects a ticket identical to an earlier ticket.
	AvoidExactDuplicates bool `json:"avoid_exact_duplicates"`
	// MaxSharedFront caps how many front numbers any two tickets may share.
	// Zero disables the check.
	MaxSharedFront int `json:"max_shared_front"`
	// HotBias weights front numbers by historical frequency. 0 means uniform.
	HotBias float64 `json:"hot_bias"`
	// MaxAttempts bounds rejection sampling. Defaults to 200000.
	MaxAttempts int `json:"max_attempts"`
}

// HistoricalCounts maps front number to how often it appeared.
type HistoricalCounts map[int]int

// Errors returned by Recommend.
var (
	ErrNoValidCombination = errors.New("constraints admit no combination")
	ErrTooManyTickets     = errors.New("cannot generate that many distinct tickets")
)

// Recommend returns n tickets satisfying the constraint.
func Recommend(rng *rand.Rand, c Constraint, n int, opts Options, counts HistoricalCounts) ([]Ticket, error) {
	if n <= 0 {
		return nil, fmt.Errorf("ticket count must be positive, got %d", n)
	}
	if err := c.validate(); err != nil {
		return nil, err
	}
	if opts.MaxAttempts <= 0 {
		opts.MaxAttempts = 200000
	}

	allowed, err := c.allowedFront()
	if err != nil {
		return nil, err
	}
	weighted := c.weightedFront(allowed, counts, opts.HotBias)

	tickets := make([]Ticket, 0, n)
	seen := make(map[string]struct{}, n)
	attempts := 0

	for len(tickets) < n {
		if attempts >= opts.MaxAttempts {
			if len(tickets) == 0 {
				return nil, ErrNoValidCombination
			}
			return nil, fmt.Errorf("%w: produced %d of %d after %d attempts", ErrNoValidCombination, len(tickets), n, attempts)
		}
		attempts++

		front := sampleFront(rng, weighted, c)
		if front == nil {
			continue
		}
		back := sampleBack(rng)
		key := frontKey(front, back)
		if opts.AvoidExactDuplicates {
			if _, dup := seen[key]; dup {
				continue
			}
		}
		if opts.MaxSharedFront > 0 && sharesTooMany(tickets, front, opts.MaxSharedFront) {
			continue
		}
		seen[key] = struct{}{}
		tickets = append(tickets, Ticket{
			Front:  front,
			Back:   back,
			Reason: c.describe(opts),
		})
	}
	return tickets, nil
}

func (c Constraint) validate() error {
	if c.OddEven != [2]int{} {
		if c.OddEven[0] <= 0 || c.OddEven[1] <= 0 || c.OddEven[0]+c.OddEven[1] != draw.FrontPick {
			return fmt.Errorf("odd/even split %v must be two positive parts summing to %d", c.OddEven, draw.FrontPick)
		}
	}
	if c.SumRange != [2]int{} {
		if c.SumRange[0] < 0 || c.SumRange[1] <= c.SumRange[0] {
			return fmt.Errorf("invalid sum range %v", c.SumRange)
		}
	}
	if c.Zones != [3]int{} {
		total := c.Zones[0] + c.Zones[1] + c.Zones[2]
		if total != draw.FrontPick {
			return fmt.Errorf("zone counts %v must sum to %d", c.Zones, draw.FrontPick)
		}
		for _, z := range c.Zones {
			if z < 0 {
				return fmt.Errorf("zone counts must be non-negative: %v", c.Zones)
			}
		}
	}
	if c.MaxConsecutive != nil && *c.MaxConsecutive < 0 {
		return fmt.Errorf("max consecutive must be non-negative, got %d", *c.MaxConsecutive)
	}
	for _, n := range append(append([]int(nil), c.Include...), c.Exclude...) {
		if n < 1 || n > draw.FrontMax {
			return fmt.Errorf("number %d out of range 1..%d", n, draw.FrontMax)
		}
	}
	if len(c.Include) > draw.FrontPick {
		return fmt.Errorf("include lists %d numbers, at most %d allowed", len(c.Include), draw.FrontPick)
	}
	return nil
}

func (c Constraint) allowedFront() ([]int, error) {
	banned := make(map[int]struct{}, len(c.Exclude))
	for _, n := range c.Exclude {
		banned[n] = struct{}{}
	}
	required := make(map[int]struct{}, len(c.Include))
	for _, n := range c.Include {
		if _, ban := banned[n]; ban {
			return nil, fmt.Errorf("number %d is both included and excluded", n)
		}
		required[n] = struct{}{}
	}
	allowed := make([]int, 0, draw.FrontMax)
	for n := 1; n <= draw.FrontMax; n++ {
		if _, ban := banned[n]; ban {
			continue
		}
		allowed = append(allowed, n)
	}
	if len(allowed) < draw.FrontPick {
		return nil, fmt.Errorf("only %d numbers remain after exclusion, need %d", len(allowed), draw.FrontPick)
	}
	return allowed, nil
}

func (c Constraint) weightedFront(allowed []int, counts HistoricalCounts, bias float64) []int {
	out := append([]int(nil), allowed...)
	if bias <= 0 || len(counts) == 0 {
		return out
	}
	maxCount := 0
	for _, n := range out {
		if counts[n] > maxCount {
			maxCount = counts[n]
		}
	}
	if maxCount == 0 {
		return out
	}
	expanded := make([]int, 0, len(out)*2)
	for _, n := range out {
		weight := 1.0 + bias*float64(counts[n])/float64(maxCount)
		times := int(weight * 10)
		if times < 1 {
			times = 1
		}
		for i := 0; i < times; i++ {
			expanded = append(expanded, n)
		}
	}
	return expanded
}

func sampleFront(rng *rand.Rand, weighted []int, c Constraint) []int {
	pool := append([]int(nil), weighted...)
	chosen := make([]int, 0, draw.FrontPick)
	seen := make(map[int]struct{}, draw.FrontPick)
	required := append([]int(nil), c.Include...)

	for _, n := range required {
		if _, dup := seen[n]; dup {
			continue
		}
		seen[n] = struct{}{}
		chosen = append(chosen, n)
	}

	for len(chosen) < draw.FrontPick && len(pool) > 0 {
		idx := rng.Intn(len(pool))
		n := pool[idx]
		pool[idx] = pool[len(pool)-1]
		pool = pool[:len(pool)-1]
		if _, dup := seen[n]; dup {
			continue
		}
		seen[n] = struct{}{}
		chosen = append(chosen, n)
	}
	if len(chosen) != draw.FrontPick {
		return nil
	}
	sort.Ints(chosen)
	if !c.accepts(chosen) {
		return nil
	}
	return chosen
}

func (c Constraint) accepts(front []int) bool {
	if c.OddEven != [2]int{} {
		odd := 0
		for _, n := range front {
			if n%2 == 1 {
				odd++
			}
		}
		if odd != c.OddEven[0] || len(front)-odd != c.OddEven[1] {
			return false
		}
	}
	if c.SumRange != [2]int{} {
		sum := 0
		for _, n := range front {
			sum += n
		}
		if sum < c.SumRange[0] || sum > c.SumRange[1] {
			return false
		}
	}
	if c.Zones != [3]int{} {
		var z [3]int
		for _, n := range front {
			switch {
			case n <= 12:
				z[0]++
			case n <= 24:
				z[1]++
			default:
				z[2]++
			}
		}
		if z != c.Zones {
			return false
		}
	}
	if c.MaxConsecutive != nil {
		pairs := 0
		for i := 1; i < len(front); i++ {
			if front[i]-front[i-1] == 1 {
				pairs++
			}
		}
		if pairs > *c.MaxConsecutive {
			return false
		}
	}
	for _, n := range c.Exclude {
		for _, f := range front {
			if f == n {
				return false
			}
		}
	}
	return true
}

func sampleBack(rng *rand.Rand) []int {
	pool := make([]int, draw.BackMax)
	for i := range pool {
		pool[i] = i + 1
	}
	out := make([]int, 0, draw.BackPick)
	for len(out) < draw.BackPick {
		idx := rng.Intn(len(pool))
		out = append(out, pool[idx])
		pool[idx] = pool[len(pool)-1]
		pool = pool[:len(pool)-1]
	}
	sort.Ints(out)
	return out
}

func frontKey(front, back []int) string {
	key := make([]byte, 0, 16)
	for i, n := range front {
		if i > 0 {
			key = append(key, '-')
		}
		key = appendInt(key, n)
	}
	key = append(key, '|')
	for i, n := range back {
		if i > 0 {
			key = append(key, '-')
		}
		key = appendInt(key, n)
	}
	return string(key)
}

func appendInt(dst []byte, v int) []byte {
	if v >= 10 {
		dst = append(dst, byte('0'+v/10))
	}
	return append(dst, byte('0'+v%10))
}

func sharesTooMany(existing []Ticket, front []int, limit int) bool {
	for _, t := range existing {
		shared := 0
		for _, a := range t.Front {
			for _, b := range front {
				if a == b {
					shared++
				}
			}
		}
		if shared > limit {
			return true
		}
	}
	return false
}

func (c Constraint) describe(opts Options) string {
	parts := make([]string, 0, 6)
	if c.OddEven != [2]int{} {
		parts = append(parts, fmt.Sprintf("奇偶 %d:%d", c.OddEven[0], c.OddEven[1]))
	}
	if c.SumRange != [2]int{} {
		parts = append(parts, fmt.Sprintf("和值 %d-%d", c.SumRange[0], c.SumRange[1]))
	}
	if c.Zones != [3]int{} {
		parts = append(parts, fmt.Sprintf("三区 %d-%d-%d", c.Zones[0], c.Zones[1], c.Zones[2]))
	}
	if c.MaxConsecutive != nil {
		parts = append(parts, fmt.Sprintf("连号≤%d", *c.MaxConsecutive))
	}
	if len(c.Include) > 0 {
		parts = append(parts, fmt.Sprintf("必含 %v", c.Include))
	}
	if len(c.Exclude) > 0 {
		parts = append(parts, fmt.Sprintf("排除 %v", c.Exclude))
	}
	if opts.HotBias > 0 {
		parts = append(parts, fmt.Sprintf("热度权重 %.1f", opts.HotBias))
	}
	if len(parts) == 0 {
		return "无约束随机采样"
	}
	return join(parts, "; ")
}

func join(parts []string, sep string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += sep
		}
		out += p
	}
	return out
}

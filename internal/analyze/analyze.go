// Package analyze computes descriptive statistics over historical draws.
//
// Every number here describes the past. None of it predicts the next draw:
// Super Lotto draws are independent and uniformly random over the legal
// combinations.
package analyze

import (
	"math"
	"sort"

	"github.com/veriyoung/super-lotto-tool/internal/draw"
)

// PrizeOdds are the exact probabilities for one ticket, independent of history.
const (
	// FrontCombinations is C(35,5).
	FrontCombinations = 324632
	// BackCombinations is C(12,2).
	BackCombinations = 66
	// TotalCombinations is FrontCombinations * BackCombinations.
	TotalCombinations = FrontCombinations * BackCombinations
	// JackpotOdds is 1 / TotalCombinations.
	JackpotOdds = 1.0 / float64(TotalCombinations)
)

// NumberStat aggregates occurrences of one number in one area.
type NumberStat struct {
	Number    int     `json:"number"`
	Count     int     `json:"count"`
	Frequency float64 `json:"frequency"`
	Expected  float64 `json:"expected"`
	Deviation float64 `json:"deviation"`
	Missed    int     `json:"missed"`
}

// AreaReport summarizes one number area.
type AreaReport struct {
	Total       int            `json:"total"`
	Numbers     []NumberStat   `json:"numbers"`
	Hottest     []NumberStat   `json:"hottest"`
	Coldest     []NumberStat   `json:"coldest"`
	SumMin      int            `json:"sum_min"`
	SumMax      int            `json:"sum_max"`
	SumMean     float64        `json:"sum_mean"`
	SumStdDev   float64        `json:"sum_std_dev"`
	OddEven     map[string]int `json:"odd_even"`
	Consecutive int            `json:"consecutive_pairs"`
}

// Report is the full descriptive summary.
type Report struct {
	Draws      int        `json:"draws"`
	Front      AreaReport `json:"front"`
	Back       AreaReport `json:"back"`
	Disclaimer string     `json:"disclaimer"`
}

const disclaimer = "统计仅描述历史分布。大乐透每期开奖相互独立，历史结果不构成对下一期的预测。"

// Build computes the report from draws. Draws are expected in any order.
func Build(draws []draw.Draw) Report {
	report := Report{Draws: len(draws), Disclaimer: disclaimer}
	if len(draws) == 0 {
		report.Front = emptyArea(draw.FrontMax)
		report.Back = emptyArea(draw.BackMax)
		return report
	}
	report.Front = areaStats(draws, draw.FrontMax, func(d draw.Draw) []int { return d.Front })
	report.Back = areaStats(draws, draw.BackMax, func(d draw.Draw) []int { return d.Back })
	return report
}

func emptyArea(max int) AreaReport {
	nums := make([]NumberStat, 0, max)
	for n := 1; n <= max; n++ {
		nums = append(nums, NumberStat{Number: n})
	}
	return AreaReport{Numbers: nums, OddEven: map[string]int{}}
}

func areaStats(draws []draw.Draw, max int, pick func(draw.Draw) []int) AreaReport {
	counts := make([]int, max+1)
	missed := make([]int, max+1)
	sums := make([]int, 0, len(draws))
	oddEven := map[string]int{}
	consecutive := 0

	for i, d := range draws {
		nums := pick(d)
		sum := 0
		odd := 0
		for _, n := range nums {
			if n < 1 || n > max {
				continue
			}
			counts[n]++
			missed[n] = i + 1
			sum += n
			if n%2 == 1 {
				odd++
			}
		}
		sums = append(sums, sum)
		oddEven[oddEvenKey(odd, len(nums)-odd)]++
		consecutive += consecutivePairs(nums)
	}

	total := len(draws)
	perNumberDraws := float64(total) * float64(len(pick(draws[0]))) / float64(max)

	stats := make([]NumberStat, 0, max)
	for n := 1; n <= max; n++ {
		missedRuns := 0
		if m := missed[n]; m > 0 {
			missedRuns = total - m
		} else {
			missedRuns = total
		}
		stats = append(stats, NumberStat{
			Number:    n,
			Count:     counts[n],
			Frequency: float64(counts[n]) / float64(total),
			Expected:  perNumberDraws,
			Deviation: float64(counts[n]) - perNumberDraws,
			Missed:    missedRuns,
		})
	}

	return AreaReport{
		Total:       total,
		Numbers:     stats,
		Hottest:     topByCount(stats, 5, true),
		Coldest:     topByCount(stats, 5, false),
		SumMin:      minInt(sums),
		SumMax:      maxInt(sums),
		SumMean:     mean(sums),
		SumStdDev:   stddev(sums),
		OddEven:     oddEven,
		Consecutive: consecutive,
	}
}

func topByCount(stats []NumberStat, n int, desc bool) []NumberStat {
	out := append([]NumberStat(nil), stats...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Count == out[j].Count {
			return out[i].Number < out[j].Number
		}
		if desc {
			return out[i].Count > out[j].Count
		}
		return out[i].Count < out[j].Count
	})
	if len(out) > n {
		out = out[:n]
	}
	return out
}

func oddEvenKey(odd, even int) string {
	return itoa(odd) + ":" + itoa(even)
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var buf [8]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}

func consecutivePairs(nums []int) int {
	sorted := append([]int(nil), nums...)
	sort.Ints(sorted)
	pairs := 0
	for i := 1; i < len(sorted); i++ {
		if sorted[i]-sorted[i-1] == 1 {
			pairs++
		}
	}
	return pairs
}

func minInt(values []int) int {
	if len(values) == 0 {
		return 0
	}
	m := values[0]
	for _, v := range values[1:] {
		if v < m {
			m = v
		}
	}
	return m
}

func maxInt(values []int) int {
	if len(values) == 0 {
		return 0
	}
	m := values[0]
	for _, v := range values[1:] {
		if v > m {
			m = v
		}
	}
	return m
}

func mean(values []int) float64 {
	if len(values) == 0 {
		return 0
	}
	sum := 0
	for _, v := range values {
		sum += v
	}
	return float64(sum) / float64(len(values))
}

func stddev(values []int) float64 {
	if len(values) < 2 {
		return 0
	}
	m := mean(values)
	var acc float64
	for _, v := range values {
		d := float64(v) - m
		acc += d * d
	}
	return math.Sqrt(acc / float64(len(values)-1))
}

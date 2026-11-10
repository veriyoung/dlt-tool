package analyze

import (
	"math"
	"testing"

	"github.com/veriyoung/super-lotto-tool/internal/draw"
)

func sample() []draw.Draw {
	return []draw.Draw{
		{Issue: "1", Front: []int{1, 2, 3, 4, 5}, Back: []int{1, 2}},
		{Issue: "2", Front: []int{1, 2, 3, 34, 35}, Back: []int{1, 3}},
		{Issue: "3", Front: []int{6, 7, 8, 9, 10}, Back: []int{4, 5}},
	}
}

func TestBuildCountsFrontNumbers(t *testing.T) {
	r := Build(sample())
	if r.Draws != 3 {
		t.Fatalf("expected 3 draws, got %d", r.Draws)
	}
	if len(r.Front.Numbers) != draw.FrontMax {
		t.Fatalf("expected %d front numbers, got %d", draw.FrontMax, len(r.Front.Numbers))
	}
	one := r.Front.Numbers[0]
	if one.Number != 1 || one.Count != 2 {
		t.Fatalf("number 1 wrong: %+v", one)
	}
	thousand := r.Front.Numbers[34]
	if thousand.Number != 35 || thousand.Count != 1 {
		t.Fatalf("number 35 wrong: %+v", thousand)
	}
}

func TestExpectedFrequencyMatchesUniformModel(t *testing.T) {
	r := Build(sample())
	one := r.Front.Numbers[0]
	want := 3 * 5.0 / 35.0
	if math.Abs(one.Expected-want) > 1e-9 {
		t.Fatalf("expected %v got %v", want, one.Expected)
	}
}

func TestMissedCountsDrawsSinceLastAppearance(t *testing.T) {
	r := Build(sample())
	thirtyFive := r.Front.Numbers[34]
	if thirtyFive.Missed != 1 {
		t.Fatalf("35 last appeared one draw before the latest, missed should be 1, got %d", thirtyFive.Missed)
	}
	eleven := r.Front.Numbers[10]
	if eleven.Count != 0 || eleven.Missed != 3 {
		t.Fatalf("11 never appeared, missed should equal total: %+v", eleven)
	}
}

func TestOddEvenAndSum(t *testing.T) {
	r := Build(sample())
	if r.Front.SumMin != 15 || r.Front.SumMax != 75 {
		t.Fatalf("unexpected sum range %d..%d", r.Front.SumMin, r.Front.SumMax)
	}
	if r.Front.OddEven["3:2"] != 2 || r.Front.OddEven["2:3"] != 1 {
		t.Fatalf("unexpected odd/even distribution: %+v", r.Front.OddEven)
	}
	if r.Front.Consecutive == 0 {
		t.Fatal("expected consecutive pairs to be counted")
	}
}

func TestConsecutivePairs(t *testing.T) {
	if got := consecutivePairs([]int{1, 2, 4, 5, 6}); got != 3 {
		t.Fatalf("expected 3 consecutive pairs, got %d", got)
	}
	if got := consecutivePairs([]int{1, 3, 5, 7, 9}); got != 0 {
		t.Fatalf("expected 0 consecutive pairs, got %d", got)
	}
}

func TestHottestAndColdest(t *testing.T) {
	r := Build(sample())
	if r.Front.Hottest[0].Number != 1 {
		t.Fatalf("expected 1 to be hottest, got %+v", r.Front.Hottest[0])
	}
	if r.Front.Coldest[0].Count != 0 {
		t.Fatalf("expected coldest to be an unseen number, got %+v", r.Front.Coldest[0])
	}
}

func TestEmptyStoreProducesEmptyReport(t *testing.T) {
	r := Build(nil)
	if r.Draws != 0 {
		t.Fatalf("expected 0 draws")
	}
	if len(r.Front.Numbers) != draw.FrontMax || len(r.Back.Numbers) != draw.BackMax {
		t.Fatalf("expected full number grids")
	}
	if r.Front.SumMin != 0 || r.Front.SumMax != 0 {
		t.Fatalf("expected zero sums on empty data")
	}
}

func TestJackpotOddsConstants(t *testing.T) {
	if TotalCombinations != FrontCombinations*BackCombinations {
		t.Fatalf("combination product mismatch")
	}
	want := 1.0 / 21425712.0
	if math.Abs(JackpotOdds-want) > 1e-15 {
		t.Fatalf("jackpot odds %v want %v", JackpotOdds, want)
	}
}

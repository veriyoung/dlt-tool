package pick

import (
	"errors"
	"math/rand"
	"testing"

	"github.com/veriyoung/super-lotto-tool/internal/draw"
)

func fixedRand(seed int64) *rand.Rand { return rand.New(rand.NewSource(seed)) }

func TestRecommendRespectsNumberRules(t *testing.T) {
	rng := fixedRand(1)
	tickets, err := Recommend(rng, Constraint{}, 50, Options{Seed: 1, AvoidExactDuplicates: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tickets) != 50 {
		t.Fatalf("expected 50 tickets, got %d", len(tickets))
	}
	for _, tk := range tickets {
		d := draw.Draw{Issue: "x", Front: tk.Front, Back: tk.Back}
		if err := d.Validate(); err != nil {
			t.Fatalf("invalid ticket %+v: %v", tk, err)
		}
	}
}

func TestRecommendRespectsOddEven(t *testing.T) {
	rng := fixedRand(2)
	tickets, err := Recommend(rng, Constraint{OddEven: [2]int{3, 2}}, 20, Options{Seed: 2}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tk := range tickets {
		odd := 0
		for _, n := range tk.Front {
			if n%2 == 1 {
				odd++
			}
		}
		if odd != 3 {
			t.Fatalf("expected 3 odd, got %d in %+v", odd, tk.Front)
		}
	}
}

func TestRecommendRespectsSumRangeAndZones(t *testing.T) {
	rng := fixedRand(3)
	c := Constraint{SumRange: [2]int{60, 100}, Zones: [3]int{2, 2, 1}}
	tickets, err := Recommend(rng, c, 20, Options{Seed: 3}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tk := range tickets {
		sum := 0
		var zones [3]int
		for _, n := range tk.Front {
			sum += n
			switch {
			case n <= 12:
				zones[0]++
			case n <= 24:
				zones[1]++
			default:
				zones[2]++
			}
		}
		if sum < 60 || sum > 100 {
			t.Fatalf("sum %d out of range for %+v", sum, tk.Front)
		}
		if zones != [3]int{2, 2, 1} {
			t.Fatalf("zones %v wrong for %+v", zones, tk.Front)
		}
	}
}

func TestRecommendRespectsIncludeExcludeAndConsecutiveCap(t *testing.T) {
	rng := fixedRand(4)
	zero := 0
	c := Constraint{Include: []int{7}, Exclude: []int{1, 2, 3}, MaxConsecutive: &zero}
	tickets, err := Recommend(rng, c, 20, Options{Seed: 4}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tk := range tickets {
		found := false
		for _, n := range tk.Front {
			if n == 7 {
				found = true
			}
			if n == 1 || n == 2 || n == 3 {
				t.Fatalf("excluded number present in %+v", tk.Front)
			}
		}
		if !found {
			t.Fatalf("required number 7 missing from %+v", tk.Front)
		}
		for i := 1; i < len(tk.Front); i++ {
			if tk.Front[i]-tk.Front[i-1] == 1 {
				t.Fatalf("consecutive pair present in %+v", tk.Front)
			}
		}
	}
}

func TestRecommendAvoidsDuplicateTickets(t *testing.T) {
	rng := fixedRand(5)
	tickets, err := Recommend(rng, Constraint{}, 30, Options{Seed: 5, AvoidExactDuplicates: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]struct{}{}
	for _, tk := range tickets {
		key := frontKey(tk.Front, tk.Back)
		if _, dup := seen[key]; dup {
			t.Fatalf("duplicate ticket %+v", tk)
		}
		seen[key] = struct{}{}
	}
}

func TestRecommendLimitsSharedFrontNumbers(t *testing.T) {
	rng := fixedRand(6)
	tickets, err := Recommend(rng, Constraint{}, 10, Options{Seed: 6, MaxSharedFront: 2}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < len(tickets); i++ {
		for j := i + 1; j < len(tickets); j++ {
			shared := 0
			for _, a := range tickets[i].Front {
				for _, b := range tickets[j].Front {
					if a == b {
						shared++
					}
				}
			}
			if shared > 2 {
				t.Fatalf("tickets %d and %d share %d front numbers", i, j, shared)
			}
		}
	}
}

func TestRecommendIsReproducibleWithSeed(t *testing.T) {
	c := Constraint{OddEven: [2]int{2, 3}}
	a, err := Recommend(fixedRand(9), c, 5, Options{Seed: 9}, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Recommend(fixedRand(9), c, 5, Options{Seed: 9}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := range a {
		for j := range a[i].Front {
			if a[i].Front[j] != b[i].Front[j] {
				t.Fatalf("seed did not reproduce ticket %d: %+v vs %+v", i, a[i], b[i])
			}
		}
	}
}

func TestRecommendRejectsImpossibleConstraint(t *testing.T) {
	rng := fixedRand(7)
	exclude := make([]int, 0, draw.FrontMax)
	for n := 1; n <= draw.FrontMax; n++ {
		exclude = append(exclude, n)
	}
	if _, err := Recommend(rng, Constraint{Exclude: exclude}, 1, Options{}, nil); err == nil {
		t.Fatal("expected error when all numbers are excluded")
	}
}

func TestRecommendRejectsBadInput(t *testing.T) {
	rng := fixedRand(8)
	cases := []Constraint{
		{OddEven: [2]int{3, 3}},
		{SumRange: [2]int{100, 50}},
		{Zones: [3]int{1, 1, 1}},
		{Include: []int{1, 1, 2, 3, 4, 5}},
		{Exclude: []int{40}},
	}
	for _, c := range cases {
		if _, err := Recommend(rng, c, 1, Options{}, nil); err == nil {
			t.Fatalf("expected validation error for %+v", c)
		}
	}
	if _, err := Recommend(rng, Constraint{}, 0, Options{}, nil); err == nil {
		t.Fatal("expected error for zero ticket count")
	}
}

func TestRecommendHotBiasStaysValid(t *testing.T) {
	rng := fixedRand(10)
	counts := HistoricalCounts{}
	for n := 1; n <= draw.FrontMax; n++ {
		counts[n] = n
	}
	tickets, err := Recommend(rng, Constraint{}, 10, Options{Seed: 10, HotBias: 1.5}, counts)
	if err != nil {
		t.Fatal(err)
	}
	for _, tk := range tickets {
		d := draw.Draw{Issue: "x", Front: tk.Front, Back: tk.Back}
		if err := d.Validate(); err != nil {
			t.Fatalf("invalid ticket under hot bias: %v", err)
		}
	}
}

func TestRecommendAvoidExactDuplicatesCanExhaust(t *testing.T) {
	rng := fixedRand(11)
	zero := 0
	c := Constraint{Include: []int{1, 2, 3, 4}, MaxConsecutive: &zero}
	_, err := Recommend(rng, c, 100, Options{Seed: 11, AvoidExactDuplicates: true, MaxAttempts: 5000}, nil)
	if err == nil {
		t.Fatal("expected exhaustion error for highly constrained ticket set")
	}
	if !errors.Is(err, ErrNoValidCombination) && !errors.Is(err, ErrTooManyTickets) {
		t.Fatalf("unexpected error type: %v", err)
	}
}

func TestRecentNumbers(t *testing.T) {
	draws := []draw.Draw{
		{Issue: "1", Front: []int{1, 2, 3, 4, 5}, Back: []int{1, 2}},
		{Issue: "2", Front: []int{6, 7, 8, 9, 10}, Back: []int{3, 4}},
	}
	got := RecentNumbers(draws, 1)
	want := []int{6, 7, 8, 9, 10}
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
	if RecentNumbers(draws, 0) != nil {
		t.Fatal("expected nil for n=0")
	}
}

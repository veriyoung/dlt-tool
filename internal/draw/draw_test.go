package draw

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateRejectsOutOfRangeAndDuplicates(t *testing.T) {
	cases := []struct {
		name string
		d    Draw
	}{
		{"front out of range", Draw{Issue: "1", Front: []int{0, 2, 3, 4, 5}, Back: []int{1, 2}}},
		{"front duplicate", Draw{Issue: "1", Front: []int{1, 1, 3, 4, 5}, Back: []int{1, 2}}},
		{"front too short", Draw{Issue: "1", Front: []int{1, 2, 3, 4}, Back: []int{1, 2}}},
		{"back out of range", Draw{Issue: "1", Front: []int{1, 2, 3, 4, 5}, Back: []int{0, 2}}},
		{"back too many", Draw{Issue: "1", Front: []int{1, 2, 3, 4, 5}, Back: []int{1, 2, 3}}},
		{"missing issue", Draw{Front: []int{1, 2, 3, 4, 5}, Back: []int{1, 2}}},
	}
	for _, tc := range cases {
		if err := tc.d.Validate(); err == nil {
			t.Fatalf("%s: expected error", tc.name)
		}
	}
}

func TestNormalizeSortsAreas(t *testing.T) {
	d := Draw{Issue: "1", Front: []int{5, 3, 1, 4, 2}, Back: []int{9, 3}}.Normalize()
	got := []int{d.Front[0], d.Front[1], d.Front[2], d.Front[3], d.Front[4]}
	want := []int{1, 2, 3, 4, 5}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("front not sorted: %v", d.Front)
		}
	}
	if d.Back[0] != 3 || d.Back[1] != 9 {
		t.Fatalf("back not sorted: %v", d.Back)
	}
}

func TestParseLines(t *testing.T) {
	text := strings.Join([]string{
		"# comment",
		"issue,date,front,back",
		"24001,2024-01-01,5 3 1 4 2,9 3",
		"24002,2024-01-03,6 7 8 9 10,11 12",
	}, "\n")
	draws, err := ParseLines(text)
	if err != nil {
		t.Fatal(err)
	}
	if len(draws) != 2 {
		t.Fatalf("expected 2 draws, got %d", len(draws))
	}
	if draws[0].Issue != "24001" || draws[0].Front[0] != 1 || draws[0].Back[1] != 9 {
		t.Fatalf("unexpected first draw: %+v", draws[0])
	}
	if draws[1].Issue != "24002" || draws[1].Back[1] != 12 {
		t.Fatalf("unexpected second draw: %+v", draws[1])
	}
}

func TestParseLinesAcceptsCombinedNumberField(t *testing.T) {
	draws, err := ParseLines("24003,2024-01-06,6 7 8 9 10 11 12")
	if err != nil {
		t.Fatal(err)
	}
	if len(draws) != 1 || draws[0].Back[0] != 11 || draws[0].Back[1] != 12 {
		t.Fatalf("unexpected combined parse: %+v", draws)
	}
}

func TestParseLinesRejectsBadInput(t *testing.T) {
	for _, bad := range []string{
		"24001,2024-01-01,1 2 3 4 5",
		"abc,2024-01-01,1 2 3 4 5,1 2",
		"24001,2024-01-01,1 2 3 4 36,1 2",
	} {
		if _, err := ParseLines(bad); err == nil {
			t.Fatalf("expected error for %q", bad)
		}
	}
}

func TestStoreUpsertAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "draws.json")

	s := &Store{}
	if err := s.Upsert(Draw{Issue: "24002", Date: "2024-01-03", Front: []int{1, 2, 3, 4, 5}, Back: []int{1, 2}}); err != nil {
		t.Fatal(err)
	}
	if err := s.Upsert(Draw{Issue: "24001", Date: "2024-01-01", Front: []int{6, 7, 8, 9, 10}, Back: []int{3, 4}}); err != nil {
		t.Fatal(err)
	}
	if err := s.Upsert(Draw{Issue: "24002", Date: "2024-01-03", Front: []int{11, 12, 13, 14, 15}, Back: []int{5, 6}}); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Draws) != 2 {
		t.Fatalf("expected 2 draws, got %d", len(loaded.Draws))
	}
	if loaded.Draws[0].Issue != "24001" {
		t.Fatalf("draws not sorted by issue: %+v", loaded.Draws)
	}
	if loaded.Draws[1].Front[0] != 11 {
		t.Fatalf("upsert did not replace: %+v", loaded.Draws[1])
	}
	latest, ok := loaded.Latest()
	if !ok || latest.Issue != "24002" {
		t.Fatalf("latest draw wrong: %+v ok=%v", latest, ok)
	}
}

func TestLoadMissingFileIsEmpty(t *testing.T) {
	s, err := Load(filepath.Join(t.TempDir(), "absent.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Draws) != 0 {
		t.Fatalf("expected empty store")
	}
}

func TestLoadRejectsInvalidDraw(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(path, []byte(`{"draws":[{"issue":"1","front":[1,2,3,4,99],"back":[1,2]}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected validation error")
	}
}

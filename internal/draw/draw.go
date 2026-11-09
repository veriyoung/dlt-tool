// Package draw models a Super Lotto (大乐透) draw and the on-disk store.
//
// A draw consists of five distinct front-area numbers from 1..35 and two
// distinct back-area numbers from 1..12. The package never fabricates history:
// callers must import real data through Parse or Load.
package draw

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	FrontMax  = 35
	FrontPick = 5
	BackMax   = 12
	BackPick  = 2
)

// Draw is one Super Lotto result.
type Draw struct {
	Issue    string    `json:"issue"`
	Date     string    `json:"date"`
	Front    []int     `json:"front"`
	Back     []int     `json:"back"`
	PoolUSD  float64   `json:"pool_usd,omitempty"`
	SalesUSD float64   `json:"sales_usd,omitempty"`
	DrawnAt  time.Time `json:"-"`
}

// Validate reports whether the draw satisfies Super Lotto number rules.
func (d Draw) Validate() error {
	if strings.TrimSpace(d.Issue) == "" {
		return fmt.Errorf("draw: issue is required")
	}
	if err := validateArea("front", d.Front, FrontMax, FrontPick); err != nil {
		return fmt.Errorf("draw %s: %w", d.Issue, err)
	}
	if err := validateArea("back", d.Back, BackMax, BackPick); err != nil {
		return fmt.Errorf("draw %s: %w", d.Issue, err)
	}
	return nil
}

func validateArea(name string, nums []int, max, want int) error {
	if len(nums) != want {
		return fmt.Errorf("%s area needs %d numbers, got %d", name, want, len(nums))
	}
	seen := make(map[int]struct{}, want)
	for _, n := range nums {
		if n < 1 || n > max {
			return fmt.Errorf("%s number %d out of range 1..%d", name, n, max)
		}
		if _, dup := seen[n]; dup {
			return fmt.Errorf("%s number %d appears twice", name, n)
		}
		seen[n] = struct{}{}
	}
	return nil
}

// Normalize sorts both areas ascending and returns the canonical form.
func (d Draw) Normalize() Draw {
	out := d
	out.Front = sortedCopy(d.Front)
	out.Back = sortedCopy(d.Back)
	return out
}

func sortedCopy(in []int) []int {
	out := append([]int(nil), in...)
	sort.Ints(out)
	return out
}

// Store is the whole on-disk dataset.
type Store struct {
	Draws []Draw `json:"draws"`
}

// Load reads a dataset from path. A missing file yields an empty store.
func Load(path string) (*Store, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Store{}, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var s Store
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	for i := range s.Draws {
		s.Draws[i] = s.Draws[i].Normalize()
		if err := s.Draws[i].Validate(); err != nil {
			return nil, err
		}
	}
	sortByIssue(s.Draws)
	return &s, nil
}

// Save writes the dataset atomically through a temporary file.
func (s *Store) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("prepare dir: %w", err)
	}
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("encode store: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}

// Upsert inserts or replaces a draw keyed by issue.
func (s *Store) Upsert(d Draw) error {
	d = d.Normalize()
	if err := d.Validate(); err != nil {
		return err
	}
	for i := range s.Draws {
		if s.Draws[i].Issue == d.Issue {
			s.Draws[i] = d
			return nil
		}
	}
	s.Draws = append(s.Draws, d)
	sortByIssue(s.Draws)
	return nil
}

func sortByIssue(draws []Draw) {
	sort.Slice(draws, func(i, j int) bool { return draws[i].Issue < draws[j].Issue })
}

// Latest returns the highest issue number draw.
func (s *Store) Latest() (Draw, bool) {
	if len(s.Draws) == 0 {
		return Draw{}, false
	}
	return s.Draws[len(s.Draws)-1], true
}

var linePattern = regexp.MustCompile(`^\s*(\d{4,7})\s*[,\s]\s*(\d{4}-\d{2}-\d{2})?\s*[,\s]*(.*)$`)

// ParseLines parses the compact CSV/TSV export used by the CLI:
//
//	issue,date,front,back
//	24001,2024-01-01,1 5 12 23 34,3 9
//
// Both space-separated and comma-separated number lists are accepted.
func ParseLines(text string) ([]Draw, error) {
	var out []Draw
	for i, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(strings.ToLower(line), "issue") {
			continue
		}
		d, err := parseLine(line)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", i+1, err)
		}
		out = append(out, d)
	}
	return out, nil
}

func parseLine(line string) (Draw, error) {
	fields := strings.Split(line, ",")
	if len(fields) < 3 {
		return Draw{}, fmt.Errorf("expected issue,date,front[,back], got %q", line)
	}
	issue := strings.TrimSpace(fields[0])
	date := strings.TrimSpace(fields[1])
	if _, err := strconv.Atoi(issue); err != nil {
		return Draw{}, fmt.Errorf("issue %q is not numeric", issue)
	}
	var frontText, backText string
	switch {
	case len(fields) >= 4:
		frontText = fields[2]
		backText = fields[3]
	default:
		parts := strings.Fields(fields[2])
		if len(parts) != FrontPick+BackPick {
			return Draw{}, fmt.Errorf("expected %d numbers, got %d", FrontPick+BackPick, len(parts))
		}
		frontText = strings.Join(parts[:FrontPick], " ")
		backText = strings.Join(parts[FrontPick:], " ")
	}
	front, err := parseNumbers(frontText)
	if err != nil {
		return Draw{}, fmt.Errorf("front: %w", err)
	}
	back, err := parseNumbers(backText)
	if err != nil {
		return Draw{}, fmt.Errorf("back: %w", err)
	}
	d := Draw{Issue: issue, Date: date, Front: front, Back: back}.Normalize()
	if err := d.Validate(); err != nil {
		return Draw{}, err
	}
	return d, nil
}

func parseNumbers(text string) ([]int, error) {
	fields := strings.FieldsFunc(text, func(r rune) bool { return r == ' ' || r == '-' || r == '|' })
	nums := make([]int, 0, len(fields))
	for _, f := range fields {
		n, err := strconv.Atoi(f)
		if err != nil {
			return nil, fmt.Errorf("%q is not a number", f)
		}
		nums = append(nums, n)
	}
	return nums, nil
}

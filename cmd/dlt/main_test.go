package main

import (
	"reflect"
	"testing"
)

func TestParseInts(t *testing.T) {
	got, err := parseInts("3:2", 2)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []int{3, 2}) {
		t.Fatalf("got %v", got)
	}

	if _, err := parseInts("3:2:1", 2); err == nil {
		t.Fatal("expected arity error")
	}
	if _, err := parseInts("a:b", 2); err == nil {
		t.Fatal("expected parse error")
	}
	if _, err := parseInts("", -1); err == nil {
		t.Fatal("expected empty error")
	}
}

func TestParseIntsAcceptsCommasAndSpaces(t *testing.T) {
	got, err := parseInts("7, 15 22", -1)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []int{7, 15, 22}) {
		t.Fatalf("got %v", got)
	}
}

func TestDedupeSortsAndRemovesDuplicates(t *testing.T) {
	got := dedupe([]int{5, 1, 5, 3, 1})
	if !reflect.DeepEqual(got, []int{1, 3, 5}) {
		t.Fatalf("got %v", got)
	}
}

func TestFormatNumsPadsTwoDigits(t *testing.T) {
	if got := formatNums([]int{1, 9, 10, 35}); got != "01 09 10 35" {
		t.Fatalf("got %q", got)
	}
}

func TestSortStrings(t *testing.T) {
	items := []string{"3:2", "1:4", "2:3"}
	sortStrings(items)
	want := []string{"1:4", "2:3", "3:2"}
	if !reflect.DeepEqual(items, want) {
		t.Fatalf("got %v", items)
	}
}

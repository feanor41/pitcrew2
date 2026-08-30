package projectcontext

import (
	"reflect"
	"testing"
)

func TestCategoriesAreExactOrderedAndDefensive(t *testing.T) {
	want := []string{"stack", "runtime", "deployment", "architecture", "documentation", "sdd"}
	got := Categories()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Categories() = %v; want %v", got, want)
	}
	got[0] = "changed"
	if again := Categories(); !reflect.DeepEqual(again, want) {
		t.Fatalf("Categories() shares mutable storage: %v", again)
	}
}

func TestCloneRecordIsDefensive(t *testing.T) {
	record := validRecord()
	clone := CloneRecord(record)
	clone.Facts["stack"][0].Assertion = "changed"
	clone.Facts["runtime"] = append(clone.Facts["runtime"], clone.Facts["runtime"][0])
	if record.Facts["stack"][0].Assertion != "stack fact" || len(record.Facts["runtime"]) != 1 {
		t.Fatalf("CloneRecord shares mutable storage: %#v", record)
	}
}

func TestMissingCategoriesUsesCanonicalOrder(t *testing.T) {
	record := validRecord()
	record.Facts["runtime"] = nil
	record.Facts["documentation"] = []Fact{}
	got, err := MissingCategories(record)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"runtime", "documentation"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("MissingCategories() = %v; want %v", got, want)
	}
	if got, err := MissingCategories(validRecord()); err != nil || len(got) != 0 {
		t.Fatalf("complete record missing=%v err=%v", got, err)
	}
}

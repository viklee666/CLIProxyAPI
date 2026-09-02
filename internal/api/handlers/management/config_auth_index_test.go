package management

import (
	"reflect"
	"testing"
)

func TestDeletedAuthIndices(t *testing.T) {
	t.Parallel()

	got := deletedAuthIndices([]string{"keep", "drop", "drop", " ", "also-drop"}, []string{"keep", "new"})
	want := []string{"drop", "also-drop"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("deletedAuthIndices() = %#v, want %#v", got, want)
	}
	if gotEmpty := deletedAuthIndices([]string{"a"}, []string{"a"}); len(gotEmpty) != 0 {
		t.Fatalf("deletedAuthIndices(same) = %#v, want empty", gotEmpty)
	}
}

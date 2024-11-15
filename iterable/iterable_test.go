package iterable

import (
	"reflect"
	"testing"
)

func TestFilterAndToArray(t *testing.T) {

	ints := []int{1, 2, 3, 4, 5}

	actual := FromSlice(ints).Filter(func(i int) bool { return i%2 == 0 }).ToArray()

	expected := []int{2, 4}

	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Expected %v, got %v", expected, actual)
	}

}

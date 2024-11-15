package launchy

import (
	"reflect"
	"testing"
)

func TestFilterAndToArray(t *testing.T) {

	actual := scrubSystemCtlOutput([]byte("● qubes-dbsetup.service       loaded      failed   failed  qubes-dbsetup"))
	expected := "qubes-dbsetup.service       loaded      failed   failed  qubes-dbsetup"

	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Expected %v, got %v", expected, actual)
	}

}

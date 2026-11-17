package docker

import "testing"

func TestDockerQuantities(t *testing.T) {
	for _, test := range []struct {
		s     string
		value float64
	}{{"1.5GiB", 1610612736}, {"32MiB", 33554432}, {"123kB", 123000}, {"4B", 4}} {
		value, err := parseQuantity(test.s)
		if err != nil || value != test.value {
			t.Fatal(test.s, value, err)
		}
	}
	if _, err := parseQuantity("--"); err == nil {
		t.Fatal("unavailable must not become zero")
	}
}

//go:build !linux

package podtoast

import "fmt"

func startTrafficCapture(*trafficCounters) (func(), error) {
	return nil, fmt.Errorf("packet capture requires Linux")
}

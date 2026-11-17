//go:build !linux

package podtoast

import (
	"context"
	"fmt"
)

func (s *TrafficServer) start(context.Context, TrafficConfig) (func() error, error) {
	return nil, fmt.Errorf("traffic control requires Linux")
}

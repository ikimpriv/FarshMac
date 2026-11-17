//go:build !linux

package podtoast

import "context"

func trafficOverview(context.Context) *TrafficOverview {
	return &TrafficOverview{Warnings: []string{"Pod socket inventory requires Linux"}}
}

package kube

import (
	"context"
	"io"
	"time"

	corev1 "k8s.io/api/core/v1"
)

// LogOptions selects which container log to read and how much of it.
type LogOptions struct {
	Container  string
	TailLines  int64 // 0 = everything.
	Previous   bool  // The previous instance of the container.
	Follow     bool
	Since      time.Duration // 0 = from the start.
	Timestamps bool
}

// Logs streams a container's log. The reader ends when the context ends.
func (c *Client) Logs(ctx context.Context, ns, pod string, opts LogOptions) (io.ReadCloser, error) {
	o := &corev1.PodLogOptions{Container: opts.Container, Previous: opts.Previous, Follow: opts.Follow, Timestamps: opts.Timestamps}
	if opts.TailLines > 0 {
		o.TailLines = &opts.TailLines
	}
	if opts.Since > 0 {
		seconds := int64(opts.Since / time.Second)
		if seconds < 1 {
			seconds = 1
		}
		o.SinceSeconds = &seconds
	}
	return c.stream.CoreV1().Pods(ns).GetLogs(pod, o).Stream(ctx)
}

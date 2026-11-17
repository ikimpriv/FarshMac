//go:build linux

package podtoast

import (
	"context"
	"encoding/binary"
	"fmt"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

// TPACKET_V2: a bounded 4 MiB kernel ring, 256-byte snapshots. Only headers are
// inspected; payloads are neither exported nor persisted. No libpcap required.
func startTrafficCapture(c *trafficCounters) (func(), error) {
	fd, e := syscall.Socket(syscall.AF_PACKET, syscall.SOCK_RAW|syscall.SOCK_CLOEXEC, 0x0300)
	if e != nil {
		return nil, fmt.Errorf("packet capture requires NET_RAW: %w", e)
	}
	fail := func(e error) (func(), error) { syscall.Close(fd); return nil, e }
	if e = syscall.SetsockoptInt(fd, 263, 10, 1); e != nil {
		return fail(e)
	}
	filter := []syscall.SockFilter{{Code: 0x06, K: 256}}
	program := syscall.SockFprog{Len: uint16(len(filter)), Filter: &filter[0]}
	if _, _, errno := syscall.Syscall6(syscall.SYS_SETSOCKOPT, uintptr(fd), syscall.SOL_SOCKET, syscall.SO_ATTACH_FILTER, uintptr(unsafe.Pointer(&program)), unsafe.Sizeof(program), 0); errno != 0 {
		return fail(errno)
	}
	req := [4]uint32{1 << 20, 4, 2048, 2048}
	if _, _, errno := syscall.Syscall6(syscall.SYS_SETSOCKOPT, uintptr(fd), 263, 5, uintptr(unsafe.Pointer(&req[0])), unsafe.Sizeof(req), 0); errno != 0 {
		return fail(errno)
	}
	ring, e := syscall.Mmap(fd, 0, 4<<20, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	if e != nil {
		return fail(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer syscall.Close(fd)
		defer syscall.Munmap(ring)
		index := 0
		statsAt := time.Now()
		tick := time.NewTicker(2 * time.Millisecond)
		defer tick.Stop()
		for {
			if ctx.Err() != nil {
				return
			}
			frame := ring[index*2048 : (index+1)*2048]
			status := (*uint32)(unsafe.Pointer(&frame[0]))
			if atomic.LoadUint32(status)&1 == 0 {
				select {
				case <-ctx.Done():
					return
				case <-tick.C:
				}
				continue
			}
			snap := int(binary.NativeEndian.Uint32(frame[8:12]))
			mac := int(binary.NativeEndian.Uint16(frame[12:14]))
			pkttype := frame[42]
			loopback := binary.NativeEndian.Uint16(frame[40:42]) == 772
			if (!loopback || pkttype == 4) && mac >= 52 && snap >= 14 && mac+snap <= len(frame) {
				if p, ok := decodeTrafficPacket(frame[mac : mac+snap]); ok {
					c.packet(p, pkttype == 4, loopback, time.Now())
				} else {
					c.mu.Lock()
					c.unparsed++
					c.mu.Unlock()
				}
			}
			atomic.StoreUint32(status, 0)
			index = (index + 1) % 2048
			if time.Since(statsAt) > time.Second {
				var stats [2]uint32
				size := uint32(unsafe.Sizeof(stats))
				_, _, err := syscall.Syscall6(syscall.SYS_GETSOCKOPT, uintptr(fd), 263, 6, uintptr(unsafe.Pointer(&stats[0])), uintptr(unsafe.Pointer(&size)), 0)
				if err == 0 {
					c.mu.Lock()
					c.dropped += uint64(stats[1])
					c.mu.Unlock()
				}
				statsAt = time.Now()
			}
		}
	}()
	var once sync.Once
	return func() { once.Do(func() { cancel(); <-done }) }, nil
}

package podtoast

import (
	"encoding/binary"
	"net/netip"
	"testing"
	"time"
)

func TestTrafficPacketHeaders(t *testing.T) {
	frame := make([]byte, 14+40)
	binary.BigEndian.PutUint16(frame[12:14], 0x0800)
	ip := frame[14:]
	ip[0] = 0x45
	ip[9] = 6
	binary.BigEndian.PutUint16(ip[2:4], 40)
	copy(ip[12:16], []byte{10, 0, 0, 1})
	copy(ip[16:20], []byte{10, 0, 0, 2})
	binary.BigEndian.PutUint16(ip[20:22], 40000)
	binary.BigEndian.PutUint16(ip[22:24], 8554)
	ip[33] = 2
	p, ok := decodeTrafficPacket(frame)
	if !ok || p.src.String() != "10.0.0.1:40000" || p.dst.Port() != 8554 || !p.syn || p.bytes != 40 {
		t.Fatalf("bad parse: %+v", p)
	}
	c := newTrafficCounters()
	now := time.Now()
	c.packet(p, true, false, now)
	p.fin = true
	p.syn = false
	c.packet(p, true, false, now)
	p.fin = false
	p.syn = true
	p.seq = 7
	c.packet(p, true, false, now)
	s := c.snapshot()
	if len(s.Flows) != 2 || len(s.Groups) != 1 || s.Groups[0].Connections != 2 || s.Groups[0].Sent != 120 {
		t.Fatalf("reused tuple lost short flow: %+v", s)
	}
	for n := 0; n < maxTrafficFlows+10; n++ {
		x := p
		x.src = netip.AddrPortFrom(p.src.Addr(), uint16(n+1))
		c.packet(x, true, false, now)
	}
	if len(c.snapshot().Flows) > maxTrafficFlows || c.snapshot().Evicted == 0 {
		t.Fatal("flow history unbounded")
	}
}

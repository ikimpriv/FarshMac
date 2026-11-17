package podtoast

import (
	"encoding/binary"
	"net/netip"
)

// Parse headers only. No application bytes, TLS records, credentials or payloads
// are retained. Unparseable fragments/extension chains are counted explicitly.
func decodeTrafficPacket(frame []byte) (p decodedTrafficPacket, ok bool) {
	if len(frame) < 14 {
		return
	}
	ether := binary.BigEndian.Uint16(frame[12:14])
	offset := 14
	for i := 0; i < 2 && (ether == 0x8100 || ether == 0x88a8); i++ {
		if len(frame) < offset+4 {
			return
		}
		ether = binary.BigEndian.Uint16(frame[offset+2 : offset+4])
		offset += 4
	}
	data := frame[offset:]
	var src, dst netip.Addr
	var proto byte
	transport := 0
	v6 := false
	switch ether {
	case 0x0800:
		if len(data) < 20 || data[0]>>4 != 4 {
			return
		}
		transport = int(data[0]&15) * 4
		if transport < 20 || len(data) < transport {
			return
		}
		if binary.BigEndian.Uint16(data[6:8])&0x1fff != 0 {
			return
		}
		proto = data[9]
		src = netip.AddrFrom4([4]byte(data[12:16]))
		dst = netip.AddrFrom4([4]byte(data[16:20]))
		p.bytes = uint64(binary.BigEndian.Uint16(data[2:4]))
	case 0x86dd:
		if len(data) < 40 || data[0]>>4 != 6 {
			return
		}
		v6 = true
		proto = data[6]
		transport = 40
		src = netip.AddrFrom16([16]byte(data[8:24]))
		dst = netip.AddrFrom16([16]byte(data[24:40]))
		p.bytes = 40 + uint64(binary.BigEndian.Uint16(data[4:6]))
		for i := 0; i < 8 && (proto == 0 || proto == 43 || proto == 60 || proto == 44 || proto == 51); i++ {
			if len(data) < transport+8 {
				return
			}
			next := data[transport]
			size := (int(data[transport+1]) + 1) * 8
			if proto == 44 {
				if binary.BigEndian.Uint16(data[transport+2:transport+4])&0xfff8 != 0 {
					return
				}
				size = 8
			}
			if proto == 51 {
				size = (int(data[transport+1]) + 2) * 4
			}
			if len(data) < transport+size {
				return
			}
			transport += size
			proto = next
		}
	default:
		return
	}
	if proto != 6 && proto != 17 || len(data) < transport+8 || p.bytes < uint64(transport+8) {
		return
	}
	ports := data[transport:]
	p.src = netip.AddrPortFrom(src, binary.BigEndian.Uint16(ports[:2]))
	p.dst = netip.AddrPortFrom(dst, binary.BigEndian.Uint16(ports[2:4]))
	p.proto = "udp"
	if proto == 6 {
		if len(ports) < 20 {
			return
		}
		p.proto = "tcp"
		flags := ports[13]
		p.syn = flags&2 != 0
		p.ack = flags&16 != 0
		p.fin = flags&1 != 0
		p.rst = flags&4 != 0
		p.seq = binary.BigEndian.Uint32(ports[4:8])
	}
	if v6 {
		p.proto += "6"
	}
	return p, true
}

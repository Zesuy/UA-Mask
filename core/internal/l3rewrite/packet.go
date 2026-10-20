package l3rewrite

import (
	"fmt"
	"hash/fnv"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
)

// initialTCPWindow is the fixed TCP window value applied to SYN packets when
// TCP initial-window rewriting is enabled.
const initialTCPWindow = uint16(65535)

// packet wraps a decoded IP packet together with its TCP layer when present.
type packet struct {
	networkLayer gopacket.NetworkLayer
	tcp          *layers.TCP
	isIPv6       bool
}

// parsePacket decodes an IPv4/IPv6 packet, extracting the optional TCP
// transport layer. The decoding layer parser is rebuilt on every call so the
// function remains safe for concurrent use.
func parsePacket(data []byte) (*packet, error) {
	if len(data) < 20 {
		return nil, fmt.Errorf("packet too short: %d bytes", len(data))
	}

	p := &packet{tcp: &layers.TCP{}}
	version := (data[0] >> 4) & 0xF
	p.isIPv6 = version == 6

	var layerType gopacket.LayerType
	var ipLayer gopacket.DecodingLayer
	if p.isIPv6 {
		ip6 := &layers.IPv6{}
		layerType = layers.LayerTypeIPv6
		ipLayer = ip6
		p.networkLayer = ip6
	} else {
		ip4 := &layers.IPv4{}
		layerType = layers.LayerTypeIPv4
		ipLayer = ip4
		p.networkLayer = ip4
	}

	parser := gopacket.NewDecodingLayerParser(layerType, ipLayer, p.tcp)
	parser.IgnoreUnsupported = true

	var decoded []gopacket.LayerType
	if err := parser.DecodeLayers(data, &decoded); err != nil {
		return nil, fmt.Errorf("decode layers: %w", err)
	}

	hasTCP := false
	for _, lt := range decoded {
		if lt == layers.LayerTypeTCP {
			hasTCP = true
			break
		}
	}
	if !hasTCP {
		p.tcp = nil
	}
	return p, nil
}

// serialize re-encodes the packet after field modifications, recomputing
// lengths and checksums.
func (p *packet) serialize() ([]byte, error) {
	networkLayer := p.networkLayer
	buffer := gopacket.NewSerializeBuffer()
	opts := gopacket.SerializeOptions{
		FixLengths:       true,
		ComputeChecksums: true,
	}

	if p.tcp != nil {
		payload := p.tcp.Payload
		p.tcp.Checksum = 0
		p.tcp.Payload = nil
		if err := p.tcp.SetNetworkLayerForChecksum(networkLayer); err != nil {
			return nil, err
		}

		if p.isIPv6 {
			ip6 := networkLayer.(*layers.IPv6)
			ip6.NextHeader = layers.IPProtocolTCP
			if err := gopacket.SerializeLayers(buffer, opts, ip6, p.tcp, gopacket.Payload(payload)); err != nil {
				return nil, err
			}
		} else {
			ip4 := networkLayer.(*layers.IPv4)
			ip4.Checksum = 0
			if err := gopacket.SerializeLayers(buffer, opts, ip4, p.tcp, gopacket.Payload(payload)); err != nil {
				return nil, err
			}
		}
		return buffer.Bytes(), nil
	}

	if p.isIPv6 {
		return buffer.Bytes(), gopacket.SerializeLayers(buffer, opts, networkLayer.(*layers.IPv6))
	}
	ip4 := networkLayer.(*layers.IPv4)
	ip4.Checksum = 0
	return buffer.Bytes(), gopacket.SerializeLayers(buffer, opts, ip4)
}

// rewrite applies the configured L3 rewrites to raw packet bytes. It returns
// the modified packet bytes and true when at least one field changed, or nil
// and false when the packet is left untouched.
func rewrite(data []byte, cfg Config) ([]byte, bool, error) {
	p, err := parsePacket(data)
	if err != nil {
		return nil, false, err
	}

	modified := false
	if p.tcp != nil {
		if cfg.TCPTS {
			modified = clearTCPTimestamp(p.tcp) || modified
		}
		if cfg.TCPWIN {
			modified = setInitialTCPWindow(p.tcp) || modified
		}
	}
	if cfg.IPID {
		modified = zeroIPID(p) || modified
	}

	if !modified {
		return nil, false, nil
	}
	out, err := p.serialize()
	if err != nil {
		return nil, false, err
	}
	return out, true, nil
}

// clearTCPTimestamp removes the TCP timestamp option from the TCP layer.
// It returns true when the timestamp option was found and removed.
func clearTCPTimestamp(tcp *layers.TCP) bool {
	if len(tcp.Options) == 0 {
		return false
	}

	modified := false
	newOptions := make([]layers.TCPOption, 0, len(tcp.Options))
	for _, opt := range tcp.Options {
		if opt.OptionType == layers.TCPOptionKindTimestamps {
			modified = true
			continue
		}
		if opt.OptionType == layers.TCPOptionKindNop || opt.OptionType == layers.TCPOptionKindEndList {
			continue
		}
		newOptions = append(newOptions, opt)
	}
	if modified {
		tcp.Options = newOptions
	}
	return modified
}

// setInitialTCPWindow sets the TCP initial window to a fixed value for pure
// SYN packets. It returns true when the window value was changed.
func setInitialTCPWindow(tcp *layers.TCP) bool {
	if !(tcp.SYN && !tcp.ACK) {
		return false
	}
	if tcp.Window == initialTCPWindow {
		return false
	}
	tcp.Window = initialTCPWindow
	return true
}

// zeroIPID sets the IPv4 Identification field to zero. IPv6 has no such field
// and is left untouched. It returns true when the field was changed.
func zeroIPID(p *packet) bool {
	if p.isIPv6 {
		return false
	}
	ip4 := p.networkLayer.(*layers.IPv4)
	if ip4.Id == 0 {
		return false
	}
	ip4.Id = 0
	return true
}

// flowHash hashes the TCP/IP 4-tuple so that packets belonging to the same
// flow are dispatched to the same worker, preserving packet ordering.
func flowHash(payload []byte) uint32 {
	if len(payload) < 1 {
		return 0
	}

	h := fnv.New32a()
	version := (payload[0] >> 4) & 0xF

	switch version {
	case 4:
		if len(payload) < 20 {
			return 0
		}
		// Source IP (12-15) and destination IP (16-19).
		h.Write(payload[12:20])
		if payload[9] == 6 {
			ihl := int(payload[0]&0x0F) * 4
			if ihl >= 20 && len(payload) >= ihl+4 {
				// TCP source port and destination port.
				h.Write(payload[ihl : ihl+4])
			}
		}
	case 6:
		if len(payload) < 40 {
			return 0
		}
		// Source IP (8-23) and destination IP (24-39).
		h.Write(payload[8:40])
		if payload[6] == 6 && len(payload) >= 44 {
			// TCP source port and destination port.
			h.Write(payload[40:44])
		}
	default:
		return 0
	}
	return h.Sum32()
}

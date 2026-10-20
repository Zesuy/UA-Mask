package l3rewrite

import (
	"net"
	"testing"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
)

func buildIPv4TCP(t *testing.T, tcp *layers.TCP, ipID uint16) []byte {
	t.Helper()
	ip := &layers.IPv4{
		Version:  4,
		IHL:      5,
		TTL:      64,
		Id:       ipID,
		Protocol: layers.IPProtocolTCP,
		SrcIP:    net.IPv4(192, 168, 1, 10),
		DstIP:    net.IPv4(8, 8, 8, 8),
	}
	buf := gopacket.NewSerializeBuffer()
	opts := gopacket.SerializeOptions{FixLengths: true, ComputeChecksums: true}
	if err := tcp.SetNetworkLayerForChecksum(ip); err != nil {
		t.Fatalf("SetNetworkLayerForChecksum: %v", err)
	}
	if err := gopacket.SerializeLayers(buf, opts, ip, tcp); err != nil {
		t.Fatalf("SerializeLayers: %v", err)
	}
	return buf.Bytes()
}

func parseIPv4TCP(t *testing.T, data []byte) (*layers.IPv4, *layers.TCP) {
	t.Helper()
	p := gopacket.NewPacket(data, layers.LayerTypeIPv4, gopacket.Default)
	ip, ok := p.Layer(layers.LayerTypeIPv4).(*layers.IPv4)
	if !ok {
		t.Fatalf("expected IPv4 layer, got %v", p.Layers())
	}
	tcp, ok := p.Layer(layers.LayerTypeTCP).(*layers.TCP)
	if !ok {
		t.Fatalf("expected TCP layer, got %v", p.Layers())
	}
	return ip, tcp
}

func hasTimestamp(tcp *layers.TCP) bool {
	for _, opt := range tcp.Options {
		if opt.OptionType == layers.TCPOptionKindTimestamps {
			return true
		}
	}
	return false
}

func synTCP(window uint16, withTimestamp bool) *layers.TCP {
	tcp := &layers.TCP{
		SrcPort: 40000,
		DstPort: 80,
		Seq:     1000,
		SYN:     true,
		Window:  window,
	}
	if withTimestamp {
		tcp.Options = []layers.TCPOption{
			{OptionType: layers.TCPOptionKindNop, OptionLength: 1},
			{OptionType: layers.TCPOptionKindNop, OptionLength: 1},
			{OptionType: layers.TCPOptionKindTimestamps, OptionLength: 10, OptionData: []byte{1, 2, 3, 4, 5, 6, 7, 8}},
		}
	}
	return tcp
}

func TestRewriteClearsTCPTimestamp(t *testing.T) {
	data := buildIPv4TCP(t, synTCP(8192, true), 0)
	out, changed, err := rewrite(data, Config{TCPTS: true})
	if err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	if !changed {
		t.Fatalf("rewrite reported no change for packet with timestamp")
	}
	_, tcp := parseIPv4TCP(t, out)
	if hasTimestamp(tcp) {
		t.Fatalf("timestamp option was not removed: %+v", tcp.Options)
	}
}

func TestRewriteKeepsPacketWithoutTimestamp(t *testing.T) {
	data := buildIPv4TCP(t, synTCP(8192, false), 0)
	_, changed, err := rewrite(data, Config{TCPTS: true})
	if err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	if changed {
		t.Fatalf("rewrite reported a change for a packet with no timestamp")
	}
}

func TestRewriteSetsInitialTCPWindow(t *testing.T) {
	data := buildIPv4TCP(t, synTCP(1024, false), 0)
	out, changed, err := rewrite(data, Config{TCPWIN: true})
	if err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	if !changed {
		t.Fatalf("rewrite reported no change for SYN with non-default window")
	}
	_, tcp := parseIPv4TCP(t, out)
	if tcp.Window != 65535 {
		t.Fatalf("window = %d, want 65535", tcp.Window)
	}
}

func TestRewriteLeavesNonSynWindow(t *testing.T) {
	tcp := &layers.TCP{SrcPort: 40000, DstPort: 80, Seq: 1000, ACK: true, Window: 1024}
	data := buildIPv4TCP(t, tcp, 0)
	_, changed, err := rewrite(data, Config{TCPWIN: true})
	if err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	if changed {
		t.Fatalf("rewrite changed window on a non-SYN packet")
	}
}

func TestRewriteZeroesIPID(t *testing.T) {
	data := buildIPv4TCP(t, synTCP(8192, false), 0xbeef)
	out, changed, err := rewrite(data, Config{IPID: true})
	if err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	if !changed {
		t.Fatalf("rewrite reported no change for non-zero IP ID")
	}
	ip, _ := parseIPv4TCP(t, out)
	if ip.Id != 0 {
		t.Fatalf("IPv4 id = %d, want 0", ip.Id)
	}
}

func TestRewriteLeavesZeroIPID(t *testing.T) {
	data := buildIPv4TCP(t, synTCP(8192, false), 0)
	_, changed, err := rewrite(data, Config{IPID: true})
	if err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	if changed {
		t.Fatalf("rewrite changed an already-zero IP ID")
	}
}

func TestRewriteAllFeatures(t *testing.T) {
	data := buildIPv4TCP(t, synTCP(2048, true), 0x1234)
	out, changed, err := rewrite(data, Config{TCPTS: true, TCPWIN: true, IPID: true})
	if err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	if !changed {
		t.Fatalf("rewrite reported no change")
	}
	ip, tcp := parseIPv4TCP(t, out)
	if hasTimestamp(tcp) {
		t.Fatalf("timestamp was not removed")
	}
	if tcp.Window != 65535 {
		t.Fatalf("window = %d, want 65535", tcp.Window)
	}
	if ip.Id != 0 {
		t.Fatalf("IPv4 id = %d, want 0", ip.Id)
	}
}

func TestRewriteDisabledConfig(t *testing.T) {
	data := buildIPv4TCP(t, synTCP(1024, true), 0x1234)
	out, changed, err := rewrite(data, Config{})
	if err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	if changed || out != nil {
		t.Fatalf("disabled rewrite should be a no-op (changed=%v)", changed)
	}
}

func TestFlowHashStablePerFlow(t *testing.T) {
	a := buildIPv4TCP(t, synTCP(8192, false), 0)
	b := buildIPv4TCP(t, &layers.TCP{SrcPort: 40000, DstPort: 80, Seq: 2000, ACK: true, Window: 4096}, 1)
	if flowHash(a) != flowHash(b) {
		t.Fatalf("same 4-tuple produced different hashes: %d vs %d", flowHash(a), flowHash(b))
	}
}

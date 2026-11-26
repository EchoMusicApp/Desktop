package lansync

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	mdnsAddr        = "224.0.0.251:5353"
	ServiceType     = "_echomusic._tcp.local."
	dnsClassIN      = 1
	dnsTypeA        = 1
	dnsTypePTR      = 12
	dnsTypeTXT      = 16
	dnsTypeSRV      = 33
	defaultTTL      = 120
	peerExpiry      = 45 * time.Second
	announceEvery   = 15 * time.Second
	queryEvery      = 5 * time.Second
)

type Peer struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Platform string    `json:"platform"`
	Host     string    `json:"host"`
	Port     int       `json:"port"`
	LastSeen time.Time `json:"lastSeen"`
}

func (p Peer) BaseURL() string {
	return fmt.Sprintf("http://%s:%d", p.Host, p.Port)
}

type mdnsService struct {
	selfID       string
	selfName     string
	selfPlatform string
	port         int

	mu     sync.RWMutex
	peers  map[string]Peer // keyed by device ID
	byInst map[string]*partialPeer
	conns  []*net.UDPConn
	onPeer func([]Peer)
}

type partialPeer struct {
	instance string
	target   string
	port     int
	host     string
	txt      map[string]string
	seenAt   time.Time
}

func newMDNSService(selfID, selfName, selfPlatform string, port int, onPeer func([]Peer)) *mdnsService {
	return &mdnsService{
		selfID:       selfID,
		selfName:     selfName,
		selfPlatform: selfPlatform,
		port:         port,
		peers:        make(map[string]Peer),
		byInst:       make(map[string]*partialPeer),
		onPeer:       onPeer,
	}
}

func (m *mdnsService) Start(ctx context.Context) error {
	maddr, err := net.ResolveUDPAddr("udp4", mdnsAddr)
	if err != nil {
		return err
	}

	lc := net.ListenConfig{
		Control: func(network, address string, c syscall.RawConn) error {
			var setErr error
			_ = c.Control(func(fd uintptr) {
				_ = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
				setReusePort(fd)
			})
			return setErr
		},
	}

	pc, err := lc.ListenPacket(ctx, "udp4", ":5353")
	if err != nil {
		// Fallback to ephemeral UDP port if 5353 is unavailable on this host.
		pc, err = lc.ListenPacket(ctx, "udp4", ":0")
		if err != nil {
			return err
		}
	}
	conn, ok := pc.(*net.UDPConn)
	if !ok {
		pc.Close()
		return fmt.Errorf("unexpected packet conn %T", pc)
	}

	ifaces, _ := net.Interfaces()
	joined := 0
	for i := range ifaces {
		ifi := &ifaces[i]
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagMulticast == 0 || ifi.Flags&net.FlagLoopback != 0 {
			continue
		}
		if err := joinMulticastGroup(conn, ifi, maddr); err == nil {
			joined++
		}
	}
	if joined == 0 {
		_ = joinMulticastGroup(conn, nil, maddr)
	}

	m.mu.Lock()
	m.conns = append(m.conns, conn)
	m.mu.Unlock()

	go m.readLoop(ctx, conn)
	go m.announceAndQueryLoop(ctx, conn, maddr)
	return nil
}

func (m *mdnsService) Stop() {
	m.mu.Lock()
	conns := m.conns
	m.conns = nil
	m.mu.Unlock()
	for _, c := range conns {
		_ = c.Close()
	}
}

func (m *mdnsService) Peers() []Peer {
	m.mu.RLock()
	defer m.mu.RUnlock()
	now := time.Now()
	out := make([]Peer, 0, len(m.peers))
	for _, p := range m.peers {
		if now.Sub(p.LastSeen) <= peerExpiry {
			out = append(out, p)
		}
	}
	return out
}

func (m *mdnsService) readLoop(ctx context.Context, conn *net.UDPConn) {
	buf := make([]byte, 8192)
	for {
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		n, src, err := conn.ReadFromUDP(buf)
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
			}
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			return
		}
		m.handlePacket(conn, src, buf[:n])
	}
}

func (m *mdnsService) announceAndQueryLoop(ctx context.Context, conn *net.UDPConn, maddr *net.UDPAddr) {
	m.sendAnnounce(conn, maddr)
	m.sendQuery(conn, maddr)

	announceTick := time.NewTicker(announceEvery)
	queryTick := time.NewTicker(queryEvery)
	defer announceTick.Stop()
	defer queryTick.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-announceTick.C:
			m.sendAnnounce(conn, maddr)
		case <-queryTick.C:
			m.sendQuery(conn, maddr)
			m.pruneExpired()
		}
	}
}

func (m *mdnsService) pruneExpired() {
	now := time.Now()
	m.mu.Lock()
	changed := false
	for id, p := range m.peers {
		if now.Sub(p.LastSeen) > peerExpiry {
			delete(m.peers, id)
			changed = true
		}
	}
	var snapshot []Peer
	if changed && m.onPeer != nil {
		for _, p := range m.peers {
			snapshot = append(snapshot, p)
		}
	}
	m.mu.Unlock()
	if changed && m.onPeer != nil {
		m.onPeer(snapshot)
	}
}

func (m *mdnsService) instanceName() string {
	clean := strings.ReplaceAll(m.selfID, ".", "")
	if len(clean) > 12 {
		clean = clean[:12]
	}
	return fmt.Sprintf("EchoMusic-%s.%s", clean, ServiceType)
}

func (m *mdnsService) hostName() string {
	clean := strings.ReplaceAll(m.selfID, ".", "")
	if len(clean) > 12 {
		clean = clean[:12]
	}
	return fmt.Sprintf("echomusic-%s.local.", clean)
}

func (m *mdnsService) sendQuery(conn *net.UDPConn, maddr *net.UDPAddr) {
	pkt := buildPTRQuery(ServiceType)
	_, _ = conn.WriteToUDP(pkt, maddr)
}

func (m *mdnsService) sendAnnounce(conn *net.UDPConn, dst *net.UDPAddr) {
	ip := primaryLocalIPv4()
	if ip == nil {
		return
	}
	pkt := buildServiceResponse(
		ServiceType,
		m.instanceName(),
		m.hostName(),
		m.port,
		ip,
		map[string]string{
			"id":       m.selfID,
			"name":     m.selfName,
			"platform": m.selfPlatform,
			"ip":       ip.String(),
			"version":  "1",
		},
	)
	_, _ = conn.WriteToUDP(pkt, dst)
}

func (m *mdnsService) handlePacket(conn *net.UDPConn, src *net.UDPAddr, data []byte) {
	msg, err := parseDNSMessage(data)
	if err != nil {
		return
	}
	if !msg.isResponse {
		for _, q := range msg.questions {
			qname := strings.ToLower(q.name)
			if qname == strings.ToLower(ServiceType) || qname == strings.ToLower(m.instanceName()) || qname == strings.ToLower(m.hostName()) {
				maddr, err := net.ResolveUDPAddr("udp4", mdnsAddr)
				if err == nil {
					m.sendAnnounce(conn, maddr)
				}
				break
			}
		}
		return
	}

	allRecords := append(append([]dnsRecord{}, msg.answers...), msg.additionals...)
	if len(allRecords) == 0 {
		return
	}

	now := time.Now()
	m.mu.Lock()
	var updated bool
	hostIPs := make(map[string]string)

	for _, rr := range allRecords {
		if rr.rtype == dnsTypeA && len(rr.rdata) == 4 {
			hostIPs[strings.ToLower(rr.name)] = net.IP(rr.rdata).String()
		}
	}

	for _, rr := range allRecords {
		switch rr.rtype {
		case dnsTypePTR:
			if strings.EqualFold(rr.name, ServiceType) && rr.target != "" {
				key := strings.ToLower(rr.target)
				pp := m.byInst[key]
				if pp == nil {
					pp = &partialPeer{instance: rr.target, txt: make(map[string]string)}
					m.byInst[key] = pp
				}
				pp.seenAt = now
			}
		case dnsTypeSRV:
			key := strings.ToLower(rr.name)
			if strings.HasSuffix(key, strings.ToLower(ServiceType)) {
				pp := m.byInst[key]
				if pp == nil {
					pp = &partialPeer{instance: rr.name, txt: make(map[string]string)}
					m.byInst[key] = pp
				}
				pp.target = strings.ToLower(rr.target)
				pp.port = int(rr.port)
				pp.seenAt = now
			}
		case dnsTypeTXT:
			key := strings.ToLower(rr.name)
			if strings.HasSuffix(key, strings.ToLower(ServiceType)) {
				pp := m.byInst[key]
				if pp == nil {
					pp = &partialPeer{instance: rr.name, txt: make(map[string]string)}
					m.byInst[key] = pp
				}
				for k, v := range rr.txt {
					pp.txt[k] = v
				}
				pp.seenAt = now
			}
		}
	}

	selfIP := primaryLocalIPv4()
	for _, pp := range m.byInst {
		if pp.port <= 0 {
			continue
		}
		host := pp.txt["ip"]
		if host == "" {
			host = hostIPs[pp.target]
		}
		if host == "" && src != nil && src.IP != nil {
			if ip4 := src.IP.To4(); ip4 != nil && (selfIP == nil || !ip4.Equal(selfIP)) {
				host = ip4.String()
			}
		}
		if host == "" {
			continue
		}
		id := pp.txt["id"]
		if id == "" {
			id = pp.instance
		}
		if id == m.selfID {
			continue
		}
		name := pp.txt["name"]
		if name == "" {
			name = strings.TrimSuffix(pp.instance, "."+ServiceType)
		}
		platform := pp.txt["platform"]
		if platform == "" {
			platform = "unknown"
		}
		peer := Peer{
			ID:       id,
			Name:     name,
			Platform: platform,
			Host:     host,
			Port:     pp.port,
			LastSeen: now,
		}
		prev, exists := m.peers[id]
		if !exists || prev.Host != peer.Host || prev.Port != peer.Port || prev.Name != peer.Name {
			updated = true
		}
		m.peers[id] = peer
	}

	var snapshot []Peer
	if updated && m.onPeer != nil {
		for _, p := range m.peers {
			snapshot = append(snapshot, p)
		}
	}
	m.mu.Unlock()

	if updated && m.onPeer != nil {
		m.onPeer(snapshot)
	}
}

func primaryLocalIPv4() net.IP {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	for _, ifi := range ifaces {
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := ifi.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			ip4 := ipNet.IP.To4()
			if ip4 != nil && !ip4.IsLoopback() && !ip4.IsLinkLocalUnicast() {
				return ip4
			}
		}
	}
	return nil
}

// --- Minimal DNS/mDNS Wire Codec ---

type dnsQuestion struct {
	name  string
	qtype uint16
}

type dnsRecord struct {
	name   string
	rtype  uint16
	rdata  []byte
	target string
	port   uint16
	txt    map[string]string
}

type dnsMessage struct {
	isResponse  bool
	questions   []dnsQuestion
	answers     []dnsRecord
	additionals []dnsRecord
}

func encodeDNSName(buf *bytes.Buffer, name string) {
	name = strings.TrimSuffix(name, ".")
	if name == "" {
		buf.WriteByte(0)
		return
	}
	for _, label := range strings.Split(name, ".") {
		if len(label) > 63 {
			label = label[:63]
		}
		buf.WriteByte(byte(len(label)))
		buf.WriteString(label)
	}
	buf.WriteByte(0)
}

func buildPTRQuery(service string) []byte {
	var buf bytes.Buffer
	_ = binary.Write(&buf, binary.BigEndian, uint16(0)) // ID
	_ = binary.Write(&buf, binary.BigEndian, uint16(0)) // Flags: standard query
	_ = binary.Write(&buf, binary.BigEndian, uint16(1)) // QDCOUNT
	_ = binary.Write(&buf, binary.BigEndian, uint16(0)) // ANCOUNT
	_ = binary.Write(&buf, binary.BigEndian, uint16(0)) // NSCOUNT
	_ = binary.Write(&buf, binary.BigEndian, uint16(0)) // ARCOUNT
	encodeDNSName(&buf, service)
	_ = binary.Write(&buf, binary.BigEndian, uint16(dnsTypePTR))
	_ = binary.Write(&buf, binary.BigEndian, uint16(dnsClassIN))
	return buf.Bytes()
}

func buildServiceResponse(service, instance, host string, port int, ip net.IP, txt map[string]string) []byte {
	var buf bytes.Buffer
	_ = binary.Write(&buf, binary.BigEndian, uint16(0))      // ID
	_ = binary.Write(&buf, binary.BigEndian, uint16(0x8400)) // Flags: standard response, authoritative
	_ = binary.Write(&buf, binary.BigEndian, uint16(0))      // QDCOUNT
	_ = binary.Write(&buf, binary.BigEndian, uint16(4))      // ANCOUNT (PTR, SRV, TXT, A)
	_ = binary.Write(&buf, binary.BigEndian, uint16(0))      // NSCOUNT
	_ = binary.Write(&buf, binary.BigEndian, uint16(0))      // ARCOUNT

	// 1. PTR
	var ptrData bytes.Buffer
	encodeDNSName(&ptrData, instance)
	writeRR(&buf, service, dnsTypePTR, dnsClassIN, defaultTTL, ptrData.Bytes())

	// 2. SRV
	var srvData bytes.Buffer
	_ = binary.Write(&srvData, binary.BigEndian, uint16(0)) // priority
	_ = binary.Write(&srvData, binary.BigEndian, uint16(0)) // weight
	_ = binary.Write(&srvData, binary.BigEndian, uint16(port))
	encodeDNSName(&srvData, host)
	writeRR(&buf, instance, dnsTypeSRV, dnsClassIN|0x8000, defaultTTL, srvData.Bytes())

	// 3. TXT
	var txtData bytes.Buffer
	for k, v := range txt {
		entry := k + "=" + v
		if len(entry) > 255 {
			entry = entry[:255]
		}
		txtData.WriteByte(byte(len(entry)))
		txtData.WriteString(entry)
	}
	if txtData.Len() == 0 {
		txtData.WriteByte(0)
	}
	writeRR(&buf, instance, dnsTypeTXT, dnsClassIN|0x8000, defaultTTL, txtData.Bytes())

	// 4. A
	ip4 := ip.To4()
	if ip4 == nil {
		ip4 = net.IPv4(127, 0, 0, 1).To4()
	}
	writeRR(&buf, host, dnsTypeA, dnsClassIN|0x8000, defaultTTL, ip4)

	return buf.Bytes()
}

func writeRR(buf *bytes.Buffer, name string, rtype, rclass uint16, ttl uint32, rdata []byte) {
	encodeDNSName(buf, name)
	_ = binary.Write(buf, binary.BigEndian, rtype)
	_ = binary.Write(buf, binary.BigEndian, rclass)
	_ = binary.Write(buf, binary.BigEndian, ttl)
	_ = binary.Write(buf, binary.BigEndian, uint16(len(rdata)))
	buf.Write(rdata)
}

func parseDNSMessage(data []byte) (dnsMessage, error) {
	if len(data) < 12 {
		return dnsMessage{}, fmt.Errorf("short dns packet")
	}
	flags := binary.BigEndian.Uint16(data[2:4])
	qd := int(binary.BigEndian.Uint16(data[4:6]))
	an := int(binary.BigEndian.Uint16(data[6:8]))
	ns := int(binary.BigEndian.Uint16(data[8:10]))
	ar := int(binary.BigEndian.Uint16(data[10:12]))

	msg := dnsMessage{isResponse: (flags & 0x8000) != 0}
	off := 12

	for i := 0; i < qd; i++ {
		name, next, err := decodeDNSName(data, off)
		if err != nil || next+4 > len(data) {
			return msg, err
		}
		qtype := binary.BigEndian.Uint16(data[next : next+2])
		off = next + 4
		msg.questions = append(msg.questions, dnsQuestion{name: name, qtype: qtype})
	}

	readRRs := func(count int) ([]dnsRecord, error) {
		var out []dnsRecord
		for i := 0; i < count; i++ {
			name, next, err := decodeDNSName(data, off)
			if err != nil || next+10 > len(data) {
				return out, fmt.Errorf("truncated rr")
			}
			rtype := binary.BigEndian.Uint16(data[next : next+2])
			rdlen := int(binary.BigEndian.Uint16(data[next+8 : next+10]))
			rstart := next + 10
			rend := rstart + rdlen
			if rend > len(data) {
				return out, fmt.Errorf("truncated rdata")
			}
			rec := dnsRecord{name: name, rtype: rtype, rdata: data[rstart:rend]}
			switch rtype {
			case dnsTypePTR:
				if target, _, err := decodeDNSName(data, rstart); err == nil {
					rec.target = target
				}
			case dnsTypeSRV:
				if rdlen >= 6 {
					rec.port = binary.BigEndian.Uint16(data[rstart+4 : rstart+6])
					if target, _, err := decodeDNSName(data, rstart+6); err == nil {
						rec.target = target
					}
				}
			case dnsTypeTXT:
				rec.txt = parseTXT(data[rstart:rend])
			}
			out = append(out, rec)
			off = rend
		}
		return out, nil
	}

	if an > 0 {
		msg.answers, _ = readRRs(an)
	}
	if ns > 0 {
		_, _ = readRRs(ns)
	}
	if ar > 0 {
		msg.additionals, _ = readRRs(ar)
	}
	return msg, nil
}

func parseTXT(b []byte) map[string]string {
	out := make(map[string]string)
	i := 0
	for i < len(b) {
		l := int(b[i])
		i++
		if l == 0 || i+l > len(b) {
			break
		}
		kv := string(b[i : i+l])
		i += l
		if k, v, ok := strings.Cut(kv, "="); ok {
			out[k] = v
		}
	}
	return out
}

func decodeDNSName(data []byte, off int) (string, int, error) {
	var labels []string
	cur := off
	jumped := false
	nextOff := off
	seen := 0

	for {
		if cur >= len(data) || seen > 64 {
			return "", 0, fmt.Errorf("bad dns name")
		}
		seen++
		b := data[cur]
		if b == 0 {
			if !jumped {
				nextOff = cur + 1
			}
			break
		}
		if b&0xC0 == 0xC0 {
			if cur+1 >= len(data) {
				return "", 0, fmt.Errorf("bad dns pointer")
			}
			ptr := int(binary.BigEndian.Uint16(data[cur:cur+2]) & 0x3FFF)
			if !jumped {
				nextOff = cur + 2
				jumped = true
			}
			cur = ptr
			continue
		}
		l := int(b)
		cur++
		if cur+l > len(data) {
			return "", 0, fmt.Errorf("bad dns label")
		}
		labels = append(labels, string(data[cur:cur+l]))
		cur += l
	}
	if len(labels) == 0 {
		return ".", nextOff, nil
	}
	return strings.Join(labels, ".") + ".", nextOff, nil
}

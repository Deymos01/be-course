package main

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"net"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	headerLen       = 12
	recordHeaderLen = 10
	classIN         = 1
	flagQR          = 0x8000
	flagTC          = 0x0200
	maxDatagram     = 65535
	maxLabelLen     = 63
	maxNameLen      = 255
	responseTimeout = 5 * time.Second
)

var codes = map[string]uint16{
	"A":     1,
	"NS":    2,
	"CNAME": 5,
	"MX":    15,
	"TXT":   16,
	"AAAA":  28,
}

var typeNames = map[uint16]string{
	1:  "A",
	2:  "NS",
	5:  "CNAME",
	15: "MX",
	16: "TXT",
	28: "AAAA",
}

type question struct {
	name  string
	qtype uint16
	class uint16
}

type response struct {
	rcode   byte
	answers []string
	minTTL  uint32
	records int
}

type CacheEntry struct {
	answers []string
	exp     time.Time
}

var cache = make(map[string]CacheEntry)

func main() {
	os.Exit(run())
}

func run() int {
	if len(os.Args) != 3 {
		fmt.Fprintf(os.Stderr, "usage: %s <host> <port>\n", os.Args[0])
		return 2
	}

	ip := net.ParseIP(os.Args[1])
	if ip == nil {
		fmt.Fprintf(os.Stderr, "invalid server address %q\n", os.Args[1])
		return 1
	}

	port, err := strconv.Atoi(os.Args[2])
	if err != nil || port < 1 || port > 65535 {
		fmt.Fprintf(os.Stderr, "invalid port %q\n", os.Args[2])
		return 1
	}

	addr := &net.UDPAddr{
		IP:   ip,
		Port: port,
	}
	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dial UDP: %v\n", err)
		return 1
	}
	defer conn.Close()

	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		name, typeName := fields[0], fields[1]

		qtype, ok := codes[typeName]
		if !ok {
			fmt.Fprintf(os.Stderr, "unknown query type %q\n", typeName)
			return 2
		}

		key := canonicalName(name) + "|" + strconv.Itoa(int(qtype))
		if entry, ok := cache[key]; ok && time.Now().Before(entry.exp) {
			printBlock(name, typeName, response{answers: entry.answers})
			continue
		}

		txID := uint16(rand.Intn(1 << 16))
		packet, err := buildQuery(name, qtype, txID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "build query: %v\n", err)
			return 2
		}
		want := question{name: canonicalName(name), qtype: qtype, class: classIN}

		res, err := exchange(conn, packet, txID, want)
		if err != nil {
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				fmt.Printf("query %s %s\nstatus TIMEOUT\nend\n", name, typeName)
				return 1
			}
			fmt.Fprintf(os.Stderr, "exchange: %v\n", err)
			return 1
		}

		printBlock(name, typeName, res)
		if res.rcode == 0 && res.records > 0 && res.minTTL > 0 {
			cache[key] = CacheEntry{
				answers: res.answers,
				exp:     time.Now().Add(time.Duration(res.minTTL) * time.Second),
			}
		}
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "read stdin: %v\n", err)
		return 1
	}
	return 0
}

func exchange(conn *net.UDPConn, packet []byte, txID uint16, want question) (response, error) {
	if _, err := conn.Write(packet); err != nil {
		return response{}, fmt.Errorf("send query: %w", err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(responseTimeout)); err != nil {
		return response{}, fmt.Errorf("set read deadline: %w", err)
	}

	buf := make([]byte, maxDatagram)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			return response{}, err
		}
		msg := buf[:n]
		if len(msg) < headerLen {
			continue
		}
		if binary.BigEndian.Uint16(msg[0:2]) != txID {
			continue
		}
		res, err := parseResponse(msg, want)
		if err != nil {
			continue
		}
		return res, nil
	}
}

func parseResponse(msg []byte, want question) (response, error) {
	res := response{minTTL: math.MaxUint32}
	if len(msg) < headerLen {
		return res, errors.New("short header")
	}
	flags := binary.BigEndian.Uint16(msg[2:4])
	if flags&flagQR == 0 {
		return res, errors.New("not a response")
	}
	if flags&flagTC != 0 {
		return res, errors.New("truncated response")
	}
	res.rcode = byte(flags & 0x0F)

	opcode := (flags >> 11) & 0x0F
	if opcode != 0 {
		return res, fmt.Errorf("unexpected opcode %d", opcode)
	}

	qd := int(binary.BigEndian.Uint16(msg[4:6]))
	an := int(binary.BigEndian.Uint16(msg[6:8]))
	
	if qd >1 {
		return res, fmt.Errorf("unexpected QDCOUNT %d", qd)
	}

	offset := headerLen
	if qd == 1 {
		name, next, err := readName(msg, offset, len(msg))
		if err != nil {
			return res, fmt.Errorf("question name: %w", err)
		}
		if next+4 > len(msg) {
			return res, errors.New("short question")
		}
		qtype := binary.BigEndian.Uint16(msg[next : next+2])
		class := binary.BigEndian.Uint16(msg[next+2 : next+4])
		if !strings.EqualFold(name, want.name) || qtype != want.qtype || class != want.class {
			return res, errors.New("question mismatch")
		}
		offset = next + 4
	}

	for i := range an {
		_, next, err := readName(msg, offset, len(msg))
		if err != nil {
			return res, fmt.Errorf("answer %d name: %w", i+1, err)
		}
		if next+recordHeaderLen > len(msg) {
			return res, errors.New("short record header")
		}
		rtype := binary.BigEndian.Uint16(msg[next : next+2])
		ttl := binary.BigEndian.Uint32(msg[next+4 : next+8])
		rdlen := int(binary.BigEndian.Uint16(msg[next+8 : next+10]))

		start := next + recordHeaderLen
		end := start + rdlen
		if end > len(msg) {
			return res, errors.New("short rdata")
		}

		res.records++
		if ttl < res.minTTL {
			res.minTTL = ttl
		}
		offset = end

		if _, ok := typeNames[rtype]; !ok {
			continue
		}
		value, err := formatValue(rtype, msg, start, end)
		if err != nil {
			return res, fmt.Errorf("answer %d: %w", i+1, err)
		}
		res.answers = append(res.answers,
			fmt.Sprintf("answer %s %s %d", typeNames[rtype], value, ttl))
	}
	return res, nil
}

func formatValue(rtype uint16, msg []byte, start, end int) (string, error) {
	rdata := msg[start:end]
	switch rtype {
	case 1: // A
		if len(rdata) != 4 {
			return "", fmt.Errorf("A: rdata length %d, want 4", len(rdata))
		}
		return netip.AddrFrom4([4]byte(rdata)).String(), nil
	case 28: // AAAA
		if len(rdata) != 16 {
			return "", fmt.Errorf("AAAA: rdata length %d, want 16", len(rdata))
		}
		return netip.AddrFrom16([16]byte(rdata)).String(), nil
	case 2, 5: // NS, CNAME
		name, next, err := readName(msg, start, end)
		if err != nil {
			return "", fmt.Errorf("%s: %w", typeNames[rtype], err)
		}
		if next != end {
			return "", fmt.Errorf("%s: %d extra bytes after name", typeNames[rtype], end-next)
		}
		return name, nil
	case 15: // MX
		if len(rdata) < 2 {
			return "", errors.New("MX: missing preference")
		}
		pref := binary.BigEndian.Uint16(rdata[:2])
		name, next, err := readName(msg, start+2, end)
		if err != nil {
			return "", fmt.Errorf("MX: %w", err)
		}
		if next != end {
			return "", fmt.Errorf("MX: %d extra bytes after name", end-next)
		}
		return fmt.Sprintf("%d %s", pref, name), nil
	case 16: // TXT
		var sb strings.Builder
		for pos := 0; pos < len(rdata); {
			n := int(rdata[pos])
			pos++
			if pos+n > len(rdata) {
				return "", errors.New("TXT: string exceeds rdata")
			}
			sb.Write(rdata[pos : pos+n])
			pos += n
		}
		return sb.String(), nil
	default:
		return "", fmt.Errorf("unsupported type %d", rtype)
	}
}

func readName(msg []byte, offset, limit int) (string, int, error) {
	if offset < 0 || limit > len(msg) || offset > limit {
		return "", 0, errors.New("name offset out of range")
	}
	var labels []string
	next := -1
	seen := make(map[int]bool)

	for {
		if offset >= limit {
			return "", 0, errors.New("name exceeds limit")
		}
		if seen[offset] {
			return "", 0, errors.New("compression loop")
		}
		seen[offset] = true

		length := int(msg[offset])
		switch {
		case length&0xC0 == 0xC0:
			if offset+2 > limit {
				return "", 0, errors.New("truncated compression pointer")
			}
			ptr := int(binary.BigEndian.Uint16(msg[offset:offset+2]) & 0x3FFF)
			if ptr >= len(msg) {
				return "", 0, errors.New("compression pointer out of message")
			}
			if next == -1 {
				next = offset + 2
			}
			offset = ptr
			limit = len(msg)
		case length&0xC0 != 0:
			return "", 0, fmt.Errorf("reserved label type 0x%02x", length)
		case length == 0:
			if next == -1 {
				next = offset + 1
			}
			if len(labels) == 0 {
				return ".", next, nil
			}
			return strings.Join(labels, ".") + ".", next, nil
		default:
			end := offset + 1 + length
			if end > limit {
				return "", 0, errors.New("label exceeds limit")
			}
			labels = append(labels, string(msg[offset+1:end]))
			offset = end
		}
	}
}

func encodeName(name string) ([]byte, error) {
	if name == "." {
		return []byte{0}, nil
	}
	name = strings.TrimSuffix(name, ".")
	if name == "" {
		return nil, errors.New("empty name")
	}
	parts := strings.Split(name, ".")

	out := []byte{}
	total := 1
	for _, part := range parts {
		if len(part) == 0 {
			return nil, fmt.Errorf("empty label in %q", name)
		}
		if len(part) > maxLabelLen {
			return nil, fmt.Errorf("label %q longer than %d bytes", part, maxLabelLen)
		}
		out = append(out, byte(len(part)))
		out = append(out, []byte(part)...)
		total += 1 + len(part)
	}
	if total > maxNameLen {
		return nil, fmt.Errorf("name %q longer than %d bytes", name, maxNameLen)
	}
	return append(out, 0), nil
}

func buildQuery(name string, qtype uint16, txID uint16) ([]byte, error) {
	header := make([]byte, 12)
	binary.BigEndian.PutUint16(header[0:2], txID)
	binary.BigEndian.PutUint16(header[2:4], 0x0100)
	binary.BigEndian.PutUint16(header[4:6], 1)

	qname, err := encodeName(name)
	if err != nil {
		return nil, err
	}

	question := make([]byte, 4)
	binary.BigEndian.PutUint16(question[0:2], qtype)
	binary.BigEndian.PutUint16(question[2:4], 1)

	var packet []byte
	packet = append(packet, header...)
	packet = append(packet, qname...)
	packet = append(packet, question...)

	return packet, nil
}

func canonicalName(s string) string {
	s = strings.TrimSuffix(s, ".")
	if s == "" {
		return "."
	}
	return strings.ToLower(s) + "."
}

func printBlock(name, typeName string, res response) {
	fmt.Printf("query %s %s\n", name, typeName)
	fmt.Printf("status %s\n", convertRcode(res.rcode))
	for _, line := range res.answers {
		fmt.Println(line)
	}
	fmt.Println("end")
}

func convertRcode(rcode byte) string {
	var out string
	switch rcode {
	case 0:
		out = "NOERROR"
	case 1:
		out = "FORMERR"
	case 2:
		out = "SERVFAIL"
	case 3:
		out = "NXDOMAIN"
	case 5:
		out = "REFUSED"
	default:
		out = fmt.Sprintf("RCODE%d", rcode)
	}
	return out
}

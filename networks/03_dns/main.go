package main

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
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

type CacheEntry struct {
	answers []string
	exp     time.Time
}

var cache = make(map[string]CacheEntry)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintf(os.Stderr, "usage: %s <host> <port>\n", os.Args[0])
		os.Exit(2)
	}

	ip := net.ParseIP(os.Args[1])
	if ip == nil {
		fmt.Fprintf(os.Stderr, "invalid server address %q\n", os.Args[1])
		os.Exit(1)
	}

	port, err := strconv.Atoi(os.Args[2])
	if err != nil || port < 1 || port > 65535 {
		fmt.Fprintf(os.Stderr, "invalid port %q\n", os.Args[2])
		os.Exit(1)
	}

	addr := &net.UDPAddr{
		IP:   ip,
		Port: port,
	}
	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dial UDP: %v\n", err)
		os.Exit(1)
	}

	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}

		cacheKey := strings.ToLower(fields[0]) + "|" + fields[1]
		if entry, ok := cache[cacheKey]; ok && time.Now().Before(entry.exp) {
			fmt.Printf("query %s %s\nstatus NOERROR\n", fields[0], fields[1])
			for _, ans := range entry.answers {
				fmt.Println(ans)
			}
			fmt.Println("end")
			continue
		}

		qtype, ok := codes[fields[1]]
		if !ok {
			fmt.Fprintf(os.Stderr, "unknown query type %q\n", fields[1])
			os.Exit(1)
		}
		txID := uint16(rand.Intn(65536))
		packet, err := buildQuery(fields[0], qtype, txID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "build query: %v\n", err)
			os.Exit(1)
		}
		if _, err := conn.Write(packet); err != nil {
			fmt.Fprintf(os.Stderr, "send query: %v\n", err)
			os.Exit(1)
		}
		deadline := time.Now().Add(5 * time.Second)
		if err := conn.SetReadDeadline(deadline); err != nil {
			fmt.Fprintf(os.Stderr, "set read deadline: %v\n", err)
			os.Exit(1)
		}

		for {
			buffer := make([]byte, 4096)
			n, err := conn.Read(buffer)
			if err != nil {
				var netErr net.Error
				if errors.As(err, &netErr) && netErr.Timeout() {
					fmt.Printf("query %s %s\nstatus TIMEOUT\nend\n", fields[0], fields[1])
					os.Exit(1)
				}
				fmt.Fprintf(os.Stderr, "read response: %v\n", err)
				os.Exit(1)
			}

			if n < 12 {
				continue
			}
			respTxID := binary.BigEndian.Uint16(buffer[0:2])
			if respTxID != txID {
				continue
			}

			rcode := buffer[3] & 0x0F
			offset := 12
			_, offset = readName(buffer, offset)
			offset += 4
			rcodeString := convertRcode(rcode)

			fmt.Printf("query %s %s\nstatus %s\n", fields[0], fields[1], rcodeString)

			anCount := binary.BigEndian.Uint16(buffer[6:8])
			var cachedAnswers []string
			minTTL := uint32(math.MaxUint32)

			for i := 0; i < int(anCount); i++ {
				_, offset = readName(buffer, offset)

				rtype := binary.BigEndian.Uint16(buffer[offset : offset+2])
				offset += 4

				ttl := binary.BigEndian.Uint32(buffer[offset : offset+4])
				offset += 4
				minTTL = min(minTTL, ttl)

				rdlen := binary.BigEndian.Uint16(buffer[offset : offset+2])
				offset += 2

				rdata := buffer[offset : offset+int(rdlen)]

				var value string
				switch rtype {
				case 1, 28: // "A", "AAAA"
					value = net.IP(rdata).String()
				case 2, 5: // "NS", "CNAME"
					value, _ = readName(buffer, offset)
				case 15: // "MX"
					priority := binary.BigEndian.Uint16(rdata[0:2])
					name, _ := readName(buffer, offset+2)
					value = fmt.Sprintf("%d %s", priority, name)
				case 16: // "TXT"
					var parts []string
					pos := 0
					for pos < len(rdata) {
						strLen := int(rdata[pos])
						pos++
						parts = append(parts, string(rdata[pos:pos+strLen]))
						pos += strLen
					}
					value = strings.Join(parts, "")
				default:
					offset += int(rdlen)
					continue
				}

				offset += int(rdlen)
				answer := fmt.Sprintf("answer %s %s %d\n", typeNames[rtype], value, ttl)
				cachedAnswers = append(cachedAnswers, answer)
				fmt.Print(answer)
			}

			fmt.Println("end")

			if rcode == 0 && anCount > 0 && minTTL > 0 && minTTL != math.MaxUint32 {
				cache[cacheKey] = CacheEntry{
					answers: cachedAnswers,
					exp:     time.Now().Add(time.Duration(minTTL) * time.Second),
				}
			}

			break
		}
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "read stdin: %v\n", err)
		os.Exit(1)
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
		if len(part) > 63 {
			return nil, fmt.Errorf("label %q longer than 63 bytes", part)
		}
		out = append(out, byte(len(part)))
		out = append(out, []byte(part)...)
		total += 1 + len(parts)
	}
	if total > 255 {
		return nil, fmt.Errorf("name %q longer than 255 bytes", name)
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

func readName(packet []byte, offset int) (string, int) {
	var labels []string

	end := -1

	for i := 0; i < 128; i++ {
		if offset >= len(packet) {
			break
		}

		length := packet[offset]
		if length&0xC0 == 0xC0 {
			if offset+1 >= len(packet) {
				break
			}

			if end == -1 {
				end = offset + 2
			}

			offset = (int(length&0x3F) << 8) | int(packet[offset+1])
			continue
		} else if length == 0 {
			if end == -1 {
				end = offset + 1
			}
			break
		} else {
			offset++
			labels = append(labels, string(packet[offset:offset+int(length)]))
			offset += int(length)
		}
	}

	if end == -1 {
		end = offset
	}

	if len(labels) == 0 {
		return ".", end
	}

	return strings.Join(labels, ".") + ".", end
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

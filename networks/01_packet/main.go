package main

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"unicode"
)

type fields map[string]string

type ipv4Packet struct {
	fields         fields
	headerLen      int
	totalLen       int
	protocol       byte
	fragmentOffset int
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	frame, err := readFrame()
	if err != nil {
		return err
	}

	ethernet := parseEthernet(frame)
	printFields(ethernet)

	if ethernet["eth.ethertype"] != "0x0800" {
		return nil
	}

	ipSlice := frame[14:]
	ip, err := parseIPv4(ipSlice)
	if err != nil {
		return err
	}
	printFields(ip.fields)

	ipLen := min(ip.totalLen, len(ipSlice))
	ipPayload := ipSlice[ip.headerLen:ipLen]
	payloadLen := ip.totalLen - ip.headerLen
	transportHeaderLen := 0

	if ip.fragmentOffset == 0 {
		switch ip.protocol {
		case 6:
			tcp, headerLen, err := parseTCP(ipPayload, payloadLen)
			if err != nil {
				return err
			}

			printFields(tcp)
			transportHeaderLen = headerLen

		case 17:
			udp, err := parseUDP(ipPayload, payloadLen)
			if err != nil {
				return err
			}

			printFields(udp)
			transportHeaderLen = 8
		}
	}

	payloadLen -= transportHeaderLen
	if payloadLen < 0 {
		payloadLen = 0
	}

	fmt.Printf("payload.length %d\n", payloadLen)
	return nil
}

func readFrame() ([]byte, error) {
	raw, err := io.ReadAll(os.Stdin)
	if err != nil {
		return nil, err
	}

	var hexData strings.Builder

	for _, r := range string(raw) {
		if unicode.IsSpace(r) || r == ':' {
			continue
		}

		if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
			return nil, fmt.Errorf("invalid character in hex dump: %q", r)
		}

		hexData.WriteRune(r)
	}

	frame, err := hex.DecodeString(hexData.String())
	if err != nil {
		return nil, err
	}

	if len(frame) < 14 {
		return nil, fmt.Errorf("frame too short: %d bytes", len(frame))
	}

	return frame, nil
}

func parseEthernet(frame []byte) fields {
	return fields{
		"eth.dst":       net.HardwareAddr(frame[0:6]).String(),
		"eth.src":       net.HardwareAddr(frame[6:12]).String(),
		"eth.ethertype": fmt.Sprintf("0x%04x", binary.BigEndian.Uint16(frame[12:14])),
	}
}

func parseIPv4(packet []byte) (ipv4Packet, error) {
	if len(packet) < 20 {
		return ipv4Packet{}, fmt.Errorf("IPv4 header too short: %d bytes", len(packet))
	}

	version := packet[0] >> 4
	if version != 4 {
		return ipv4Packet{}, fmt.Errorf("invalid IPv4 version: %d", version)
	}

	headerLen := int(packet[0]&0x0f) * 4
	if headerLen < 20 || len(packet) < headerLen {
		return ipv4Packet{}, fmt.Errorf("invalid IPv4 header length: %d", headerLen)
	}

	totalLen := int(binary.BigEndian.Uint16(packet[2:4]))
	if totalLen < headerLen {
		return ipv4Packet{}, fmt.Errorf("invalid IPv4 total length: %d", totalLen)
	}

	flagsAndOffset := binary.BigEndian.Uint16(packet[6:8])

	var flags []string

	if flagsAndOffset&0x4000 != 0 {
		flags = append(flags, "DF")
	}

	if flagsAndOffset&0x2000 != 0 {
		flags = append(flags, "MF")
	}

	flagValue := "none"
	if len(flags) > 0 {
		flagValue = strings.Join(flags, ",")
	}

	fragmentOffset := int(flagsAndOffset&0x1fff) * 8

	return ipv4Packet{
		fields: fields{
			"ip.version":        strconv.Itoa(int(version)),
			"ip.ihl_bytes":      strconv.Itoa(headerLen),
			"ip.total_length":   strconv.Itoa(totalLen),
			"ip.id":             fmt.Sprintf("0x%04x", binary.BigEndian.Uint16(packet[4:6])),
			"ip.flags":          flagValue,
			"ip.frag_offset":    strconv.Itoa(fragmentOffset),
			"ip.ttl":            strconv.Itoa(int(packet[8])),
			"ip.protocol":       strconv.Itoa(int(packet[9])),
			"ip.src":            net.IP(packet[12:16]).String(),
			"ip.dst":            net.IP(packet[16:20]).String(),
			"ip.checksum_valid": strconv.FormatBool(validateIPv4Checksum(packet[:headerLen])),
		},
		headerLen:      headerLen,
		totalLen:       totalLen,
		protocol:       packet[9],
		fragmentOffset: fragmentOffset,
	}, nil
}

func validateIPv4Checksum(header []byte) bool {
	var sum uint32

	for i := 0; i < len(header); i += 2 {
		if i == 10 {
			continue
		}

		sum += uint32(binary.BigEndian.Uint16(header[i : i+2]))
	}

	for sum > 0xffff {
		sum = (sum >> 16) + (sum & 0xffff)
	}

	return ^uint16(sum) == binary.BigEndian.Uint16(header[10:12])
}

func parseTCP(segment []byte, ipPayloadLen int) (fields, int, error) {
	if ipPayloadLen < 20 || len(segment) < 20 {
		return nil, 0, fmt.Errorf("TCP header too short")
	}

	headerLen := int(segment[12]>>4) * 4
	if headerLen < 20 || headerLen > ipPayloadLen {
		return nil, 0, fmt.Errorf("invalid TCP header length: %d", headerLen)
	}

	flagByte := segment[13]

	flagDefinitions := []struct {
		mask byte
		name string
	}{
		{0x01, "FIN"},
		{0x02, "SYN"},
		{0x04, "RST"},
		{0x08, "PSH"},
		{0x10, "ACK"},
		{0x20, "URG"},
	}

	var flags []string

	for _, definition := range flagDefinitions {
		if flagByte&definition.mask != 0 {
			flags = append(flags, definition.name)
		}
	}

	flagValue := "none"
	if len(flags) > 0 {
		flagValue = strings.Join(flags, ",")
	}

	return fields{
		"tcp.src_port":          strconv.Itoa(int(binary.BigEndian.Uint16(segment[0:2]))),
		"tcp.dst_port":          strconv.Itoa(int(binary.BigEndian.Uint16(segment[2:4]))),
		"tcp.seq":               strconv.FormatUint(uint64(binary.BigEndian.Uint32(segment[4:8])), 10),
		"tcp.ack":               strconv.FormatUint(uint64(binary.BigEndian.Uint32(segment[8:12])), 10),
		"tcp.data_offset_bytes": strconv.Itoa(headerLen),
		"tcp.flags":             flagValue,
		"tcp.window":            strconv.Itoa(int(binary.BigEndian.Uint16(segment[14:16]))),
	}, headerLen, nil
}

func parseUDP(segment []byte, ipPayloadLen int) (fields, error) {
	if ipPayloadLen < 8 || len(segment) < 8 {
		return nil, fmt.Errorf("UDP header too short")
	}

	return fields{
		"udp.src_port": strconv.Itoa(int(binary.BigEndian.Uint16(segment[0:2]))),
		"udp.dst_port": strconv.Itoa(int(binary.BigEndian.Uint16(segment[2:4]))),
		"udp.length":   strconv.Itoa(int(binary.BigEndian.Uint16(segment[4:6]))),
	}, nil
}

func printFields(values fields) {
	for key, value := range values {
		fmt.Printf("%s %s\n", key, value)
	}
}

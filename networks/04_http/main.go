package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
)

var crlf = []byte{'\r', '\n'}

type header struct {
	name  string
	value string
}

type message struct {
	isRequest bool
	method    string
	target    string
	status    string
	reason    string
	headers   []header
	body      []byte
}

func main() {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		fail("bad_start_line")
	}

	msg, reason := parseMessage(data)
	if reason != "" {
		fail(reason)
	}

	printMessage(msg)
}

func fail(reason string) {
	os.Stdout.WriteString("error " + reason + "\n")
	os.Exit(1)
}

func parseMessage(data []byte) (*message, string) {
	idx := bytes.Index(data, crlf)
	if idx < 0 {
		return nil, "bad_start_line"
	}

	msg, reason := parseStartLine(data[:idx])
	if reason != "" {
		return nil, reason
	}

	headers, contentLen, chunked, bodyStart, reason := parseHeaders(data, idx+2)
	if reason != "" {
		return nil, reason
	}
	msg.headers = headers

	body, reason := parseBody(data, bodyStart, contentLen, chunked)
	if reason != "" {
		return nil, reason
	}
	msg.body = body

	return msg, ""
}

func parseStartLine(line []byte) (*message, string) {
	if bytes.HasPrefix(line, []byte("HTTP/")) {
		parts := bytes.SplitN(line, []byte{' '}, 3)
		if len(parts) != 3 {
			return nil, "bad_start_line"
		}

		if !bytes.Equal(parts[0], []byte("HTTP/1.1")) {
			return nil, "bad_start_line"
		}

		if len(parts[1]) != 3 || !isDigits(parts[1]) {
			return nil, "bad_start_line"
		}

		if !hasNoCRLF(parts[2]) {
			return nil, "bad_start_line"
		}

		return &message{
			isRequest: false,
			status:    string(parts[1]),
			reason:    string(parts[2]),
		}, ""
	}

	parts := bytes.Split(line, []byte{' '})
	if len(parts) != 3 {
		return nil, "bad_start_line"
	}

	if len(parts[0]) == 0 || len(parts[1]) == 0 || len(parts[2]) == 0 {
		return nil, "bad_start_line"
	}

	if !bytes.Equal(parts[2], []byte("HTTP/1.1")) {
		return nil, "bad_start_line"
	}

	if !validToken(parts[0]) {
		return nil, "bad_start_line"
	}
	if !validRequestTarget(parts[1]) {
		return nil, "bad_start_line"
	}

	return &message{
		isRequest: true,
		method:    string(parts[0]),
		target:    string(parts[1]),
	}, ""
}

func validToken(b []byte) bool {
	if len(b) == 0 {
		return false
	}
	for _, c := range b {
		if !isTokenByte(c) {
			return false
		}
	}
	return true
}

func validRequestTarget(b []byte) bool {
	if len(b) == 0 {
		return false
	}
	for _, c := range b {
		if c <= 0x1F || c == 0x7F {
			return false
		}
	}
	return true
}

func hasNoCRLF(b []byte) bool {
	for _, c := range b {
		if c == '\r' || c == '\n' {
			return false
		}
	}
	return true
}

func parseHeaders(data []byte, pos int) ([]header, int, bool, int, string) {
	var headers []header
	contentLen := -1
	chunked := false
	var firstContentLenStr string

	for {
		end := bytes.Index(data[pos:], crlf)
		if end < 0 {
			return nil, 0, false, 0, "bad_header"
		}

		line := data[pos : pos+end]
		pos += end + 2

		if len(line) == 0 {
			break
		}

		colon := bytes.IndexByte(line, ':')
		if colon <= 0 {
			return nil, 0, false, 0, "bad_header"
		}

		name := line[:colon]
		if !validFieldName(name) {
			return nil, 0, false, 0, "bad_header"
		}

		val := bytes.Trim(line[colon+1:], " \t")

		if !hasNoCRLF(val) {
			return nil, 0, false, 0, "bad_header"
		}

		lowName := string(bytes.ToLower(name))
		strVal := string(val)

		headers = append(headers, header{
			name:  lowName,
			value: strVal,
		})

		if lowName == "content-length" {
			if !isDigits(val) {
				return nil, 0, false, 0, "bad_header"
			}

			normalized := string(trimLeadingZeros(val))

			if firstContentLenStr == "" {
				firstContentLenStr = normalized
				contentLen = parseDecimalCap(val, len(data))
			} else if firstContentLenStr != normalized {
				return nil, 0, false, 0, "bad_header"
			}
		}

		if lowName == "transfer-encoding" && isChunkedValue(strVal) {
			chunked = true
		}
	}

	return headers, contentLen, chunked, pos, ""
}

func trimLeadingZeros(b []byte) []byte {
	i := 0
	for i < len(b)-1 && b[i] == '0' {
		i++
	}
	return b[i:]
}

func validFieldName(name []byte) bool {
	if len(name) == 0 {
		return false
	}

	for _, b := range name {
		if !isTokenByte(b) {
			return false
		}
	}

	return true
}

func isTokenByte(b byte) bool {
	if (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') {
		return true
	}

	switch b {
	case '!', '#', '$', '%', '&', '\'', '*', '+', '-', '.', '^', '_', '`', '|', '~':
		return true
	}

	return false
}

func parseBody(data []byte, bodyStart int, contentLen int, chunked bool) ([]byte, string) {
	rest := data[bodyStart:]

	if chunked {
		return parseChunked(rest)
	}

	if contentLen >= 0 {
		if len(rest) < contentLen {
			return nil, "incomplete_body"
		}

		return rest[:contentLen], ""
	}

	return []byte{}, ""
}

func parseChunked(data []byte) ([]byte, string) {
	var body []byte
	pos := 0

	capLim := len(data) + 1

	for {
		end := bytes.Index(data[pos:], crlf)
		if end < 0 {
			return nil, "bad_chunk"
		}

		lineSize := data[pos : pos+end]
		pos += end + 2

		lineSize = bytes.Trim(lineSize, " \t")

		if i := bytes.IndexByte(lineSize, ';'); i >= 0 {
			lineSize = lineSize[:i]
		}

		lineSize = bytes.Trim(lineSize, " \t")

		if len(lineSize) == 0 || !isHexDigits(lineSize) {
			return nil, "bad_chunk"
		}

		size := parseHexCap(lineSize, capLim)

		if size == 0 {
			for {
				trailEnd := bytes.Index(data[pos:], crlf)
				if trailEnd < 0 {
					return nil, "bad_chunk"
				}

				trailLine := data[pos : pos+trailEnd]
				pos += trailEnd + 2
				if len(trailLine) == 0 {
					return body, ""
				}
			}
		}

		if size > len(data)-pos {
			return nil, "bad_chunk"
		}

		body = append(body, data[pos:pos+size]...)
		pos += size

		if len(data)-pos < 2 || data[pos] != '\r' || data[pos+1] != '\n' {
			return nil, "bad_chunk"
		}

		pos += 2
	}
}

func isDigits(b []byte) bool {
	if len(b) == 0 {
		return false
	}

	for _, c := range b {
		if c < '0' || c > '9' {
			return false
		}
	}

	return true
}

func isHexDigits(b []byte) bool {
	if len(b) == 0 {
		return false
	}

	for _, c := range b {
		if !isHexByte(c) {
			return false
		}
	}

	return true
}

func isHexByte(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

func parseDecimalCap(b []byte, limit int) int {
	n := 0
	capLimit := limit + 1

	for _, c := range b {
		d := int(c - '0')

		if n > capLimit/10 {
			n = capLimit
			continue
		}

		n = min(n*10+d, capLimit)
	}

	return n
}

func parseHexCap(b []byte, capLimit int) int {
	n := 0

	for _, c := range b {
		var d int

		switch {
		case c >= '0' && c <= '9':
			d = int(c - '0')
		case c >= 'a' && c <= 'f':
			d = int(c-'a') + 10
		case c >= 'A' && c <= 'F':
			d = int(c-'A') + 10
		default:
			return capLimit
		}

		if n > capLimit/16 {
			n = capLimit
			continue
		}

		n = min(n*16+d, capLimit)
	}

	return n
}

func isChunkedValue(s string) bool {
	for part := range strings.SplitSeq(s, ",") {
		part = strings.Trim(part, " \t")

		if strings.ToLower(part) == "chunked" {
			return true
		}
	}

	return false
}

func isPrintable(b []byte) bool {
	for _, c := range b {
		if c < 0x20 || c > 0x7E {
			return false
		}
	}

	return true
}

func printMessage(msg *message) {
	var buf bytes.Buffer

	if msg.isRequest {
		fmt.Fprint(&buf, "type request\n")
		fmt.Fprintf(&buf, "method %s\n", msg.method)
		fmt.Fprintf(&buf, "target %s\n", msg.target)
		fmt.Fprint(&buf, "version HTTP/1.1\n")
	} else {
		fmt.Fprint(&buf, "type response\n")
		fmt.Fprint(&buf, "version HTTP/1.1\n")
		fmt.Fprintf(&buf, "status %s\n", msg.status)
		fmt.Fprintf(&buf, "reason %s\n", msg.reason)
	}

	for _, h := range msg.headers {
		fmt.Fprintf(&buf, "header %s", h.name)

		if h.value != "" {
			fmt.Fprintf(&buf, " %s", h.value)
		}

		buf.WriteByte('\n')
	}

	fmt.Fprintf(&buf, "body.length %d\n", len(msg.body))

	if len(msg.body) > 0 && isPrintable(msg.body) {
		fmt.Fprint(&buf, "body.text ")
		buf.Write(msg.body)
		buf.WriteByte('\n')
	}

	os.Stdout.Write(buf.Bytes())
}

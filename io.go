package main

import (
	"bufio"
	"io"
	"net"
	"strings"
)

func ioReadFull(r io.Reader, b []byte) (int, error) { return io.ReadFull(r, b) }
func ioCopy(dst io.Writer, src io.Reader) (int64, error) {
	return io.Copy(dst, src)
}

func newBufioReader(c net.Conn) *bufio.Reader { return bufio.NewReader(c) }

// parseHostHeader scans raw HTTP header bytes for "Host:".
func parseHostHeader(header []byte) string {
	s := string(header)
	for _, line := range strings.Split(s, "\r\n") {
		l := strings.ToLower(line)
		if strings.HasPrefix(l, "host:") {
			return strings.ToLower(strings.TrimSpace(line[5:]))
		}
	}
	return ""
}

// parseRequestLine returns method and path of the first HTTP line.
func parseRequestLine(line string) (string, string) {
	parts := strings.Fields(strings.TrimSpace(line))
	if len(parts) >= 2 {
		return parts[0], parts[1]
	}
	return "", ""
}

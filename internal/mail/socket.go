package mail

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
)

// SocketServer accepts pipe-mode relays from the `xlistman deliver` command
// over a local Unix socket. Each connection carries one message: the first
// line is the recipient address, the remainder is the raw RFC 822 message.
// Relaying to the running daemon keeps a single writer to the database
// (see ADR 0002).
type SocketServer struct {
	Path   string
	Server *LMTPServer
}

// ListenAndServe accepts pipe-mode relays until ctx is cancelled.
func (s *SocketServer) ListenAndServe(ctx context.Context) error {
	_ = os.Remove(s.Path)
	ln, err := net.Listen("unix", s.Path)
	if err != nil {
		return fmt.Errorf("socket listen: %w", err)
	}
	defer os.Remove(s.Path)
	defer ln.Close()

	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
				return fmt.Errorf("socket accept: %w", err)
			}
		}
		go s.handleConn(ctx, conn)
	}
}

func (s *SocketServer) handleConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()

	r := bufio.NewReader(conn)
	rcpt, err := r.ReadString('\n')
	if err != nil {
		return
	}
	rcpt = strings.TrimSpace(rcpt)

	raw, err := io.ReadAll(r)
	if err != nil {
		return
	}

	if err := s.Server.ProcessMessage(ctx, rcpt, raw); err != nil {
		fmt.Fprintf(conn, "ERR %v\n", err)
		return
	}
	fmt.Fprintln(conn, "OK")
}

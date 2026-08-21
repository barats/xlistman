package mail

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"fmt"
	"math/big"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// smtpServer is a minimal in-process SMTP server that can speak plaintext,
// offer STARTTLS, or accept implicit TLS, so the client's TLS modes can be
// exercised without an external MTA.
type smtpServer struct {
	t         *testing.T
	ln        net.Listener
	tlsCfg    *tls.Config
	implicit  bool // wrap every accepted connection in TLS immediately
	offerTLS  bool // advertise STARTTLS in the EHLO response
	allowAuth bool // advertise AUTH PLAIN on encrypted sessions

	mu            sync.Mutex
	tlsUsed       bool
	authSeen      bool
	authPlaintext bool // AUTH received on an unencrypted session
	authUser      string
	authPass      string
}

type smtpServerOpt func(*smtpServer)

func withImplicit() smtpServerOpt { return func(s *smtpServer) { s.implicit = true } }
func withoutTLS() smtpServerOpt   { return func(s *smtpServer) { s.offerTLS = false } }
func withoutAuth() smtpServerOpt  { return func(s *smtpServer) { s.allowAuth = false } }
func withBadCert() smtpServerOpt {
	return func(s *smtpServer) { s.tlsCfg = tlsConfigFor(tlsCerts["bad"]) }
}

func startSMTPServer(t *testing.T, opts ...smtpServerOpt) *smtpServer {
	t.Helper()
	s := &smtpServer{t: t, offerTLS: true, allowAuth: true}
	for _, o := range opts {
		o(s)
	}
	if s.tlsCfg == nil {
		s.tlsCfg = tlsConfigFor(tlsCerts["good"])
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	s.ln = ln
	t.Cleanup(func() { ln.Close() })
	go s.serve()
	return s
}

func (s *smtpServer) addr() string { return s.ln.Addr().String() }

func (s *smtpServer) sawTLS() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tlsUsed
}

func (s *smtpServer) sawAuth() (bool, bool, string, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.authSeen, s.authPlaintext, s.authUser, s.authPass
}

func (s *smtpServer) markTLS() {
	s.mu.Lock()
	s.tlsUsed = true
	s.mu.Unlock()
}

func (s *smtpServer) markAuth(plaintext bool, payload string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.authSeen = true
	s.authPlaintext = plaintext
	// AUTH PLAIN payload is <identity>\0<user>\0<pass>, base64-encoded.
	decoded, err := base64.StdEncoding.DecodeString(payload)
	if err == nil {
		parts := strings.Split(string(decoded), "\x00")
		if len(parts) == 3 {
			s.authUser = parts[1]
			s.authPass = parts[2]
		}
	}
}

func (s *smtpServer) serve() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.handle(conn)
	}
}

func (s *smtpServer) handle(conn net.Conn) {
	defer conn.Close()
	if s.implicit {
		tconn := tls.Server(conn, s.tlsCfg)
		if err := tconn.Handshake(); err != nil {
			return
		}
		s.markTLS()
		conn = tconn
	}
	fmt.Fprintf(conn, "220 localhost ESMTP fake\r\n")
	s.session(conn, s.implicit)
}

// session runs the SMTP command loop. secured reports whether the session is
// already encrypted; after a STARTTLS upgrade the loop recurses with secured
// (the greeting is only sent on the first call).
func (s *smtpServer) session(conn net.Conn, secured bool) {
	r := bufio.NewReader(conn)
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) == 0 {
			continue
		}
		cmd := strings.ToUpper(fields[0])
		switch cmd {
		case "EHLO":
			fmt.Fprintf(conn, "250-localhost\r\n")
			if s.offerTLS && !secured {
				fmt.Fprintf(conn, "250-STARTTLS\r\n")
			}
			if s.allowAuth && secured {
				fmt.Fprintf(conn, "250-AUTH PLAIN\r\n")
			}
			fmt.Fprintf(conn, "250 SIZE 10000000\r\n")
		case "STARTTLS":
			fmt.Fprintf(conn, "220 2.0.0 Ready to start TLS\r\n")
			tconn := tls.Server(conn, s.tlsCfg)
			if err := tconn.Handshake(); err != nil {
				return
			}
			s.markTLS()
			s.session(tconn, true)
			return
		case "AUTH":
			plaintext := !secured
			payload := ""
			if len(fields) >= 3 {
				payload = fields[2]
			}
			s.markAuth(plaintext, payload)
			fmt.Fprintf(conn, "235 2.7.0 Authentication successful\r\n")
		case "MAIL":
			fmt.Fprintf(conn, "250 2.1.0 Ok\r\n")
		case "RCPT":
			fmt.Fprintf(conn, "250 2.1.5 Ok\r\n")
		case "DATA":
			fmt.Fprintf(conn, "354 End data with <CR><LF>.<CR><LF>\r\n")
			for {
				dline, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if strings.TrimSpace(dline) == "." {
					break
				}
			}
			fmt.Fprintf(conn, "250 2.0.0 Ok: queued\r\n")
		case "QUIT":
			fmt.Fprintf(conn, "221 2.0.0 Bye\r\n")
			return
		case "RSET", "NOOP":
			fmt.Fprintf(conn, "250 2.0.0 Ok\r\n")
		default:
			fmt.Fprintf(conn, "500 5.5.1 Unrecognized command\r\n")
		}
	}
}

var tlsCerts = map[string]*tls.Certificate{}

func init() {
	tlsCerts["good"] = selfSignedCert([]net.IP{net.ParseIP("127.0.0.1")}, []string{"localhost"})
	tlsCerts["bad"] = selfSignedCert(nil, []string{"wrong.example.com"})
}

func tlsConfigFor(cert *tls.Certificate) *tls.Config {
	return &tls.Config{Certificates: []tls.Certificate{*cert}}
}

func selfSignedCert(ips []net.IP, dns []string) *tls.Certificate {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		panic(err)
	}
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  ips,
		DNSNames:     dns,
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	if err != nil {
		panic(err)
	}
	return &tls.Certificate{Certificate: [][]byte{der}, PrivateKey: priv}
}

const testMessage = "Subject: test\r\n\r\nbody\r\n"

// sendTo relays one message through the client to the fake server and returns
// any error, asserting the message was accepted (fake queues without error).
func sendTo(t *testing.T, srv *smtpServer, c *SMTPClient) error {
	t.Helper()
	if c.Host == "" {
		c.Host = "127.0.0.1"
		c.Port = portOf(t, srv)
	}
	return c.Send("sender@example.com", "rcpt@example.com", []byte(testMessage))
}

func portOf(t *testing.T, srv *smtpServer) int {
	t.Helper()
	_, portStr, err := net.SplitHostPort(srv.addr())
	if err != nil {
		t.Fatalf("split hostport: %v", err)
	}
	var port int
	fmt.Sscanf(portStr, "%d", &port)
	return port
}

func TestSMTPClient_TLSModes(t *testing.T) {
	tests := []struct {
		name       string
		serverOpts []smtpServerOpt
		client     SMTPClient
		wantErr    bool
		wantTLS    bool
	}{
		{
			name:    "starttls default opportunistic uses TLS when offered",
			client:  SMTPClient{TLS: "starttls", TLSInsecureSkipVerify: true},
			wantTLS: true,
		},
		{
			name:       "starttls opportunistic falls back to plaintext when not offered",
			serverOpts: []smtpServerOpt{withoutTLS()},
			client:     SMTPClient{TLS: "starttls"},
		},
		{
			name:       "starttls-required fails when not offered",
			serverOpts: []smtpServerOpt{withoutTLS()},
			client:     SMTPClient{TLS: "starttls-required"},
			wantErr:    true,
		},
		{
			name:    "starttls-required succeeds when offered",
			client:  SMTPClient{TLS: "starttls-required", TLSInsecureSkipVerify: true},
			wantTLS: true,
		},
		{
			name:       "none stays plaintext even when offered",
			serverOpts: []smtpServerOpt{},
			client:     SMTPClient{TLS: "none"},
		},
		{
			name:       "implicit TLS",
			serverOpts: []smtpServerOpt{withImplicit()},
			client:     SMTPClient{TLS: "implicit", TLSInsecureSkipVerify: true},
			wantTLS:    true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := startSMTPServer(t, tt.serverOpts...)
			err := sendTo(t, srv, &tt.client)
			if tt.wantErr != (err != nil) {
				t.Fatalf("Send error = %v, wantErr = %v", err, tt.wantErr)
			}
			if srv.sawTLS() != tt.wantTLS {
				t.Errorf("server saw TLS = %v, want %v", srv.sawTLS(), tt.wantTLS)
			}
		})
	}
}

func TestSMTPClient_StartTLSRequiredError(t *testing.T) {
	srv := startSMTPServer(t, withoutTLS())
	err := sendTo(t, srv, &SMTPClient{TLS: "starttls-required"})
	if err == nil {
		t.Fatal("expected error when STARTTLS required but not offered")
	}
	if !strings.Contains(err.Error(), "does not offer STARTTLS") {
		t.Errorf("error = %q, want to mention missing STARTTLS", err)
	}
}

func TestSMTPClient_Auth(t *testing.T) {
	t.Run("auth over implicit TLS", func(t *testing.T) {
		srv := startSMTPServer(t, withImplicit())
		err := sendTo(t, srv, &SMTPClient{TLS: "implicit", TLSInsecureSkipVerify: true, Username: "user", Password: "pass"})
		if err != nil {
			t.Fatalf("Send: %v", err)
		}
		seen, plaintext, user, pass := srv.sawAuth()
		if !seen || plaintext {
			t.Fatalf("auth seen=%v plaintext=%v, want seen with TLS", seen, plaintext)
		}
		if user != "user" || pass != "pass" {
			t.Errorf("auth credentials = %q/%q, want user/pass", user, pass)
		}
	})

	t.Run("auth over STARTTLS", func(t *testing.T) {
		srv := startSMTPServer(t)
		err := sendTo(t, srv, &SMTPClient{TLS: "starttls-required", TLSInsecureSkipVerify: true, Username: "user", Password: "pass"})
		if err != nil {
			t.Fatalf("Send: %v", err)
		}
		seen, plaintext, _, _ := srv.sawAuth()
		if !seen || plaintext {
			t.Errorf("auth seen=%v plaintext=%v, want seen over TLS", seen, plaintext)
		}
	})

	t.Run("auth refused without TLS (starttls not offered)", func(t *testing.T) {
		srv := startSMTPServer(t, withoutTLS())
		err := sendTo(t, srv, &SMTPClient{TLS: "starttls", Username: "user", Password: "pass"})
		if err == nil {
			t.Fatal("expected error when auth is requested without TLS")
		}
		if !strings.Contains(err.Error(), "without TLS") {
			t.Errorf("error = %q, want to mention TLS", err)
		}
		if seen, _, _, _ := srv.sawAuth(); seen {
			t.Error("server received AUTH despite client refusing to send it")
		}
	})

	t.Run("auth refused without TLS (tls none)", func(t *testing.T) {
		srv := startSMTPServer(t, withoutTLS())
		err := sendTo(t, srv, &SMTPClient{TLS: "none", Username: "user", Password: "pass"})
		if err == nil {
			t.Fatal("expected error when auth is requested on a plaintext connection")
		}
		if seen, _, _, _ := srv.sawAuth(); seen {
			t.Error("server received AUTH despite client refusing to send it")
		}
	})
}

func TestSMTPClient_InsecureSkipVerify(t *testing.T) {
	// The fake server presents a cert for wrong.example.com only; verifying
	// against ServerName=127.0.0.1 must fail unless skip-verify is set.
	t.Run("verification fails by default", func(t *testing.T) {
		srv := startSMTPServer(t, withImplicit(), withBadCert())
		err := sendTo(t, srv, &SMTPClient{TLS: "implicit"})
		if err == nil {
			t.Fatal("expected certificate verification failure")
		}
	})

	t.Run("skip-verify accepts mismatched cert", func(t *testing.T) {
		srv := startSMTPServer(t, withImplicit(), withBadCert())
		err := sendTo(t, srv, &SMTPClient{TLS: "implicit", TLSInsecureSkipVerify: true})
		if err != nil {
			t.Fatalf("Send with InsecureSkipVerify: %v", err)
		}
		if !srv.sawTLS() {
			t.Error("server did not see TLS")
		}
	})
}

func TestSMTPClient_SinkMode(t *testing.T) {
	dir := t.TempDir()
	c := &SMTPClient{Mode: "sink", SinkDir: dir}
	if err := c.Send("sender@example.com", "rcpt@example.com", []byte(testMessage)); err != nil {
		t.Fatalf("Send: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read sink dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("sink files = %d, want 1", len(entries))
	}
}

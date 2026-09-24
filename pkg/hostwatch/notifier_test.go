package hostwatch

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"fmt"
	"io"
	"math/big"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func testCert(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "fake smtp"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(leaf)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}, pool
}

type fakeSMTP struct {
	ln            net.Listener
	tlsCfg        *tls.Config
	offerStartTLS bool
	rejectRcpt    bool

	mu    sync.Mutex
	auth  string
	from  string
	rcpts []string
	data  string
	done  chan struct{}
}

func startFakeSMTP(t *testing.T, implicit, offerStartTLS bool) (*fakeSMTP, *x509.CertPool) {
	t.Helper()
	cert, pool := testCert(t)
	f := &fakeSMTP{
		tlsCfg:        &tls.Config{Certificates: []tls.Certificate{cert}},
		offerStartTLS: offerStartTLS,
		done:          make(chan struct{}),
	}
	var err error
	if implicit {
		f.ln, err = tls.Listen("tcp", "127.0.0.1:0", f.tlsCfg)
	} else {
		f.ln, err = net.Listen("tcp", "127.0.0.1:0")
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.ln.Close() })
	go f.serve()
	return f, pool
}

func (f *fakeSMTP) port() int { return f.ln.Addr().(*net.TCPAddr).Port }

func (f *fakeSMTP) serve() {
	defer close(f.done)
	conn, err := f.ln.Accept()
	if err != nil {
		return
	}
	defer func() { conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	r := bufio.NewReader(conn)
	say := func(s string) { fmt.Fprintf(conn, "%s\r\n", s) }

	say("220 fake ESMTP")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		verb := strings.ToUpper(strings.SplitN(line, " ", 2)[0])
		switch verb {
		case "EHLO", "HELO":
			say("250-fake")
			if _, isTLS := conn.(*tls.Conn); f.offerStartTLS && !isTLS {
				say("250-STARTTLS")
			}
			say("250 AUTH PLAIN")
		case "STARTTLS":
			say("220 ready")
			tc := tls.Server(conn, f.tlsCfg)
			if tc.Handshake() != nil {
				return
			}
			conn, r = tc, bufio.NewReader(tc)
		case "AUTH":
			f.mu.Lock()
			f.auth = line
			f.mu.Unlock()
			say("235 ok")
		case "MAIL":
			f.mu.Lock()
			f.from = line
			f.mu.Unlock()
			say("250 ok")
		case "RCPT":
			if f.rejectRcpt {
				say("550 no such user")
				continue
			}
			f.mu.Lock()
			f.rcpts = append(f.rcpts, line)
			f.mu.Unlock()
			say("250 ok")
		case "DATA":
			say("354 go ahead")
			var b strings.Builder
			for {
				l, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if l == ".\r\n" {
					break
				}
				b.WriteString(l)
			}
			f.mu.Lock()
			f.data = b.String()
			f.mu.Unlock()
			say("250 queued")
		case "QUIT":
			say("221 bye")
			return
		default:
			say("502 unknown")
		}
	}
}

func (f *fakeSMTP) wait(t *testing.T) {
	t.Helper()
	select {
	case <-f.done:
	case <-time.After(10 * time.Second):
		t.Fatal("fake SMTP session did not end")
	}
}

func testNotifier(t *testing.T, f *fakeSMTP, pool *x509.CertPool, implicit bool) *SMTPNotifier {
	t.Helper()
	from, _ := mail.ParseAddress("CDN Alerts <alerts@example.com>")
	to, _ := mail.ParseAddressList("ops@example.com, oncall@example.com")
	n := NewSMTPNotifier(SMTPConfig{
		Host: "127.0.0.1", Port: f.port(),
		Username: "alerts@example.com", Password: "not-a-real-password",
		From: from, To: to,
	})
	n.implicitTLS = implicit
	n.tlsConfig = &tls.Config{RootCAs: pool, ServerName: "127.0.0.1", MinVersion: tls.VersionTLS12}
	n.timeout = 10 * time.Second
	return n
}

func TestSMTPNotifierDelivers(t *testing.T) {
	for _, implicit := range []bool{false, true} {
		t.Run("implicit="+strconv.FormatBool(implicit), func(t *testing.T) {
			f, pool := startFakeSMTP(t, implicit, !implicit)
			n := testNotifier(t, f, pool, implicit)

			err := n.Send(context.Background(), Message{Subject: "[CRITICAL] cdn: disk / 91.0%", Body: "Host: cdn\nline two\n"})
			if err != nil {
				t.Fatalf("Send: %v", err)
			}
			f.wait(t)

			f.mu.Lock()
			defer f.mu.Unlock()
			if f.from != "MAIL FROM:<alerts@example.com>" {
				t.Errorf("from = %q", f.from)
			}
			if len(f.rcpts) != 2 || f.rcpts[1] != "RCPT TO:<oncall@example.com>" {
				t.Errorf("rcpts = %q", f.rcpts)
			}
			creds, _ := base64.StdEncoding.DecodeString(strings.TrimPrefix(f.auth, "AUTH PLAIN "))
			if string(creds) != "\x00alerts@example.com\x00not-a-real-password" {
				t.Errorf("auth = %q", creds)
			}
			parsed, err := mail.ReadMessage(strings.NewReader(f.data))
			if err != nil {
				t.Fatalf("delivered message does not parse: %v", err)
			}
			if got := parsed.Header.Get("Subject"); got != "[CRITICAL] cdn: disk / 91.0%" {
				t.Errorf("subject = %q", got)
			}
		})
	}
}

func TestSMTPNotifierRefusesPlaintext(t *testing.T) {
	f, pool := startFakeSMTP(t, false, false)
	err := testNotifier(t, f, pool, false).Send(context.Background(), Message{Subject: "s", Body: "b"})
	if err == nil || !strings.Contains(err.Error(), "STARTTLS") {
		t.Fatalf("err = %v, want a STARTTLS refusal", err)
	}
	f.ln.Close()
	f.wait(t)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.auth != "" || f.from != "" {
		t.Fatalf("credentials or envelope sent without TLS: auth=%q from=%q", f.auth, f.from)
	}
}

func TestSMTPNotifierRejectsUntrustedCertificate(t *testing.T) {
	f, _ := startFakeSMTP(t, false, true)
	n := testNotifier(t, f, x509.NewCertPool(), false)
	if err := n.Send(context.Background(), Message{Subject: "s", Body: "b"}); err == nil {
		t.Fatal("sent over a certificate nobody trusts")
	}
}

func TestSMTPNotifierReportsRejectedRecipient(t *testing.T) {
	f, pool := startFakeSMTP(t, false, true)
	f.rejectRcpt = true
	err := testNotifier(t, f, pool, false).Send(context.Background(), Message{Subject: "s", Body: "b"})
	if err == nil || !strings.Contains(err.Error(), "RCPT TO ops@example.com") {
		t.Fatalf("err = %v", err)
	}
}

func TestSMTPNotifierDialFailure(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	from, _ := mail.ParseAddress("alerts@example.com")
	n := NewSMTPNotifier(SMTPConfig{Host: "127.0.0.1", Port: port, From: from})
	if err := n.Send(context.Background(), Message{}); err == nil || !strings.Contains(err.Error(), "smtp dial") {
		t.Fatalf("err = %v", err)
	}
}

func TestNewSMTPNotifierPicksTLSModeByPort(t *testing.T) {
	if !NewSMTPNotifier(SMTPConfig{Host: "h", Port: 465}).implicitTLS {
		t.Error("465 must use implicit TLS")
	}
	if NewSMTPNotifier(SMTPConfig{Host: "h", Port: 587}).implicitTLS {
		t.Error("587 must use STARTTLS")
	}
}

func TestBuildMessage(t *testing.T) {
	from, _ := mail.ParseAddress("CDN Alerts <alerts@example.com>")
	to, _ := mail.ParseAddressList("ops@example.com")
	long := strings.Repeat("x", 300)
	raw := buildMessage(from, to, Message{
		Subject: "[CRITICAL] cdn: disk /mnt/evil\r\nBcc: attacker@example.com 95.0% ü",
		Body:    "Host: cdn\n" + long + "\nçok dolu\n",
	}, t0)

	msg, err := mail.ReadMessage(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatalf("message does not parse: %v", err)
	}
	if msg.Header.Get("Bcc") != "" {
		t.Fatal("a newline in the subject injected a header")
	}
	subject, err := new(mime.WordDecoder).DecodeHeader(msg.Header.Get("Subject"))
	if err != nil || !strings.Contains(subject, "Bcc: attacker@example.com 95.0% ü") {
		t.Fatalf("subject = %q (%v)", subject, err)
	}
	if msg.Header.Get("Date") != t0.Format(time.RFC1123Z) || !strings.HasSuffix(msg.Header.Get("Message-ID"), "@hostwatch>") {
		t.Errorf("date %q id %q", msg.Header.Get("Date"), msg.Header.Get("Message-ID"))
	}
	for _, line := range strings.Split(string(raw), "\r\n") {
		// RFC 5322 hard limit; the 300 character body line must have been wrapped.
		if len(line) > 998 || len(line) >= len(long) {
			t.Fatalf("unwrapped line of %d octets", len(line))
		}
	}
	body, _ := io.ReadAll(quotedprintable.NewReader(msg.Body))
	if want := "Host: cdn\r\n" + long + "\r\nçok dolu\r\n"; string(body) != want {
		t.Fatalf("body round trip = %q", body)
	}
}

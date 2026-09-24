package hostwatch

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

type Message struct {
	Subject string
	Body    string
}

type Notifier interface {
	Send(ctx context.Context, msg Message) error
}

// SMTPNotifier delivers over STARTTLS, or implicit TLS on port 465. There is
// no plaintext mode: the session carries the SMTP password.
type SMTPNotifier struct {
	cfg         SMTPConfig
	implicitTLS bool
	tlsConfig   *tls.Config
	timeout     time.Duration
	now         func() time.Time
}

func NewSMTPNotifier(cfg SMTPConfig) *SMTPNotifier {
	return &SMTPNotifier{
		cfg:         cfg,
		implicitTLS: cfg.Port == 465,
		tlsConfig:   &tls.Config{ServerName: cfg.Host, MinVersion: tls.VersionTLS12},
		timeout:     30 * time.Second,
		now:         time.Now,
	}
}

func (n *SMTPNotifier) Send(ctx context.Context, msg Message) error {
	ctx, cancel := context.WithTimeout(ctx, n.timeout)
	defer cancel()

	addr := net.JoinHostPort(n.cfg.Host, strconv.Itoa(n.cfg.Port))
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	var conn net.Conn
	var err error
	if n.implicitTLS {
		conn, err = (&tls.Dialer{NetDialer: dialer, Config: n.tlsConfig}).DialContext(ctx, "tcp", addr)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("smtp dial %s: %w", addr, err)
	}
	deadline, _ := ctx.Deadline()
	_ = conn.SetDeadline(deadline)

	c, err := smtp.NewClient(conn, n.cfg.Host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("smtp greeting: %w", err)
	}
	defer c.Close()

	if !n.implicitTLS {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return errors.New("smtp server does not offer STARTTLS; refusing to send credentials in plaintext")
		}
		if err := c.StartTLS(n.tlsConfig); err != nil {
			return fmt.Errorf("smtp starttls: %w", err)
		}
	}
	if n.cfg.Username != "" {
		if err := c.Auth(smtp.PlainAuth("", n.cfg.Username, n.cfg.Password, n.cfg.Host)); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}
	if err := c.Mail(n.cfg.From.Address); err != nil {
		return fmt.Errorf("smtp MAIL FROM: %w", err)
	}
	for _, to := range n.cfg.To {
		if err := c.Rcpt(to.Address); err != nil {
			return fmt.Errorf("smtp RCPT TO %s: %w", to.Address, err)
		}
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("smtp DATA: %w", err)
	}
	if _, err := w.Write(buildMessage(n.cfg.From, n.cfg.To, msg, n.now())); err != nil {
		w.Close()
		return fmt.Errorf("smtp write: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp end of data: %w", err)
	}
	// The server has accepted the message by now; a failed QUIT must not turn
	// into a retry that delivers it twice.
	_ = c.Quit()
	return nil
}

func buildMessage(from *mail.Address, to []*mail.Address, msg Message, now time.Time) []byte {
	recipients := make([]string, len(to))
	for i, a := range to {
		recipients[i] = a.String()
	}

	var b bytes.Buffer
	header := func(k, v string) { fmt.Fprintf(&b, "%s: %s\r\n", k, v) }
	header("From", from.String())
	header("To", strings.Join(recipients, ", "))
	header("Subject", mime.QEncoding.Encode("utf-8", headerSafe(msg.Subject)))
	header("Date", now.Format(time.RFC1123Z))
	header("Message-ID", "<"+messageID()+"@hostwatch>")
	header("MIME-Version", "1.0")
	header("Content-Type", "text/plain; charset=utf-8")
	header("Content-Transfer-Encoding", "quoted-printable")
	b.WriteString("\r\n")

	qp := quotedprintable.NewWriter(&b)
	_, _ = qp.Write([]byte(strings.ReplaceAll(msg.Body, "\n", "\r\n")))
	_ = qp.Close()
	return b.Bytes()
}

// headerSafe strips line breaks: subjects carry mountpoints, which gopsutil
// unescapes from mountinfo and which may therefore contain a newline.
func headerSafe(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\r' || r == '\n' {
			return ' '
		}
		return r
	}, s)
}

func messageID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(b)
}

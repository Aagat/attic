package delivery

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Enabled                                          bool
	Host                                             string
	Port                                             int
	TLSMode, Username, Password, Sender, Destination string
	Timeout                                          time.Duration
}

// Validate returns field names only; credentials and server responses stay private.
func (c Config) Validate() error {
	if !c.Enabled {
		return nil
	}
	if c.Host == "" || strings.ContainsAny(c.Host, "\r\n /@") {
		return errors.New("SMTP_HOST")
	}
	if c.Port < 1 || c.Port > 65535 {
		return errors.New("SMTP_PORT")
	}
	if c.TLSMode != "starttls" && c.TLSMode != "implicit_tls" && c.TLSMode != "none" {
		return errors.New("SMTP_TLS_MODE")
	}
	if c.TLSMode == "none" && (c.Username != "" || c.Password != "") {
		return errors.New("SMTP_TLS_MODE")
	}
	if (c.Username == "") != (c.Password == "") {
		return errors.New("SMTP_USERNAME/SMTP_PASSWORD")
	}
	if !validAddress(c.Sender) {
		return errors.New("SMTP_SENDER")
	}
	if !validAddress(c.Destination) {
		return errors.New("SMTP_DESTINATION")
	}
	if c.Timeout <= 0 || c.Timeout > 5*time.Minute {
		return errors.New("SMTP_TIMEOUT")
	}
	return nil
}

func validAddress(s string) bool {
	a, err := mail.ParseAddress(s)
	return err == nil && a.Address == s && !strings.ContainsAny(s, "\r\n") && isASCII(s)
}
func isASCII(s string) bool {
	for _, r := range s {
		if r > 127 {
			return false
		}
	}
	return true
}

// MessageID is stable across safe retries and contains no source or credentials.
func MessageID(id string) string { return fmt.Sprintf("<%x@attic.invalid>", sha256.Sum256([]byte(id))) }

type SMTP struct{ config Config }

func NewSMTP(c Config) (*SMTP, error) {
	if !c.Enabled {
		return nil, errors.New("SMTP_ENABLED")
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &SMTP{config: c}, nil
}

// Send hides MIME, TLS, authentication, cancellation and SMTP error classification.
// Only a positive DATA completion counts as accepted. A missing completion is
// uncertain and must never be automatically resent.
func (s *SMTP) Send(ctx context.Context, claim *Claim, pdf io.Reader) Result {
	c := s.config
	if !validAddress(claim.Destination) {
		return Result{Outcome: "rejected"}
	}
	ctx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()
	client, closeClient, err := s.connect(ctx)
	if err != nil {
		return classify(err, false)
	}
	defer closeClient()
	if err := client.Mail(c.Sender); err != nil {
		return classify(err, false)
	}
	if err := client.Rcpt(claim.Destination); err != nil {
		return classify(err, false)
	}
	data, err := client.Data()
	if err != nil {
		return classify(err, false)
	}
	if err := writeMessage(data, c.Sender, claim, pdf); err != nil {
		return classify(err, false)
	}
	if err := data.Close(); err != nil {
		return classify(err, true)
	}
	// QUIT failure cannot undo a successful DATA acceptance.
	return Result{Outcome: "accepted", Code: "250"}
}

func classify(err error, afterData bool) Result {
	var smtpErr *textproto.Error
	if errors.As(err, &smtpErr) {
		if smtpErr.Code >= 500 {
			return Result{Outcome: "rejected", Code: strconv.Itoa(smtpErr.Code)}
		}
		if smtpErr.Code >= 400 {
			return Result{Outcome: "transient_failure", Code: strconv.Itoa(smtpErr.Code)}
		}
	}
	if afterData {
		return Result{Outcome: "uncertain"}
	}
	return Result{Outcome: "transient_failure"}
}

func cleanHeader(s string) string { return strings.Join(strings.Fields(s), " ") }
func writeMessage(w io.Writer, from string, c *Claim, pdf io.Reader) error {
	multi := multipart.NewWriter(w)
	subject := mime.QEncoding.Encode("utf-8", cleanHeader(c.Title))
	_, err := fmt.Fprintf(w, "From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\nMessage-ID: %s\r\nMIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=%q\r\n\r\n", from, c.Destination, subject, time.Now().UTC().Format(time.RFC1123Z), c.MessageID, multi.Boundary())
	if err != nil {
		return err
	}
	body, err := multi.CreatePart(textproto.MIMEHeader{"Content-Type": {"text/plain; charset=utf-8"}, "Content-Transfer-Encoding": {"quoted-printable"}})
	if err != nil {
		return err
	}
	if _, err := io.WriteString(body, "Your article is attached as a PDF.\r\nSaved with Attic.\r\n"); err != nil {
		return err
	}
	part, err := multi.CreatePart(textproto.MIMEHeader{"Content-Type": {"application/pdf"}, "Content-Disposition": {mime.FormatMediaType("attachment", map[string]string{"filename": cleanHeader(c.Artifact.Filename)})}, "Content-Transfer-Encoding": {"base64"}})
	if err != nil {
		return err
	}
	encoder := base64.NewEncoder(base64.StdEncoding, &lineWriter{w: part})
	n, err := io.Copy(encoder, io.LimitReader(pdf, c.Artifact.ByteSize+1))
	if err != nil {
		return err
	}
	if n != c.Artifact.ByteSize {
		return errors.New("artifact size mismatch")
	}
	if err := encoder.Close(); err != nil {
		return err
	}
	return multi.Close()
}

type lineWriter struct {
	w      io.Writer
	column int
}

func (w *lineWriter) Write(p []byte) (int, error) {
	total := 0
	for len(p) > 0 {
		n := min(len(p), 76-w.column)
		written, err := w.w.Write(p[:n])
		total += written
		w.column += written
		p = p[written:]
		if err != nil {
			return total, err
		}
		if written != n {
			return total, io.ErrShortWrite
		}
		if w.column == 76 {
			if _, err := io.WriteString(w.w, "\r\n"); err != nil {
				return total, err
			}
			w.column = 0
		}
	}
	return total, nil
}

// TestConnection performs TLS and authentication only: never MAIL, RCPT or DATA.
func (s *SMTP) TestConnection(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, s.config.Timeout)
	defer cancel()
	_, closeClient, err := s.connect(ctx)
	defer closeClient()
	if err != nil {
		return errors.New("SMTP connection, TLS verification or authentication failed; check host, port, certificate and app password")
	}
	return nil
}
func (s *SMTP) connect(ctx context.Context) (*smtp.Client, func(), error) {
	c := s.config
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort(c.Host, strconv.Itoa(c.Port)))
	if err != nil {
		return nil, func() {}, err
	}

	ok := false
	defer func() {
		if !ok {
			conn.Close()
		}
	}()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer func() {
		if !ok {
			stop()
		}
	}()

	deadline, _ := ctx.Deadline()
	conn.SetDeadline(deadline)
	tlsConfig := &tls.Config{ServerName: c.Host, MinVersion: tls.VersionTLS12}
	var transport net.Conn = conn
	if c.TLSMode == "implicit_tls" {
		secure := tls.Client(conn, tlsConfig)
		if err := secure.HandshakeContext(ctx); err != nil {
			return nil, func() {}, &textproto.Error{Code: 550, Msg: "TLS verification failed"}
		}
		transport = secure
	}
	client, err := smtp.NewClient(transport, c.Host)
	if err != nil {
		return nil, func() {}, err
	}

	if c.TLSMode == "starttls" {
		if err := client.StartTLS(tlsConfig); err != nil {
			return nil, func() {}, &textproto.Error{Code: 550, Msg: "TLS verification failed"}
		}
	}
	if c.Username != "" {
		if err := client.Auth(smtp.PlainAuth("", c.Username, c.Password, c.Host)); err != nil {
			return nil, func() {}, err
		}
	}
	ok = true
	return client, func() { stop(); client.Close(); conn.Close() }, nil
}

// SendTest submits one plain text setup message, without a delivery worker or
// automatic retry. A missing DATA acknowledgement is explicitly uncertain.
func (s *SMTP) SendTest(ctx context.Context) Result {
	ctx, cancel := context.WithTimeout(ctx, s.config.Timeout)
	defer cancel()
	client, closeClient, err := s.connect(ctx)
	defer closeClient()
	if err != nil {
		return classify(err, false)
	}
	if err = client.Mail(s.config.Sender); err != nil {
		return classify(err, false)
	}
	if err = client.Rcpt(s.config.Destination); err != nil {
		return classify(err, false)
	}
	w, err := client.Data()
	if err != nil {
		return classify(err, false)
	}
	_, err = fmt.Fprintf(w, "From: %s\r\nTo: %s\r\nSubject: Attic SMTP test\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nThis is an explicitly requested Attic SMTP test. SMTP acceptance does not confirm Kindle receipt. Use Send to Kindle on a saved PDF to test document delivery.\r\n", s.config.Sender, s.config.Destination)
	if err != nil {
		return classify(err, true)
	}
	if err = w.Close(); err != nil {
		return classify(err, true)
	}
	return Result{Outcome: "accepted", Code: "250"}
}

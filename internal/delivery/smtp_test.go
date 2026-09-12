package delivery

import (
	"attic/internal/domain"
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/mail"
	"net/textproto"
	"strconv"
	"strings"
	"testing"
	"time"
)

// A real SMTP conversation is the test seam, including DATA's final response.
func catchSMTP(t *testing.T, reject string, drop bool) (Config, <-chan []byte) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	messages := make(chan []byte, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(3 * time.Second))
		wire := textproto.NewConn(conn)
		wire.PrintfLine("220 localhost ESMTP")
		for {
			line, err := wire.ReadLine()
			if err != nil {
				return
			}
			switch {
			case strings.HasPrefix(line, "EHLO"):
				wire.PrintfLine("250 localhost")
			case strings.HasPrefix(line, "RCPT") && reject != "":
				wire.PrintfLine("%s private server detail", reject)
			case line == "DATA":
				wire.PrintfLine("354 send message")
				data, err := wire.ReadDotBytes()
				if err != nil {
					return
				}
				messages <- data
				if drop {
					return
				}
				wire.PrintfLine("250 accepted")
			default:
				wire.PrintfLine("250 OK")
			}
		}
	}()
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	n, _ := strconv.Atoi(port)
	return Config{Enabled: true, Host: "127.0.0.1", Port: n, TLSMode: "none", Sender: "attic@example.test", Destination: "reader@example.test", Timeout: time.Second}, messages
}

func TestSMTPDelivery(t *testing.T) {
	pdf := bytes.Repeat([]byte("%PDF-1.7\nattachment bytes\x00\xff"), 100)
	for _, tc := range []struct {
		name, reject, want string
		drop               bool
	}{{"accepted", "", "accepted", false}, {"temporary rejection", "451", "transient_failure", false}, {"permanent rejection", "550", "rejected", false}, {"lost receipt", "", "uncertain", true}} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, messages := catchSMTP(t, tc.reject, tc.drop)
			sender, err := NewSMTP(cfg)
			if err != nil {
				t.Fatal(err)
			}
			claim := &Claim{JobID: "test", MessageID: MessageID("test"), Destination: cfg.Destination, Title: "Résumé\r\nBcc: injected", Artifact: domain.Artifact{Filename: "読書 résumé.pdf", ByteSize: int64(len(pdf))}}
			got := sender.Send(context.Background(), claim, bytes.NewReader(pdf))
			if got.Outcome != tc.want {
				t.Fatalf("result %+v", got)
			}
			if tc.reject != "" {
				return
			}
			raw := <-messages
			msg, err := mail.ReadMessage(bytes.NewReader(raw))
			if err != nil {
				t.Fatal(err)
			}
			if msg.Header.Get("Bcc") != "" || msg.Header.Get("Message-ID") != claim.MessageID {
				t.Fatal("unsafe or unstable headers")
			}
			subject, err := new(mime.WordDecoder).DecodeHeader(msg.Header.Get("Subject"))
			if err != nil || subject != "Résumé Bcc: injected" {
				t.Fatalf("subject %q %v", subject, err)
			}
			_, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
			if err != nil {
				t.Fatal(err)
			}
			parts := multipart.NewReader(msg.Body, params["boundary"])
			part, err := parts.NextPart()
			if err != nil {
				t.Fatal(err)
			}
			part.Close()
			part, err = parts.NextPart()
			if err != nil {
				t.Fatal(err)
			}
			if part.FileName() != claim.Artifact.Filename {
				t.Fatalf("filename %q", part.FileName())
			}
			data, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding, part))
			if err != nil || !bytes.Equal(data, pdf) {
				t.Fatalf("attachment changed: %v", err)
			}
		})
	}
}

func TestSMTPRequiresConfiguredTLSAndPrivateCredentials(t *testing.T) {
	cfg, messages := catchSMTP(t, "", false)
	cfg.TLSMode = "starttls"
	sender, err := NewSMTP(cfg)
	if err != nil {
		t.Fatal(err)
	}
	got := sender.Send(context.Background(), &Claim{Destination: cfg.Destination}, strings.NewReader("pdf"))
	if got.Outcome != "rejected" {
		t.Fatalf("TLS downgrade: %+v", got)
	}
	select {
	case <-messages:
		t.Fatal("sent without required TLS")
	default:
	}
	cfg.TLSMode = "none"
	cfg.Username = "secret-user"
	cfg.Password = "secret-password"
	if _, err := NewSMTP(cfg); err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("unsafe config %v", err)
	}
	cfg.Username = ""
	cfg.Password = ""
	cfg.Destination = "reader@example.test\r\nBcc: bad@example.test"
	if _, err := NewSMTP(cfg); err == nil {
		t.Fatal("accepted header injection")
	}
}

func TestConnectionCheckNeverSendsMail(t *testing.T) {
	cfg, messages := catchSMTP(t, "", false)
	sender, _ := NewSMTP(cfg)
	if err := sender.TestConnection(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-messages:
		t.Fatal("connection check sent a message")
	default:
	}
}
func TestExplicitTestEmailResults(t *testing.T) {
	for _, tc := range []struct {
		reject string
		drop   bool
		want   string
	}{{"", false, "accepted"}, {"550", false, "rejected"}, {"", true, "uncertain"}} {
		cfg, _ := catchSMTP(t, tc.reject, tc.drop)
		sender, _ := NewSMTP(cfg)
		if got := sender.SendTest(context.Background()); got.Outcome != tc.want {
			t.Fatalf("test message result: %+v", got)
		}
	}
}

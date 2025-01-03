package mailer

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"mime/multipart"
	"net/smtp"
	"time"
)

// Mailer handles sending emails to Kindle
type Mailer struct {
	kindleEmail    string
	senderEmail    string
	senderPassword string
	smtpHost       string
	smtpPort       string
}

// NewMailer creates a new Mailer instance
func NewMailer(kindleEmail, senderEmail, senderPassword string) *Mailer {
	return &Mailer{
		kindleEmail:    kindleEmail,
		senderEmail:    senderEmail,
		senderPassword: senderPassword,
		smtpHost:       "smtp.gmail.com",
		smtpPort:       "587",
	}
}

// SendToKindle sends the PDF to the configured Kindle email address
func (m *Mailer) SendToKindle(pdf []byte) error {
	// Create email message
	msg, err := m.createEmail(pdf)
	if err != nil {
		return fmt.Errorf("failed to create email: %w", err)
	}

	// Send email
	auth := smtp.PlainAuth("", m.senderEmail, m.senderPassword, m.smtpHost)
	addr := fmt.Sprintf("%s:%s", m.smtpHost, m.smtpPort)

	if err := smtp.SendMail(addr, auth, m.senderEmail, []string{m.kindleEmail}, msg); err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}

	return nil
}

// createEmail creates the email message with the PDF attachment
func (m *Mailer) createEmail(pdf []byte) ([]byte, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	// Write headers
	headers := fmt.Sprintf("From: %s\r\n"+
		"To: %s\r\n"+
		"Subject: Convert\r\n"+
		"MIME-Version: 1.0\r\n"+
		"Content-Type: multipart/mixed; boundary=%s\r\n\r\n",
		m.senderEmail, m.kindleEmail, writer.Boundary())
	buf.WriteString(headers)

	// Write PDF attachment
	part, err := writer.CreatePart(map[string][]string{
		"Content-Type":              {"application/pdf"},
		"Content-Transfer-Encoding": {"base64"},
		"Content-Disposition":       {fmt.Sprintf(`attachment; filename="document-%s.pdf"`, time.Now().Format("20060102150405"))},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create attachment part: %w", err)
	}

	encoder := base64.NewEncoder(base64.StdEncoding, part)
	if _, err := encoder.Write(pdf); err != nil {
		return nil, fmt.Errorf("failed to write attachment: %w", err)
	}
	encoder.Close()

	writer.Close()
	return buf.Bytes(), nil
}

// sendMail sends the email using SMTP
func sendMail(from string, to string, msg []byte) error {
	// TODO: Implement SMTP sending
	return errors.New("SMTP sending not implemented")
}

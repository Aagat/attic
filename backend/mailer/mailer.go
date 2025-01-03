package mailer

import (
	"errors"
)

// SendToKindle sends the PDF to the configured Kindle email address
func SendToKindle(pdf []byte) error {
	// TODO: Implement email sending using net/smtp
	return errors.New("email sending not implemented")
}

// createEmail creates the email message with the PDF attachment
func createEmail(from string, to string, pdf []byte) ([]byte, error) {
	// TODO: Implement email message creation with attachment
	return nil, errors.New("email creation not implemented")
}

// sendMail sends the email using SMTP
func sendMail(from string, to string, msg []byte) error {
	// TODO: Implement SMTP sending
	return errors.New("SMTP sending not implemented")
}

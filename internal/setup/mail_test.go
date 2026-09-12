package setup

import (
	"attic/internal/delivery"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func inputMail() MailInput {
	return MailInput{Enabled: true, Host: "smtp.example.test", Port: 587, TLSMode: "starttls", Username: "owner", Password: "private-secret", Sender: "owner@example.test", Destination: "reader@kindle.test"}
}
func TestMailPersistenceRotationAndRedaction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth", "mail.json")
	m, err := NewMail(path, delivery.Config{}, false)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	m.BeforeEnable = func() error { calls++; return nil }
	in := inputMail()
	if err = m.Update(in); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal("enabling did not pause existing delivery")
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("unsafe permissions")
	}
	b, _ := json.Marshal(m.View())
	if strings.Contains(string(b), in.Password) {
		t.Fatal("password leaked")
	}
	in.Password = ""
	in.Host = "rotated.example.test"
	if err = m.Update(in); err != nil {
		t.Fatal(err)
	}
	reloaded, err := NewMail(path, delivery.Config{}, false)
	if err != nil || reloaded.Config().Password != "private-secret" {
		t.Fatal("blank password or restart lost credentials")
	}
	in.Password = "new-secret"
	if err = m.Update(in); err != nil {
		t.Fatal(err)
	}
	if m.Config().Password != "new-secret" {
		t.Fatal("rotation failed")
	}
	in.ClearCredentials = true
	if err = m.Update(in); err != nil {
		t.Fatal(err)
	}
	if m.Config().Password != "" || m.Config().Username != "" {
		t.Fatal("clear failed")
	}
}
func TestDeploymentManagedMailCannotBeOverridden(t *testing.T) {
	m, _ := NewMail(filepath.Join(t.TempDir(), "mail"), delivery.Config{Host: "deployment"}, true)
	if m.Update(inputMail()) != ErrManaged || m.Config().Host != "deployment" {
		t.Fatal("deployment overridden")
	}
}
func TestInvalidMailDoesNotReplaceExistingSettings(t *testing.T) {
	m, _ := NewMail(filepath.Join(t.TempDir(), "mail"), delivery.Config{}, false)
	in := inputMail()
	if err := m.Update(in); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"none", "insecure_skip_verify"} {
		in.TLSMode = mode
		if m.Update(in) == nil {
			t.Fatal("insecure authenticated SMTP allowed")
		}
	}
	if m.Config().TLSMode != "starttls" {
		t.Fatal("invalid write replaced settings")
	}
}
func TestRejectWorldReadableCredentials(t *testing.T) {
	p := filepath.Join(t.TempDir(), "mail")
	os.WriteFile(p, []byte(`{}`), 0644)
	if _, err := NewMail(p, delivery.Config{}, false); err != ErrStorage {
		t.Fatal("unsafe permissions accepted")
	}
}

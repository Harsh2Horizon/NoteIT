package auth

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"time"
)

type Mailer interface {
	SendVerification(context.Context, string, string) error
}

type SMTPMailer struct {
	Host, Port, Username, Password, From string
	RequireTLS                           bool
}

func (m SMTPMailer) SendVerification(ctx context.Context, email, link string) error {
	d := net.Dialer{Timeout: 5 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(m.Host, m.Port))
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	c, err := smtp.NewClient(conn, m.Host)
	if err != nil {
		return err
	}
	defer c.Close()
	if ok, _ := c.Extension("STARTTLS"); ok {
		if err = c.StartTLS(&tls.Config{ServerName: m.Host, MinVersion: tls.VersionTLS12}); err != nil {
			return err
		}
	} else if m.RequireTLS || m.Username != "" {
		return fmt.Errorf("SMTP server must support STARTTLS")
	}
	if m.Username != "" {
		if err = c.Auth(smtp.PlainAuth("", m.Username, m.Password, m.Host)); err != nil {
			return err
		}
	}
	if err = c.Mail(m.From); err != nil {
		return err
	}
	if err = c.Rcpt(email); err != nil {
		return err
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "From: NoteIt <%s>\r\nTo: %s\r\nSubject: Verify your NoteIt email\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\nWelcome to NoteIt.\r\n\r\nVerify your email using this link (valid for 30 minutes):\r\n%s\r\n\r\nIf you did not request this email, you can ignore it.\r\n", m.From, email, link)
	if err != nil {
		return err
	}
	if err = w.Close(); err != nil {
		return err
	}
	// DATA acknowledgement is the delivery hand-off; a failed QUIT must not invalidate the token.
	_ = c.Quit()
	return nil
}

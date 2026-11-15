// Package mailer sends transactional email through local SMTP (Mailpit in
// development). No external or paid mail provider is involved.
package mailer

import (
	"fmt"
	"net/smtp"
	"strings"
)

// Mailer sends plain-text/HTML email over unauthenticated local SMTP.
type Mailer struct {
	Host    string
	Port    int
	From    string
	Mailpit string // web UI URL for developer hints
}

// Message describes an outgoing email.
type Message struct {
	To      string
	Subject string
	Text    string
	HTML    string
}

// Send delivers msg via SMTP. Returns error if the local SMTP server is down.
func (m *Mailer) Send(msg Message) error {
	addr := fmt.Sprintf("%s:%d", m.Host, m.Port)
	from := parseFromAddress(m.From)

	var b strings.Builder
	b.WriteString("From: " + m.From + "\r\n")
	b.WriteString("To: " + msg.To + "\r\n")
	b.WriteString("Subject: " + msg.Subject + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: multipart/alternative; boundary=veylo-boundary\r\n\r\n")

	b.WriteString("--veylo-boundary\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n")
	b.WriteString(msg.Text + "\r\n")
	if msg.HTML != "" {
		b.WriteString("--veylo-boundary\r\nContent-Type: text/html; charset=utf-8\r\n\r\n")
		b.WriteString(msg.HTML + "\r\n")
	}
	b.WriteString("--veylo-boundary--\r\n")

	return smtp.SendMail(addr, nil, from, []string{msg.To}, []byte(b.String()))
}

func parseFromAddress(from string) string {
	if i := strings.LastIndex(from, "<"); i >= 0 && strings.HasSuffix(from, ">") {
		return from[i+1 : len(from)-1]
	}
	return from
}

// EmailTemplate renders a simple branded HTML body around content.
func EmailTemplate(title, body, ctaURL, ctaLabel, mailpitURL string) Message {
	html := fmt.Sprintf(`<!doctype html>
<html><body style="margin:0;padding:24px;background:#f4f4f8;font-family:-apple-system,Segoe UI,Roboto,sans-serif;">
  <div style="max-width:480px;margin:0 auto;background:#ffffff;border-radius:16px;padding:32px;">
    <div style="font-size:22px;font-weight:700;color:#5b4fc4;letter-spacing:-0.5px;">Veylo</div>
    <h2 style="margin:16px 0 8px;color:#1a1a2e;">%s</h2>
    <p style="color:#555;font-size:15px;line-height:1.6;">%s</p>
    %s
    <p style="color:#999;font-size:12px;margin-top:24px;">This is a local development email captured by Mailpit (%s).</p>
  </div>
</body></html>`, title, body, ctaHTML(ctaURL, ctaLabel), mailpitURL)

	text := fmt.Sprintf("%s\n\n%s\n\n%s\n\n(Local development email — view in Mailpit: %s)", title, body, ctaLabel+": "+ctaURL, mailpitURL)
	return Message{Text: text, HTML: html}
}

func ctaHTML(url, label string) string {
	if url == "" {
		return ""
	}
	return fmt.Sprintf(`<a href="%s" style="display:inline-block;margin:16px 0;padding:12px 24px;background:#5b4fc4;color:#fff;border-radius:10px;text-decoration:none;font-size:14px;font-weight:600;">%s</a>`, url, label)
}

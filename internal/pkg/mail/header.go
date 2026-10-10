package mail

import "strings"

// HeaderText makes s safe to write as the value of a message header: every run
// of carriage returns and line feeds becomes one space.
//
// A header ends at a line break, so a value written as it came — a subject
// built from process variables, a notification title — could end its header
// early and start new ones: a Bcc nobody configured, or a blank line and a
// body of the author's choosing. Folding to a space keeps a subject that
// legitimately spans lines readable rather than refusing to send it.
//
// Addresses are not folded but refused: Send rejects a line break in the
// sender or any recipient before a byte of the message is written.
func HeaderText(s string) string {
	if !strings.ContainsAny(s, "\r\n") {
		return s
	}
	return strings.Join(strings.FieldsFunc(s, func(r rune) bool { return r == '\r' || r == '\n' }), " ")
}

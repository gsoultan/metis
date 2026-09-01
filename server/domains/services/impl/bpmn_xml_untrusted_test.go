package impl

import (
	"fmt"
	"strings"
	"testing"
)

// BPMN files are uploaded by users, so the parser's input is hostile by
// default. These properties currently hold because encoding/xml is strict:
// it refuses an entity it was not given and never fetches an external one.
//
// They are pinned because that is one line from being untrue. A developer
// meeting "invalid character entity" on a file some other tool exported has an
// obvious-looking fix — set Strict to false, or hand the decoder an Entity map
// — and either one silently turns every case below into a working attack. The
// failure would look like a parser becoming more tolerant.
func TestParseRefusesHostileXML(t *testing.T) {
	cases := []struct {
		name    string
		payload string
		why     string
	}{
		{
			name: "an entity that expands exponentially",
			payload: `<?xml version="1.0"?>
<!DOCTYPE definitions [
 <!ENTITY lol "lol">
 <!ENTITY lol1 "&lol;&lol;&lol;&lol;&lol;&lol;&lol;&lol;&lol;&lol;">
 <!ENTITY lol2 "&lol1;&lol1;&lol1;&lol1;&lol1;&lol1;&lol1;&lol1;&lol1;&lol1;">
 <!ENTITY lol3 "&lol2;&lol2;&lol2;&lol2;&lol2;&lol2;&lol2;&lol2;&lol2;&lol2;">
 <!ENTITY lol4 "&lol3;&lol3;&lol3;&lol3;&lol3;&lol3;&lol3;&lol3;&lol3;&lol3;">
]>
<definitions xmlns="http://www.omg.org/spec/BPMN/20100524/MODEL"><process id="&lol4;" name="p"/></definitions>`,
			why: "a few kilobytes would become gigabytes of memory on the server",
		},
		{
			name: "an external entity naming a local file",
			payload: `<?xml version="1.0"?>
<!DOCTYPE definitions [ <!ENTITY xxe SYSTEM "file:///etc/passwd"> ]>
<definitions xmlns="http://www.omg.org/spec/BPMN/20100524/MODEL"><process id="&xxe;" name="p"/></definitions>`,
			why: "the file's contents would be readable back through the definition",
		},
		{
			name: "an external entity naming the cloud metadata endpoint",
			payload: `<?xml version="1.0"?>
<!DOCTYPE definitions [ <!ENTITY xxe SYSTEM "http://169.254.169.254/latest/meta-data/iam/"> ]>
<definitions xmlns="http://www.omg.org/spec/BPMN/20100524/MODEL"><process id="&xxe;" name="p"/></definitions>`,
			why: "this reaches IAM credentials, and unlike a service task it never touches the egress guard",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			parser := &BPMNXMLParser{}
			if _, err := parser.Parse(strings.NewReader(testCase.payload)); err == nil {
				t.Fatalf("the parser accepted it — %s", testCase.why)
			}
		})
	}
}

// Nested subprocesses are mapped by a function that calls itself, and a Go
// stack overflow is fatal: it cannot be recovered, so one request would take
// the whole engine down. Nothing in this package bounds the depth — the
// standard library's own limit is what stops it, at ten thousand levels.
//
// Pinned so that a change to a hand-rolled or streaming decoder cannot quietly
// remove the only thing standing between an upload and a crash.
func TestParseRefusesDeeplyNestedSubprocesses(t *testing.T) {
	// Comfortably past the standard library's limit, and still inside the
	// 2 MiB the request-size interceptor allows: this is reachable over HTTP.
	const depth = 20000

	var b strings.Builder
	b.WriteString(`<definitions xmlns="http://www.omg.org/spec/BPMN/20100524/MODEL"><process id="p" name="p">`)
	for i := range depth {
		fmt.Fprintf(&b, `<subProcess id="s%d">`, i)
	}
	b.WriteString(strings.Repeat(`</subProcess>`, depth))
	b.WriteString(`</process></definitions>`)

	if size := b.Len(); size > 2<<20 {
		t.Fatalf("the payload is %d bytes, past the body limit, so this no longer tests a reachable case", size)
	}

	parser := &BPMNXMLParser{}
	if _, err := parser.Parse(strings.NewReader(b.String())); err == nil {
		t.Fatal("a 20,000-deep definition was accepted; the subprocess mapping recurses, and a stack overflow would kill the process rather than fail the request")
	}
}

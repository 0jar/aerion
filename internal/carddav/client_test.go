package carddav

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFixDateValue(t *testing.T) {
	prefix := []byte("<D:getlastmodified>")
	suffix := []byte("</D:getlastmodified>")

	tests := []struct {
		name     string
		dateStr  string
		expected string
	}{
		{
			name:     "numeric +0000 converted to GMT",
			dateStr:  "Fri, 10 Oct 2025 13:41:36 +0000",
			expected: "<D:getlastmodified>Fri, 10 Oct 2025 13:41:36 GMT</D:getlastmodified>",
		},
		{
			name:     "numeric +0530 converted to UTC then GMT",
			dateStr:  "Fri, 10 Oct 2025 19:11:36 +0530",
			expected: "<D:getlastmodified>Fri, 10 Oct 2025 13:41:36 GMT</D:getlastmodified>",
		},
		{
			name:     "negative offset -0500 converted to UTC then GMT",
			dateStr:  "Fri, 10 Oct 2025 08:41:36 -0500",
			expected: "<D:getlastmodified>Fri, 10 Oct 2025 13:41:36 GMT</D:getlastmodified>",
		},
		{
			name:     "already GMT unchanged",
			dateStr:  "Fri, 10 Oct 2025 13:41:36 GMT",
			expected: "<D:getlastmodified>Fri, 10 Oct 2025 13:41:36 GMT</D:getlastmodified>",
		},
		{
			name:     "non-date string unchanged",
			dateStr:  "not-a-date",
			expected: "<D:getlastmodified>not-a-date</D:getlastmodified>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := string(fixDateValue(prefix, tt.dateStr, suffix))
			if result != tt.expected {
				t.Errorf("got %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestGetlastmodifiedRegex(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "replaces numeric offset in XML",
			input:    `<D:getlastmodified>Fri, 10 Oct 2025 13:41:36 +0000</D:getlastmodified>`,
			expected: `<D:getlastmodified>Fri, 10 Oct 2025 13:41:36 GMT</D:getlastmodified>`,
		},
		{
			name:     "preserves GMT dates",
			input:    `<D:getlastmodified>Fri, 10 Oct 2025 13:41:36 GMT</D:getlastmodified>`,
			expected: `<D:getlastmodified>Fri, 10 Oct 2025 13:41:36 GMT</D:getlastmodified>`,
		},
		{
			name:     "handles whitespace around date",
			input:    `<D:getlastmodified>  Fri, 10 Oct 2025 13:41:36 +0000  </D:getlastmodified>`,
			expected: `<D:getlastmodified>Fri, 10 Oct 2025 13:41:36 GMT</D:getlastmodified>`,
		},
		{
			name:     "no getlastmodified unchanged",
			input:    `<D:displayname>Test</D:displayname>`,
			expected: `<D:displayname>Test</D:displayname>`,
		},
		{
			name:     "multiple getlastmodified elements",
			input:    `<D:getlastmodified>Fri, 10 Oct 2025 13:41:36 +0000</D:getlastmodified><D:getlastmodified>Sat, 11 Oct 2025 10:00:00 +0000</D:getlastmodified>`,
			expected: `<D:getlastmodified>Fri, 10 Oct 2025 13:41:36 GMT</D:getlastmodified><D:getlastmodified>Sat, 11 Oct 2025 10:00:00 GMT</D:getlastmodified>`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getlastmodifiedRe.ReplaceAllFunc([]byte(tt.input), func(match []byte) []byte {
				sub := getlastmodifiedRe.FindSubmatch(match)
				if len(sub) < 4 {
					return match
				}
				dateStr := string(sub[2])
				return fixDateValue(sub[1], dateStr, sub[3])
			})
			if string(result) != tt.expected {
				t.Errorf("got %q, want %q", string(result), tt.expected)
			}
		})
	}
}

func TestFixETagValue(t *testing.T) {
	prefix := []byte("<D:getetag>")
	suffix := []byte("</D:getetag>")

	tests := []struct {
		name     string
		etagStr  string
		expected string
	}{
		{
			name:     "unquoted ETag gets quoted",
			etagStr:  "abc123",
			expected: `<D:getetag>"abc123"</D:getetag>`,
		},
		{
			name:     "already quoted ETag unchanged",
			etagStr:  `"abc123"`,
			expected: `<D:getetag>"abc123"</D:getetag>`,
		},
		{
			name:     "empty string gets quoted",
			etagStr:  "",
			expected: `<D:getetag>""</D:getetag>`,
		},
		{
			name:     "ETag with special characters gets quoted",
			etagStr:  "63c2-5b0-5f1e2a3b",
			expected: `<D:getetag>"63c2-5b0-5f1e2a3b"</D:getetag>`,
		},
		{
			name:     "XML-entity-encoded quotes unchanged (murena.io)",
			etagStr:  `&quot;df8b8abeff032a71c6c1d76db352996f&quot;`,
			expected: `<D:getetag>&quot;df8b8abeff032a71c6c1d76db352996f&quot;</D:getetag>`,
		},
		{
			name:     "weak ETag with literal quotes strips W/",
			etagStr:  `W/"abc123"`,
			expected: `<D:getetag>"abc123"</D:getetag>`,
		},
		{
			name:     "weak ETag unquoted strips W/ and quotes",
			etagStr:  "W/abc123",
			expected: `<D:getetag>"abc123"</D:getetag>`,
		},
		{
			name:     "weak ETag lowercase strips w/",
			etagStr:  `w/"abc123"`,
			expected: `<D:getetag>"abc123"</D:getetag>`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := string(fixETagValue(prefix, tt.etagStr, suffix))
			if result != tt.expected {
				t.Errorf("got %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestGetetagRegex(t *testing.T) {
	applyEtagFix := func(input string) string {
		return string(getetagRe.ReplaceAllFunc([]byte(input), func(match []byte) []byte {
			sub := getetagRe.FindSubmatch(match)
			if len(sub) < 4 {
				return match
			}
			etagStr := strings.TrimSpace(string(sub[2]))
			return fixETagValue(sub[1], etagStr, sub[3])
		}))
	}

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "unquoted ETag gets quoted",
			input:    `<D:getetag>abc123</D:getetag>`,
			expected: `<D:getetag>"abc123"</D:getetag>`,
		},
		{
			name:     "already quoted ETag unchanged",
			input:    `<D:getetag>"abc123"</D:getetag>`,
			expected: `<D:getetag>"abc123"</D:getetag>`,
		},
		{
			name:     "whitespace around value trimmed and quoted",
			input:    `<D:getetag>  abc123  </D:getetag>`,
			expected: `<D:getetag>"abc123"</D:getetag>`,
		},
		{
			name:     "non-ETag XML unchanged",
			input:    `<D:displayname>Test</D:displayname>`,
			expected: `<D:displayname>Test</D:displayname>`,
		},
		{
			name:     "multiple getetag elements",
			input:    `<D:getetag>abc</D:getetag><D:getetag>def</D:getetag>`,
			expected: `<D:getetag>"abc"</D:getetag><D:getetag>"def"</D:getetag>`,
		},
		{
			name:     "mixed getlastmodified and getetag",
			input:    `<D:getlastmodified>Fri, 10 Oct 2025 13:41:36 GMT</D:getlastmodified><D:getetag>abc123</D:getetag>`,
			expected: `<D:getlastmodified>Fri, 10 Oct 2025 13:41:36 GMT</D:getlastmodified><D:getetag>"abc123"</D:getetag>`,
		},
		{
			name:     "XML-entity-encoded quotes unchanged (murena.io)",
			input:    `<d:getetag>&quot;df8b8abeff032a71c6c1d76db352996f&quot;</d:getetag>`,
			expected: `<d:getetag>&quot;df8b8abeff032a71c6c1d76db352996f&quot;</d:getetag>`,
		},
		{
			name:     "weak ETag gets normalized",
			input:    `<D:getetag>W/"abc123"</D:getetag>`,
			expected: `<D:getetag>"abc123"</D:getetag>`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := applyEtagFix(tt.input)
			if result != tt.expected {
				t.Errorf("got %q, want %q", result, tt.expected)
			}
		})
	}
}

// ============================================================================
// PUT / DELETE wrappers (Phase 2b.2.b.1)
// ============================================================================

// fakeCardDAVServer is a minimal httptest server that records the last request
// received and responds with the configured status + headers. Used by the
// Put/Delete tests to assert request shape and exercise error paths.
type fakeCardDAVServer struct {
	srv          *httptest.Server
	lastMethod   string
	lastPath     string
	lastIfMatch  string
	lastBody     []byte
	respStatus   int
	respETag     string
	respBody     string
}

func newFakeServer(status int, etag, body string) *fakeCardDAVServer {
	f := &fakeCardDAVServer{respStatus: status, respETag: etag, respBody: body}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.lastMethod = r.Method
		f.lastPath = r.URL.Path
		f.lastIfMatch = r.Header.Get("If-Match")
		f.lastBody, _ = io.ReadAll(r.Body)
		_ = r.Body.Close()
		if f.respETag != "" {
			w.Header().Set("ETag", f.respETag)
		}
		w.WriteHeader(f.respStatus)
		_, _ = w.Write([]byte(f.respBody))
	}))
	return f
}

func (f *fakeCardDAVServer) close() { f.srv.Close() }

func newTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	c, err := NewClient(baseURL, "user", "pass")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c
}

func TestPutContact_HappyPath(t *testing.T) {
	fake := newFakeServer(201, `"etag-after"`, "")
	defer fake.close()

	c := newTestClient(t, fake.srv.URL)
	etag, err := c.PutContact("/addressbook/", "/addressbook/contact.vcf", "etag-before", []byte("BEGIN:VCARD\r\nEND:VCARD\r\n"))
	if err != nil {
		t.Fatalf("PutContact: %v", err)
	}
	if etag != "etag-after" {
		t.Errorf("returned etag = %q, want %q", etag, "etag-after")
	}
	if fake.lastMethod != http.MethodPut {
		t.Errorf("method = %q", fake.lastMethod)
	}
	if fake.lastPath != "/addressbook/contact.vcf" {
		t.Errorf("path = %q", fake.lastPath)
	}
	if fake.lastIfMatch != `"etag-before"` {
		t.Errorf("If-Match = %q, want %q (quoted exact-match)", fake.lastIfMatch, `"etag-before"`)
	}
	if !bytes.Contains(fake.lastBody, []byte("BEGIN:VCARD")) {
		t.Errorf("body wasn't sent: %q", fake.lastBody)
	}
}

func TestPutContact_PreconditionFailed(t *testing.T) {
	fake := newFakeServer(http.StatusPreconditionFailed, `"server-etag"`, "")
	defer fake.close()

	c := newTestClient(t, fake.srv.URL)
	_, err := c.PutContact("/addressbook/", "/addressbook/contact.vcf", "stale-etag", []byte("dummy"))
	var pre *ErrPreconditionFailed
	if !errors.As(err, &pre) {
		t.Fatalf("expected *ErrPreconditionFailed, got %T: %v", err, err)
	}
	if pre.Href != "/addressbook/contact.vcf" {
		t.Errorf("conflict href = %q", pre.Href)
	}
	if pre.ServerETag != `"server-etag"` {
		t.Errorf("conflict server etag = %q", pre.ServerETag)
	}
}

func TestPutContact_OtherStatusError(t *testing.T) {
	fake := newFakeServer(http.StatusForbidden, "", "denied")
	defer fake.close()

	c := newTestClient(t, fake.srv.URL)
	_, err := c.PutContact("/addressbook/", "/addressbook/contact.vcf", "etag", []byte("dummy"))
	if err == nil {
		t.Fatal("expected error on 403")
	}
	var pre *ErrPreconditionFailed
	if errors.As(err, &pre) {
		t.Fatalf("403 should not surface as ErrPreconditionFailed")
	}
}

func TestDeleteContact_HappyPath(t *testing.T) {
	fake := newFakeServer(http.StatusNoContent, "", "")
	defer fake.close()

	c := newTestClient(t, fake.srv.URL)
	if err := c.DeleteContact("/addressbook/", "/addressbook/contact.vcf", "etag-before"); err != nil {
		t.Fatalf("DeleteContact: %v", err)
	}
	if fake.lastMethod != http.MethodDelete {
		t.Errorf("method = %q", fake.lastMethod)
	}
	if fake.lastIfMatch != `"etag-before"` {
		t.Errorf("If-Match = %q", fake.lastIfMatch)
	}
}

func TestDeleteContact_PreconditionFailed(t *testing.T) {
	fake := newFakeServer(http.StatusPreconditionFailed, `"server-etag"`, "")
	defer fake.close()

	c := newTestClient(t, fake.srv.URL)
	err := c.DeleteContact("/addressbook/", "/addressbook/contact.vcf", "stale-etag")
	var pre *ErrPreconditionFailed
	if !errors.As(err, &pre) {
		t.Fatalf("expected *ErrPreconditionFailed, got %T: %v", err, err)
	}
}

func TestDeleteContact_NotFoundIsIdempotent(t *testing.T) {
	fake := newFakeServer(http.StatusNotFound, "", "")
	defer fake.close()

	c := newTestClient(t, fake.srv.URL)
	if err := c.DeleteContact("/addressbook/", "/addressbook/contact.vcf", "etag"); err != nil {
		t.Errorf("404 should be idempotent success, got: %v", err)
	}
}

func TestQuotedETag(t *testing.T) {
	cases := []struct {
		in, out string
	}{
		{"abc", `"abc"`},
		{`"abc"`, `"abc"`},
		{`  "abc"  `, `"abc"`},
		{"", `""`},
	}
	for _, c := range cases {
		if got := quotedETag(c.in); got != c.out {
			t.Errorf("quotedETag(%q) = %q, want %q", c.in, got, c.out)
		}
	}
}

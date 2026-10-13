package notes

import (
	"reflect"
	"strings"
	"testing"
)

func TestNoteValidation(t *testing.T) {
	tags, err := normalizeTags([]string{" Work ", "work", "Ideas", ""})
	if err != nil || !reflect.DeepEqual(tags, []string{"work", "ideas"}) {
		t.Fatalf("tags: %v %v", tags, err)
	}
	if _, err = normalizeTags([]string{"bad\nname"}); err == nil {
		t.Fatal("accepted a control character")
	}
	for _, pair := range [][2]string{{strings.Repeat("界", 201), ""}, {"", strings.Repeat("x", 200001)}, {"", "null\x00"}} {
		if validateText(pair[0], pair[1]) == nil {
			t.Fatal("accepted invalid content")
		}
	}
	if err = validateText("Unicode ✨", "# Heading\n\n- item\n\n<script>text, never executed</script>"); err != nil {
		t.Fatal(err)
	}
}

func TestPublicationAccess(t *testing.T) {
	emails, err := normalizeAccess("restricted", []string{" Alex@Example.com ", "alex@example.com"})
	if err != nil || !reflect.DeepEqual(emails, []string{"alex@example.com"}) {
		t.Fatalf("emails: %v %v", emails, err)
	}
	if _, err = normalizeAccess("other", nil); err == nil {
		t.Fatal("accepted invalid visibility")
	}
	if _, err = normalizeAccess("restricted", []string{"bad address"}); err == nil {
		t.Fatal("accepted invalid email")
	}
	if got, err := normalizeAccess("public", []string{"alex@example.com"}); err != nil || len(got) != 0 {
		t.Fatal("public note retained allowlist")
	}
}

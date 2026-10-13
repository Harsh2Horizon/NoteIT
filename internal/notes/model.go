package notes

import (
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"noteit/internal/platform"
)

type Note struct {
	ID            string     `json:"id"`
	Title         string     `json:"title"`
	Content       string     `json:"content"`
	Excerpt       string     `json:"excerpt,omitempty"`
	Tags          []string   `json:"tags"`
	Pinned        bool       `json:"pinned"`
	Archived      bool       `json:"archived"`
	DeletedAt     *time.Time `json:"deleted_at"`
	Version       int64      `json:"version"`
	Visibility    string     `json:"visibility"`
	ShareToken    *string    `json:"-"`
	ShareURL      *string    `json:"share_url"`
	AllowedEmails []string   `json:"allowed_emails"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

const columns = `id::text,title,content,tags,pinned,archived,deleted_at,version,visibility,share_token,allowed_emails,created_at,updated_at`
const summaryColumns = `id::text,title,'' AS content,tags,pinned,archived,deleted_at,version,visibility,share_token,allowed_emails,created_at,updated_at,left(content,180)`

func scan(row pgx.Row, summary bool, base string) (Note, error) {
	var n Note
	args := []any{&n.ID, &n.Title, &n.Content, &n.Tags, &n.Pinned, &n.Archived, &n.DeletedAt, &n.Version, &n.Visibility, &n.ShareToken, &n.AllowedEmails, &n.CreatedAt, &n.UpdatedAt}
	if summary {
		args = append(args, &n.Excerpt)
	}
	err := row.Scan(args...)
	if n.ShareToken != nil {
		link := base + "/s/" + *n.ShareToken
		n.ShareURL = &link
	}
	return n, err
}

func normalizeTags(tags []string) ([]string, error) {
	if len(tags) > 20 {
		return nil, errors.New("Use at most 20 tags")
	}
	result := []string{}
	seen := map[string]bool{}
	for _, tag := range tags {
		tag = strings.ToLower(strings.TrimSpace(tag))
		if tag == "" {
			continue
		}
		if utf8.RuneCountInString(tag) > 40 || strings.IndexFunc(tag, unicode.IsControl) >= 0 {
			return nil, errors.New("Each tag must be at most 40 characters without control characters")
		}
		if !seen[tag] {
			result = append(result, tag)
			seen[tag] = true
		}
	}
	return result, nil
}

func validateText(title, content string) error {
	if !utf8.ValidString(title) || !utf8.ValidString(content) || strings.ContainsRune(title, 0) || strings.ContainsRune(content, 0) {
		return errors.New("Notes must be valid UTF-8 text without null characters")
	}
	if utf8.RuneCountInString(title) > 200 {
		return errors.New("Title must be at most 200 characters")
	}
	if len(content) > 200000 {
		return errors.New("Note content must be at most 200 KB")
	}
	return nil
}

func normalizeAccess(visibility string, emails []string) ([]string, error) {
	if visibility != "private" && visibility != "restricted" && visibility != "public" {
		return nil, errors.New("Visibility must be private, restricted, or public")
	}
	result := []string{}
	if visibility != "restricted" {
		return result, nil
	}
	if len(emails) > 50 {
		return nil, errors.New("Use at most 50 allowed email addresses")
	}
	seen := map[string]bool{}
	for _, raw := range emails {
		email, err := platform.Email(raw)
		if err != nil {
			return nil, err
		}
		if !seen[email] {
			result = append(result, email)
			seen[email] = true
		}
	}
	return result, nil
}

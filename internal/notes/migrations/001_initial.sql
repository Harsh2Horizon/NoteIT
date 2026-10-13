CREATE TABLE notes (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id uuid NOT NULL,
    title text NOT NULL DEFAULT '' CHECK (char_length(title) <= 200),
    content text NOT NULL DEFAULT '' CHECK (octet_length(content) <= 200000),
    tags text[] NOT NULL DEFAULT '{}' CHECK (cardinality(tags) <= 20),
    pinned boolean NOT NULL DEFAULT false,
    archived boolean NOT NULL DEFAULT false,
    deleted_at timestamptz,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    visibility text NOT NULL DEFAULT 'private' CHECK (visibility IN ('private','restricted','public')),
    share_token text UNIQUE,
    allowed_emails text[] NOT NULL DEFAULT '{}' CHECK (cardinality(allowed_emails) <= 50),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    search_document tsvector GENERATED ALWAYS AS (
        setweight(to_tsvector('simple',title),'A') || setweight(to_tsvector('simple',content),'B')
    ) STORED,
    CHECK (visibility = 'restricted' OR cardinality(allowed_emails) = 0),
    CHECK (visibility = 'private' OR share_token IS NOT NULL)
);
CREATE INDEX notes_owner_order_idx ON notes(owner_id, pinned DESC, updated_at DESC, id);
CREATE INDEX notes_search_idx ON notes USING gin(search_document);
CREATE INDEX notes_tags_idx ON notes USING gin(tags);

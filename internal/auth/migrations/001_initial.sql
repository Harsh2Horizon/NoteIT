CREATE TABLE users (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    email text NOT NULL UNIQUE CHECK (email = lower(email)),
    password_hash text NOT NULL,
    verified_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE verification_tokens (
    token_hash text PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX verification_user_idx ON verification_tokens(user_id);
CREATE INDEX verification_expiry_idx ON verification_tokens(expires_at);

CREATE TABLE sessions (
    token_hash text PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX session_user_idx ON sessions(user_id);
CREATE INDEX session_expiry_idx ON sessions(expires_at);

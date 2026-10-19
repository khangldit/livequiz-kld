-- +goose Up
CREATE TABLE users (
    id            BIGSERIAL PRIMARY KEY,
    email         TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    display_name  TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE quizzes (
    id          BIGSERIAL PRIMARY KEY,
    owner_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title       TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_quizzes_owner ON quizzes(owner_id);

CREATE TABLE questions (
    id             BIGSERIAL PRIMARY KEY,
    quiz_id        BIGINT NOT NULL REFERENCES quizzes(id) ON DELETE CASCADE,
    position       INT NOT NULL,
    text           TEXT NOT NULL,
    options        TEXT[] NOT NULL,
    correct_index  INT NOT NULL,
    time_limit_sec INT NOT NULL DEFAULT 20,
    UNIQUE (quiz_id, position),
    CHECK (array_length(options, 1) BETWEEN 2 AND 4),
    CHECK (correct_index >= 0 AND correct_index < array_length(options, 1)),
    CHECK (time_limit_sec BETWEEN 5 AND 120)
);

-- +goose Down
DROP TABLE questions;
DROP TABLE quizzes;
DROP TABLE users;
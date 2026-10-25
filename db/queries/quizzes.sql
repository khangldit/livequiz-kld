-- name: CreateQuiz :one
INSERT INTO quizzes (owner_id, title, description)
VALUES ($1, $2, $3)
RETURNING *;

-- name: CreateQuestion :exec
INSERT INTO questions (quiz_id, position, text, options, correct_index, time_limit_sec)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: ListQuizzesByOwner :many
SELECT q.id, q.title, q.description, q.created_at,
       COUNT(qs.id)::int AS question_count
FROM quizzes q
LEFT JOIN questions qs ON qs.quiz_id = q.id
WHERE q.owner_id = $1
GROUP BY q.id
ORDER BY q.created_at DESC;

-- name: GetQuiz :one
SELECT * FROM quizzes WHERE id = $1 AND owner_id = $2;

-- name: ListQuestionsByQuiz :many
SELECT * FROM questions WHERE quiz_id = $1 ORDER BY position;

-- name: DeleteQuiz :execrows
DELETE FROM quizzes WHERE id = $1 AND owner_id = $2;
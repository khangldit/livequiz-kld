-include .env
export

db-up:
	docker compose up -d db

run:
	go run ./cmd/api

migrate-new:
	goose -dir db/migrations create $(name) sql

migrate-up:
	goose -dir db/migrations postgres "$(DATABASE_URL)" up

migrate-down:
	goose -dir db/migrations postgres "$(DATABASE_URL)" down

migrate-status:
	goose -dir db/migrations postgres "$(DATABASE_URL)" status

sqlc:
	sqlc generate
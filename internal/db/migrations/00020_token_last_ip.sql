-- +goose Up

-- Client address of a personal access token's last use, shown next to it.
ALTER TABLE personal_tokens ADD COLUMN last_used_ip inet;

-- +goose Down
ALTER TABLE personal_tokens DROP COLUMN last_used_ip;

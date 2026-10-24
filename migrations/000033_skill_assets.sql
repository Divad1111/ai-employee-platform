-- +goose Up
ALTER TABLE skill_packages ADD COLUMN raw_skill_md TEXT NOT NULL DEFAULT '';
ALTER TABLE skill_packages ADD COLUMN revision BIGINT NOT NULL DEFAULT 1;

-- +goose Down
ALTER TABLE skill_packages DROP COLUMN revision;
ALTER TABLE skill_packages DROP COLUMN raw_skill_md;

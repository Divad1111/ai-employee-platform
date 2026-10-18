-- +goose Up
-- Keep one Feishu card per task. Migration 000029 may already have merged
-- earlier per-task conversations; split those turns back out and preserve the
-- original card IDs where a task card existed.
DROP INDEX IF EXISTS idx_task_conversations_employee_active;

CREATE TEMP TABLE task_conversation_restore_map ON COMMIT DROP AS
WITH ranked_conversations AS (
    SELECT id AS conversation_id,
           employee_id,
           ROW_NUMBER() OVER (
               PARTITION BY employee_id ORDER BY created_at, id
           ) AS position
    FROM task_conversations
), ranked_turns AS (
    SELECT t.job_id,
           t.status,
           t.created_at,
           c.employee_id,
           ROW_NUMBER() OVER (
               PARTITION BY c.employee_id ORDER BY t.created_at, t.job_id
           ) AS position
    FROM task_conversation_turns t
    JOIN task_conversations c ON c.id = t.conversation_id
)
SELECT c.conversation_id, t.job_id
FROM ranked_conversations c
JOIN ranked_turns t
  ON t.employee_id = c.employee_id
 AND t.position = c.position;

UPDATE task_conversations c
SET merged_into_id = NULL,
    current_job_id = m.job_id,
    status = j.status,
    updated_at = NOW()
FROM task_conversation_restore_map m
JOIN jobs j ON j.id = m.job_id
WHERE c.id = m.conversation_id;

UPDATE task_conversation_turns t
SET conversation_id = m.conversation_id,
    turn_no = 1
FROM task_conversation_restore_map m
WHERE t.job_id = m.job_id;

-- Tasks created after 000029 have no old task card. Give each one its own
-- conversation row so the service can create a proper Feishu card for it.
INSERT INTO task_conversations
    (id, employee_id, employee_name, owner_user_id, owner_open_id, chat_id,
     current_job_id, card_message_id, status, active_provider, active_model,
     created_at, updated_at, merged_into_id)
SELECT 'TC-RESTORE-' || md5(j.id),
       base.employee_id, base.employee_name, base.owner_user_id, base.owner_open_id, base.chat_id,
       j.id, '', j.status, base.active_provider, base.active_model,
       j.created_at, NOW(), NULL
FROM task_conversation_turns t
JOIN jobs j ON j.id = t.job_id
JOIN LATERAL (
    SELECT c.employee_id, c.employee_name, c.owner_user_id, c.owner_open_id,
           c.chat_id, c.active_provider, c.active_model
    FROM task_conversations c
    WHERE c.employee_id = j.employee_id AND c.merged_into_id IS NULL
    ORDER BY c.created_at DESC, c.id DESC
    LIMIT 1
) base ON TRUE
WHERE NOT EXISTS (
    SELECT 1 FROM task_conversation_restore_map m WHERE m.job_id = j.id
);

UPDATE task_conversation_turns t
SET conversation_id = 'TC-RESTORE-' || md5(t.job_id),
    turn_no = 1
WHERE NOT EXISTS (
    SELECT 1 FROM task_conversation_restore_map m WHERE m.job_id = t.job_id
);

UPDATE task_conversation_messages m
SET conversation_id = t.conversation_id
FROM task_conversation_turns t
WHERE m.job_id = t.job_id;

UPDATE task_conversation_deltas d
SET conversation_id = t.conversation_id
FROM task_conversation_turns t
WHERE d.job_id = t.job_id;

UPDATE task_conversation_activities a
SET conversation_id = t.conversation_id
FROM task_conversation_turns t
WHERE a.job_id = t.job_id;

-- +goose Down
-- The split is a data repair; there is no safe reverse migration.
SELECT 1;

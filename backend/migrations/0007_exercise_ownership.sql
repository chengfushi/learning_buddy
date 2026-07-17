-- 0007: 测评题目绑定生成用户，防止其他登录用户猜 ID 作答并获取答案。

ALTER TABLE exercises
    ADD COLUMN IF NOT EXISTS user_id BIGINT;

DO $migration$
BEGIN
    IF NOT EXISTS (
        SELECT 1
          FROM pg_constraint
         WHERE conname = 'fk_exercises_user'
           AND conrelid = 'exercises'::regclass
    ) THEN
        ALTER TABLE exercises
            ADD CONSTRAINT fk_exercises_user
            FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE;
    END IF;
END
$migration$;

-- 历史题目优先按会话所有者回填，其次按唯一作答用户回填。
UPDATE exercises AS exercise
   SET user_id = session.user_id
  FROM agent_sessions AS session
 WHERE exercise.user_id IS NULL
   AND exercise.session_id = session.id;

WITH unique_attempt_owner AS (
    SELECT exercise_id, MIN(user_id) AS user_id
      FROM quiz_attempts
     GROUP BY exercise_id
    HAVING COUNT(DISTINCT user_id) = 1
)
UPDATE exercises AS exercise
   SET user_id = owner.user_id
  FROM unique_attempt_owner AS owner
 WHERE exercise.user_id IS NULL
   AND exercise.id = owner.exercise_id;

-- 无法安全确定所有者的历史题目显式失效，避免将答案错配给其他用户。
DELETE FROM quiz_attempts
 WHERE exercise_id IN (SELECT id FROM exercises WHERE user_id IS NULL);
DELETE FROM exercises WHERE user_id IS NULL;

ALTER TABLE exercises
    ALTER COLUMN user_id SET NOT NULL;

CREATE INDEX IF NOT EXISTS idx_exercises_user ON exercises(user_id);

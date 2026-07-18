-- 会话按全局或指定资料分域，防止续聊时混用不同 RAG 上下文。
ALTER TABLE agent_sessions
    ADD COLUMN IF NOT EXISTS material_id BIGINT;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'fk_agent_sessions_material'
          AND conrelid = 'agent_sessions'::regclass
    ) THEN
        ALTER TABLE agent_sessions
            ADD CONSTRAINT fk_agent_sessions_material
            FOREIGN KEY (material_id) REFERENCES materials(id) ON DELETE CASCADE;
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_as_user_material_created
    ON agent_sessions(user_id, material_id, created_at DESC);

-- 资料删除会级联删除其会话；已生成题目保留，只解除可选会话关联。
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'exercises_session_id_fkey'
          AND conrelid = 'exercises'::regclass
          AND confdeltype = 'n'
    ) THEN
        ALTER TABLE exercises
            DROP CONSTRAINT IF EXISTS exercises_session_id_fkey;
        ALTER TABLE exercises
            ADD CONSTRAINT exercises_session_id_fkey
            FOREIGN KEY (session_id) REFERENCES agent_sessions(id) ON DELETE SET NULL;
    END IF;
END $$;

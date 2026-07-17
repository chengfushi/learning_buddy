#!/usr/bin/env bash
set -euo pipefail

# 管理员连接参数可由环境变量覆盖；测试只创建并删除一个随机临时库。
: "${PGHOST:=localhost}"
: "${PGPORT:=5432}"
: "${PGUSER:=postgres}"
: "${PGPASSWORD:=postgres}"
export PGHOST PGPORT PGUSER PGPASSWORD

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
TEMP_NAME="learning_buddy_migration_test_${$}"
TEST_DB="${MIGRATION_TEST_DB:-$TEMP_NAME}"
TEST_SCHEMA=""

cleanup() {
  if [[ -n "$TEST_SCHEMA" ]]; then
    psql -X -v ON_ERROR_STOP=1 -d "$TEST_DB" \
      -c "DROP SCHEMA IF EXISTS \"$TEST_SCHEMA\" CASCADE" >/dev/null
  else
    dropdb --if-exists --force "$TEST_DB" >/dev/null
  fi
}
trap cleanup EXIT

if [[ -n "${MIGRATION_TEST_DB:-}" ]]; then
  # 已有库模式只创建隔离 schema，适合扩展已安装但本机扩展安装文件不可用的环境。
  TEST_SCHEMA="$TEMP_NAME"
  psql -X -v ON_ERROR_STOP=1 -d "$TEST_DB" \
    -c "CREATE SCHEMA \"$TEST_SCHEMA\"" >/dev/null
  export PGOPTIONS="-c search_path=$TEST_SCHEMA,public"
else
  createdb "$TEST_DB"
fi

for migration in "$ROOT_DIR"/backend/migrations/000{1..6}_*.sql; do
  psql -X -v ON_ERROR_STOP=1 -d "$TEST_DB" -f "$migration" >/dev/null
done

psql -X -v ON_ERROR_STOP=1 -d "$TEST_DB" >/dev/null <<'SQL'
INSERT INTO agent_sessions (id, user_id, title)
VALUES ('00000000-0000-0000-0000-000000000001', 2, 'migration owner');

INSERT INTO exercises (id, session_id, question)
VALUES
  (101, '00000000-0000-0000-0000-000000000001', 'owned by session'),
  (102, NULL, 'owned by unique attempt'),
  (103, NULL, 'orphan');

INSERT INTO quiz_attempts (user_id, exercise_id, choice)
VALUES
  (3, 102, 'A'),
  (2, 103, 'B'),
  (3, 103, 'C');
SQL

psql -X -v ON_ERROR_STOP=1 -d "$TEST_DB" \
  -f "$ROOT_DIR/backend/migrations/0007_exercise_ownership.sql" >/dev/null

result=$(psql -X -At -v ON_ERROR_STOP=1 -d "$TEST_DB" <<'SQL'
SELECT concat_ws(':',
    (SELECT user_id FROM exercises WHERE id = 101),
    (SELECT user_id FROM exercises WHERE id = 102),
    (SELECT COUNT(*) FROM exercises WHERE id = 103),
    (SELECT COUNT(*) FROM quiz_attempts WHERE exercise_id = 103),
    (SELECT is_nullable
       FROM information_schema.columns
      WHERE table_schema = current_schema()
        AND table_name = 'exercises'
        AND column_name = 'user_id'),
    (SELECT COUNT(*)
       FROM pg_constraint AS constraint_row
       JOIN pg_attribute AS attribute
         ON attribute.attrelid = constraint_row.conrelid
        AND attribute.attnum = ANY (constraint_row.conkey)
      WHERE constraint_row.contype = 'f'
        AND constraint_row.conrelid = 'exercises'::regclass
        AND constraint_row.confrelid = 'users'::regclass
        AND attribute.attname = 'user_id')
);
SQL
)

expected="2:3:0:0:NO:1"
if [[ "$result" != "$expected" ]]; then
  echo "migration assertions failed: expected $expected, got $result" >&2
  exit 1
fi

echo "migration assertions passed: $result"

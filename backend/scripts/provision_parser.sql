-- 使用管理员 DSN 执行；密码从 PARSER_DB_PASSWORD 环境变量读取，不写入仓库。
\getenv parser_password PARSER_DB_PASSWORD

SELECT format(
    'CREATE ROLE learning_parser LOGIN PASSWORD %L',
    :'parser_password'
)
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'learning_parser')
\gexec

SELECT format(
    'ALTER ROLE learning_parser WITH LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS PASSWORD %L',
    :'parser_password'
)
\gexec

DO $ownership_check$
DECLARE
    parser_oid OID := (SELECT oid FROM pg_roles WHERE rolname = 'learning_parser');
BEGIN
    IF EXISTS (SELECT 1 FROM pg_class WHERE relowner = parser_oid)
       OR EXISTS (SELECT 1 FROM pg_namespace WHERE nspowner = parser_oid)
       OR EXISTS (SELECT 1 FROM pg_proc WHERE proowner = parser_oid)
       OR EXISTS (SELECT 1 FROM pg_database WHERE datdba = parser_oid) THEN
        RAISE EXCEPTION 'learning_parser owns database objects; transfer ownership before provisioning';
    END IF;
END
$ownership_check$;

-- 重复执行时先清除历史角色继承与非必要直接授权，再重建白名单。
SELECT format('REVOKE %I FROM learning_parser', parent.rolname)
  FROM pg_auth_members AS membership
  JOIN pg_roles AS member ON member.oid = membership.member
  JOIN pg_roles AS parent ON parent.oid = membership.roleid
 WHERE member.rolname = 'learning_parser'
\gexec

SELECT format(
    'REVOKE ALL PRIVILEGES ON ALL TABLES IN SCHEMA %I FROM learning_parser',
    nspname
)
  FROM pg_namespace
 WHERE nspname !~ '^pg_' AND nspname <> 'information_schema'
\gexec

SELECT format(
    'REVOKE ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA %I FROM learning_parser',
    nspname
)
  FROM pg_namespace
 WHERE nspname !~ '^pg_' AND nspname <> 'information_schema'
\gexec

SELECT format(
    'REVOKE ALL PRIVILEGES ON SCHEMA %I FROM learning_parser',
    nspname
)
  FROM pg_namespace
 WHERE nspname !~ '^pg_' AND nspname <> 'information_schema'
\gexec

SELECT format('REVOKE ALL PRIVILEGES ON DATABASE %I FROM learning_parser', current_database())
\gexec
SELECT format('GRANT CONNECT ON DATABASE %I TO learning_parser', current_database())
\gexec

GRANT USAGE ON SCHEMA public TO learning_parser;

GRANT SELECT (id, team_id, parse_status, parse_generation)
    ON TABLE materials TO learning_parser;
GRANT UPDATE (content)
    ON TABLE materials TO learning_parser;

GRANT SELECT (material_id)
    ON TABLE material_chunks TO learning_parser;
GRANT INSERT (team_id, material_id, chunk_idx, content, embedding)
    ON TABLE material_chunks TO learning_parser;
GRANT DELETE
    ON TABLE material_chunks TO learning_parser;
GRANT USAGE
    ON SEQUENCE material_chunks_id_seq TO learning_parser;

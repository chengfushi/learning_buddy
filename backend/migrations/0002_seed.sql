-- learning_buddy 种子数据（幂等，可重复执行）
-- 超级管理员 / 演示老师 / 演示学生 + 系统级公共库 + 私有 team

-- 系统级公共库（type='public'，不写入 team_members，可见性计算中特判）
INSERT INTO teams (id, name, type, join_code, owner_id)
VALUES (1, '公共知识库', 'public', NULL, NULL)
ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, type = EXCLUDED.type;

-- 超级管理员（email: super@local.dev / password: Admin@123）
INSERT INTO users (id, email, password_hash, display_name, role, subscription)
VALUES (1, 'super@local.dev', '$2a$10$WbGpqHwU2T8yr6r7nVyqh.ZfapPVxOI4wLA3v1EKOi3AEQBP1/XcG', '系统管理员', 'super_admin', 'pro')
ON CONFLICT (email) DO NOTHING;

-- 演示老师（email: teacher@local.dev / password: Teacher@123）
INSERT INTO users (id, email, password_hash, display_name, role, subscription)
VALUES (2, 'teacher@local.dev', '$2a$10$3uX3v8DBiw3vA9XyGfRxte29qpFkOr38GJ73nU/TtFQjY55hwzYfe', '示范老师', 'teacher', 'free')
ON CONFLICT (email) DO NOTHING;

-- 演示学生（email: student@local.dev / password: Student@123）
INSERT INTO users (id, email, password_hash, display_name, role, subscription)
VALUES (3, 'student@local.dev', '$2a$10$JjYm8YbP.5DCUV66FHIt9e4JERUttkus3yg8DP.e0jRmrrpnBv7lS', '示范学生', 'student', 'free')
ON CONFLICT (email) DO NOTHING;

-- 老师 / 学生的私有 team（注册流程也会为每个学生自动建；此处为演示账号补齐）
INSERT INTO teams (id, name, type, join_code, owner_id)
VALUES (2, '示范老师的私有资料', 'private', NULL, 2)
ON CONFLICT (id) DO NOTHING;

INSERT INTO teams (id, name, type, join_code, owner_id)
VALUES (3, '示范学生的私有资料', 'private', NULL, 3)
ON CONFLICT (id) DO NOTHING;

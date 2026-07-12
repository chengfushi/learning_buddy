#!/usr/bin/env bash
# E2E acceptance for 智能学伴 P0 主流程
# 前置：PostgreSQL 17 运行且迁移已应用；backend 监听 :8080；agent 监听 :8000
# 用法：bash tests/e2e/e2e.sh
set -u
BASE=${BASE:-http://127.0.0.1:8080}
AGENT=${AGENT:-http://127.0.0.1:8000}
TS=$(date +%s)
jget() { python3 -c "import sys,json;d=json.load(sys.stdin);print($1)"; }

echo "[0] Agent 健康检查"; curl -s -m 5 $AGENT/health; echo

echo "[1] 注册 teacher"
R_T=$(curl -s -X POST $BASE/api/auth/register -H 'Content-Type: application/json' \
  -d "{\"email\":\"teacher_$TS@lb.test\",\"password\":\"pass1234\",\"display_name\":\"老师E\",\"role\":\"teacher\"}")
TOK_T=$(echo "$R_T" | jget "d['access_token']")
echo "    TOK_T len=${#TOK_T}"

echo "[2] 注册 student"
R_S=$(curl -s -X POST $BASE/api/auth/register -H 'Content-Type: application/json' \
  -d "{\"email\":\"student_$TS@lb.test\",\"password\":\"pass1234\",\"display_name\":\"学生E\",\"role\":\"student\"}")
TOK_S=$(echo "$R_S" | jget "d['access_token']")
UID_S=$(echo "$R_S" | jget "d['user']['id']")
echo "    TOK_S len=${#TOK_S} UID_S=$UID_S"

echo "[3] teacher 建组"
R_TEAM=$(curl -s -X POST $BASE/api/teams -H 'Content-Type: application/json' -H "Authorization: Bearer $TOK_T" \
  -d '{"name":"高一物理E"}')
TEAM_ID=$(echo "$R_TEAM" | jget "d['id']")
JOIN_CODE=$(echo "$R_TEAM" | jget "d['join_code']")
echo "    TEAM_ID=$TEAM_ID JOIN_CODE=$JOIN_CODE"

echo "[4] student 凭码加入（应 pending）"
curl -s -X POST $BASE/api/teams/join -H 'Content-Type: application/json' -H "Authorization: Bearer $TOK_S" \
  -d "{\"code\":\"$JOIN_CODE\"}"; echo

echo "[5] teacher 审批学生"
curl -s -X POST $BASE/api/teams/$TEAM_ID/members/$UID_S/approve -H "Authorization: Bearer $TOK_T"; echo

echo "[6] teacher 上传资料（触发异步解析）"
R_M=$(curl -s -X POST $BASE/api/materials -H 'Content-Type: application/json' -H "Authorization: Bearer $TOK_T" \
  -d "{\"team_id\":$TEAM_ID,\"title\":\"牛顿运动定律\",\"subject\":\"物理\",\"content\":\"牛顿第一定律：任何物体都要保持匀速直线运动或静止状态，直到外力迫使它改变运动状态为止。牛顿第二定律：物体的加速度跟作用力成正比，跟物体的质量成反比，即 F=ma。牛顿第三定律：两个物体之间的作用力和反作用力，在同一条直线上，大小相等，方向相反。\"}")
MAT_ID=$(echo "$R_M" | jget "d['material']['ID']")
echo "    MAT_ID=$MAT_ID"

echo "[6.5] teacher 设 shared=true（R2：学生仅在 shared 时可见 teacher 资料）"
curl -s -X PUT $BASE/api/materials/$MAT_ID -H 'Content-Type: application/json' -H "Authorization: Bearer $TOK_T" \
  -d '{"shared":true}'; echo

echo "[7] 轮询 parse_status（Go 字段名 ParseStatus）"
for i in $(seq 1 12); do
  ST=$(curl -s $BASE/api/materials/$MAT_ID -H "Authorization: Bearer $TOK_T" | jget "d['material']['ParseStatus']")
  echo "    尝试$i: ParseStatus=$ST"
  [ "$ST" = "done" ] && break
  sleep 1
done

echo "[8] student AI 答疑（SSE 流式，应带引用）"
curl -sN -m 25 -X POST $BASE/api/agent/chat -H 'Content-Type: application/json' -H "Authorization: Bearer $TOK_S" \
  -d "{\"question\":\"牛顿第二定律是什么？\",\"material_id\":$MAT_ID}" | grep -E '"type": ?"(token|done|error)"' | head -25
echo ""

echo "[9] 学习计划（F7）"
curl -s -X POST $BASE/api/agent/plan -H 'Content-Type: application/json' -H "Authorization: Bearer $TOK_S" \
  -d '{"goal":"两周内掌握牛顿三大定律并能解题","deadline":"2026-07-26"}'; echo

echo "[10] 智能测评（F8）"
curl -s -X POST $BASE/api/agent/quiz -H 'Content-Type: application/json' -H "Authorization: Bearer $TOK_S" \
  -d "{\"topic\":\"牛顿定律\",\"material_id\":$MAT_ID,\"count\":3}"; echo

echo "[11] 学习记录（F6）"
curl -s -X POST $BASE/api/learning/records -H 'Content-Type: application/json' -H "Authorization: Bearer $TOK_S" \
  -d "{\"material_id\":$MAT_ID,\"duration_s\":120,\"progress\":0.5}"; echo

echo "[12] 进度看板（F9）"
curl -s $BASE/api/learning/progress -H "Authorization: Bearer $TOK_S"; echo

echo "[13] R2 权限隔离：另一 student（未加入、非成员）对该资料答疑应无引用"
R_S2=$(curl -s -X POST $BASE/api/auth/register -H 'Content-Type: application/json' \
  -d "{\"email\":\"student2_$TS@lb.test\",\"password\":\"pass1234\",\"display_name\":\"学生F\",\"role\":\"student\"}")
TOK_S2=$(echo "$R_S2" | jget "d['access_token']")
curl -s -m 25 -X POST $BASE/api/agent/chat -H 'Content-Type: application/json' -H "Authorization: Bearer $TOK_S2" \
  -d "{\"question\":\"牛顿第二定律是什么？\",\"material_id\":$MAT_ID}" | grep -E '"type": ?"(done|error)"' | head -3
echo ""

echo "[14] 笔记（F3）"
curl -s -X POST $BASE/api/materials/$MAT_ID/notes -H 'Content-Type: application/json' -H "Authorization: Bearer $TOK_S" \
  -d '{"content":"重点：F=ma 注意单位统一","quote":"牛顿第二定律"}'; echo

echo "E2E 完成"

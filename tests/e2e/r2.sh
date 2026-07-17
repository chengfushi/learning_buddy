#!/usr/bin/env bash
# R2 权限隔离端到端验证：成员可见 shared 资料，非成员不可见
# 前置：PostgreSQL 17 运行且迁移已应用；backend :8080；agent :8000
# 用法：bash tests/e2e/r2.sh
set -euo pipefail
BASE=${BASE:-http://127.0.0.1:8080}
TS=$(date +%s)
jget() { python3 -c "import sys,json;d=json.load(sys.stdin);print($1)"; }
mk() { curl -s -X POST $BASE/api/auth/register -H 'Content-Type: application/json' -d "{\"email\":\"$1@$TS.lb\",\"password\":\"pass1234\",\"display_name\":\"$2\",\"role\":\"$3\"}"; }

RT=$(mk t_$TS T teacher);  TOK_T=$(echo  "$RT" |jget "d['access_token']")
RS=$(mk s_$TS S student);  TOK_S=$(echo  "$RS" |jget "d['access_token']"); UID_S=$(echo "$RS"|jget "d['user']['id']")
RS2=$(mk s2_$TS S2 student); TOK_S2=$(echo "$RS2"|jget "d['access_token']")

RTM=$(curl -s -X POST $BASE/api/teams -H 'Content-Type: application/json' -H "Authorization: Bearer $TOK_T" -d '{"name":"r2"}')
TID=$(echo "$RTM"|jget "d['id']"); CODE=$(echo "$RTM"|jget "d['join_code']")

curl -s -X POST $BASE/api/teams/join -H 'Content-Type: application/json' -H "Authorization: Bearer $TOK_S" -d "{\"code\":\"$CODE\"}" >/dev/null
curl -s -X POST $BASE/api/teams/$TID/members/$UID_S/approve -H "Authorization: Bearer $TOK_T" >/dev/null

RM=$(curl -s -X POST $BASE/api/materials -H 'Content-Type: application/json' -H "Authorization: Bearer $TOK_T" -d "{\"team_id\":$TID,\"title\":\"N\",\"content\":\"牛顿第二定律：F=ma，力等于质量乘加速度。\"}")
MID=$(echo "$RM"|jget "d['material']['ID']")

# shared=false 时，即使 approved 成员也不能按 ID 读取或写笔记。
MEMBER_DRAFT_READ=$(curl -sS -o /dev/null -w '%{http_code}' "$BASE/api/materials/$MID" -H "Authorization: Bearer $TOK_S")
MEMBER_DRAFT_NOTE=$(curl -sS -o /dev/null -w '%{http_code}' -X POST "$BASE/api/materials/$MID/notes" \
  -H 'Content-Type: application/json' -H "Authorization: Bearer $TOK_S" -d '{"content":"must be denied"}')
[ "$MEMBER_DRAFT_READ" = "404" ] || { echo "R2 失败：成员按 ID 读取了 shared=false 草稿（HTTP $MEMBER_DRAFT_READ）" >&2; exit 1; }
[ "$MEMBER_DRAFT_NOTE" = "404" ] || { echo "R2 失败：成员向 shared=false 草稿写入笔记（HTTP $MEMBER_DRAFT_NOTE）" >&2; exit 1; }

curl -s -X PUT $BASE/api/materials/$MID -H 'Content-Type: application/json' -H "Authorization: Bearer $TOK_T" -d '{"shared":true}' >/dev/null

# shared=true 仅对 approved 成员开放；非成员的详情、team 列表和笔记入口都必须拒绝。
MEMBER_READ=$(curl -sS -o /dev/null -w '%{http_code}' "$BASE/api/materials/$MID" -H "Authorization: Bearer $TOK_S")
NON_MEMBER_READ=$(curl -sS -o /dev/null -w '%{http_code}' "$BASE/api/materials/$MID" -H "Authorization: Bearer $TOK_S2")
NON_MEMBER_NOTE_CREATE=$(curl -sS -o /dev/null -w '%{http_code}' -X POST "$BASE/api/materials/$MID/notes" \
  -H 'Content-Type: application/json' -H "Authorization: Bearer $TOK_S2" -d '{"content":"must be denied"}')
NON_MEMBER_NOTE_LIST=$(curl -sS -o /dev/null -w '%{http_code}' "$BASE/api/materials/$MID/notes" -H "Authorization: Bearer $TOK_S2")
NON_MEMBER_TEAM_COUNT=$(curl -fsS "$BASE/api/teams/$TID/materials" -H "Authorization: Bearer $TOK_S2" | jget "len(d['materials'])")
[ "$MEMBER_READ" = "200" ] || { echo "R2 失败：已审批成员无法读取 shared 资料（HTTP $MEMBER_READ）" >&2; exit 1; }
[ "$NON_MEMBER_READ" = "404" ] || { echo "R2 失败：非成员按 ID 读取了 shared 资料（HTTP $NON_MEMBER_READ）" >&2; exit 1; }
[ "$NON_MEMBER_NOTE_CREATE" = "404" ] || { echo "R2 失败：非成员向 shared 资料写入笔记（HTTP $NON_MEMBER_NOTE_CREATE）" >&2; exit 1; }
[ "$NON_MEMBER_NOTE_LIST" = "404" ] || { echo "R2 失败：非成员列出了 shared 资料笔记（HTTP $NON_MEMBER_NOTE_LIST）" >&2; exit 1; }
[ "$NON_MEMBER_TEAM_COUNT" = "0" ] || { echo "R2 失败：非成员 team 视图返回了 $NON_MEMBER_TEAM_COUNT 条资料" >&2; exit 1; }

# 测评 material_id 必须先走 repository 统一可见性，不能把猜到的 ID 下发给 Agent。
NON_MEMBER_QUIZ=$(curl -sS -o /dev/null -w '%{http_code}' -X POST "$BASE/api/agent/quiz" \
  -H 'Content-Type: application/json' -H "Authorization: Bearer $TOK_S2" \
  -d "{\"topic\":\"越权测评\",\"material_id\":$MID,\"count\":1}")
[ "$NON_MEMBER_QUIZ" = "404" ] || { echo "R2 失败：非成员使用 material_id 生成测评（HTTP $NON_MEMBER_QUIZ）" >&2; exit 1; }

STATUS=""
for _ in $(seq 1 20); do
  STATUS=$(curl -fsS $BASE/api/materials/$MID -H "Authorization: Bearer $TOK_T" | jget "d['material']['ParseStatus']")
  [ "$STATUS" = "done" ] && break
  [ "$STATUS" = "failed" ] && { echo "解析失败，无法执行 R2 验证" >&2; exit 1; }
  sleep 0.5
done
[ "$STATUS" = "done" ] || { echo "解析超时，最终状态：$STATUS" >&2; exit 1; }

# 用 Python 稳健解析 SSE，提取最后一个 done 事件的 citations 数
parse_cites() {
  local TOK=$1
  curl -fsSN -m 20 -X POST $BASE/api/agent/chat -H 'Content-Type: application/json' -H "Authorization: Bearer $TOK" \
    -d "{\"question\":\"牛顿第二定律是什么？\",\"material_id\":$MID}" \
  | python3 -c "
import sys,json
cites=None
for line in sys.stdin:
    line=line.strip()
    if not line.startswith('data:'): continue
    payload=line[5:].strip()
    try: d=json.loads(payload)
    except: continue
    if d.get('type')=='done': cites=d.get('citations',[])
print(len(cites) if cites is not None else 'NO_DONE_EVENT')
"
}

MEMBER_CITES=$(parse_cites "$TOK_S")
NON_MEMBER_CHAT=$(curl -sS -o /dev/null -w '%{http_code}' -X POST "$BASE/api/agent/chat" \
  -H 'Content-Type: application/json' -H "Authorization: Bearer $TOK_S2" \
  -d "{\"question\":\"牛顿第二定律是什么？\",\"material_id\":$MID}")
echo "成员 student   citations: $MEMBER_CITES"
echo "非成员 student2 chat HTTP: $NON_MEMBER_CHAT"

[[ "$MEMBER_CITES" =~ ^[0-9]+$ ]] || { echo "成员答疑未收到 done 事件" >&2; exit 1; }
[ "$MEMBER_CITES" -gt 0 ] || { echo "R2 失败：已审批成员未召回 shared 资料" >&2; exit 1; }
[ "$NON_MEMBER_CHAT" = "404" ] || { echo "R2 失败：非成员使用 material_id 答疑返回 HTTP $NON_MEMBER_CHAT" >&2; exit 1; }
echo "R2 验证通过"

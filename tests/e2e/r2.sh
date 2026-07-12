#!/usr/bin/env bash
# R2 权限隔离端到端验证：成员可见 shared 资料，非成员不可见
# 前置：PostgreSQL 17 运行且迁移已应用；backend :8080；agent :8000
# 用法：bash tests/e2e/r2.sh
set -u
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
curl -s -X PUT $BASE/api/materials/$MID -H 'Content-Type: application/json' -H "Authorization: Bearer $TOK_T" -d '{"shared":true}' >/dev/null
sleep 1

# 用 Python 稳健解析 SSE，提取最后一个 done 事件的 citations 数
parse_cites() {
  local TOK=$1
  curl -sN -m 20 -X POST $BASE/api/agent/chat -H 'Content-Type: application/json' -H "Authorization: Bearer $TOK" \
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

echo -n "成员 student   citations: "; parse_cites "$TOK_S"
echo -n "非成员 student2 citations: "; parse_cites "$TOK_S2"
echo "R2 验证结束"

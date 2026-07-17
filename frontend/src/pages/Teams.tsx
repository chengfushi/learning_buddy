import { useEffect, useState, type FormEvent } from "react";
import { api, type Team, type TeamMember } from "../api";
import { useAuth } from "../auth";

export default function Teams() {
  const { user } = useAuth();
  const [teams, setTeams] = useState<Team[]>([]);
  const [newName, setNewName] = useState("");
  const [joinCode, setJoinCode] = useState("");
  const [manageId, setManageId] = useState<number | null>(null);
  const [members, setMembers] = useState<TeamMember[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadingMembers, setLoadingMembers] = useState(false);
  const [err, setErr] = useState("");

  const load = () => {
    setLoading(true);
    api
      .listTeams()
      .then((r) => setTeams(r.teams))
      .catch((e) => setErr(e instanceof Error ? e.message : "加载失败"))
      .finally(() => setLoading(false));
  };
  useEffect(() => {
    load();
  }, []);

  const create = async (e: FormEvent) => {
    e.preventDefault();
    setErr("");
    try {
      await api.createTeam(newName);
      setNewName("");
      load();
    } catch (ex) {
      setErr(ex instanceof Error ? ex.message : "创建失败");
    }
  };

  const join = async (e: FormEvent) => {
    e.preventDefault();
    setErr("");
    try {
      const r = await api.joinByCode(joinCode);
      setJoinCode("");
      setErr(r.status === "pending" ? "已提交申请，等待老师审批。" : "已加入。");
      load();
    } catch (ex) {
      setErr(ex instanceof Error ? ex.message : "加入失败");
    }
  };

  const manage = (id: number) => {
    setManageId(id);
    setMembers([]);
    setLoadingMembers(true);
    setErr("");
    api
      .listMembers(id)
      .then((r) => setMembers(r.members))
      .catch((e) => setErr(e instanceof Error ? e.message : "加载成员失败"))
      .finally(() => setLoadingMembers(false));
  };

  const approve = async (uid: number) => {
    if (manageId === null) return;
    try {
      await api.approveMember(manageId, uid);
      manage(manageId);
    } catch (ex) {
      setErr(ex instanceof Error ? ex.message : "审批失败");
    }
  };

  const isOwner = (t: Team) => user != null && t.OwnerID === user.ID;

  return (
    <div className="page">
      <h2>团队 / 知识库</h2>
      {err && <div className="err">{err}</div>}

      {user?.Role === "teacher" && (
        <form className="card form" onSubmit={create}>
          <h3>创建学习小组（老师）</h3>
          <input
            placeholder="小组名称"
            value={newName}
            onChange={(e) => setNewName(e.target.value)}
            required
          />
          <button className="primary" type="submit">
            创建并生成班级码
          </button>
        </form>
      )}

      {user?.Role === "student" && (
        <form className="card form" onSubmit={join}>
          <h3>凭班级码加入（学生）</h3>
          <input
            placeholder="输入老师提供的班级码"
            value={joinCode}
            onChange={(e) => setJoinCode(e.target.value)}
            required
          />
          <button className="primary" type="submit">
            申请加入
          </button>
        </form>
      )}

      <div className="grid">
        {loading ? (
          <div className="muted small">正在加载团队…</div>
        ) : teams.length === 0 ? (
          <div className="muted small">还没有团队或知识库。</div>
        ) : (
          teams.map((t) => (
            <div key={t.ID} className="card">
              <div className="title">{t.Name}</div>
              <div className="muted small">
                <em className="tag">{t.Type}</em>
              </div>
              {isOwner(t) && t.JoinCode && (
                <div className="code">
                  班级码：<b>{t.JoinCode}</b>
                </div>
              )}
              {isOwner(t) && (
                <button className="ghost" onClick={() => manage(t.ID)}>
                  管理成员 / 审批
                </button>
              )}
            </div>
          ))
        )}
      </div>

      {manageId !== null && (
        <div className="card">
          <h3>成员审批</h3>
          <ul className="list">
            {loadingMembers ? (
              <li className="muted small">正在加载成员…</li>
            ) : (
              <>
                {members.map((m) => (
                  <li key={m.UserID} className="row">
                    <span>
                      用户 #{m.UserID} · 状态：<b>{m.Status}</b>
                    </span>
                    {m.Status === "pending" && (
                      <button className="primary" onClick={() => approve(m.UserID)}>
                        通过
                      </button>
                    )}
                  </li>
                ))}
                {members.length === 0 && <li className="muted small">暂无成员。</li>}
              </>
            )}
          </ul>
          <button className="ghost" onClick={() => setManageId(null)}>
            关闭
          </button>
        </div>
      )}
    </div>
  );
}

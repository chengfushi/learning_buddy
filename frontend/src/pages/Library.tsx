import { useEffect, useState } from "react";
import { api, type Material, type Team } from "../api";

export default function Library({ onOpenMaterial }: { onOpenMaterial: (id: number) => void }) {
  const [teams, setTeams] = useState<Team[]>([]);
  const [teamId, setTeamId] = useState<number | null>(null);
  const [materials, setMaterials] = useState<Material[]>([]);
  const [canWrite, setCanWrite] = useState(false);
  const [loadingTeams, setLoadingTeams] = useState(true);
  const [loadingMaterials, setLoadingMaterials] = useState(false);
  const [retryingId, setRetryingId] = useState<number | null>(null);
  const [showForm, setShowForm] = useState(false);
  const [title, setTitle] = useState("");
  const [content, setContent] = useState("");
  const [err, setErr] = useState("");

  const loadTeams = () => {
    setLoadingTeams(true);
    setErr("");
    api
      .listTeams()
      .then((r) => {
        setTeams(r.teams);
        if (r.teams.length && teamId === null) setTeamId(r.teams[0].ID);
      })
      .catch((e) => setErr(e instanceof Error ? e.message : "加载失败"))
      .finally(() => setLoadingTeams(false));
  };

  const loadMaterials = () => {
    if (teamId === null) {
      setMaterials([]);
      setCanWrite(false);
      setLoadingMaterials(false);
      return;
    }
    setLoadingMaterials(true);
    setMaterials([]);
    setCanWrite(false);
    setErr("");
    api
      .listTeamMaterials(teamId)
      .then((r) => {
        setMaterials(r.materials);
        setCanWrite(r.can_write);
      })
      .catch((e) => setErr(e instanceof Error ? e.message : "加载失败"))
      .finally(() => setLoadingMaterials(false));
  };

  useEffect(() => {
    loadTeams();
  }, []);
  useEffect(() => {
    loadMaterials();
  }, [teamId]);

  const create = async (e: React.FormEvent) => {
    e.preventDefault();
    setErr("");
    if (teamId === null) return;
    try {
      await api.createMaterial({ team_id: teamId, title, content, file_type: "txt" });
      setTitle("");
      setContent("");
      setShowForm(false);
      loadMaterials();
    } catch (ex) {
      setErr(ex instanceof Error ? ex.message : "创建失败");
    }
  };

  const retryParse = async (e: React.MouseEvent, materialId: number) => {
    e.stopPropagation();
    setErr("");
    setRetryingId(materialId);
    try {
      const result = await api.retryMaterialParse(materialId);
      setMaterials((current) =>
        current.map((material) => (material.ID === materialId ? result.material : material)),
      );
    } catch (ex) {
      setErr(ex instanceof Error ? ex.message : "重试失败");
    } finally {
      setRetryingId(null);
    }
  };

  return (
    <div className="layout">
      <aside className="side">
        <div className="side-title">我的知识库</div>
        {loadingTeams && <div className="muted small">正在加载知识库…</div>}
        {teams.map((t) => (
          <div
            key={t.ID}
            className={t.ID === teamId ? "side-item on" : "side-item"}
            onClick={() => setTeamId(t.ID)}
          >
            <span>{t.Name}</span>
            <em className="tag">{t.Type}</em>
          </div>
        ))}
      </aside>
      <main className="main">
        <div className="toolbar">
          <h2>{teams.find((t) => t.ID === teamId)?.Name ?? "资料"}</h2>
          {canWrite && (
            <button className="primary" onClick={() => setShowForm((v) => !v)}>
              + 新建资料
            </button>
          )}
        </div>
        {err && <div className="err">{err}</div>}
        {showForm && (
          <form className="card form" onSubmit={create}>
            <input
              placeholder="标题"
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              required
            />
            <textarea
              placeholder="正文内容（保存后自动解析入库，可用于 AI 答疑）"
              rows={5}
              value={content}
              onChange={(e) => setContent(e.target.value)}
            />
            <button className="primary" type="submit">
              保存并解析
            </button>
          </form>
        )}
        <ul className="list">
          {loadingTeams || loadingMaterials ? (
            <li className="muted small">正在加载资料…</li>
          ) : (
            <>
              {materials.map((m) => (
                <li key={m.ID} className="card row" onClick={() => onOpenMaterial(m.ID)}>
                  <div>
                    <div className="title">{m.Title}</div>
                    <div className="muted small">
                      {m.Subject || m.Chapter || "无章节"} · 状态：{m.ParseStatus}
                      {m.Shared ? " · 已共享" : ""}
                    </div>
                  </div>
                  <div className="row-actions">
                    {canWrite && m.ParseStatus === "failed" && (
                      <button
                        className="ghost"
                        type="button"
                        disabled={retryingId === m.ID}
                        onClick={(e) => retryParse(e, m.ID)}
                      >
                        {retryingId === m.ID ? "重试中…" : "重试解析"}
                      </button>
                    )}
                    <span className="arrow">›</span>
                  </div>
                </li>
              ))}
              {materials.length === 0 && <li className="muted small">该知识库暂无资料。</li>}
            </>
          )}
        </ul>
      </main>
    </div>
  );
}

import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { api, type Material, type MaterialProcessing, type Team } from "../api";

export default function Library({ onOpenMaterial }: { onOpenMaterial: (id: number) => void }) {
  const [teams, setTeams] = useState<Team[]>([]);
  const [teamId, setTeamId] = useState<number | null>(null);
  const [materials, setMaterials] = useState<Material[]>([]);
  const [processing, setProcessing] = useState<Record<number, MaterialProcessing>>({});
  const [canWrite, setCanWrite] = useState(false);
  const [loadingTeams, setLoadingTeams] = useState(true);
  const [loadingMaterials, setLoadingMaterials] = useState(false);
  const [retryingId, setRetryingId] = useState<number | null>(null);
  const [showForm, setShowForm] = useState(false);
  const [title, setTitle] = useState("");
  const [content, setContent] = useState("");
  const [file, setFile] = useState<File | null>(null);
  const [dragging, setDragging] = useState(false);
  const [saving, setSaving] = useState(false);
  const [err, setErr] = useState("");
  const teamIdRef = useRef<number | null>(teamId);
  const teamsRequestRef = useRef(0);
  const materialsRequestRef = useRef(0);
  const processingRequestRef = useRef(0);
  const createRequestRef = useRef(0);
  const retryRequestRef = useRef(0);
  const reloadTimerRef = useRef<number | undefined>(undefined);

  const loadTeams = () => {
    const teamsRequest = ++teamsRequestRef.current;
    setLoadingTeams(true);
    setErr("");
    api
      .listTeams()
      .then((r) => {
        if (teamsRequest !== teamsRequestRef.current) return;
        setTeams(r.teams);
        if (r.teams.length && teamIdRef.current === null) setTeamId(r.teams[0].ID);
      })
      .catch((e) => {
        if (teamsRequest === teamsRequestRef.current) {
          setErr(e instanceof Error ? e.message : "加载失败");
        }
      })
      .finally(() => {
        if (teamsRequest === teamsRequestRef.current) setLoadingTeams(false);
      });
  };

  const loadMaterials = () => {
    const targetTeamId = teamId;
    const materialsRequest = ++materialsRequestRef.current;
    const isCurrentRequest = () =>
      materialsRequest === materialsRequestRef.current && targetTeamId === teamIdRef.current;
    if (targetTeamId === null) {
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
      .listTeamMaterials(targetTeamId)
      .then((r) => {
        if (!isCurrentRequest()) return;
        setMaterials(r.materials);
        setCanWrite(r.can_write);
      })
      .catch((e) => {
        if (isCurrentRequest()) setErr(e instanceof Error ? e.message : "加载失败");
      })
      .finally(() => {
        if (isCurrentRequest()) setLoadingMaterials(false);
      });
  };

  useEffect(() => {
    loadTeams();
    return () => {
      teamsRequestRef.current += 1;
    };
  }, []);

  useLayoutEffect(() => {
    teamIdRef.current = teamId;
    materialsRequestRef.current += 1;
    processingRequestRef.current += 1;
    createRequestRef.current += 1;
    retryRequestRef.current += 1;
    if (reloadTimerRef.current !== undefined) {
      window.clearTimeout(reloadTimerRef.current);
      reloadTimerRef.current = undefined;
    }
    setMaterials([]);
    setCanWrite(false);
    setLoadingMaterials(teamId !== null);
    setProcessing({});
    setErr("");
    setShowForm(false);
    setTitle("");
    setContent("");
    setFile(null);
    setDragging(false);
    setSaving(false);
    setRetryingId(null);
    return () => {
      materialsRequestRef.current += 1;
      processingRequestRef.current += 1;
      createRequestRef.current += 1;
      retryRequestRef.current += 1;
      if (reloadTimerRef.current !== undefined) {
        window.clearTimeout(reloadTimerRef.current);
        reloadTimerRef.current = undefined;
      }
    };
  }, [teamId]);

  useEffect(() => {
    loadMaterials();
  }, [teamId]);
  useEffect(() => {
    const targetTeamId = teamId;
    const active = materials.filter((material) =>
      ["pending", "parsing"].includes(material.ParseStatus),
    );
    if (targetTeamId === null || active.length === 0) return;
    const refresh = () => {
      const processingRequest = ++processingRequestRef.current;
      const isCurrentRequest = () =>
        processingRequest === processingRequestRef.current && targetTeamId === teamIdRef.current;
      return Promise.all(
        active.map(async (material) => ({
          id: material.ID,
          run: (await api.getMaterialProcessing(material.ID)).processing,
        })),
      )
        .then((results) => {
          if (!isCurrentRequest()) return;
          setProcessing((current) => {
            const next = { ...current };
            for (const result of results) {
              if (result.run) next[result.id] = result.run;
            }
            return next;
          });
          if (results.some((result) => ["done", "failed"].includes(result.run?.Status ?? ""))) {
            if (reloadTimerRef.current !== undefined) {
              window.clearTimeout(reloadTimerRef.current);
            }
            reloadTimerRef.current = window.setTimeout(() => {
              reloadTimerRef.current = undefined;
              if (targetTeamId === teamIdRef.current) loadMaterials();
            }, 300);
          }
        })
        .catch(() => undefined);
    };
    void refresh();
    const timer = window.setInterval(refresh, 2000);
    return () => {
      window.clearInterval(timer);
      processingRequestRef.current += 1;
      if (reloadTimerRef.current !== undefined) {
        window.clearTimeout(reloadTimerRef.current);
        reloadTimerRef.current = undefined;
      }
    };
  }, [materials, teamId]);

  const create = async (e: React.FormEvent) => {
    e.preventDefault();
    if (saving) return;
    setErr("");
    const targetTeamId = teamId;
    if (targetTeamId === null) return;
    const createRequest = ++createRequestRef.current;
    const isCurrentRequest = () =>
      createRequest === createRequestRef.current && targetTeamId === teamIdRef.current;
    setSaving(true);
    try {
      if (file) {
        await api.uploadMaterial({ team_id: targetTeamId, title: title || file.name, file });
      } else {
        await api.createMaterial({ team_id: targetTeamId, title, content, file_type: "txt" });
      }
      if (!isCurrentRequest()) return;
      setTitle("");
      setContent("");
      setFile(null);
      setShowForm(false);
      loadMaterials();
    } catch (ex) {
      if (isCurrentRequest()) setErr(ex instanceof Error ? ex.message : "创建失败");
    } finally {
      if (isCurrentRequest()) setSaving(false);
    }
  };

  const retryParse = async (e: React.MouseEvent, materialId: number) => {
    e.stopPropagation();
    if (retryingId !== null) return;
    const targetTeamId = teamId;
    const retryRequest = ++retryRequestRef.current;
    const isCurrentRequest = () =>
      retryRequest === retryRequestRef.current && targetTeamId === teamIdRef.current;
    setErr("");
    setRetryingId(materialId);
    try {
      const result = await api.retryMaterialParse(materialId);
      if (!isCurrentRequest()) return;
      setMaterials((current) =>
        current.map((material) => (material.ID === materialId ? result.material : material)),
      );
    } catch (ex) {
      if (isCurrentRequest()) setErr(ex instanceof Error ? ex.message : "重试失败");
    } finally {
      if (isCurrentRequest()) setRetryingId(null);
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
              disabled={file !== null}
            />
            <div
              className={dragging ? "upload-zone upload-zone-on" : "upload-zone"}
              onDragOver={(event) => {
                event.preventDefault();
                setDragging(true);
              }}
              onDragLeave={() => setDragging(false)}
              onDrop={(event) => {
                event.preventDefault();
                setDragging(false);
                const next = event.dataTransfer.files[0];
                if (next) {
                  setFile(next);
                  if (!title) setTitle(next.name.replace(/\.[^.]+$/, ""));
                }
              }}
            >
              <input
                aria-label="选择资料文件"
                type="file"
                accept=".txt,.md,.docx,.pdf"
                onChange={(event) => {
                  const next = event.target.files?.[0] ?? null;
                  setFile(next);
                  if (next && !title) setTitle(next.name.replace(/\.[^.]+$/, ""));
                }}
              />
              <span>
                {file ? `已选择：${file.name}` : "拖入 TXT、MD、DOCX 或 PDF（最大 50 MiB）"}
              </span>
            </div>
            <button className="primary" type="submit" disabled={saving}>
              {saving ? "上传解析中…" : "保存并解析"}
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
                      {processing[m.ID] ? ` · 阶段：${processing[m.ID].Stage}` : ""}
                      {m.Shared ? " · 已共享" : ""}
                    </div>
                  </div>
                  <div className="row-actions">
                    {canWrite && m.ParseStatus === "failed" && (
                      <button
                        className="ghost"
                        type="button"
                        disabled={retryingId !== null}
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

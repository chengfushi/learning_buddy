import { useEffect, useState, type FormEvent } from "react";
import { api, type Material, type MaterialNote } from "../api";

export default function Reader({
  materialId,
  onBack,
  onAsk,
}: {
  materialId: number;
  onBack: () => void;
  onAsk: (materialId: number) => void;
}) {
  const [material, setMaterial] = useState<Material | null>(null);
  const [notes, setNotes] = useState<MaterialNote[]>([]);
  const [note, setNote] = useState("");
  const [err, setErr] = useState("");

  const load = () => {
    api
      .getMaterial(materialId)
      .then((r) => setMaterial(r.material))
      .catch((e) => setErr(e instanceof Error ? e.message : "加载失败"));
    api
      .listNotes(materialId)
      .then((r) => setNotes(r.notes))
      .catch(() => undefined);
  };
  useEffect(() => {
    load();
  }, [materialId]);

  const addNote = async (e: FormEvent) => {
    e.preventDefault();
    setErr("");
    try {
      await api.createNote(materialId, note);
      setNote("");
      load();
    } catch (ex) {
      setErr(ex instanceof Error ? ex.message : "笔记保存失败");
    }
  };

  return (
    <div className="reader">
      <div className="toolbar">
        <button className="ghost" onClick={onBack}>
          ‹ 返回
        </button>
        <button className="primary" onClick={() => onAsk(materialId)}>
          用 AI 学伴提问此资料
        </button>
      </div>
      {err && <div className="err">{err}</div>}
      {material && (
        <article className="card">
          <h2>{material.Title}</h2>
          <div className="muted small">
            {material.Subject || material.Chapter || "无章节"} · 状态：{material.ParseStatus}
            {material.Shared ? " · 已共享" : ""}
          </div>
          <div className="prose">
            {material.Content || "（暂无正文，可能是未解析的文件类资料）"}
          </div>
        </article>
      )}
      <section className="card">
        <h3>我的笔记</h3>
        <form className="form" onSubmit={addNote}>
          <textarea
            placeholder="写下你的理解或标注…"
            rows={3}
            value={note}
            onChange={(e) => setNote(e.target.value)}
          />
          <button className="primary" type="submit">
            添加笔记
          </button>
        </form>
        <ul className="notes">
          {notes.map((n) => (
            <li key={n.ID} className="note">
              {n.Content}
            </li>
          ))}
          {notes.length === 0 && <li className="muted small">还没有笔记。</li>}
        </ul>
      </section>
    </div>
  );
}

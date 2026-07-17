import { useEffect, useState, type FormEvent } from "react";
import ReactMarkdown from "react-markdown";
import { api, type Material, type MaterialAsset, type MaterialNote } from "../api";

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
  const [assets, setAssets] = useState<MaterialAsset[]>([]);
  const [sourceURL, setSourceURL] = useState("");
  const [note, setNote] = useState("");
  const [loadingMaterial, setLoadingMaterial] = useState(true);
  const [loadingNotes, setLoadingNotes] = useState(true);
  const [err, setErr] = useState("");
  const [notesErr, setNotesErr] = useState("");

  const load = () => {
    setMaterial(null);
    setNotes([]);
    setAssets([]);
    setSourceURL("");
    setLoadingMaterial(true);
    setLoadingNotes(true);
    setErr("");
    setNotesErr("");
    api
      .getMaterial(materialId)
      .then((r) => setMaterial(r.material))
      .catch((e) => setErr(e instanceof Error ? e.message : "加载失败"))
      .finally(() => setLoadingMaterial(false));
    api
      .listNotes(materialId)
      .then((r) => setNotes(r.notes))
      .catch((e) => setNotesErr(e instanceof Error ? e.message : "笔记加载失败"))
      .finally(() => setLoadingNotes(false));
    api
      .listMaterialAssets(materialId)
      .then((result) => setAssets(result.assets))
      .catch(() => undefined);
    api
      .getMaterialSourceURL(materialId)
      .then((result) => setSourceURL(result.url))
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
      {loadingMaterial ? (
        <div className="muted small">正在加载资料…</div>
      ) : material ? (
        <article className="card">
          <h2>{material.Title}</h2>
          <div className="muted small">
            {material.Subject || material.Chapter || "无章节"} · 状态：{material.ParseStatus}
            {material.Shared ? " · 已共享" : ""}
          </div>
          {sourceURL && (
            <a className="source-download" href={sourceURL}>
              下载原文件
            </a>
          )}
          {material.Summary && <div className="material-summary">{material.Summary}</div>}
          <div className="prose">
            <ReactMarkdown>
              {material.Content || "（暂无正文，可能是未解析的文件类资料）"}
            </ReactMarkdown>
          </div>
          {assets.length > 0 && (
            <section className="asset-gallery" aria-label="资料图片">
              {assets.map((asset) => (
                <figure key={asset.id} id={`asset-${asset.id}`}>
                  <img
                    src={asset.url}
                    alt={asset.caption || `第 ${asset.page_number ?? "?"} 页图片`}
                    loading="lazy"
                  />
                  {(asset.caption || asset.page_number) && (
                    <figcaption>{asset.caption || `第 ${asset.page_number} 页`}</figcaption>
                  )}
                </figure>
              ))}
            </section>
          )}
        </article>
      ) : null}
      <section className="card">
        <h3>我的笔记</h3>
        {notesErr && <div className="err">{notesErr}</div>}
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
          {loadingNotes ? (
            <li className="muted small">正在加载笔记…</li>
          ) : (
            <>
              {notes.map((n) => (
                <li key={n.ID} className="note">
                  {n.Content}
                </li>
              ))}
              {notes.length === 0 && <li className="muted small">还没有笔记。</li>}
            </>
          )}
        </ul>
      </section>
    </div>
  );
}

import { useEffect, useState, type FormEvent } from "react";
import { api, type LearningRecord, type ProgressSummary } from "../api";

export default function Learning() {
  const [summary, setSummary] = useState<ProgressSummary | null>(null);
  const [records, setRecords] = useState<LearningRecord[]>([]);
  const [duration, setDuration] = useState(10);
  const [progress, setProgress] = useState(0);
  const [score, setScore] = useState("");
  const [loadingSummary, setLoadingSummary] = useState(true);
  const [loadingRecords, setLoadingRecords] = useState(true);
  const [err, setErr] = useState("");
  const [recordsErr, setRecordsErr] = useState("");

  const load = () => {
    setSummary(null);
    setRecords([]);
    setLoadingSummary(true);
    setLoadingRecords(true);
    setErr("");
    setRecordsErr("");
    api
      .getProgress()
      .then((r) => setSummary(r.summary))
      .catch((e) => setErr(e instanceof Error ? e.message : "加载失败"))
      .finally(() => setLoadingSummary(false));
    api
      .listLearningRecords()
      .then((r) => setRecords(r.records))
      .catch((e) => setRecordsErr(e instanceof Error ? e.message : "学习记录加载失败"))
      .finally(() => setLoadingRecords(false));
  };
  useEffect(() => {
    load();
  }, []);

  const record = async (e: FormEvent) => {
    e.preventDefault();
    setErr("");
    try {
      await api.createLearningRecord({
        duration_s: duration * 60,
        progress,
        score: score === "" ? undefined : Number(score),
      });
      setProgress(0);
      setScore("");
      load();
    } catch (ex) {
      setErr(ex instanceof Error ? ex.message : "记录失败");
    }
  };

  return (
    <div className="page">
      <h2>学习中心</h2>
      {err && <div className="err">{err}</div>}

      {loadingSummary ? (
        <div className="muted small">正在加载学习统计…</div>
      ) : summary ? (
        <div className="stats">
          <div className="stat">
            <div className="num">{Math.round(summary.total_duration_s / 60)}</div>
            <div className="muted small">学习分钟</div>
          </div>
          <div className="stat">
            <div className="num">{Math.round(summary.avg_progress)}%</div>
            <div className="muted small">平均完成度</div>
          </div>
          <div className="stat">
            <div className="num">{summary.quiz_count}</div>
            <div className="muted small">测评次数</div>
          </div>
          <div className="stat">
            <div className="num">{Math.round(summary.quiz_accuracy)}%</div>
            <div className="muted small">测评正确率</div>
          </div>
        </div>
      ) : null}

      <form className="card form" onSubmit={record}>
        <h3>记录本次学习</h3>
        <label>
          时长（分钟）
          <input
            type="number"
            min={0}
            value={duration}
            onChange={(e) => setDuration(Number(e.target.value))}
          />
        </label>
        <label>
          完成度（0-100）
          <input
            type="number"
            min={0}
            max={100}
            value={progress}
            onChange={(e) => setProgress(Number(e.target.value))}
          />
        </label>
        <label>
          得分（可选）
          <input
            type="number"
            min={0}
            max={100}
            value={score}
            onChange={(e) => setScore(e.target.value)}
          />
        </label>
        <button className="primary" type="submit">
          保存记录
        </button>
      </form>

      <div className="card">
        <h3>近期记录</h3>
        {recordsErr && <div className="err">{recordsErr}</div>}
        <ul className="list">
          {loadingRecords ? (
            <li className="muted small">正在加载学习记录…</li>
          ) : (
            <>
              {records.map((r) => (
                <li key={r.ID} className="row">
                  <span>
                    {r.CreatedAt.slice(0, 10)} · {Math.round(r.DurationS / 60)} 分钟 · 完成度{" "}
                    {Math.round(r.Progress)}%
                    {r.Score != null ? ` · 得分 ${Math.round(r.Score)}` : ""}
                  </span>
                </li>
              ))}
              {records.length === 0 && (
                <li className="muted small">还没有学习记录，去资料里学一会吧。</li>
              )}
            </>
          )}
        </ul>
      </div>
    </div>
  );
}

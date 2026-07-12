import { useState } from "react";
import { api, type Citation, type Exercise, type StudyPlan } from "../api";

interface Msg {
  role: string;
  content: string;
  citations?: Citation[];
}

export default function Companion({ materialId }: { materialId?: number }) {
  const [tab, setTab] = useState<"chat" | "plan" | "quiz">("chat");
  const [messages, setMessages] = useState<Msg[]>([]);
  const [input, setInput] = useState("");
  const [busy, setBusy] = useState(false);

  const [goal, setGoal] = useState("");
  const [deadline, setDeadline] = useState("");
  const [plan, setPlan] = useState<StudyPlan | null>(null);

  const [topic, setTopic] = useState("");
  const [count, setCount] = useState(3);
  const [exercises, setExercises] = useState<Exercise[]>([]);
  const [results, setResults] = useState<Record<number, boolean>>({});

  const ask = async () => {
    const q = input.trim();
    if (!q || busy) return;
    setInput("");
    setBusy(true);
    setMessages((m) => [...m, { role: "user", content: q }, { role: "assistant", content: "" }]);
    let acc = "";
    await api.chatStream(
      { question: q, material_id: materialId },
      {
        onToken: (t) => {
          acc += t;
          setMessages((m) => {
            const copy = [...m];
            copy[copy.length - 1] = { role: "assistant", content: acc };
            return copy;
          });
        },
        onCitations: (c) =>
          setMessages((m) => {
            const copy = [...m];
            copy[copy.length - 1] = { ...copy[copy.length - 1], citations: c };
            return copy;
          }),
        onEnd: () => setBusy(false),
        onError: (msg) => {
          setMessages((m) => {
            const copy = [...m];
            copy[copy.length - 1] = { role: "assistant", content: `出错了：${msg}` };
            return copy;
          });
          setBusy(false);
        },
      },
    );
  };

  const genPlan = async () => {
    if (!goal.trim()) return;
    const r = await api.createPlan(goal, deadline || undefined);
    setPlan(r.plan);
  };

  const genQuiz = async () => {
    if (!topic.trim()) return;
    const r = await api.createQuiz(topic, count, materialId);
    setExercises(r.exercises);
    setResults({});
  };

  const answer = async (ex: Exercise, choice: string) => {
    const r = await api.answerQuiz(ex.ID, choice);
    setResults((prev) => ({ ...prev, [ex.ID]: r.is_correct }));
  };

  return (
    <div className="page">
      <div className="tabs">
        <button className={tab === "chat" ? "on" : ""} onClick={() => setTab("chat")}>
          AI 答疑
        </button>
        <button className={tab === "plan" ? "on" : ""} onClick={() => setTab("plan")}>
          学习计划
        </button>
        <button className={tab === "quiz" ? "on" : ""} onClick={() => setTab("quiz")}>
          智能测评
        </button>
      </div>

      {tab === "chat" && (
        <div className="chat">
          <div className="messages">
            {messages.map((m, i) => (
              <div key={i} className={m.role === "user" ? "bubble user" : "bubble"}>
                <div className="bubble-text">{m.content || "…"}</div>
                {m.citations && m.citations.length > 0 && (
                  <div className="citations">
                    引用：
                    {m.citations.map((c, j) => (
                      <span key={j} className="cite">
                        资料#{c.material_id}
                        {c.chapter ? `·${c.chapter}` : ""}
                      </span>
                    ))}
                  </div>
                )}
              </div>
            ))}
          </div>
          <div className="composer">
            <input
              placeholder={materialId ? "针对当前资料提问…" : "向学伴提问…"}
              value={input}
              onChange={(e) => setInput(e.target.value)}
              onKeyDown={(e) => e.key === "Enter" && ask()}
              disabled={busy}
            />
            <button className="primary" onClick={ask} disabled={busy}>
              {busy ? "思考中…" : "发送"}
            </button>
          </div>
        </div>
      )}

      {tab === "plan" && (
        <div className="page-soft">
          <div className="card form">
            <input
              placeholder="学习目标，例如：掌握人工智能基础"
              value={goal}
              onChange={(e) => setGoal(e.target.value)}
            />
            <input type="date" value={deadline} onChange={(e) => setDeadline(e.target.value)} />
            <button className="primary" onClick={genPlan}>
              生成学习计划
            </button>
          </div>
          {plan && (
            <div className="card">
              <h3>{plan.Title}</h3>
              <ol>
                {plan.Items?.map((it, i) => (
                  <li key={i}>
                    <b>{it.date}</b>：{it.task}
                  </li>
                ))}
              </ol>
            </div>
          )}
        </div>
      )}

      {tab === "quiz" && (
        <div className="page-soft">
          <div className="card form">
            <input
              placeholder="测评主题，例如：机器学习"
              value={topic}
              onChange={(e) => setTopic(e.target.value)}
            />
            <input
              type="number"
              min={1}
              max={10}
              value={count}
              onChange={(e) => setCount(Number(e.target.value))}
            />
            <button className="primary" onClick={genQuiz}>
              生成测评
            </button>
          </div>
          {exercises.map((ex) => (
            <div key={ex.ID} className="card">
              <div className="title">{ex.Question}</div>
              <div className="options">
                {(ex.Options ?? []).map((opt, i) => (
                  <button
                    key={i}
                    className={results[ex.ID] !== undefined ? "opt done" : "opt"}
                    onClick={() => answer(ex, opt)}
                    disabled={results[ex.ID] !== undefined}
                  >
                    {opt}
                  </button>
                ))}
              </div>
              {results[ex.ID] !== undefined && (
                <div className={results[ex.ID] ? "ok" : "bad"}>
                  {results[ex.ID] ? "✓ 回答正确" : "✗ 回答错误"}
                </div>
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

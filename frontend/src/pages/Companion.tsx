import { useEffect, useState } from "react";
import { api, type Citation, type Exercise, type StudyPlan } from "../api";
import type { MaterialNavigationTarget } from "../material-navigation";

interface Msg {
  role: "user" | "assistant";
  content: string;
  citations?: Citation[];
  messageId?: number;
  feedback?: "up" | "down";
  stageMs?: Record<string, number>;
  degradedStages?: string[];
}

const stageLabels: Record<string, string> = {
  analyze_query: "问题理解",
  query_analysis: "问题理解",
  embedding: "语义检索",
  retrieve: "知识库召回",
  rerank: "候选精排",
  expand: "上下文扩展",
};

function stageLabel(stage: string): string {
  return stageLabels[stage] ?? stage.replace(/_/g, " ");
}

function validStageTimings(stageMs?: Record<string, number>): [string, number][] {
  return Object.entries(stageMs ?? {}).filter(
    (entry) => Number.isFinite(entry[1]) && entry[1] >= 0,
  );
}

function sessionStorageKey(materialId?: number): string {
  return materialId === undefined
    ? "lb_chat_session:global"
    : `lb_chat_session:material:${materialId}`;
}

function storedSession(materialId?: number): string | undefined {
  const scoped = globalThis.localStorage?.getItem(sessionStorageKey(materialId));
  if (scoped) return scoped;
  // 只为旧版全局会话提供一次兼容读取，绝不把它带入资料作用域。
  return materialId === undefined
    ? (globalThis.localStorage?.getItem("lb_chat_session") ?? undefined)
    : undefined;
}

export default function Companion({
  materialId,
  onOpenMaterial,
}: {
  materialId?: number;
  onOpenMaterial?: (target: MaterialNavigationTarget) => void;
}) {
  const [tab, setTab] = useState<"chat" | "plan" | "quiz">("chat");
  const [messages, setMessages] = useState<Msg[]>([]);
  const [sessionId, setSessionId] = useState<string | undefined>(() => storedSession(materialId));
  const [rewrittenQuery, setRewrittenQuery] = useState("");
  const [input, setInput] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");

  const [goal, setGoal] = useState("");
  const [deadline, setDeadline] = useState("");
  const [plan, setPlan] = useState<StudyPlan | null>(null);
  const [planBusy, setPlanBusy] = useState(false);

  const [topic, setTopic] = useState("");
  const [count, setCount] = useState(3);
  const [exercises, setExercises] = useState<Exercise[]>([]);
  const [results, setResults] = useState<Record<number, boolean>>({});
  const [quizBusy, setQuizBusy] = useState(false);
  const [answeringId, setAnsweringId] = useState<number | null>(null);

  useEffect(() => {
    setSessionId(storedSession(materialId));
    setMessages([]);
    setRewrittenQuery("");
  }, [materialId]);

  const updateLastAssistant = (update: Partial<Msg>) =>
    setMessages((current) => {
      const copy = [...current];
      copy[copy.length - 1] = { ...copy[copy.length - 1], ...update };
      return copy;
    });

  const ask = async () => {
    const q = input.trim();
    if (!q || busy) return;
    setInput("");
    setBusy(true);
    setErr("");
    setRewrittenQuery("");
    setMessages((m) => [...m, { role: "user", content: q }, { role: "assistant", content: "" }]);
    let acc = "";
    try {
      await api.chatStream(
        { question: q, material_id: materialId, session_id: sessionId },
        {
          onToken: (text) => {
            acc += text;
            updateLastAssistant({ content: acc });
          },
          onCitations: (citations) => updateLastAssistant({ citations }),
          onMeta: (meta) => {
            setSessionId(meta.session_id);
            globalThis.localStorage?.setItem(sessionStorageKey(materialId), meta.session_id);
            if (meta.rewrite_applied) setRewrittenQuery(meta.rewritten_query);
          },
          onDone: (done) => {
            setSessionId(done.session_id);
            globalThis.localStorage?.setItem(sessionStorageKey(materialId), done.session_id);
            updateLastAssistant({
              citations: done.citations,
              messageId: done.message_id,
              stageMs: done.stage_ms,
              degradedStages: done.degraded_stages,
            });
          },
          onEnd: () => setBusy(false),
          onError: (message) => {
            updateLastAssistant({ content: `出错了：${message}` });
            setBusy(false);
          },
        },
      );
    } catch (ex) {
      updateLastAssistant({
        content: `出错了：${ex instanceof Error ? ex.message : "对话请求失败"}`,
      });
    } finally {
      setBusy(false);
    }
  };

  const feedback = async (index: number, rating: "up" | "down") => {
    const message = messages[index];
    if (!message.messageId) return;
    const reason =
      rating === "down" ? window.prompt("哪里没有帮到你？（选填，最多 500 字）") : undefined;
    if (reason !== null && reason !== undefined && reason.length > 500) {
      setErr("反馈原因不能超过 500 字");
      return;
    }
    try {
      await api.submitFeedback(message.messageId, rating, reason ?? undefined);
      setMessages((current) =>
        current.map((item, i) => (i === index ? { ...item, feedback: rating } : item)),
      );
    } catch (ex) {
      setErr(ex instanceof Error ? ex.message : "反馈提交失败");
    }
  };

  const genPlan = async () => {
    if (!goal.trim() || planBusy) return;
    setErr("");
    setPlanBusy(true);
    try {
      setPlan((await api.createPlan(goal, deadline || undefined)).plan);
    } catch (ex) {
      setErr(ex instanceof Error ? ex.message : "计划生成失败");
    } finally {
      setPlanBusy(false);
    }
  };

  const genQuiz = async () => {
    if (!topic.trim() || quizBusy) return;
    setErr("");
    setQuizBusy(true);
    try {
      setExercises((await api.createQuiz(topic, count, materialId)).exercises);
      setResults({});
    } catch (ex) {
      setErr(ex instanceof Error ? ex.message : "测评生成失败");
    } finally {
      setQuizBusy(false);
    }
  };

  const answer = async (exercise: Exercise, choice: string) => {
    if (answeringId !== null || results[exercise.ID] !== undefined) return;
    setErr("");
    setAnsweringId(exercise.ID);
    try {
      const result = await api.answerQuiz(exercise.ID, choice);
      setResults((prev) => ({
        ...prev,
        [exercise.ID]: result.is_correct,
      }));
    } catch (error) {
      setErr(error instanceof Error ? error.message : "答案提交失败");
    } finally {
      setAnsweringId(null);
    }
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
      {err && <div className="err">{err}</div>}

      {tab === "chat" && (
        <div className="chat">
          {rewrittenQuery && <div className="query-rewrite">已理解为：{rewrittenQuery}</div>}
          <div className="messages">
            {messages.map((message, index) => (
              <div key={index} className={message.role === "user" ? "bubble user" : "bubble"}>
                <div className="bubble-text">{message.content || "…"}</div>
                {!!message.citations?.length && (
                  <div className="citations">
                    引用：
                    {message.citations.map((citation, citationIndex) => (
                      <button
                        key={citation.chunk_id ?? citationIndex}
                        className="cite"
                        onClick={() =>
                          onOpenMaterial?.({
                            materialId: citation.material_id,
                            pageNumber: citation.page_number,
                            assetId: citation.asset_id,
                          })
                        }
                      >
                        {citation.title || `资料#${citation.material_id}`}
                        {citation.page_number
                          ? `·第 ${citation.page_number} 页`
                          : citation.chapter
                            ? `·${citation.chapter}`
                            : ""}
                      </button>
                    ))}
                  </div>
                )}
                {!!message.degradedStages?.length && (
                  <div className="rag-degraded" role="status">
                    本次检索使用了降级路径：
                    {[...new Set(message.degradedStages)].map(stageLabel).join("、")}
                  </div>
                )}
                {validStageTimings(message.stageMs).length > 0 && (
                  <details className="rag-timings">
                    <summary>检索耗时</summary>
                    <dl>
                      {validStageTimings(message.stageMs).map(([stage, duration]) => (
                        <div key={stage}>
                          <dt>{stageLabel(stage)}</dt>
                          <dd>{duration} ms</dd>
                        </div>
                      ))}
                    </dl>
                  </details>
                )}
                {message.role === "assistant" && message.messageId && (
                  <div className="feedback-controls">
                    <span>这个回答有帮助吗？</span>
                    <button
                      aria-label="点赞"
                      className={message.feedback === "up" ? "selected" : ""}
                      onClick={() => feedback(index, "up")}
                    >
                      👍
                    </button>
                    <button
                      aria-label="点踩"
                      className={message.feedback === "down" ? "selected" : ""}
                      onClick={() => feedback(index, "down")}
                    >
                      👎
                    </button>
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
            <button className="primary" onClick={genPlan} disabled={planBusy}>
              {planBusy ? "生成中…" : "生成学习计划"}
            </button>
          </div>
          {plan && (
            <div className="card">
              <h3>{plan.Title}</h3>
              <ol>
                {plan.Items?.map((item, i) => (
                  <li key={i}>
                    <b>{item.date}</b>：{item.task}
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
            <button className="primary" onClick={genQuiz} disabled={quizBusy}>
              {quizBusy ? "生成中…" : "生成测评"}
            </button>
          </div>
          {exercises.map((exercise) => (
            <div key={exercise.ID} className="card">
              <div className="title">{exercise.Question}</div>
              <div className="options">
                {(exercise.Options ?? []).map((option, i) => (
                  <button
                    key={i}
                    className={results[exercise.ID] !== undefined ? "opt done" : "opt"}
                    onClick={() => answer(exercise, String.fromCharCode("A".charCodeAt(0) + i))}
                    disabled={answeringId !== null || results[exercise.ID] !== undefined}
                  >
                    {option}
                  </button>
                ))}
              </div>
              {results[exercise.ID] !== undefined && (
                <div className={results[exercise.ID] ? "ok" : "bad"}>
                  {results[exercise.ID] ? "✓ 回答正确" : "✗ 回答错误"}
                </div>
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

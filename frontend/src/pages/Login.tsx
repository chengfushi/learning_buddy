import { useState, type FormEvent } from "react";
import { useAuth } from "../auth";

export default function Login() {
  const { login, register } = useAuth();
  const [mode, setMode] = useState<"login" | "register">("login");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [name, setName] = useState("");
  const [role, setRole] = useState("student");
  const [err, setErr] = useState("");

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setErr("");
    try {
      if (mode === "login") await login(email, password);
      else await register(email, password, name, role);
    } catch (ex) {
      setErr(ex instanceof Error ? ex.message : "操作失败");
    }
  };

  return (
    <div className="auth-wrap">
      <div className="auth-card">
        <h1 className="brand">智能学伴</h1>
        <p className="muted">自主学习 · AI 辅助学习</p>
        <div className="seg">
          <button className={mode === "login" ? "seg-on" : ""} onClick={() => setMode("login")}>
            登录
          </button>
          <button
            className={mode === "register" ? "seg-on" : ""}
            onClick={() => setMode("register")}
          >
            注册
          </button>
        </div>
        <form onSubmit={submit}>
          {mode === "register" && (
            <>
              <input placeholder="昵称" value={name} onChange={(e) => setName(e.target.value)} />
              <select value={role} onChange={(e) => setRole(e.target.value)}>
                <option value="student">学生</option>
                <option value="teacher">老师</option>
              </select>
            </>
          )}
          <input
            placeholder="邮箱"
            type="email"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            required
          />
          <input
            placeholder="密码"
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            required
          />
          {err && <div className="err">{err}</div>}
          <button className="primary" type="submit">
            {mode === "login" ? "登录" : "注册并登录"}
          </button>
        </form>
        <p className="muted small">
          演示账号：teacher@local.dev / Teacher@123（老师）· student@local.dev / Student@123（学生）
        </p>
      </div>
    </div>
  );
}

import { useState } from "react";
import { useAuth } from "./auth";
import Login from "./pages/Login";
import Library from "./pages/Library";
import Reader from "./pages/Reader";
import Teams from "./pages/Teams";
import Companion from "./pages/Companion";
import Learning from "./pages/Learning";
import type { MaterialNavigationTarget } from "./material-navigation";

type View = "library" | "teams" | "companion" | "learning";

export default function App() {
  const { user, ready, logout } = useAuth();
  const [view, setView] = useState<View>("library");
  const [openMaterial, setOpenMaterial] = useState<MaterialNavigationTarget | null>(null);
  const [companionMat, setCompanionMat] = useState<number | undefined>(undefined);

  if (!ready) return <div className="loading">加载中…</div>;
  if (!user) return <Login />;

  return (
    <div className="app">
      <header className="topbar">
        <div className="logo">智能学伴</div>
        <nav>
          <button className={view === "library" ? "nav-on" : ""} onClick={() => setView("library")}>
            知识库
          </button>
          <button className={view === "teams" ? "nav-on" : ""} onClick={() => setView("teams")}>
            团队
          </button>
          <button
            className={view === "companion" ? "nav-on" : ""}
            onClick={() => setView("companion")}
          >
            AI 学伴
          </button>
          <button
            className={view === "learning" ? "nav-on" : ""}
            onClick={() => setView("learning")}
          >
            学习中心
          </button>
        </nav>
        <div className="user">
          <span>
            {user.DisplayName}（{user.Role}）
          </span>
          <button className="ghost" onClick={logout}>
            退出
          </button>
        </div>
      </header>
      <div className="content">
        {openMaterial !== null ? (
          <Reader
            materialId={openMaterial.materialId}
            focus={{ pageNumber: openMaterial.pageNumber, assetId: openMaterial.assetId }}
            onBack={() => setOpenMaterial(null)}
            onAsk={(id) => {
              setCompanionMat(id);
              setOpenMaterial(null);
              setView("companion");
            }}
          />
        ) : (
          <>
            {view === "library" && (
              <Library onOpenMaterial={(id) => setOpenMaterial({ materialId: id })} />
            )}
            {view === "teams" && <Teams />}
            {view === "companion" && (
              <Companion materialId={companionMat} onOpenMaterial={setOpenMaterial} />
            )}
            {view === "learning" && <Learning />}
          </>
        )}
      </div>
    </div>
  );
}

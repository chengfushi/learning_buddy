import { useQuery } from "react-query";
import { z } from "zod";

// 后端响应校验（约定：后端响应用 zod 校验）
const MaterialSchema = z.object({
  ID: z.number(),
  Title: z.string(),
});
const MaterialsSchema = z.object({ items: z.array(MaterialSchema) });

export function App() {
  const { data } = useQuery(["materials"], async () => {
    const r = await fetch("/api/materials");
    return MaterialsSchema.parse(await r.json());
  });

  // AI 辅助学习（SSE 流式）的入口见 docs/engineering-standards.md R4：
  // 用 fetch + ReadableStream 携带 Authorization，不要用 EventSource 把 token 放进 URL。
  return (
    <main style={{ padding: 24 }}>
      <h1>智能学伴</h1>
      <p>资料数：{data?.items.length ?? 0}</p>
    </main>
  );
}

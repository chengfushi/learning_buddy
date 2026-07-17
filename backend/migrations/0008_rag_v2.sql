-- 0008: RAG v2 影子索引、语义元数据、资产、检索追踪与反馈闭环。
-- 全部为兼容性加法：legacy-v1 继续为 active，新解析写入 rag-v2，评测后再切换。

CREATE EXTENSION IF NOT EXISTS pg_trgm;

ALTER TABLE materials
    ADD COLUMN IF NOT EXISTS summary TEXT,
    ADD COLUMN IF NOT EXISTS semantic_keywords TEXT[] NOT NULL DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS suggested_questions TEXT[] NOT NULL DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS normalized_storage_key VARCHAR(512),
    ADD COLUMN IF NOT EXISTS parser_version VARCHAR(40) NOT NULL DEFAULT 'legacy',
    ADD COLUMN IF NOT EXISTS index_version VARCHAR(40) NOT NULL DEFAULT 'legacy-v1',
    ADD COLUMN IF NOT EXISTS cleaning_stats JSONB NOT NULL DEFAULT '{}'::jsonb;

ALTER TABLE material_chunks
    ADD COLUMN IF NOT EXISTS index_version VARCHAR(40) NOT NULL DEFAULT 'legacy-v1',
    ADD COLUMN IF NOT EXISTS kind VARCHAR(20) NOT NULL DEFAULT 'body',
    ADD COLUMN IF NOT EXISTS heading_path TEXT,
    ADD COLUMN IF NOT EXISTS page_number INT,
    ADD COLUMN IF NOT EXISTS token_count INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS lexical_text TEXT NOT NULL DEFAULT '';

ALTER TABLE material_chunks
    ADD COLUMN IF NOT EXISTS lexical_tsv TSVECTOR
    GENERATED ALWAYS AS (to_tsvector('simple', COALESCE(lexical_text, ''))) STORED;

UPDATE material_chunks
   SET index_version = 'legacy-v1', kind = 'body'
 WHERE index_version IS NULL OR kind IS NULL;

DROP INDEX IF EXISTS uq_material_chunks_material_idx;
CREATE UNIQUE INDEX IF NOT EXISTS uq_material_chunks_version_kind_idx
    ON material_chunks(material_id, index_version, kind, chunk_idx);
CREATE INDEX IF NOT EXISTS idx_chunk_material_version
    ON material_chunks(material_id, index_version, kind, chunk_idx);
CREATE INDEX IF NOT EXISTS idx_chunk_lexical_trgm
    ON material_chunks USING gin (lexical_text gin_trgm_ops);
CREATE INDEX IF NOT EXISTS idx_chunk_lexical_tsv
    ON material_chunks USING gin (lexical_tsv);
DROP INDEX IF EXISTS idx_chunk_vec;
CREATE INDEX IF NOT EXISTS idx_chunk_vec_hnsw
    ON material_chunks USING hnsw (embedding vector_cosine_ops)
    WITH (m = 16, ef_construction = 128);

CREATE TABLE IF NOT EXISTS material_assets (
    id BIGSERIAL PRIMARY KEY,
    material_id BIGINT NOT NULL REFERENCES materials(id) ON DELETE CASCADE,
    parse_generation BIGINT NOT NULL,
    index_version VARCHAR(40) NOT NULL DEFAULT 'rag-v2',
    storage_key VARCHAR(512) NOT NULL,
    sha256 CHAR(64) NOT NULL,
    mime_type VARCHAR(100) NOT NULL,
    page_number INT,
    chunk_idx INT,
    ocr_text TEXT,
    caption TEXT,
    width INT,
    height INT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(material_id, parse_generation, sha256)
);
CREATE INDEX IF NOT EXISTS idx_material_assets_material
    ON material_assets(material_id, index_version, page_number);

ALTER TABLE material_chunks
    ADD COLUMN IF NOT EXISTS asset_id BIGINT REFERENCES material_assets(id) ON DELETE SET NULL;

CREATE TABLE IF NOT EXISTS rag_index_versions (
    version VARCHAR(40) PRIMARY KEY,
    status VARCHAR(20) NOT NULL CHECK (status IN ('building', 'active', 'retired')),
    embedding_model VARCHAR(120) NOT NULL,
    embedding_dim INT NOT NULL,
    parser_version VARCHAR(40) NOT NULL,
    chunk_config JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    activated_at TIMESTAMPTZ
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_rag_index_active
    ON rag_index_versions(status) WHERE status = 'active';
INSERT INTO rag_index_versions
    (version, status, embedding_model, embedding_dim, parser_version, chunk_config, activated_at)
VALUES
    ('legacy-v1', 'active', 'text-embedding-v4', 1024, 'legacy', '{}'::jsonb, now()),
    ('rag-v2', 'building', 'text-embedding-v4', 1024, 'rag-v2',
     '{"short_document_chars":5000,"max_chunk_tokens":3000,"overlap_tokens":300}'::jsonb,
     NULL)
ON CONFLICT (version) DO NOTHING;

CREATE TABLE IF NOT EXISTS rag_processing_runs (
    id BIGSERIAL PRIMARY KEY,
    material_id BIGINT NOT NULL REFERENCES materials(id) ON DELETE CASCADE,
    parse_generation BIGINT NOT NULL,
    index_version VARCHAR(40) NOT NULL,
    stage VARCHAR(30) NOT NULL,
    status VARCHAR(20) NOT NULL CHECK (status IN ('running', 'done', 'failed', 'stale')),
    parser_version VARCHAR(40) NOT NULL,
    cleaning_rules_version VARCHAR(40) NOT NULL,
    progress JSONB NOT NULL DEFAULT '{}'::jsonb,
    error TEXT,
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at TIMESTAMPTZ,
    UNIQUE(material_id, parse_generation, index_version)
);

CREATE TABLE IF NOT EXISTS rag_runs (
    id UUID PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    session_id UUID REFERENCES agent_sessions(id) ON DELETE SET NULL,
    message_id BIGINT REFERENCES agent_messages(id) ON DELETE SET NULL,
    trace_id VARCHAR(80) NOT NULL,
    original_query TEXT NOT NULL,
    rewritten_query TEXT NOT NULL,
    rewrite_applied BOOLEAN NOT NULL DEFAULT false,
    index_version VARCHAR(40) NOT NULL,
    stage_durations JSONB NOT NULL DEFAULT '{}'::jsonb,
    degraded_stages TEXT[] NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_rag_runs_user_created
    ON rag_runs(user_id, created_at DESC);

CREATE TABLE IF NOT EXISTS rag_run_hits (
    id BIGSERIAL PRIMARY KEY,
    run_id UUID NOT NULL REFERENCES rag_runs(id) ON DELETE CASCADE,
    chunk_id BIGINT REFERENCES material_chunks(id) ON DELETE SET NULL,
    material_id BIGINT NOT NULL REFERENCES materials(id) ON DELETE CASCADE,
    rank INT NOT NULL,
    vector_score DOUBLE PRECISION,
    lexical_score DOUBLE PRECISION,
    rrf_score DOUBLE PRECISION NOT NULL DEFAULT 0,
    rerank_score DOUBLE PRECISION,
    selected BOOLEAN NOT NULL DEFAULT false
);
CREATE INDEX IF NOT EXISTS idx_rag_run_hits_run ON rag_run_hits(run_id, rank);

CREATE TABLE IF NOT EXISTS message_feedback (
    id BIGSERIAL PRIMARY KEY,
    message_id BIGINT NOT NULL REFERENCES agent_messages(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    rating VARCHAR(10) NOT NULL CHECK (rating IN ('up', 'down')),
    reason VARCHAR(500),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(message_id, user_id)
);
CREATE INDEX IF NOT EXISTS idx_message_feedback_created
    ON message_feedback(created_at DESC);

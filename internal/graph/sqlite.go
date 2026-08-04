package graph

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"GoMental/internal/domain"

	_ "modernc.org/sqlite"
)

const metaTimeFormat = "2006-01-02T15:04:05Z07:00"

// NoteMeta is the listable metadata projected into the notes table so ListNotes
// can serve sorted/filtered/paginated pages without walking the filesystem.
type NoteMeta struct {
	ID         domain.NoteID
	Title      string
	Path       string
	Type       string
	ModifiedAt time.Time
	Tags       []domain.Tag
	Favorite   bool
}

type SQLiteStore struct {
	db *sql.DB
}

type LinkProjection struct {
	Source   domain.NoteID
	Hard     []domain.NoteLink
	Soft     []domain.InferredNoteLink
	Metadata []MetadataMembership
	// Meta carries the listable note metadata (title/path/modified/tags). When set
	// (Meta.ID != ""), ReplaceAllLinks upserts it into the notes/note_tags tables.
	Meta NoteMeta
}

func OpenSQLiteStore(path string) (*SQLiteStore, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	// WAL enables concurrent readers alongside a single writer, and busy_timeout
	// makes writers from a second connection (e.g. the background soft-link
	// rebuilder) wait for the lock instead of failing with SQLITE_BUSY. This is
	// required now that multiple in-process writers (save, delete, background
	// inference) and concurrent HTTP readers share the one workspace graph DB
	// (Guardrail G3). foreign_keys is per-connection, so it lives in the DSN to
	// apply across the whole pool.
	dsn := path + "?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	store := &SQLiteStore{db: db}
	if err := store.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *SQLiteStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func GraphPath(workspaceRoot string) string {
	return filepath.Join(workspaceRoot, ".workspace", "graph", "graph.sqlite")
}

func (s *SQLiteStore) migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `
PRAGMA foreign_keys = ON;
CREATE TABLE IF NOT EXISTS notes (
  id TEXT PRIMARY KEY,
  title TEXT NOT NULL DEFAULT '',
  path TEXT NOT NULL DEFAULT '',
  type TEXT NOT NULL DEFAULT '',
  favorite INTEGER NOT NULL DEFAULT 0,
  modified_at TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS note_tags (
  note_id TEXT NOT NULL,
  tag TEXT NOT NULL,
  PRIMARY KEY (note_id, tag),
  FOREIGN KEY (note_id) REFERENCES notes(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS links (
  source TEXT NOT NULL,
  target TEXT NOT NULL,
  resolved_id TEXT,
  display_text TEXT NOT NULL DEFAULT '',
  heading TEXT NOT NULL DEFAULT '',
  strength TEXT NOT NULL,
  score REAL NOT NULL DEFAULT 0,
  evidence TEXT NOT NULL DEFAULT '',
  algorithm TEXT NOT NULL DEFAULT '',
  computed_at TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (source, target, strength, heading)
);
CREATE INDEX IF NOT EXISTS idx_links_resolved ON links(resolved_id);
CREATE INDEX IF NOT EXISTS idx_links_source ON links(source);
CREATE INDEX IF NOT EXISTS idx_links_strength ON links(strength);
CREATE INDEX IF NOT EXISTS idx_note_tags_tag ON note_tags(tag);
CREATE INDEX IF NOT EXISTS idx_notes_title ON notes(title);
CREATE INDEX IF NOT EXISTS idx_notes_modified ON notes(modified_at);
`); err != nil {
		return err
	}
	// In-place upgrade of pre-existing databases whose notes table only had `id`.
	// The metadata columns default to '' until the next projection write/rebuild
	// repopulates them; ListNotes falls back to the filesystem while they are empty.
	if err := s.addMissingNoteColumns(ctx); err != nil {
		return err
	}
	// Created after addMissingNoteColumns so the `type` column is guaranteed to exist
	// on a just-upgraded pre-existing DB before the index references it.
	_, err := s.db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_notes_type ON notes(type)`)
	return err
}

func (s *SQLiteStore) addMissingNoteColumns(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(notes)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	present := map[string]struct{}{}
	for rows.Next() {
		var cid int
		var name, colType string
		var notNull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dflt, &pk); err != nil {
			return err
		}
		present[name] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, col := range []string{"title", "path", "type", "favorite", "modified_at"} {
		if _, ok := present[col]; ok {
			continue
		}
		definition := ` TEXT NOT NULL DEFAULT ''`
		if col == "favorite" {
			definition = ` INTEGER NOT NULL DEFAULT 0`
		}
		if _, err := s.db.ExecContext(ctx, `ALTER TABLE notes ADD COLUMN `+col+definition); err != nil {
			return err
		}
	}
	return nil
}

// UpsertNoteMeta writes the listable metadata for a single note (id, title,
// path, modified_at) and replaces its tag rows. Called on every projection write
// path so the notes table stays an authoritative, queryable note list.
func (s *SQLiteStore) UpsertNoteMeta(ctx context.Context, meta NoteMeta) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := upsertNoteMetaTx(ctx, tx, meta); err != nil {
		return err
	}
	return tx.Commit()
}

// upsertNoteMetaTx performs the note-meta upsert inside an existing transaction.
func upsertNoteMetaTx(ctx context.Context, tx *sql.Tx, meta NoteMeta) error {
	modified := ""
	if !meta.ModifiedAt.IsZero() {
		modified = meta.ModifiedAt.UTC().Format(metaTimeFormat)
	}
	favorite := 0
	if meta.Favorite {
		favorite = 1
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO notes(id, title, path, type, favorite, modified_at) VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET title = excluded.title, path = excluded.path, type = excluded.type, favorite = excluded.favorite, modified_at = excluded.modified_at`,
		string(meta.ID), meta.Title, meta.Path, meta.Type, favorite, modified); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM note_tags WHERE note_id = ?`, string(meta.ID)); err != nil {
		return err
	}
	seen := map[string]struct{}{}
	for _, tag := range meta.Tags {
		v := string(tag)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO note_tags(note_id, tag) VALUES (?, ?)`, string(meta.ID), v); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLiteStore) ReplaceOutgoingLinks(ctx context.Context, source domain.NoteID, links []domain.NoteLink) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO notes(id) VALUES (?)`, string(source)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM links WHERE source = ? AND strength = ?`, string(source), string(domain.LinkStrengthHard)); err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO links(source, target, resolved_id, display_text, heading, strength) VALUES (?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, link := range links {
		resolved := nullableID(link.ResolvedID)
		if link.Strength == "" {
			link.Strength = domain.LinkStrengthHard
		}
		if _, err := stmt.ExecContext(ctx, string(source), link.Target, resolved, link.DisplayText, link.Heading, string(link.Strength)); err != nil {
			return err
		}
		if link.ResolvedID != nil {
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO notes(id) VALUES (?)`, string(*link.ResolvedID)); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

// ReplaceMetadataLinks replaces all metadata-family (facet membership) edges for a
// single source note. Hub targets (e.g. "tag:go") are NOT inserted into notes —
// they are materialized as hub nodes from edges at read time. Deterministic and
// per-note, so it runs on the synchronous save path alongside ReplaceOutgoingLinks.
func (s *SQLiteStore) ReplaceMetadataLinks(ctx context.Context, source domain.NoteID, memberships []MetadataMembership) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO notes(id) VALUES (?)`, string(source)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM links WHERE source = ? AND strength IN (?, ?, ?)`, string(source), string(domain.LinkStrengthTag), string(domain.LinkStrengthType), string(domain.LinkStrengthHeading)); err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO links(source, target, resolved_id, strength) VALUES (?, ?, NULL, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, m := range memberships {
		if _, err := stmt.ExecContext(ctx, string(source), m.HubKey, string(m.Strength)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *SQLiteStore) ReplaceInferredLinks(ctx context.Context, source domain.NoteID, links []domain.InferredNoteLink) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO notes(id) VALUES (?)`, string(source)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM links WHERE source = ? AND strength = ?`, string(source), string(domain.LinkStrengthSoft)); err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO links(source, target, resolved_id, strength, score, evidence, algorithm, computed_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, link := range links {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO notes(id) VALUES (?)`, string(link.Target)); err != nil {
			return err
		}
		if _, err := stmt.ExecContext(ctx, string(source), string(link.Target), string(link.Target), string(domain.LinkStrengthSoft), link.Score, evidenceString(link.Evidence), link.Algorithm, link.ComputedAt.Format("2006-01-02T15:04:05Z07:00")); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *SQLiteStore) ReplaceAllLinks(ctx context.Context, projections []LinkProjection) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM links; DELETE FROM notes;`); err != nil {
		return err
	}
	noteStmt, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO notes(id) VALUES (?)`)
	if err != nil {
		return err
	}
	defer noteStmt.Close()
	hardStmt, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO links(source, target, resolved_id, display_text, heading, strength) VALUES (?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer hardStmt.Close()
	softStmt, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO links(source, target, resolved_id, strength, score, evidence, algorithm, computed_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer softStmt.Close()
	metaStmt, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO links(source, target, resolved_id, strength) VALUES (?, ?, NULL, ?)`)
	if err != nil {
		return err
	}
	defer metaStmt.Close()
	for _, projection := range projections {
		if err := ctx.Err(); err != nil {
			return err
		}
		if projection.Meta.ID != "" {
			if err := upsertNoteMetaTx(ctx, tx, projection.Meta); err != nil {
				return err
			}
		} else if _, err := noteStmt.ExecContext(ctx, string(projection.Source)); err != nil {
			return err
		}
		for _, membership := range projection.Metadata {
			if _, err := metaStmt.ExecContext(ctx, string(projection.Source), membership.HubKey, string(membership.Strength)); err != nil {
				return err
			}
		}
		for _, link := range projection.Hard {
			resolved := nullableID(link.ResolvedID)
			if link.Strength == "" {
				link.Strength = domain.LinkStrengthHard
			}
			if _, err := hardStmt.ExecContext(ctx, string(projection.Source), link.Target, resolved, link.DisplayText, link.Heading, string(link.Strength)); err != nil {
				return err
			}
			if link.ResolvedID != nil {
				if _, err := noteStmt.ExecContext(ctx, string(*link.ResolvedID)); err != nil {
					return err
				}
			}
		}
		for _, link := range projection.Soft {
			if _, err := noteStmt.ExecContext(ctx, string(link.Target)); err != nil {
				return err
			}
			if _, err := softStmt.ExecContext(ctx, string(projection.Source), string(link.Target), string(link.Target), string(domain.LinkStrengthSoft), link.Score, evidenceString(link.Evidence), link.Algorithm, link.ComputedAt.Format("2006-01-02T15:04:05Z07:00")); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func (s *SQLiteStore) ReplaceAllInferredLinks(ctx context.Context, projections []LinkProjection) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM links WHERE strength = ?`, string(domain.LinkStrengthSoft)); err != nil {
		return err
	}
	noteStmt, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO notes(id) VALUES (?)`)
	if err != nil {
		return err
	}
	defer noteStmt.Close()
	softStmt, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO links(source, target, resolved_id, strength, score, evidence, algorithm, computed_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer softStmt.Close()
	for _, projection := range projections {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, err := noteStmt.ExecContext(ctx, string(projection.Source)); err != nil {
			return err
		}
		for _, link := range projection.Soft {
			if _, err := noteStmt.ExecContext(ctx, string(link.Target)); err != nil {
				return err
			}
			if _, err := softStmt.ExecContext(ctx, string(projection.Source), string(link.Target), string(link.Target), string(domain.LinkStrengthSoft), link.Score, evidenceString(link.Evidence), link.Algorithm, link.ComputedAt.Format("2006-01-02T15:04:05Z07:00")); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func (s *SQLiteStore) DeleteNote(ctx context.Context, id domain.NoteID) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM links WHERE source = ? OR resolved_id = ?; DELETE FROM notes WHERE id = ?`, string(id), string(id), string(id))
	return err
}

func (s *SQLiteStore) OutgoingLinks(ctx context.Context, id domain.NoteID) ([]domain.NoteLink, error) {
	return s.queryLinks(ctx, `SELECT source, target, resolved_id, display_text, heading, strength FROM links WHERE source = ? AND strength = ? ORDER BY target`, string(id), string(domain.LinkStrengthHard))
}

func (s *SQLiteStore) Backlinks(ctx context.Context, id domain.NoteID) ([]domain.NoteLink, error) {
	return s.queryLinks(ctx, `SELECT source, target, resolved_id, display_text, heading, strength FROM links WHERE resolved_id = ? AND strength = ? ORDER BY source`, string(id), string(domain.LinkStrengthHard))
}

// Neighborhood returns the depth-bounded subgraph around id (hard + soft edges;
// metadata hubs excluded so common facets don't balloon the view). A depth-bounded
// recursive CTE finds the reachable node set from id, then only edges with both
// endpoints in that set are returned — the same result as the previous load-all-
// edges + in-Go BFS, but without materializing the whole edge table.
func (s *SQLiteStore) Neighborhood(ctx context.Context, id domain.NoteID, depth int) (domain.Graph, error) {
	if depth <= 0 {
		depth = 1
	}
	const query = `
WITH edges_norm AS (
  SELECT source AS src,
         CASE WHEN resolved_id IS NOT NULL THEN resolved_id ELSE 'unresolved:' || target END AS dst,
         strength, score
  FROM links
  WHERE strength IN ('hard', 'soft')
),
frontier(node, d) AS (
  SELECT ?, 0
  UNION
  SELECT CASE WHEN e.src = f.node THEN e.dst ELSE e.src END, f.d + 1
  FROM frontier f
  JOIN edges_norm e ON (e.src = f.node OR e.dst = f.node)
  WHERE f.d < ?
)
SELECT e.src, e.dst, e.strength, e.score
FROM edges_norm e
WHERE e.src IN (SELECT node FROM frontier) AND e.dst IN (SELECT node FROM frontier)
ORDER BY e.src, e.dst`
	rows, err := s.db.QueryContext(ctx, query, string(id), depth)
	if err != nil {
		return domain.Graph{}, err
	}
	defer rows.Close()
	var edges []domain.GraphEdge
	for rows.Next() {
		var src, dst, strength string
		var score float64
		if err := rows.Scan(&src, &dst, &strength, &score); err != nil {
			return domain.Graph{}, err
		}
		kind := domain.GraphEdgeLinksTo
		if domain.LinkStrength(strength) == domain.LinkStrengthSoft {
			kind = domain.GraphEdgeInferredRelatedTo
		}
		edges = append(edges, domain.GraphEdge{ID: fmt.Sprintf("%s|%s|%s", src, dst, strength), Source: src, Target: dst, Kind: kind, Score: score})
	}
	if err := rows.Err(); err != nil {
		return domain.Graph{}, err
	}
	return graphFromEdges(edges, true), nil
}

func (s *SQLiteStore) FullGraph(ctx context.Context, filter domain.GraphFilter) (domain.Graph, error) {
	edges, err := s.allEdges(ctx, filter)
	if err != nil {
		return domain.Graph{}, err
	}
	nodes, err := s.allNoteNodes(ctx, filter)
	if err != nil {
		return domain.Graph{}, err
	}
	return graphFromEdgesAndNodes(edges, nodes, filter.IncludeUnresolved), nil
}

// Query is the unified graph selection backing both the neighborhood and
// full-graph views (see domain.GraphQuery). When Seed is nil it selects the full
// note set; when Seed is set it selects the depth-bounded neighborhood around it.
// In both cases the metadata predicates (Types/Tags/PathPrefix) restrict which
// note nodes are kept. Unlike the legacy Neighborhood, a seeded Query honors
// IncludeMetadataLinks: facet-hub edges from the kept notes are included when
// requested — closing the "metadata links do nothing when a note is selected"
// gap. Metadata hubs are never traversed for reachability.
func (s *SQLiteStore) Query(ctx context.Context, q domain.GraphQuery) (domain.Graph, error) {
	// Reachable node-id set for a seeded neighborhood; nil means "full graph".
	var frontier map[string]struct{}
	if q.Seed != nil {
		depth := q.Depth
		if depth <= 0 {
			depth = 1
		}
		f, err := s.frontier(ctx, string(*q.Seed), depth, q.IncludeSoftLinks)
		if err != nil {
			return domain.Graph{}, err
		}
		frontier = f
	}

	keep, baseNodes, err := s.selectedNotes(ctx, q, frontier)
	if err != nil {
		return domain.Graph{}, err
	}
	edges, err := s.selectedEdges(ctx, q, keep, frontier)
	if err != nil {
		return domain.Graph{}, err
	}
	return graphFromEdgesAndNodes(edges, baseNodes, q.IncludeUnresolved), nil
}

// frontier returns the set of node ids within depth hops of seed, traversing
// hard links (and soft links when includeSoft is set). Node ids are resolved
// note ids or "unresolved:<target>" keys; metadata hubs are never traversed.
func (s *SQLiteStore) frontier(ctx context.Context, seed string, depth int, includeSoft bool) (map[string]struct{}, error) {
	strengths := []any{string(domain.LinkStrengthHard)}
	if includeSoft {
		strengths = append(strengths, string(domain.LinkStrengthSoft))
	}
	ph := strings.TrimRight(strings.Repeat("?,", len(strengths)), ",")
	query := `
WITH edges_norm AS (
  SELECT source AS src,
         CASE WHEN resolved_id IS NOT NULL THEN resolved_id ELSE 'unresolved:' || target END AS dst
  FROM links
  WHERE strength IN (` + ph + `)
),
frontier(node, d) AS (
  SELECT ?, 0
  UNION
  SELECT CASE WHEN e.src = f.node THEN e.dst ELSE e.src END, f.d + 1
  FROM frontier f
  JOIN edges_norm e ON (e.src = f.node OR e.dst = f.node)
  WHERE f.d < ?
)
SELECT DISTINCT node FROM frontier`
	args := append(append([]any{}, strengths...), seed, depth)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	set := map[string]struct{}{}
	for rows.Next() {
		var node string
		if err := rows.Scan(&node); err != nil {
			return nil, err
		}
		set[node] = struct{}{}
	}
	return set, rows.Err()
}

// selectedNotes returns the note ids to keep and their graph nodes, applying the
// metadata predicates (Types/Tags/PathPrefix; AND across axes, OR within). When
// frontier is non-nil (seeded), only notes reachable in the frontier are kept.
func (s *SQLiteStore) selectedNotes(ctx context.Context, q domain.GraphQuery, frontier map[string]struct{}) (map[string]struct{}, []domain.GraphNode, error) {
	var where []string
	var args []any
	if q.PathPrefix != "" {
		where = append(where, "id LIKE ?")
		args = append(args, q.PathPrefix+"%")
	}
	if len(q.Types) > 0 {
		ph := strings.TrimRight(strings.Repeat("?,", len(q.Types)), ",")
		where = append(where, "LOWER(type) IN ("+ph+")")
		for _, t := range q.Types {
			args = append(args, strings.ToLower(t))
		}
	}
	if len(q.Tags) > 0 {
		ph := strings.TrimRight(strings.Repeat("?,", len(q.Tags)), ",")
		where = append(where, "EXISTS (SELECT 1 FROM note_tags nt WHERE nt.note_id = notes.id AND nt.tag IN ("+ph+"))")
		for _, t := range q.Tags {
			args = append(args, string(t))
		}
	}
	if q.FavoritesOnly {
		where = append(where, "favorite = 1")
	}
	query := `SELECT id FROM notes`
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY id"
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	keep := map[string]struct{}{}
	var baseNodes []domain.GraphNode
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, nil, err
		}
		if frontier != nil {
			if _, ok := frontier[id]; !ok {
				continue
			}
		}
		keep[id] = struct{}{}
		nid := domain.NoteID(id)
		baseNodes = append(baseNodes, domain.GraphNode{ID: id, Label: id, Kind: domain.GraphNodeNote, NoteID: &nid})
	}
	return keep, baseNodes, rows.Err()
}

// selectedEdges returns the edges among the kept notes: hard always, soft when
// IncludeSoftLinks, metadata (facet-hub) when IncludeMetadataLinks. An edge is
// kept only when its source is a kept note; note→note edges also require the
// target be kept, note→unresolved edges require IncludeUnresolved (and, when
// seeded, that the unresolved target is in the frontier). Metadata edges point
// at hub keys, which are always admitted as hub nodes.
func (s *SQLiteStore) selectedEdges(ctx context.Context, q domain.GraphQuery, keep map[string]struct{}, frontier map[string]struct{}) ([]domain.GraphEdge, error) {
	strengths := []any{string(domain.LinkStrengthHard)}
	if q.IncludeSoftLinks {
		strengths = append(strengths, string(domain.LinkStrengthSoft))
	}
	if q.IncludeMetadataLinks {
		for _, ms := range domain.MetadataLinkStrengths {
			strengths = append(strengths, string(ms))
		}
	}
	ph := strings.TrimRight(strings.Repeat("?,", len(strengths)), ",")
	query := `SELECT source, target, resolved_id, strength, score FROM links WHERE strength IN (` + ph + `) ORDER BY source, target`
	rows, err := s.db.QueryContext(ctx, query, strengths...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var edges []domain.GraphEdge
	for rows.Next() {
		var source, target, strength string
		var resolved sql.NullString
		var score float64
		if err := rows.Scan(&source, &target, &resolved, &strength, &score); err != nil {
			return nil, err
		}
		if _, ok := keep[source]; !ok {
			continue
		}
		ls := domain.LinkStrength(strength)
		if ls.IsMetadata() {
			edges = append(edges, domain.GraphEdge{ID: fmt.Sprintf("%s|%s|%s", source, target, strength), Source: source, Target: target, Kind: edgeKindForStrength(ls), Score: score})
			continue
		}
		graphTarget := target
		if resolved.Valid {
			graphTarget = resolved.String
			if _, ok := keep[graphTarget]; !ok {
				continue
			}
		} else {
			if !q.IncludeUnresolved {
				continue
			}
			graphTarget = "unresolved:" + target
			if frontier != nil {
				if _, ok := frontier[graphTarget]; !ok {
					continue
				}
			}
		}
		kind := domain.GraphEdgeLinksTo
		if ls == domain.LinkStrengthSoft {
			kind = domain.GraphEdgeInferredRelatedTo
		}
		edges = append(edges, domain.GraphEdge{ID: fmt.Sprintf("%s|%s|%s", source, graphTarget, strength), Source: source, Target: graphTarget, Kind: kind, Score: score})
	}
	return edges, rows.Err()
}

// ListNotesOptions controls a ListNotes query. The zero value returns every note
// ordered by id ascending.
type ListNotesOptions struct {
	Offset        int
	Limit         int    // 0 = no limit (return all matching)
	SortBy        string // "title" | "modified" | "path" | "id" (default)
	Desc          bool
	Tag           string // optional exact-tag filter
	Type          string // optional exact-type filter (case-insensitive)
	Search        string // optional case-insensitive substring over title/id
	FavoritesOnly bool
}

// NoteRow is one listable note's stored metadata.
type NoteRow struct {
	ID         string
	Title      string
	Path       string
	Type       string
	Favorite   bool
	ModifiedAt string
	Tags       []string
}

// ListNotesResult is a page of notes plus the total count matching the filter.
type ListNotesResult struct {
	Items []NoteRow
	Total int
}

// CountNotes returns the number of notes that carry listable metadata (a non-empty
// path). Used at workspace open to decide whether the SQLite projection is complete
// enough to serve the note list, or whether to fall back to the filesystem.
func (s *SQLiteStore) CountNotes(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM notes WHERE path != ''`).Scan(&n)
	return n, err
}

// ListNotes serves a sorted/filtered/paginated page of note metadata directly
// from the notes/note_tags tables — no filesystem walk. Total is the count of
// notes matching the filter, independent of Offset/Limit.
func (s *SQLiteStore) ListNotes(ctx context.Context, opts ListNotesOptions) (ListNotesResult, error) {
	where := []string{"path != ''"}
	args := []any{}
	if opts.Tag != "" {
		where = append(where, "EXISTS (SELECT 1 FROM note_tags t WHERE t.note_id = notes.id AND t.tag = ?)")
		args = append(args, opts.Tag)
	}
	if opts.Type != "" {
		where = append(where, "LOWER(type) = ?")
		args = append(args, strings.ToLower(opts.Type))
	}
	if opts.Search != "" {
		pattern := "%" + escapeLike(strings.ToLower(opts.Search)) + "%"
		where = append(where, `(LOWER(title) LIKE ? ESCAPE '\' OR LOWER(id) LIKE ? ESCAPE '\')`)
		args = append(args, pattern, pattern)
	}
	if opts.FavoritesOnly {
		where = append(where, "favorite = 1")
	}
	whereSQL := ""
	if len(where) > 0 {
		whereSQL = " WHERE " + strings.Join(where, " AND ")
	}

	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM notes`+whereSQL, args...).Scan(&total); err != nil {
		return ListNotesResult{}, err
	}

	query := `SELECT id, title, path, type, favorite, modified_at FROM notes` + whereSQL + ` ORDER BY ` + orderByClause(opts.SortBy, opts.Desc)
	pageArgs := append([]any{}, args...)
	if opts.Limit > 0 {
		query += ` LIMIT ? OFFSET ?`
		pageArgs = append(pageArgs, opts.Limit, opts.Offset)
	}
	rows, err := s.db.QueryContext(ctx, query, pageArgs...)
	if err != nil {
		return ListNotesResult{}, err
	}
	defer rows.Close()
	items := []NoteRow{}
	index := map[string]int{}
	for rows.Next() {
		var row NoteRow
		var favorite int
		if err := rows.Scan(&row.ID, &row.Title, &row.Path, &row.Type, &favorite, &row.ModifiedAt); err != nil {
			return ListNotesResult{}, err
		}
		row.Favorite = favorite != 0
		index[row.ID] = len(items)
		items = append(items, row)
	}
	if err := rows.Err(); err != nil {
		return ListNotesResult{}, err
	}
	if err := s.attachTags(ctx, items, index); err != nil {
		return ListNotesResult{}, err
	}
	return ListNotesResult{Items: items, Total: total}, nil
}

// attachTags loads tags for the page's notes in one query and assigns them by id.
func (s *SQLiteStore) attachTags(ctx context.Context, items []NoteRow, index map[string]int) error {
	if len(items) == 0 {
		return nil
	}
	placeholders := strings.TrimRight(strings.Repeat("?,", len(items)), ",")
	args := make([]any, 0, len(items))
	for _, item := range items {
		args = append(args, item.ID)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT note_id, tag FROM note_tags WHERE note_id IN (`+placeholders+`) ORDER BY note_id, tag`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var noteID, tag string
		if err := rows.Scan(&noteID, &tag); err != nil {
			return err
		}
		if i, ok := index[noteID]; ok {
			items[i].Tags = append(items[i].Tags, tag)
		}
	}
	return rows.Err()
}

func orderByClause(sortBy string, desc bool) string {
	dir := "ASC"
	if desc {
		dir = "DESC"
	}
	switch sortBy {
	case "title":
		return "title COLLATE NOCASE " + dir + ", id ASC"
	case "modified":
		return "modified_at " + dir + ", id ASC"
	case "path":
		return "path COLLATE NOCASE " + dir + ", id ASC"
	default:
		return "id " + dir
	}
}

// escapeLike escapes LIKE wildcards so user search text matches literally.
func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

func (s *SQLiteStore) queryLinks(ctx context.Context, query string, args ...any) ([]domain.NoteLink, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var links []domain.NoteLink
	for rows.Next() {
		var source, target, display, heading, strength string
		var resolved sql.NullString
		if err := rows.Scan(&source, &target, &resolved, &display, &heading, &strength); err != nil {
			return nil, err
		}
		link := domain.NoteLink{Source: domain.NoteID(source), Target: target, DisplayText: display, Heading: heading, Strength: domain.LinkStrength(strength)}
		if resolved.Valid {
			id := domain.NoteID(resolved.String)
			link.ResolvedID = &id
		}
		links = append(links, link)
	}
	return links, rows.Err()
}

func (s *SQLiteStore) allEdges(ctx context.Context, filter domain.GraphFilter) ([]domain.GraphEdge, error) {
	includeHard := true
	includeSoft := filter.IncludeSoftLinks
	var strengths []string
	if includeHard {
		strengths = append(strengths, string(domain.LinkStrengthHard))
	}
	if includeSoft {
		strengths = append(strengths, string(domain.LinkStrengthSoft))
	}
	if filter.IncludeMetadataLinks {
		for _, ms := range domain.MetadataLinkStrengths {
			strengths = append(strengths, string(ms))
		}
	}
	placeholders := strings.TrimRight(strings.Repeat("?,", len(strengths)), ",")
	args := make([]any, 0, len(strengths)+1)
	for _, strength := range strengths {
		args = append(args, strength)
	}
	query := `SELECT source, target, resolved_id, strength, score FROM links WHERE strength IN (` + placeholders + `)`
	if filter.PathPrefix != "" {
		query += ` AND source LIKE ?`
		args = append(args, filter.PathPrefix+"%")
	}
	if filter.FavoritesOnly {
		query += ` AND EXISTS (SELECT 1 FROM notes n WHERE n.id = links.source AND n.favorite = 1)`
	}
	query += ` ORDER BY source, target`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var edges []domain.GraphEdge
	for rows.Next() {
		var source, target, strength string
		var resolved sql.NullString
		var score float64
		if err := rows.Scan(&source, &target, &resolved, &strength, &score); err != nil {
			return nil, err
		}
		if domain.LinkStrength(strength).IsMetadata() {
			// Metadata edges point at a facet hub key (e.g. "tag:go"); no resolution
			// or unresolved rewrite — the target is the hub node id as stored.
			edges = append(edges, domain.GraphEdge{ID: fmt.Sprintf("%s|%s|%s", source, target, strength), Source: source, Target: target, Kind: edgeKindForStrength(domain.LinkStrength(strength)), Score: score})
			continue
		}
		graphTarget := target
		if resolved.Valid {
			graphTarget = resolved.String
		} else if !filter.IncludeUnresolved {
			continue
		} else {
			graphTarget = "unresolved:" + target
		}
		kind := domain.GraphEdgeLinksTo
		if domain.LinkStrength(strength) == domain.LinkStrengthSoft {
			kind = domain.GraphEdgeInferredRelatedTo
		}
		edges = append(edges, domain.GraphEdge{ID: fmt.Sprintf("%s|%s|%s", source, graphTarget, strength), Source: source, Target: graphTarget, Kind: kind, Score: score})
	}
	return edges, rows.Err()
}

func (s *SQLiteStore) allNoteNodes(ctx context.Context, filter domain.GraphFilter) ([]domain.GraphNode, error) {
	query := `SELECT id FROM notes`
	args := make([]any, 0, 1)
	var where []string
	if filter.PathPrefix != "" {
		where = append(where, "id LIKE ?")
		args = append(args, filter.PathPrefix+"%")
	}
	if filter.FavoritesOnly {
		where = append(where, "favorite = 1")
	}
	if len(where) > 0 {
		query += ` WHERE ` + strings.Join(where, " AND ")
	}
	query += ` ORDER BY id`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var nodes []domain.GraphNode
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		noteID := domain.NoteID(id)
		nodes = append(nodes, domain.GraphNode{ID: id, Label: id, Kind: domain.GraphNodeNote, NoteID: &noteID})
	}
	return nodes, rows.Err()
}

func graphFromEdges(edges []domain.GraphEdge, includeUnresolved bool) domain.Graph {
	return graphFromEdgesAndNodes(edges, nil, includeUnresolved)
}

func graphFromEdgesAndNodes(edges []domain.GraphEdge, baseNodes []domain.GraphNode, includeUnresolved bool) domain.Graph {
	nodeMap := map[string]domain.GraphNode{}
	for _, node := range baseNodes {
		nodeMap[node.ID] = node
	}
	for _, edge := range edges {
		addNode(nodeMap, edge.Source, false)
		addNode(nodeMap, edge.Target, strings.HasPrefix(edge.Target, "unresolved:"))
	}
	nodes := make([]domain.GraphNode, 0, len(nodeMap))
	for _, node := range nodeMap {
		if node.Kind == domain.GraphNodeUnresolved && !includeUnresolved {
			continue
		}
		nodes = append(nodes, node)
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	return domain.Graph{Nodes: nodes, Edges: edges}
}

func addNode(nodes map[string]domain.GraphNode, id string, unresolved bool) {
	if _, ok := nodes[id]; ok {
		return
	}
	if hubKind, label, ok := hubNodeKind(id); ok {
		nodes[id] = domain.GraphNode{ID: id, Label: label, Kind: hubKind}
		return
	}
	kind := domain.GraphNodeNote
	label := id
	var noteID *domain.NoteID
	if unresolved {
		kind = domain.GraphNodeUnresolved
		label = strings.TrimPrefix(id, "unresolved:")
	} else {
		v := domain.NoteID(id)
		noteID = &v
	}
	nodes[id] = domain.GraphNode{ID: id, Label: label, Kind: kind, NoteID: noteID}
}

func nullableID(id *domain.NoteID) any {
	if id == nil {
		return nil
	}
	return string(*id)
}

func evidenceString(evidence []domain.LinkEvidence) string {
	parts := make([]string, 0, len(evidence))
	for _, item := range evidence {
		parts = append(parts, fmt.Sprintf("%s:%s", item.Kind, item.Detail))
	}
	return strings.Join(parts, ";")
}

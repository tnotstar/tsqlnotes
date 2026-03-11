package adapters

import (
	"crypto/rand"
	"database/sql"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	_ "github.com/microsoft/go-mssqldb"
	_ "modernc.org/sqlite"
)

// ConnectionConfig holds the user-supplied connection parameters
type ConnectionConfig struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Driver   string `json:"driver"` // postgres | mysql | sqlite3 | sqlserver
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Database string `json:"database"`
	Username string `json:"username"`
	Password string `json:"password"`
	SSLMode  string `json:"sslMode"`  // postgres: disable|require|verify-full
	FilePath string `json:"filePath"` // sqlite3 only
	Options  string `json:"options"`  // extra DSN options
}

// ActiveConnection bundles a live *sql.DB with its config
type ActiveConnection struct {
	DB     *sql.DB
	Config ConnectionConfig
}

var (
	mu          sync.RWMutex
	connections = map[string]*ActiveConnection{}
	// savedConfigs are persisted in-memory (no file I/O for simplicity)
	savedConfigs = map[string]ConnectionConfig{}
)

// OpenConnection opens a new *sql.DB for the given config and stores it
func OpenConnection(cfg ConnectionConfig) error {
	dsn, err := buildDSN(cfg)
	if err != nil {
		return fmt.Errorf("building DSN: %w", err)
	}

	driverName := cfg.Driver
	if driverName == "sqlite3" {
		driverName = "sqlite"
	}

	db, err := sql.Open(driverName, dsn)
	if err != nil {
		return fmt.Errorf("sql.Open: %w", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return fmt.Errorf("ping failed: %w", err)
	}

	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)

	mu.Lock()
	defer mu.Unlock()
	connections[cfg.ID] = &ActiveConnection{DB: db, Config: cfg}
	savedConfigs[cfg.ID] = cfg
	return nil
}

// GetConnection retrieves a live connection by ID
func GetConnection(id string) (*ActiveConnection, bool) {
	mu.RLock()
	defer mu.RUnlock()
	c, ok := connections[id]
	return c, ok
}

// CloseConnection removes and closes a connection by ID
func CloseConnection(id string) {
	mu.Lock()
	defer mu.Unlock()
	if c, ok := connections[id]; ok {
		c.DB.Close()
		delete(connections, id)
	}
}

// ListSavedConfigs returns all saved (possibly disconnected) configs
func ListSavedConfigs() []ConnectionConfig {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]ConnectionConfig, 0, len(savedConfigs))
	for _, v := range savedConfigs {
		v.Password = "••••••••" // never send password back
		out = append(out, v)
	}
	return out
}

// ActiveConnectionIDs returns the IDs of currently open connections
func ActiveConnectionIDs() map[string]bool {
	mu.RLock()
	defer mu.RUnlock()
	ids := map[string]bool{}
	for id := range connections {
		ids[id] = true
	}
	return ids
}

// ---- DSN builders --------------------------------------------------------

func buildDSN(cfg ConnectionConfig) (string, error) {
	switch cfg.Driver {
	case "postgres":
		sslMode := cfg.SSLMode
		if sslMode == "" {
			sslMode = "disable"
		}
		return fmt.Sprintf(
			"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
			cfg.Host, cfg.Port, cfg.Username, cfg.Password, cfg.Database, sslMode,
		), nil

	case "mysql":
		return fmt.Sprintf(
			"%s:%s@tcp(%s:%d)/%s?parseTime=true",
			cfg.Username, cfg.Password, cfg.Host, cfg.Port, cfg.Database,
		), nil

	case "sqlite3":
		if cfg.FilePath == "" {
			return "", fmt.Errorf("filePath required for sqlite3")
		}
		return cfg.FilePath, nil

	case "sqlserver":
		return fmt.Sprintf(
			"sqlserver://%s:%s@%s:%d?database=%s",
			cfg.Username, cfg.Password, cfg.Host, cfg.Port, cfg.Database,
		), nil

	default:
		return "", fmt.Errorf("unsupported driver: %s", cfg.Driver)
	}
}

// QueryRequest is the JSON body for POST /api/query
type QueryRequest struct {
	ConnID string `json:"connId"`
	SQL    string `json:"sql"`
	Limit  int    `json:"limit"` // 0 → default 1000
}

// QueryResult is returned per statement
type QueryResult struct {
	Columns      []ColumnInfo     `json:"columns"`
	Rows         []map[string]any `json:"rows"`
	RowCount     int              `json:"rowCount"`
	AffectedRows int64            `json:"affectedRows"`
	Duration     string           `json:"duration"`
	Error        string           `json:"error,omitempty"`
	IsSelect     bool             `json:"isSelect"`
}

type ColumnInfo struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// QueryHandler  POST /api/query
func QueryHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req QueryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.ConnID == "" {
		writeError(w, http.StatusBadRequest, "connId required")
		return
	}
	if req.Limit <= 0 {
		req.Limit = 1000
	}

	conn, ok := GetConnection(req.ConnID)
	if !ok {
		writeError(w, http.StatusNotFound, "connection not found or not active")
		return
	}

	// Split on semicolons - naïve but effective for ANSI SQL
	statements := splitStatements(req.SQL)
	results := make([]QueryResult, 0, len(statements))

	for _, stmt := range statements {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		res := executeStatement(conn.DB, stmt, req.Limit)
		results = append(results, res)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"results": results,
		"count":   len(results),
	})
}

// ExportHandler  POST /api/export  — returns CSV of a query result
func ExportHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req QueryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.Limit <= 0 {
		req.Limit = 100000
	}

	conn, ok := GetConnection(req.ConnID)
	if !ok {
		writeError(w, http.StatusNotFound, "connection not found")
		return
	}

	res := executeStatement(conn.DB, req.SQL, req.Limit)
	if res.Error != "" {
		writeError(w, http.StatusBadRequest, res.Error)
		return
	}

	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", `attachment; filename="export.csv"`)

	cw := csv.NewWriter(w)
	// Header row
	headers := make([]string, len(res.Columns))
	for i, c := range res.Columns {
		headers[i] = c.Name
	}
	cw.Write(headers)

	for _, row := range res.Rows {
		record := make([]string, len(res.Columns))
		for i, c := range res.Columns {
			v := row[c.Name]
			if v == nil {
				record[i] = "NULL"
			} else {
				record[i] = fmt.Sprintf("%v", v)
			}
		}
		cw.Write(record)
	}
	cw.Flush()
}

// ---- helpers ---------------------------------------------------------------

func executeStatement(db *sql.DB, stmt string, limit int) QueryResult {
	start := time.Now()
	upper := strings.ToUpper(strings.TrimSpace(stmt))
	isSelect := strings.HasPrefix(upper, "SELECT") ||
		strings.HasPrefix(upper, "WITH") ||
		strings.HasPrefix(upper, "SHOW") ||
		strings.HasPrefix(upper, "EXPLAIN") ||
		strings.HasPrefix(upper, "DESCRIBE")

	var res QueryResult
	res.IsSelect = isSelect

	if isSelect {
		rows, err := db.Query(stmt)
		if err != nil {
			res.Error = err.Error()
			res.Duration = time.Since(start).String()
			return res
		}
		defer rows.Close()

		colTypes, err := rows.ColumnTypes()
		if err != nil {
			res.Error = err.Error()
			res.Duration = time.Since(start).String()
			return res
		}

		for _, ct := range colTypes {
			res.Columns = append(res.Columns, ColumnInfo{
				Name: ct.Name(),
				Type: ct.DatabaseTypeName(),
			})
		}

		colCount := len(colTypes)
		for rows.Next() && res.RowCount < limit {
			scanArgs := make([]any, colCount)
			vals := make([]any, colCount)
			for i := range vals {
				scanArgs[i] = &vals[i]
			}
			if err := rows.Scan(scanArgs...); err != nil {
				res.Error = err.Error()
				break
			}
			rowMap := make(map[string]any, colCount)
			for i, ct := range colTypes {
				v := vals[i]
				if b, ok := v.([]byte); ok {
					v = string(b)
				}
				rowMap[ct.Name()] = v
			}
			res.Rows = append(res.Rows, rowMap)
			res.RowCount++
		}
		if err := rows.Err(); err != nil && res.Error == "" {
			res.Error = err.Error()
		}
	} else {
		result, err := db.Exec(stmt)
		if err != nil {
			res.Error = err.Error()
			res.Duration = time.Since(start).String()
			return res
		}
		affected, _ := result.RowsAffected()
		res.AffectedRows = affected
		res.RowCount = int(affected)
	}

	res.Duration = time.Since(start).String()
	return res
}

func splitStatements(sql string) []string {
	// Simple semicolon split that ignores semicolons inside string literals
	var stmts []string
	var buf strings.Builder
	inSingle := false
	inDouble := false

	for i, ch := range sql {
		switch {
		case ch == '\'' && !inDouble:
			inSingle = !inSingle
			buf.WriteRune(ch)
		case ch == '"' && !inSingle:
			inDouble = !inDouble
			buf.WriteRune(ch)
		case ch == ';' && !inSingle && !inDouble:
			s := strings.TrimSpace(buf.String())
			if s != "" {
				stmts = append(stmts, s)
			}
			buf.Reset()
		default:
			_ = i
			buf.WriteRune(ch)
		}
	}
	if s := strings.TrimSpace(buf.String()); s != "" {
		stmts = append(stmts, s)
	}
	return stmts
}

// SchemaHandler  GET /api/schema?connId=xxx[&schema=yyy][&table=zzz]
func SchemaHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	connID := r.URL.Query().Get("connId")
	if connID == "" {
		writeError(w, http.StatusBadRequest, "connId required")
		return
	}

	conn, ok := GetConnection(connID)
	if !ok {
		writeError(w, http.StatusNotFound, "connection not found or not active")
		return
	}

	schemaFilter := r.URL.Query().Get("schema")
	tableFilter := r.URL.Query().Get("table")

	switch {
	case tableFilter != "" && schemaFilter != "":
		// Return columns for a specific table
		cols, err := getColumns(conn, schemaFilter, tableFilter)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, cols)

	case schemaFilter != "":
		// Return tables within a schema
		tables, err := getTables(conn, schemaFilter)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, tables)

	default:
		// Return list of schemas/databases
		schemas, err := getSchemas(conn)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, schemas)
	}
}

// ---- introspection queries -----------------------------------------------

type SchemaNode struct {
	ID       string      `json:"id"`
	Text     string      `json:"text"`
	Type     string      `json:"type"` // schema | table | view | column
	Children bool        `json:"children"`
	Icon     string      `json:"icon,omitempty"`
	Meta     *ColumnMeta `json:"meta,omitempty"`
}

type ColumnMeta struct {
	DataType   string `json:"dataType"`
	Nullable   string `json:"nullable"`
	Default    string `json:"default"`
	PrimaryKey bool   `json:"primaryKey"`
}

func getSchemas(conn *ActiveConnection) ([]SchemaNode, error) {
	var query string
	switch conn.Config.Driver {
	case "postgres":
		query = `SELECT schema_name FROM information_schema.schemata
		         WHERE schema_name NOT IN ('pg_catalog','information_schema','pg_toast')
		         ORDER BY schema_name`
	case "mysql":
		query = `SELECT schema_name FROM information_schema.schemata
		         WHERE schema_name NOT IN ('information_schema','performance_schema','mysql','sys')
		         ORDER BY schema_name`
	case "sqlite3":
		// sqlite has no schemas - return synthetic "main"
		return []SchemaNode{{
			ID: "schema:main", Text: "main", Type: "schema", Children: true,
		}}, nil
	case "sqlserver":
		query = `SELECT name FROM sys.schemas WHERE principal_id = 1 ORDER BY name`
	default:
		return nil, fmt.Errorf("unsupported driver: %s", conn.Config.Driver)
	}

	rows, err := conn.DB.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []SchemaNode
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		nodes = append(nodes, SchemaNode{
			ID: "schema:" + name, Text: name, Type: "schema", Children: true,
		})
	}
	return nodes, rows.Err()
}

func getTables(conn *ActiveConnection, schema string) ([]SchemaNode, error) {
	var query string
	switch conn.Config.Driver {
	case "postgres":
		query = `SELECT table_name, table_type FROM information_schema.tables
		         WHERE table_schema = $1 ORDER BY table_type, table_name`
	case "mysql":
		query = `SELECT table_name, table_type FROM information_schema.tables
		         WHERE table_schema = ? ORDER BY table_type, table_name`
	case "sqlite3":
		query = `SELECT name, type FROM sqlite_master WHERE type IN ('table','view') ORDER BY type, name`
	case "sqlserver":
		query = `SELECT t.name, t.type_desc FROM sys.tables t
		         JOIN sys.schemas s ON s.schema_id = t.schema_id
		         WHERE s.name = @p1 ORDER BY t.name`
	default:
		return nil, fmt.Errorf("unsupported driver: %s", conn.Config.Driver)
	}

	var rows interface{ Scan(...any) error }
	var closeFunc func()

	if conn.Config.Driver == "sqlite3" {
		r, err := conn.DB.Query(query)
		if err != nil {
			return nil, err
		}
		closeFunc = func() { r.Close() }
		_ = rows
		defer closeFunc()

		var nodes []SchemaNode
		for r.Next() {
			var name, ttype string
			if err := r.Scan(&name, &ttype); err != nil {
				return nil, err
			}
			nodeType := "table"
			if ttype == "view" || ttype == "VIEW" {
				nodeType = "view"
			}
			nodes = append(nodes, SchemaNode{
				ID:       fmt.Sprintf("table:%s.%s", schema, name),
				Text:     name,
				Type:     nodeType,
				Children: true,
			})
		}
		return nodes, r.Err()
	}

	r, err := conn.DB.Query(query, schema)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	var nodes []SchemaNode
	for r.Next() {
		var name, ttype string
		if err := r.Scan(&name, &ttype); err != nil {
			return nil, err
		}
		nodeType := "table"
		if ttype == "VIEW" || ttype == "view" {
			nodeType = "view"
		}
		nodes = append(nodes, SchemaNode{
			ID:       fmt.Sprintf("table:%s.%s", schema, name),
			Text:     name,
			Type:     nodeType,
			Children: true,
		})
	}
	return nodes, r.Err()
}

func getColumns(conn *ActiveConnection, schema, table string) ([]SchemaNode, error) {
	var query string
	var args []any

	switch conn.Config.Driver {
	case "postgres":
		query = `
			SELECT c.column_name, c.data_type, c.is_nullable, COALESCE(c.column_default,''),
			       CASE WHEN kcu.column_name IS NOT NULL THEN true ELSE false END as is_pk
			FROM information_schema.columns c
			LEFT JOIN information_schema.key_column_usage kcu
			  ON kcu.table_schema = c.table_schema
			  AND kcu.table_name = c.table_name
			  AND kcu.column_name = c.column_name
			  AND kcu.constraint_name IN (
			    SELECT constraint_name FROM information_schema.table_constraints
			    WHERE constraint_type = 'PRIMARY KEY'
			      AND table_schema = $1 AND table_name = $2
			  )
			WHERE c.table_schema = $1 AND c.table_name = $2
			ORDER BY c.ordinal_position`
		args = []any{schema, table}

	case "mysql":
		query = `
			SELECT column_name, data_type, is_nullable,
			       COALESCE(column_default,''), column_key = 'PRI'
			FROM information_schema.columns
			WHERE table_schema = ? AND table_name = ?
			ORDER BY ordinal_position`
		args = []any{schema, table}

	case "sqlite3":
		query = fmt.Sprintf("PRAGMA table_info(%q)", table)
		r, err := conn.DB.Query(query)
		if err != nil {
			return nil, err
		}
		defer r.Close()
		var nodes []SchemaNode
		for r.Next() {
			var cid int
			var name, ctype, notnull, dflt string
			var pk int
			if err := r.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
				return nil, err
			}
			nullable := "YES"
			if notnull == "1" {
				nullable = "NO"
			}
			nodes = append(nodes, SchemaNode{
				ID:   fmt.Sprintf("col:%s.%s.%s", schema, table, name),
				Text: name,
				Type: "column",
				Meta: &ColumnMeta{DataType: ctype, Nullable: nullable, Default: dflt, PrimaryKey: pk == 1},
			})
		}
		return nodes, r.Err()

	case "sqlserver":
		query = `
			SELECT c.name, tp.name, c.is_nullable,
			       COALESCE(OBJECT_DEFINITION(c.default_object_id),''),
			       CASE WHEN ic.column_id IS NOT NULL THEN 1 ELSE 0 END
			FROM sys.columns c
			JOIN sys.types tp ON tp.user_type_id = c.user_type_id
			JOIN sys.tables t ON t.object_id = c.object_id
			JOIN sys.schemas s ON s.schema_id = t.schema_id
			LEFT JOIN sys.index_columns ic ON ic.object_id = c.object_id
			  AND ic.column_id = c.column_id
			  AND ic.index_id IN (SELECT index_id FROM sys.indexes WHERE is_primary_key=1 AND object_id = c.object_id)
			WHERE s.name = @p1 AND t.name = @p2
			ORDER BY c.column_id`
		args = []any{schema, table}
	default:
		return nil, fmt.Errorf("unsupported driver")
	}

	r, err := conn.DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	var nodes []SchemaNode
	for r.Next() {
		var name, dtype, nullable, defval string
		var isPK bool
		if err := r.Scan(&name, &dtype, &nullable, &defval, &isPK); err != nil {
			return nil, err
		}
		nodes = append(nodes, SchemaNode{
			ID:   fmt.Sprintf("col:%s.%s.%s", schema, table, name),
			Text: name,
			Type: "column",
			Meta: &ColumnMeta{DataType: dtype, Nullable: nullable, Default: defval, PrimaryKey: isPK},
		})
	}
	return nodes, r.Err()
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func randomID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func renderTemplate(w http.ResponseWriter, page string, data interface{}) {
	tmpl, err := template.ParseFiles("web/templates/layouts/base.html", "web/templates/pages/"+page)
	if err != nil {
		log.Printf("Error parsing templates: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "base", data); err != nil {
		log.Printf("Error executing template: %v", err)
	}
}

func indexHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	renderTemplate(w, "index.html", nil)
}

// ConnectionsHandler  GET /api/connections
func ConnectionsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	configs := ListSavedConfigs()
	activeIDs := ActiveConnectionIDs()

	type connItem struct {
		ConnectionConfig
		Connected bool `json:"connected"`
	}

	items := make([]connItem, 0, len(configs))
	for _, c := range configs {
		items = append(items, connItem{c, activeIDs[c.ID]})
	}

	writeJSON(w, http.StatusOK, items)
}

// ConnectHandler  POST /api/connect
func ConnectHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var cfg ConnectionConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if cfg.ID == "" {
		cfg.ID = randomID()
	}

	if err := OpenConnection(cfg); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"id":      cfg.ID,
		"message": "connected successfully",
	})
}

// DisconnectHandler  POST /api/disconnect
func DisconnectHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var body struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	CloseConnection(body.ID)
	writeJSON(w, http.StatusOK, map[string]string{"message": "disconnected"})
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok", "version": "1.0.0"})
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

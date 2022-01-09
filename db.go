package camellia

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

const (
	dbVersion = uint64(1)
	table     = "camellia"
)

const (
	colPath         = "path"
	colLastUpdateMs = "last_update_ms"
	colIsValue      = "is_value"
	colParent       = "parent"
	colValue        = "value"
)

var db *sql.DB
var dbPath = ""
var stmts map[string]*sql.Stmt

func newEntry() *Entry {
	var entry Entry
	entry.Children = make(map[string]*Entry)
	return &entry
}

func pragma(pragma string) (string, error) {
	var value string
	row := db.QueryRow(pragma)
	err := row.Scan(&value)
	if err != nil {
		return "", err
	}

	return value, nil
}

func joinPath(p []string) string {
	split := []string{}
	for _, part := range p {
		split = append(split, strings.Trim(part, "/"))
	}

	return strings.Join(split, "/")
}

func splitPath(p string) []string {
	split := strings.Split(p, "/")
	normalized := []string{}
	for _, s := range split {
		if s != "" {
			normalized = append(normalized, s)
		}
	}

	return normalized
}

func normalizePath(p string) string {
	//return joinPath(splitPath(p))
	return joinPath(splitPath(p))
}

func parentPath(p string) string {
	s := splitPath(p)
	if len(s) > 0 {
		s = s[:len(s)-1]
	} else {
		return ""
	}

	return strings.Join(s, "/")
}

func namePath(p string) string {
	s := splitPath(p)
	if len(s) > 0 {
		return s[len(s)-1]
	} else {
		return ""
	}
}

func openDB(path string) (bool, error) {
	var err error
	if path == "" {
		return false, fmt.Errorf("DB path is empty")
	}

	created := false

	db, err = sql.Open("sqlite3", path)
	if err != nil {
		return false, fmt.Errorf("error opening DB - %v", err)
	}

	currentDBVersion, err := getDBVersion()
	if err != nil {
		db.Close()
		return false, fmt.Errorf("error getting current DB version - %w", err)
	}

	if currentDBVersion == 0 {
		// DB file is new
		_, err = migrate()
		if err != nil {
			db.Close()
			return false, fmt.Errorf("error initializing DB - %w", err)
		}

		created = true
	} else if dbVersion != currentDBVersion {
		db.Close()
		return false, ErrDBVersionMismatch
	}

	err = prepareStaments()
	if err != nil {
		db.Close()
		return false, fmt.Errorf("error creating prepared statements - %w", err)
	}

	dbPath = path

	return created, nil
}

func closeDB() error {
	err := db.Close()
	if err != nil {
		return err
	}

	dbPath = ""

	return nil
}

func prepareStaments() error {
	var err error
	stmts = make(map[string]*sql.Stmt)

	stmts["getValue"], err = db.Prepare(fmt.Sprintf(
		"SELECT %s, %s FROM %s WHERE %s = ?",
		colIsValue, colValue, table, colPath))

	if err != nil {
		return err
	}

	stmts["getEntry"], err = db.Prepare(fmt.Sprintf(
		"SELECT %s, %s, %s, %s FROM %s WHERE %s = ?",
		colPath, colLastUpdateMs, colIsValue, colValue, table, colPath))

	if err != nil {
		return err
	}

	stmts["getIsValue"], err = db.Prepare(fmt.Sprintf(
		"SELECT %s FROM %s WHERE %s = ?",
		colIsValue, table, colPath))

	if err != nil {
		return err
	}

	stmts["updateValue"], err = db.Prepare(fmt.Sprintf(
		"UPDATE %s SET %s = ?, %s = ? WHERE %s = ?",
		table, colLastUpdateMs, colValue, colPath))

	if err != nil {
		return err
	}

	stmts["insertValueEntry"], err = db.Prepare(fmt.Sprintf(
		"INSERT INTO %s (%s, %s, %s, %s, %s) VALUES (?, ?, 1, ?, ?)",
		table, colPath, colLastUpdateMs, colIsValue, colParent, colValue))

	if err != nil {
		return err
	}

	stmts["insertNonValueEntry"], err = db.Prepare(fmt.Sprintf(
		"INSERT INTO %s (%s, %s, %s, %s) VALUES (?, ?, 0, ?)",
		table, colPath, colLastUpdateMs, colIsValue, colParent))

	if err != nil {
		return err
	}

	stmts["getChildren"], err = db.Prepare(fmt.Sprintf(
		"SELECT %s, %s, %s, %s FROM %s WHERE %s = ?",
		colPath, colLastUpdateMs, colIsValue, colValue, table, colParent))

	if err != nil {
		return err
	}

	stmts["getChildrenPaths"], err = db.Prepare(fmt.Sprintf(
		"SELECT %s FROM %s WHERE %s = ?",
		colPath, table, colParent))

	if err != nil {
		return err
	}

	stmts["deleteEntry"], err = db.Prepare(fmt.Sprintf("DELETE FROM %s WHERE %s = ?", table, colPath))

	if err != nil {
		return err
	}

	return nil
}

func getDBVersion() (uint64, error) {
	dbVersionStr, err := pragma("PRAGMA user_version")
	if err != nil {
		return 0, err
	}

	version, err := strconv.ParseUint(dbVersionStr, 10, 64)
	if err != nil {
		return 0, err
	}

	return version, nil
}

func migrate() (bool, error) {
	version, err := getDBVersion()
	if err != nil {
		return false, fmt.Errorf("error getting current DB version - %w", err)
	}

	tx, err := db.BeginTx(context.Background(), &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return false, err
	}

	migrated := false

	if version == 0 {
		_, err := tx.Exec(fmt.Sprintf(
			`CREATE TABLE %s (
				%s TEXT NOT NULL UNIQUE,
				%s INTEGER NOT NULL,
				%s BIT DEFAULT 0,
				%s TEXT DEFAULT '',
				%s TEXT DEFAULT '',
				PRIMARY KEY (%s)
			)`,
			table,
			colPath,
			colLastUpdateMs,
			colIsValue,
			colParent,
			colValue,
			colPath))

		if err != nil {
			tx.Rollback()
			return false, err
		}

		_, err = tx.Exec(fmt.Sprintf(
			`CREATE INDEX IF NOT EXISTS
				path_index ON %s (%s)`,
			table,
			colPath))

		if err != nil {
			tx.Rollback()
			return false, err
		}

		_, err = tx.Exec(fmt.Sprintf(
			`CREATE INDEX IF NOT EXISTS
				parent_index ON %s (%s)`,
			table,
			colParent))

		if err != nil {
			tx.Rollback()
			return false, err
		}

		_, err = tx.Exec(fmt.Sprintf(
			"INSERT INTO %s (%s, %s, %s, %s, %s) VALUES (?, ?, ?, ?, ?)",
			table, colPath, colLastUpdateMs, colIsValue, colParent, colValue),
			"", time.Now().UnixMilli(), 0, sql.NullString{}, "")

		if err != nil {
			tx.Rollback()
			return false, err
		}

		migrated = true
	}

	_, err = tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", 1))
	if err != nil {
		tx.Rollback()
		return false, err
	}

	err = tx.Commit()
	if err != nil {
		return false, err
	}

	return migrated, nil
}

func setValue(path, value string, tx *sql.Tx, force bool, skipHooks bool) error {
	sPath := splitPath(path)
	if len(path) == 0 {
		return ErrPathInvalid
	}

	now := time.Now().UnixMicro()

	entry, err := getEntry(path, tx)
	if err != nil {
		if !errors.Is(err, ErrPathNotFound) {
			return err
		}
	} else {
		if !entry.IsValue {
			/* Path exists, but it is not a value. If force == true, we delete it and its children to forcibly
			   recreate it as the new value */
			if !force {
				return ErrPathIsNotAValue
			}

			err = deleteEntry(path, tx)
			if err != nil {
				return err
			}

			if !skipHooks {
				err = callPreSetHooks(path, value)
				if err != nil {
					return fmt.Errorf("error calling pre set hooks - %w", err)
				}
			}

			_, err := tx.Stmt(stmts["insertValueEntry"]).Exec(path, now, parentPath(path), value)
			if err != nil {
				return err
			}
		} else {
			if !skipHooks {
				err = callPreSetHooks(path, value)
				if err != nil {
					return fmt.Errorf("error calling pre set hooks - %w", err)
				}
			}

			_, err := tx.Stmt(stmts["updateValue"]).Exec(now, value, path)
			if err != nil {
				return err
			}
		}

		if !skipHooks {
			err = callPostSetHooks(path, value)
			if err != nil {
				return fmt.Errorf("error calling post set hooks - %w", err)
			}
		}

		return nil
	}

	parent := ""

	// Path does not exist, create every entry in the path
	i := 0
	for i < len(sPath) {
		part := joinPath(sPath[:i])

		isValue := false
		row := tx.Stmt(stmts["getIsValue"]).QueryRow(part)
		err = row.Scan(&isValue)

		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				_, err := tx.Stmt(stmts["insertNonValueEntry"]).Exec(part, now, parent)
				if err != nil {
					return nil
				}

				parent = part
			} else {
				return err
			}
		} else {
			if isValue {
				if !force {
					return ErrPathInvalid
				}

				err := deleteEntry(part, tx)
				if err != nil {
					return err
				}

				i--
			} else {
				parent = part
			}
		}

		i++
	}

	if !skipHooks {
		err = callPreSetHooks(path, value)
		if err != nil {
			return fmt.Errorf("error calling pre set hooks - %w", err)
		}
	}

	_, err = tx.Stmt(stmts["insertValueEntry"]).Exec(path, now, parent, value)
	if err != nil {
		return err
	}

	if !skipHooks {
		err = callPostSetHooks(path, value)
		if err != nil {
			return fmt.Errorf("error calling post set hooks - %w", err)
		}
	}

	return nil
}

func setRootEntry(entry *Entry, tx *sql.Tx, force bool, skipHooks bool, onlyMerge bool) error {
	if entry.Path != "" {
		return ErrPathInvalid
	}

	parent := ""
	var visit func(entry *Entry) error

	visit = func(entry *Entry) error {
		exists := false
		isValue, err := pathIsValue(entry.Path, tx)
		if err != nil {
			if errors.Is(err, ErrPathNotFound) {
				exists = false
			} else {
				return err
			}
		} else {
			exists = true

			if isValue != entry.IsValue && !onlyMerge {
				if !force {
					return ErrPathInvalid
				} else {
					err = deleteEntry(entry.Path, tx)
					if err != nil {
						return fmt.Errorf("error deleting entry %s - %w", entry.Path, err)
					}

					exists = false
				}
			}
		}

		if !exists {
			if entry.IsValue {
				_, err := tx.Stmt(stmts["insertValueEntry"]).Exec(entry.Path, entry.LastUpdate, parent, entry.Value)
				if err != nil {
					return fmt.Errorf("error inserting value entry %s - %w", entry.Path, err)
				}
			} else {
				_, err := tx.Stmt(stmts["insertNonValueEntry"]).Exec(entry.Path, entry.LastUpdate, parent)
				if err != nil {
					return fmt.Errorf("error inserting non-value entry %s - %w", entry.Path, err)
				}
			}
		} else if !onlyMerge {
			if entry.IsValue {
				_, err := tx.Stmt(stmts["updateValue"]).Exec(entry.LastUpdate, entry.Value, entry.Path)
				if err != nil {
					return err
				}

				if err != nil {
					return fmt.Errorf("error updating value entry %s - %w", entry.Path, err)
				}
			}
		}

		if !entry.IsValue {
			prevParent := parent
			parent = entry.Path

			for _, child := range entry.Children {
				err = visit(child)
				if err != nil {
					return err
				}
			}

			parent = prevParent
		}

		return nil
	}

	return visit(entry)
}

func getValue(path string, tx *sql.Tx) (string, error) {
	row := tx.Stmt(stmts["getValue"]).QueryRow(path)

	var isValue bool
	var value string
	err := row.Scan(&isValue, &value)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrPathNotFound
		} else {
			return "", err
		}
	}

	if !isValue {
		return "", ErrPathIsNotAValue
	}

	return value, nil
}

func entriesFromRows(rows *sql.Rows) ([]*Entry, error) {
	entries := []*Entry{}

	for rows.Next() {
		entry := newEntry()
		lastUpdateMs := int64(0)

		err := rows.Scan(&entry.Path, &lastUpdateMs, &entry.IsValue, &entry.Value)
		if err != nil {
			return nil, err
		}

		entry.LastUpdate = time.Unix(lastUpdateMs/1000, (lastUpdateMs*1000000)%1000000000)

		entries = append(entries, entry)
	}

	err := rows.Err()
	if err != nil {
		return nil, err
	}

	return entries, nil
}

func getEntry(path string, tx *sql.Tx) (*Entry, error) {
	rows, err := tx.Stmt(stmts["getEntry"]).Query(path)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrPathNotFound
		} else {
			return nil, err
		}
	}

	entries, err := entriesFromRows(rows)
	if len(entries) == 0 {
		return nil, ErrPathNotFound
	}

	return entries[0], nil
}

func getEntryDepth(path string, depth int, tx *sql.Tx) (*Entry, error) {
	var root *Entry

	err := recurse(path, depth, func(entry *Entry, parent *Entry, d uint) error {
		if root == nil {
			root = entry
			return nil
		}

		name := namePath(entry.Path)
		parent.Children[name] = entry

		return nil
	}, tx)

	return root, err
}

func recurse(path string, depth int, cb func(entry *Entry, parent *Entry, depth uint) error, tx *sql.Tx) error {
	if cb == nil {
		return fmt.Errorf("not callback function specified")
	}

	root, err := getEntry(path, tx)
	if err != nil {
		return err
	}

	d := 0
	queue := [][]*Entry{}
	queue = append(queue, []*Entry{root, nil})

	for len(queue) != 0 {
		pair := queue[0]
		queue = queue[1:]

		if depth < 0 || d < depth {
			rows, err := tx.Stmt(stmts["getChildren"]).Query(pair[0].Path)
			if err != nil {
				return err
			}

			children, err := entriesFromRows(rows)
			if err != nil {
				return err
			}

			for _, child := range children {
				queue = append(queue, []*Entry{child, pair[0]})
			}
		}

		err = cb(pair[0], pair[1], uint(d))
		if err != nil {
			return fmt.Errorf("error from recurse callback - %w", err)
		}

		// We retrieve the children first, then provide the Entry, since it could be deleted in the cb
		if d < depth {
			d++
		}
	}

	return nil
}

func deleteEntry(path string, tx *sql.Tx) error {
	if path == "" {
		return ErrPathInvalid
	}

	queue := []string{}
	queue = append(queue, path)

	for len(queue) != 0 {
		p := queue[0]
		queue = queue[1:]

		rows, err := tx.Stmt(stmts["getChildrenPaths"]).Query(p)
		if err != nil {
			return err
		}

		for rows.Next() {
			child := ""
			err = rows.Scan(&child)
			if err != nil {
				return err
			}

			queue = append(queue, child)
		}

		err = rows.Err()
		if err != nil {
			return err
		}

		_, err = tx.Stmt(stmts["deleteEntry"]).Exec(p)
		if err != nil {
			return err
		}
	}

	return nil
}

func pathIsValue(path string, tx *sql.Tx) (bool, error) {
	row := tx.Stmt(stmts["getIsValue"]).QueryRow(path)
	isValue := false
	err := row.Scan(&isValue)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, ErrPathNotFound
		}

		return false, err
	}

	return isValue, nil
}

func exists(path string, tx *sql.Tx) (bool, error) {
	_, err := pathIsValue(path, tx)
	if err != nil {
		if errors.Is(err, ErrPathNotFound) {
			return false, nil
		} else {
			return false, err
		}
	} else {
		return true, nil
	}
}

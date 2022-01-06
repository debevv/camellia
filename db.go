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
	colName         = "name"
	colLastUpdateMs = "last_update_ms"
	colIsValue      = "is_value"
	colParent       = "parent"
	colValue        = "value"
)

var db *sql.DB
var dbPath = ""

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
	return joinPath(splitPath(p))
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
		return false, fmt.Errorf("error getting current DB version - %w", err)
	}

	if currentDBVersion == 0 {
		// DB file is new
		_, err = migrate()
		if err != nil {
			return false, fmt.Errorf("error initializing DB - %w", err)
		}

		created = true
	} else if dbVersion != currentDBVersion {
		return false, ErrDBVersionMismatch
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
				%s TEXT NOT NULL,
				%s INTEGER NOT NULL,
				%s BIT DEFAULT 0,
				%s INTEGER DEFAULT 0,
				%s TEXT DEFAULT ''
			)`,
			table,
			colName,
			colLastUpdateMs,
			colIsValue,
			colParent,
			colValue))

		if err != nil {
			tx.Rollback()
			return false, err
		}

		_, err = tx.Exec(fmt.Sprintf(
			`CREATE INDEX IF NOT EXISTS
				name_index ON %s (%s)`,
			table,
			colName))

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
			"INSERT INTO %s (%s, %s, %s, %s) VALUES ($1, $2, $3, $4)",
			table, colName, colLastUpdateMs, colIsValue, colParent),
			"", time.Now().UnixNano(), 0, 0)

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

func setValue(p, value string, tx *sql.Tx, force bool, skipHooks bool) error {
	path := splitPath(p)
	if len(path) == 0 {
		return ErrPathInvalid
	}

	valueName := path[len(path)-1]
	path = append(([]string{""}), path...)

	now := time.Now().UnixMicro()
	parent := int64(0)

	var err error
	i := 0
	for i < len(path)-1 {
		part := path[i]

		row := tx.QueryRow(fmt.Sprintf("SELECT %s, %s FROM %s WHERE %s = '%s' AND %s = '%d'",
			"rowid", colIsValue, table, colName, part, colParent, parent))

		id := int64(0)
		isValue := false
		err = row.Scan(&id, &isValue)

		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				res, err := tx.Exec(fmt.Sprintf(
					"INSERT INTO %s (%s, %s, %s, %s) VALUES ($1, $2, $3, $4)",
					table, colName, colLastUpdateMs, colIsValue, colParent),
					part, now, 0, parent)

				if err != nil {
					return err
				}

				parent, err = res.LastInsertId()
				if err != nil {
					return err
				}
			} else {
				return err
			}
		} else {
			if isValue {
				if !force {
					return ErrPathInvalid
				}

				err := deleteEntry(id, tx)
				if err != nil {
					return err
				}

				i--
			} else {
				parent = id
			}
		}

		i++
	}

	row := tx.QueryRow(fmt.Sprintf("SELECT %s, %s FROM %s WHERE %s = '%s' AND %s = '%d'",
		"rowid", colIsValue, table, colName, valueName, colParent, parent))

	id := int64(0)
	isValue := false
	exists := false
	err = row.Scan(&id, &isValue)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			exists = false
		} else {
			return err
		}
	} else {
		if id == 1 {
			return ErrPathInvalid
		}

		exists = true

		if !isValue {
			/* Path exists, but it is not a value. If force == true, we delete it and its children to forcibly
			   recreate it as the new value */
			if !force {
				return ErrPathInvalid
			}

			err = deleteEntry(id, tx)
			if err != nil {
				return err
			}

			exists = false
		}
	}

	if !skipHooks {
		err = callPreSetHook(p, value)
		if err != nil {
			return fmt.Errorf("error calling pre set hooks - %w", err)
		}
	}

	if !exists {
		// Create it
		_, err := tx.Exec(fmt.Sprintf(
			"INSERT INTO %s (%s, %s, %s, %s, %s) VALUES ($1, $2, $3, $4, $5)",
			table, colName, colLastUpdateMs, colIsValue, colParent, colValue),
			valueName, now, 1, parent, value)

		if err != nil {
			return err
		}
	} else {
		_, err := tx.Exec(fmt.Sprintf(
			"UPDATE %s SET %s = $1, %s = $2 WHERE %s = %d",
			table, colLastUpdateMs, colValue, "rowid", id),
			now, value)

		if err != nil {
			return err
		}
	}

	if !skipHooks {
		err = callPostSetHook(p, value)
		if err != nil {
			return fmt.Errorf("error calling post set hooks - %w", err)
		}
	}

	return nil
}

func setRootEntry(entry *Entry, tx *sql.Tx, force bool, skipHooks bool, onlyMerge bool) error {
	path := []string{}
	parentID := int64(0)

	var visit func(entry *Entry) error

	visit = func(entry *Entry) error {
		strPath := joinPath(append(path, entry.Name))
		id, isValue, err := getPathRowID(strPath, tx)
		exists := false

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
					err = deleteEntry(id, tx)
					if err != nil {
						return fmt.Errorf("error deleting entry %s - %w", strPath, err)
					}

					exists = false
				}
			}
		}

		if !exists {
			var res sql.Result
			if entry.IsValue {
				res, err = tx.Exec(fmt.Sprintf(
					"INSERT INTO %s (%s, %s, %s, %s, %s) VALUES ($1, $2, $3, $4, $5)",
					table, colName, colLastUpdateMs, colIsValue, colParent, colValue),
					entry.Name, entry.LastUpdate, 1, parentID, entry.Value)

				if err != nil {
					return fmt.Errorf("error inserting value entry %s - %w", strPath, err)
				}
			} else {
				res, err = tx.Exec(fmt.Sprintf(
					"INSERT INTO %s (%s, %s, %s, %s) VALUES ($1, $2, $3, $4)",
					table, colName, colLastUpdateMs, colIsValue, colParent),
					entry.Name, entry.LastUpdate, 0, parentID)

				if err != nil {
					return fmt.Errorf("error inserting non-value entry %s - %w", strPath, err)
				}
			}

			id, err = res.LastInsertId()
			if err != nil {
				return fmt.Errorf("error getting rowid of inserted value %s - %w", strPath, err)
			}
		} else if !onlyMerge {
			if entry.IsValue {
				_, err := tx.Exec(fmt.Sprintf(
					"UPDATE %s SET %s = $1, %s = $2 WHERE %s = %d",
					table, colLastUpdateMs, colValue, "rowid", id),
					entry.LastUpdate, entry.Value)

				if err != nil {
					return fmt.Errorf("error updating value entry %s - %w", strPath, err)
				}
			}
		}

		if !entry.IsValue {
			path = append(path, entry.Name)
			prevParent := parentID
			parentID = id

			for _, child := range entry.Children {
				err = visit(child)
				if err != nil {
					return err
				}
			}

			path = path[:len(path)-1]
			parentID = prevParent
		}

		return nil
	}

	err := visit(entry)
	return err
}

func getPathRowID(p string, tx *sql.Tx) (int64, bool, error) {
	path := splitPath(p)
	path = append([]string{""}, path...)

	lastId := int64(0)
	lastIsValue := false

	for _, part := range path {
		row := tx.QueryRow(fmt.Sprintf(
			"SELECT %s, %s FROM %s WHERE %s = '%s' AND %s = '%d'",
			"rowid", colIsValue, table, colName, part, colParent, lastId))

		id := int64(0)
		isValue := false

		err := row.Scan(&id, &isValue)

		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return 0, false, ErrPathNotFound
			} else {
				return 0, false, err
			}
		}

		lastId = id
		lastIsValue = isValue
	}

	if lastId == 0 {
		return 0, false, ErrPathNotFound
	}

	return lastId, lastIsValue, nil
}

func getValue(p string, tx *sql.Tx) (string, error) {
	id, _, err := getPathRowID(p, tx)
	if err != nil {
		return "", err
	}

	row := tx.QueryRow(fmt.Sprintf(
		"SELECT %s, %s FROM %s WHERE %s = '%d'",
		colIsValue, colValue, table, "rowid", id))

	var isValue bool
	var value string
	err = row.Scan(&isValue, &value)
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

func getEntry(p string, tx *sql.Tx, depth int) (*Entry, error) {
	id, _, err := getPathRowID(p, tx)
	if err != nil {
		return nil, err
	}

	root := newEntry()

	row := tx.QueryRow(fmt.Sprintf(
		"SELECT %s, %s, %s, %s, %s FROM %s WHERE %s = '%d'",
		"rowid", colName, colLastUpdateMs, colIsValue, colValue, table, "rowid", id))

	var lastUpdateTsMs int64
	err = row.Scan(&root.ID, &root.Name, &lastUpdateTsMs, &root.IsValue, &root.Value)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrPathNotFound
		} else {
			return nil, err
		}
	}

	root.LastUpdate = time.Unix(lastUpdateTsMs/1000, (lastUpdateTsMs*1000000)%1000000000)
	if root.IsValue {
		return root, nil
	}

	if depth == 0 {
		return root, nil
	}

	d := 0
	queue := []*Entry{}
	queue = append(queue, root)

	for len(queue) != 0 && d != depth {
		entry := queue[0]
		queue = queue[1:]

		rows, err := tx.Query(fmt.Sprintf(
			"SELECT %s, %s, %s, %s, %s FROM %s WHERE %s = '%d'",
			"rowid", colName, colLastUpdateMs, colIsValue, colValue, table, colParent, entry.ID))

		if err != nil {
			return nil, err
		}

		for rows.Next() {
			child := newEntry()
			var lastUpdateTsMs int64
			err = rows.Scan(&child.ID, &child.Name, &lastUpdateTsMs, &child.IsValue, &child.Value)
			if err != nil {
				return nil, err
			}

			child.LastUpdate = time.Unix(lastUpdateTsMs/1000, (lastUpdateTsMs*1000000)%1000000000)

			entry.Children[child.Name] = child
			queue = append(queue, child)
		}

		err = rows.Err()
		if err != nil {
			return nil, err
		}

		if depth >= 0 {
			d++
		}
	}

	return root, nil
}

func deleteEntry(entryID int64, tx *sql.Tx) error {
	if entryID == 0 || entryID == 1 {
		return ErrPathInvalid
	}

	q, deleteQ := []int64{}, []int64{}
	q = append(q, entryID)

	for len(q) != 0 {
		id := q[0]
		q = q[1:]

		deleteQ = append(deleteQ, id)

		rows, err := tx.Query(fmt.Sprintf("SELECT %s FROM %s WHERE %s = '%d'", "rowid", table, colParent, id))

		for rows.Next() {
			rowid := int64(0)
			err = rows.Scan(&rowid)
			if err != nil {
				return err
			}

			q = append(q, rowid)
		}

		err = rows.Err()
		if err != nil {
			return err
		}
	}

	for _, id := range deleteQ {
		_, err := tx.Exec(fmt.Sprintf("DELETE FROM %s WHERE %s = '%d'", table, "rowid", id))
		if err != nil {
			return err
		}
	}

	return nil
}

func exists(path string, tx *sql.Tx) (bool, error) {
	_, _, err := getPathRowID(path, tx)
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

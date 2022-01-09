package camellia

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sync/atomic"
	"time"
)

const (
	propValue      = "value"
	propChildren   = "children"
	propLastUpdate = "last_update_ms"
)

func (e *Entry) UnmarshalJSON(b []byte) error {
	jEntry := make(map[string]interface{})
	if err := json.Unmarshal(b, &jEntry); err != nil {
		return err
	}

	return e.fromJSONInterface("/", jEntry)
}

func (e Entry) MarshalJSON() ([]byte, error) {
	jEntry := make(map[string]interface{})

	jEntry[propLastUpdate] = e.LastUpdate.UnixMilli()
	if e.IsValue {
		jEntry[propValue] = e.Value
	} else {
		children := make(map[string]interface{})
		for name, child := range e.Children {
			children[name] = child
		}

		jEntry[propChildren] = children
	}

	buffer := bytes.Buffer{}
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "    ")
	err := encoder.Encode(jEntry)
	if err != nil {
		return nil, err
	}

	return buffer.Bytes(), nil
}

func ValuesToJSON(path string) (string, error) {
	mutex.Lock()
	defer mutex.Unlock()

	if atomic.LoadInt32(&initialized) == 0 {
		return "", ErrNoDB
	}

	tx, err := db.Begin()
	if err != nil {
		return "", fmt.Errorf("error beginning transaction - %w", err)
	}

	entry, err := getEntryDepth(normalizePath(path), -1, tx)
	if err != nil {
		tx.Rollback()
		return "", err
	}

	err = tx.Commit()
	if err != nil {
		return "", fmt.Errorf("error committing transaction - %w", err)
	}

	jEntry := entryToJSONValues(entry)

	w := bytes.Buffer{}
	encoder := json.NewEncoder(&w)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "    ")

	err = encoder.Encode(jEntry)
	if err != nil {
		return "", fmt.Errorf("error converting values to JSON - %w", err)
	}

	return w.String(), nil
}

func EntryToJSON(path string) (string, error) {
	mutex.Lock()
	defer mutex.Unlock()

	if atomic.LoadInt32(&initialized) == 0 {
		return "", ErrNoDB
	}

	tx, err := db.Begin()
	if err != nil {
		return "", fmt.Errorf("error beginning transaction - %w", err)
	}

	entry, err := getEntryDepth(normalizePath(path), -1, tx)
	if err != nil {
		tx.Rollback()
		return "", err
	}

	err = tx.Commit()
	if err != nil {
		return "", fmt.Errorf("error committing transaction - %w", err)
	}

	w := bytes.Buffer{}
	encoder := json.NewEncoder(&w)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "    ")

	err = encoder.Encode(entry)
	if err != nil {
		return "", fmt.Errorf("error converting entry to JSON - %w", err)
	}

	return w.String(), nil
}

func SetValuesFromJSON(reader io.Reader, onlyMerge bool) error {
	mutex.Lock()
	defer mutex.Unlock()

	if atomic.LoadInt32(&initialized) == 0 {
		return ErrNoDB
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("error beginning transaction - %w", err)
	}

	values := make(map[string]interface{})
	decoder := json.NewDecoder(reader)
	err = decoder.Decode(&values)
	if err != nil {
		return err
	}

	path := []string{}

	var visit func(entry interface{}) error
	visit = func(entry interface{}) error {
		p := joinPath(path)

		str, ok := entry.(string)
		if ok {
			if onlyMerge {
				exists, err := exists(p, tx)
				if err != nil {
					return fmt.Errorf("error checking existence of value %s - %w", p, err)
				}

				if exists {
					return nil
				}
			}

			err = setValue(p, str, tx, true, true)
			if err != nil {
				return fmt.Errorf("error setting value %s - %w", p, err)
			}
		} else {
			m, ok := entry.(map[string]interface{})
			if ok {
				for k, v := range m {
					path = append(path, k)
					err = visit(v)
					if err != nil {
						return err
					}

					path = path[:len(path)-1]
				}
			} else {
				return fmt.Errorf("invalid JSON entry at %s", p)
			}
		}

		return nil
	}

	err = visit(values)
	if err != nil {
		return err
	}

	err = tx.Commit()
	if err != nil {
		return fmt.Errorf("error committing transaction - %w", err)
	}

	return nil
}

func SetEntriesFromJSON(reader io.Reader, onlyMerge bool) error {
	mutex.Lock()
	defer mutex.Unlock()

	if atomic.LoadInt32(&initialized) == 0 {
		return ErrNoDB
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("error beginning transaction - %w", err)
	}

	entry := Entry{}
	decoder := json.NewDecoder(reader)
	err = decoder.Decode(&entry)
	if err != nil {
		return err
	}

	err = setRootEntry(&entry, tx, true, true, onlyMerge)
	if err != nil {
		return err
	}

	err = tx.Commit()
	if err != nil {
		return fmt.Errorf("error committing transaction - %w", err)
	}

	return nil
}

func entryToJSONValues(entry *Entry) interface{} {
	if entry.IsValue {
		return entry.Value
	} else {
		jEntry := make(map[string]interface{})
		for name, child := range entry.Children {
			jEntry[name] = entryToJSONValues(child)
		}

		return &jEntry
	}
}

func (e *Entry) fromJSONInterface(path string, i map[string]interface{}) error {
	path = normalizePath(path)

	e.Path = path
	e.LastUpdate = time.Now()

	if i[propValue] != nil && i[propChildren] != nil {
		return fmt.Errorf("both value and children fields are defined")
	}

	if i[propValue] != nil {
		value, ok := i[propValue].(string)
		if !ok {
			return fmt.Errorf("invalid value field")
		}

		e.Value = value
		e.IsValue = true
	} else {
		e.Children = make(map[string]*Entry)
		e.IsValue = false

		children, ok := i[propChildren].(map[string]interface{})
		if !ok {
			return fmt.Errorf("invalid children field")
		}

		for name, jChild := range children {
			itfChild, ok := jChild.(map[string]interface{})
			if !ok {
				return fmt.Errorf("invalid children field")
			}

			entry := Entry{}
			p := append(splitPath(path), name)
			err := entry.fromJSONInterface(joinPath(p), itfChild)
			if err != nil {
				return err
			}

			e.Children[name] = &entry
		}
	}

	return nil
}

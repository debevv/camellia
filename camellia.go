// camellia is a lightweight, persistent, hierarchical key-value store.
//
// Its minimal footprint (just a single SQLite .db file) makes it suitable for usage in embedded systems, or simply as a minimalist application settings container.
//
// For more info about usage and examples, see the README at https://github.com/debevv/camellia
package camellia

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

/* BaseType is the type set of built-in types accepted by Get/Set functions */
type BaseType interface {
	~int | ~uint | ~int8 | ~uint8 | ~int32 | ~uint32 | ~int64 | ~uint64 | ~float32 | ~float64 | ~bool | ~string
}

/*
TODO: go1.18 doesn't support union of explicit types and interfaces when defining type sets in constraint interfaces
So this is not possible for now:

/*
type CustomStringable interface {
	String() string
	FromString(s string) error
}

type Stringable interface {
	BaseType | CustomStringable
}

For more details: https://github.com/golang/go/issues/45346#issuecomment-862505803
*/

/* Stringable is the type set of types accepted by Get/Set functions */
type Stringable interface {
	BaseType
}

/*
Entry represents a single node in the hierarchical store.

When IsValue == true, the Entry carries a value, and it's a leaf node in the hierarchy.

When IsValue == false, the Entry does not carry a value, but its Children map can contain Entires.
*/
type Entry struct {
	Path       string
	LastUpdate time.Time
	IsValue    bool
	Value      string
	Children   map[string]*Entry
}

var (
	ErrPathInvalid       = errors.New("invalid path")
	ErrPathNotFound      = errors.New("path not found")
	ErrPathIsNotAValue   = errors.New("path is not a value")
	ErrValueEmpty        = errors.New("value is empty")
	ErrNoDB              = errors.New("no DB currently opened")
	ErrDBVersionMismatch = errors.New("DB version mismatch")
)

var initialized = int32(0)
var mutex sync.Mutex

/*
Open initializes a camellia DB for usage.

Most of the API methods will return ErrNoDB if Open is not called first.
*/
func Open(path string) (bool, error) {
	mutex.Lock()
	defer mutex.Unlock()

	if atomic.LoadInt32(&initialized) == 1 {
		return false, nil
	}

	wipeHooks()

	created, err := openDB(path)
	if err != nil {
		return false, fmt.Errorf("error opening DB - %w", err)
	}

	atomic.StoreInt32(&initialized, 1)

	return created, nil
}

/*
Close closes a camellia DB.
*/
func Close() error {
	mutex.Lock()
	defer mutex.Unlock()

	if atomic.LoadInt32(&initialized) == 0 {
		return ErrNoDB
	}

	err := closeDB()
	if err != nil {
		return fmt.Errorf("error closing DB - %w", err)
	}

	wipeHooks()

	atomic.StoreInt32(&initialized, 0)

	return nil
}

/*
IsOpen returns whether a DB is currently open.
*/
func IsOpen() bool {
	i := atomic.LoadInt32(&initialized)
	if i == 0 {
		return false
	} else {
		return true
	}
}

/*
Migrate migrates a DB at dbPath to the current supported DB schema version.

Returns true if the DB was actually migrated, false if it was already at the current supported DB schema version.
*/
func Migrate(dbPath string) (bool, error) {
	mutex.Lock()
	defer mutex.Unlock()

	created, err := Open(dbPath)
	if err != nil {
		return false, err
	}

	if created {
		return true, nil
	}

	return migrate()
}

/*
GetDBPath returns the path of the current open DB.
*/
func GetDBPath() string {
	mutex.Lock()
	defer mutex.Unlock()

	return dbPath
}

/*
GetSupportedDBSchemaVersion returns the current supported DB schema version.
*/
func GetSupportedDBSchemaVersion() uint64 {
	mutex.Lock()
	defer mutex.Unlock()

	return dbVersion
}

/*
Set sets a value of type T to the specified path.
*/
func Set[T Stringable](path string, value T) error {
	mutex.Lock()
	defer mutex.Unlock()

	if atomic.LoadInt32(&initialized) == 0 {
		return ErrNoDB
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("error beginning transaction - %w", err)
	}

	err = setValue(normalizePath(path), fmt.Sprint(value), tx, false, false)
	if err != nil {
		tx.Rollback()
		return err
	}

	err = tx.Commit()
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("error committing transaction - %w", err)
	}

	return nil
}

/*
Force sets a value of type T to the specified path.

If a non-value Entry already exists at the specified path, it is deleted first.
*/
func Force[T Stringable](path string, value T) error {
	mutex.Lock()
	defer mutex.Unlock()

	if atomic.LoadInt32(&initialized) == 0 {
		return ErrNoDB
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("error beginning transaction - %w", err)
	}

	err = setValue(normalizePath(path), fmt.Sprint(value), tx, true, false)
	if err != nil {
		tx.Rollback()
		return err
	}

	err = tx.Commit()
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("error committing transaction - %w", err)
	}

	return nil
}

/*
SetOrPanic calls Set with the specified parameters, and panics in case of error.
*/
func SetOrPanic[T Stringable](path string, value T) {
	mutex.Lock()
	defer mutex.Unlock()

	if atomic.LoadInt32(&initialized) == 0 {
		panic(ErrNoDB)
	}

	tx, err := db.Begin()
	if err != nil {
		panic(fmt.Errorf("error beginning transaction - %w", err))
	}

	err = setValue(normalizePath(path), fmt.Sprint(value), tx, false, false)
	if err != nil {
		tx.Rollback()
		panic(err)
	}

	err = tx.Commit()
	if err != nil {
		tx.Rollback()
		panic(fmt.Errorf("error committing transaction - %w", err))
	}
}

/*
ForceOrPanic calls Force with the specified parameters, and panics in case of error.
*/
func ForceOrPanic[T Stringable](path string, value T) {
	mutex.Lock()
	defer mutex.Unlock()

	if atomic.LoadInt32(&initialized) == 0 {
		panic(ErrNoDB)
	}

	tx, err := db.Begin()
	if err != nil {
		panic(fmt.Errorf("error beginning transaction - %w", err))
	}

	err = setValue(normalizePath(path), fmt.Sprint(value), tx, true, false)
	if err != nil {
		tx.Rollback()
		panic(err)
	}

	err = tx.Commit()
	if err != nil {
		tx.Rollback()
		panic(fmt.Errorf("error committing transaction - %w", err))
	}
}

/*
Get reads the value a the specified path and returns it as type T.
*/
func Get[T Stringable](path string) (T, error) {
	mutex.Lock()
	defer mutex.Unlock()

	var value T

	if atomic.LoadInt32(&initialized) == 0 {
		return value, ErrNoDB
	}

	tx, err := db.Begin()
	if err != nil {
		return value, fmt.Errorf("error beginning transaction - %w", err)
	}

	valueString, err := getValue(normalizePath(path), tx)
	if err != nil {
		tx.Rollback()
		return value, err
	}

	value, err = convertValue[T](valueString)
	if err != nil {
		tx.Rollback()
		return value, fmt.Errorf("error converting value %v to string - %w", value, err)
	}

	err = tx.Commit()
	if err != nil {
		tx.Rollback()
		return value, fmt.Errorf("error committing transaction - %w", err)
	}

	return value, nil
}

/*
GetOrPanic calls Get with the specified parameters, and panics in case of error.
*/
func GetOrPanic[T Stringable](path string) T {
	mutex.Lock()
	defer mutex.Unlock()

	var value T

	if atomic.LoadInt32(&initialized) == 0 {
		panic(ErrNoDB)
	}

	tx, err := db.Begin()
	if err != nil {
		panic(fmt.Errorf("error beginning transaction - %w", err))
	}

	valueString, err := getValue(normalizePath(path), tx)
	if err != nil {
		tx.Rollback()
		panic(err)
	}

	value, err = convertValue[T](valueString)
	if err != nil {
		tx.Rollback()
		panic(fmt.Errorf("error converting value %v to string - %w", value, err))
	}

	err = tx.Commit()
	if err != nil {
		tx.Rollback()
		panic(fmt.Errorf("error committing transaction - %w", err))
	}

	return value
}

/*
GetOrPanic calls Get with the specified parameters, and panics if the read value is empty or in case of error.
*/
func GetOrPanicEmpty[T Stringable](path string) T {
	mutex.Lock()
	defer mutex.Unlock()

	if atomic.LoadInt32(&initialized) == 0 {
		panic(ErrNoDB)
	}

	var value T

	tx, err := db.Begin()
	if err != nil {
		panic(fmt.Errorf("error beginning transaction - %w", err))
	}

	valueString, err := getValue(normalizePath(path), tx)
	if err != nil {
		tx.Rollback()
		panic(fmt.Errorf("error getting value %s - %w", path, err))
	}

	if valueString == "" {
		tx.Rollback()
		panic(ErrValueEmpty)
	}

	value, err = convertValue[T](valueString)
	if err != nil {
		tx.Rollback()
		panic(fmt.Errorf("error converting value %s - %w", path, err))
	}

	err = tx.Commit()
	if err != nil {
		tx.Rollback()
		panic(fmt.Errorf("error committing transaction - %w", err))
	}

	return value
}

/*
GetEntry returns the Entry at the specified path, including the eventual full hierarchy of children Entries.
*/
func GetEntry(path string) (*Entry, error) {
	return GetEntryDepth(path, -1)
}

/*
GetEntry returns the Entry at the specified path, including the eventual hierarchy of children Entries, but stopping
at a specified depth.

With depth > 0, stops at the specified depth.

With depth == 0, returns the Entry with an empty Children map.

With depth < 0, returns the full hierarchy of children Entries.
*/
func GetEntryDepth(path string, depth int) (*Entry, error) {
	mutex.Lock()
	defer mutex.Unlock()

	if atomic.LoadInt32(&initialized) == 0 {
		return nil, ErrNoDB
	}

	tx, err := db.Begin()
	if err != nil {
		return nil, fmt.Errorf("error beginning transaction - %w", err)
	}

	entry, err := getEntryDepth(normalizePath(path), depth, tx)
	if err != nil {
		tx.Rollback()
		return nil, err
	}

	err = tx.Commit()
	if err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("error committing transaction - %w", err)
	}

	return entry, nil
}

/*
Exists returns whether an Entry exists at the specified path.
*/
func Exists(path string) (bool, error) {
	mutex.Lock()
	defer mutex.Unlock()

	if atomic.LoadInt32(&initialized) == 0 {
		return false, ErrNoDB
	}

	tx, err := db.Begin()
	if err != nil {
		return false, fmt.Errorf("error beginning transaction - %w", err)
	}

	exists, err := exists(normalizePath(path), tx)
	if err != nil {
		tx.Rollback()
		return false, err
	}

	err = tx.Commit()
	if err != nil {
		tx.Rollback()
		return false, fmt.Errorf("error committing transaction - %w", err)
	}

	return exists, nil
}

/*
Recurse recurses, breadth-first, the hierarchy of Entries at the specified path, starting with the Entry at the path.

For each entry, calls the specified callback with the Entry itself, its parent Entry, and the current relative depth
in the hierarchy, with 0 being the depth of the Entry at the specified path.
*/
func Recurse(path string, depth int, cb func(entry *Entry, parent *Entry, depth uint) error) error {
	mutex.Lock()
	defer mutex.Unlock()

	if atomic.LoadInt32(&initialized) == 0 {
		return ErrNoDB
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("error beginning transaction - %w", err)
	}

	err = recurse(normalizePath(path), depth, cb, tx)
	if err != nil {
		tx.Rollback()
		return err
	}

	err = tx.Commit()
	if err != nil {
		return fmt.Errorf("error committing transaction - %w", err)
	}

	return nil
}

/*
Delete recursively deletes the Entry at the specified path and its children, if any.
*/
func Delete(path string) error {
	mutex.Lock()
	defer mutex.Unlock()

	if atomic.LoadInt32(&initialized) == 0 {
		return ErrNoDB
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("error beginning transaction - %w", err)
	}

	err = deleteEntry(normalizePath(path), tx)
	if err != nil {
		tx.Rollback()
		return err
	}

	err = tx.Commit()
	if err != nil {
		return fmt.Errorf("error committing transaction - %w", err)
	}

	return nil
}

/*
Wipe deletes every Entry in the database, except for the root one (at path "").
*/
func Wipe() error {
	mutex.Lock()
	defer mutex.Unlock()

	if atomic.LoadInt32(&initialized) == 0 {
		return ErrNoDB
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("error beginning transaction - %w", err)
	}

	root, err := getEntryDepth(normalizePath(""), 1, tx)
	if err != nil {
		return err
	}

	for _, child := range root.Children {
		err = deleteEntry(child.Path, tx)
		if err != nil {
			tx.Rollback()
			return err
		}
	}

	err = tx.Commit()
	if err != nil {
		return fmt.Errorf("error committing transaction - %w", err)
	}

	return nil
}

func convertValue[T Stringable](valueString string) (T, error) {
	var value T

	/*
		TODO: See up

		xValue := reflect.ValueOf(&value)
		method := xValue.MethodByName("FromString")

		if method.IsValid() {
			ret := method.Call([]reflect.Value{reflect.ValueOf(valueString)})
			if !ret[0].IsNil() {
				err, ok := ret[1].Interface().(error)
				if !ok {
					return value, fmt.Errorf("error converting value to requested type")
				} else {
					return value, err
				}
			}
		} else {
	*/
	n, err := fmt.Sscan(valueString, &value)
	if n != 1 {
		return value, fmt.Errorf("error converting value to requested type")
	}

	if err != nil {
		return value, fmt.Errorf("error converting value to requested type - %w", err)
	}
	//}

	return value, nil
}

package camellia

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type CustomStringable interface {
	String() string
	FromString(s string) error
}

type BaseType interface {
	~int | ~uint | ~int8 | ~uint8 | ~int32 | ~uint32 | ~int64 | ~uint64 | ~float32 | ~float64 | ~bool | ~string
}

/*
TODO: go1.18 doesn't support union of explicit types and interfaces when defining type sets in constraint interfaces
So this is not possible for now:

type Stringable interface {
	BaseType | CustomStringable
}

For more details: https://github.com/golang/go/issues/45346#issuecomment-862505803
*/

type Stringable interface {
	BaseType
}

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

func IsOpen() bool {
	i := atomic.LoadInt32(&initialized)
	if i == 0 {
		return false
	} else {
		return true
	}
}

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

func GetDBPath() string {
	mutex.Lock()
	defer mutex.Unlock()

	return dbPath
}

func GetSupportedDBSchemaVersion() uint64 {
	mutex.Lock()
	defer mutex.Unlock()

	return dbVersion
}

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

func GetEntry(path string) (*Entry, error) {
	return GetEntryDepth(path, -1)
}

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

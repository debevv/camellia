package camellia

import (
	"errors"
	"fmt"
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
	ID         int64
	Name       string
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
	ErrNotInitialized    = errors.New("not initialized")
	ErrDBVersionMismatch = errors.New("DB version mismatch")
)

func Init(path string) (bool, error) {
	created, err := openDB(path)
	if err != nil {
		return false, fmt.Errorf("error opening DB - %w", err)
	}

	atomic.StoreInt32(&initialized, 1)

	return created, nil
}

func Close() error {
	err := closeDB()
	if err != nil {
		return fmt.Errorf("error closing DB - %w", err)
	}

	atomic.StoreInt32(&initialized, 0)

	return nil
}

func Migrate() (bool, error) {
	return migrate()
}

func GetDBPath() string {
	return dbPath
}

func GetSupportedDBVersion() uint64 {
	return dbVersion
}

func SetValue[T Stringable](path string, value T) error {
	if atomic.LoadInt32(&initialized) == 0 {
		return ErrNotInitialized
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("error beginning transaction - %w", err)
	}

	err = setValue(path, fmt.Sprint(value), tx, false, false)
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

func ForceValue[T Stringable](path string, value T) error {
	if atomic.LoadInt32(&initialized) == 0 {
		return ErrNotInitialized
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("error beginning transaction - %w", err)
	}

	err = setValue(path, fmt.Sprint(value), tx, true, false)
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

func SetValueOrPanic[T Stringable](path string, value T) {
	if atomic.LoadInt32(&initialized) == 0 {
		panic(ErrNotInitialized)
	}

	err := SetValue(path, value)
	if err != nil {
		panic(fmt.Errorf("error setting value %v at path %s - %w", value, path, err))
	}
}

func ForceValueOrPanic[T Stringable](path string, value T) {
	if atomic.LoadInt32(&initialized) == 0 {
		panic(ErrNotInitialized)
	}

	err := ForceValue(path, value)
	if err != nil {
		panic(fmt.Errorf("error forcing value %v at path %s - %w", value, path, err))
	}
}

func GetValue[T Stringable](path string) (T, error) {
	var value T

	if atomic.LoadInt32(&initialized) == 0 {
		return value, ErrNotInitialized
	}

	tx, err := db.Begin()
	if err != nil {
		return value, fmt.Errorf("error beginning transaction - %w", err)
	}

	valueString, err := getValue(path, tx)
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
		return value, fmt.Errorf("error committing transaction - %w", err)
	}

	return value, nil
}

func GetValueOrPanic[T Stringable](path string) T {
	if atomic.LoadInt32(&initialized) == 0 {
		panic(ErrNotInitialized)
	}

	value, err := GetValue[T](path)
	if err != nil {
		panic(fmt.Errorf("error getting value %s - %w", path, err))
	}

	return value
}

func GetValueOrPanicEmpty[T Stringable](path string) T {
	var value T

	if atomic.LoadInt32(&initialized) == 0 {
		panic(ErrNotInitialized)
	}

	tx, err := db.Begin()
	if err != nil {
		panic(fmt.Errorf("error beginning transaction - %w", err))
	}

	valueString, err := getValue(path, tx)
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

	tx.Commit()

	return value
}

type Children map[string]Children

func GetChildren(path string, depth uint) (Children, error) {
	if atomic.LoadInt32(&initialized) == 0 {
		return nil, ErrNotInitialized
	}

	return nil, nil
}

func Exists(path string) (bool, error) {
	if atomic.LoadInt32(&initialized) == 0 {
		return false, ErrNotInitialized
	}

	tx, err := db.Begin()
	if err != nil {
		return false, err
	}

	_, _, err = getPathRowID(path, tx)
	if err != nil {
		if errors.Is(err, ErrPathNotFound) {
			tx.Commit()
			return false, nil
		} else {
			tx.Rollback()
			return false, err
		}
	} else {
		tx.Commit()
		return true, nil
	}
}

func DeleteEntry(path string) error {
	if atomic.LoadInt32(&initialized) == 0 {
		return ErrNotInitialized
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}

	id, _, err := getPathRowID(path, tx)
	if err != nil {
		tx.Rollback()
		return err
	}

	err = deleteEntry(id, tx)
	if err != nil {
		tx.Rollback()
		return err
	}

	err = tx.Commit()
	if err != nil {
		return err
	}

	return nil
}

func Wipe() error {
	if atomic.LoadInt32(&initialized) == 0 {
		return ErrNotInitialized
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}

	root, err := getEntry("/", tx, 1)
	if err != nil {
		return err
	}

	for _, child := range root.Children {
		err = deleteEntry(child.ID, tx)
		if err != nil {
			tx.Rollback()
			return err
		}
	}

	err = tx.Commit()
	if err != nil {
		return err
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
	n, err := fmt.Sscanf(valueString, "%v", &value)
	if n != 1 {
		return value, fmt.Errorf("error converting value to requested type")
	}

	if err != nil {
		return value, fmt.Errorf("error converting value to requested type - %w", err)
	}
	//}

	return value, nil
}

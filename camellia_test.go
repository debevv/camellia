package camellia

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"testing"
	"time"
)

var testDBPath string

func resetDB(t *testing.T) {
	err := os.Remove(testDBPath)
	var perr *fs.PathError
	if err != nil && !errors.As(err, &perr) {
		t.Fatal(err)
	}

	created, err := Init(testDBPath)
	if !created || err != nil {
		t.Fatal(err)
	}
}

func catchPanic[P any, R any](t *testing.T, p P, f func(P) R) (err error) {
	defer func() {
		r := recover()
		var ok bool
		err, ok = r.(error)
		if !ok {
			t.FailNow()
		}
	}()

	f(p)
	return
}

func TestMain(m *testing.M) {
	testDBFile, err := os.CreateTemp("", "camellia")
	if err != nil {
		os.Stderr.WriteString("Error creating test DB file")
		os.Exit(1)
	}

	testDBPath = testDBFile.Name()
	testDBFile.Close()

	ret := m.Run()
	os.RemoveAll(testDBPath)
	os.Exit(ret)
}

func TestSetGet(t *testing.T) {
	resetDB(t)

	t.Log("Should set a value")
	err := SetValue("/a/b/c/d", "d")
	if err != nil {
		t.FailNow()
	}

	t.Log("Should read the same value as the previously set one")
	v, err := GetValue[string]("/a/b/c/d")
	if err != nil {
		t.FailNow()
	}

	if v != "d" {
		t.FailNow()
	}

	v, err = GetValue[string]("a/b/c/d")
	if err != nil {
		t.FailNow()
	}

	if v != "d" {
		t.FailNow()
	}

	t.Log("Should return ErrPathIsNotAValue on getting the value of empty value (equals root path)")
	_, err = GetValue[string]("")
	if err != ErrPathIsNotAValue {
		t.FailNow()
	}

	t.Log("Should return ErrPathIsNotAValue on getting the value of root path")
	_, err = GetValue[string]("/")
	if err != ErrPathIsNotAValue {
		t.FailNow()
	}

	t.Log("Should return ErrPathInvalid on setting the value of empty path")
	err = SetValue("", "a")
	if err != ErrPathInvalid {
		t.FailNow()
	}

	err = ForceValue("", "a")
	if err != ErrPathInvalid {
		t.FailNow()
	}

	t.Log("Should return ErrPathInvalid on setting the value of root path")
	err = SetValue("/", "a")
	if err != ErrPathInvalid {
		t.FailNow()
	}

	err = ForceValue("/", "a")
	if err != ErrPathInvalid {
		t.FailNow()
	}

	t.Log("Should return ErrPathNotFound on getting value at non-existing path")
	_, err = GetValue[string]("/a/b/c/d/e")
	if err != ErrPathNotFound {
		t.FailNow()
	}

	_, err = GetValue[string]("/z")
	if err != ErrPathNotFound {
		t.FailNow()
	}

	t.Log("Should return ErrPathIsNotAValue on getting the value of an entry that is not a value")
	_, err = GetValue[string]("/a/b")
	if err != ErrPathIsNotAValue {
		t.FailNow()
	}

	resetDB(t)

	err = SetValue("/a1/b1/c1/d1", "d")
	if err != nil {
		t.FailNow()
	}

	err = SetValue("/a1/b1/c2/d1", "d")
	if err != nil {
		t.FailNow()
	}

	t.Log("Should overwrite a non-value entry with a value one, on user explicit choice")
	err = ForceValue("/a1/b1", "b")
	if err != nil {
		t.Fatal()
	}

	v, err = GetValue[string]("/a1/b1")
	if err != nil {
		t.FailNow()
	}

	if v != "b" {
		t.FailNow()
	}

	t.Log("Should delete the children of an overwritten non-value entry")
	_, err = GetValue[string]("/a1/b1/c1")
	if err != ErrPathNotFound {
		t.FailNow()
	}

	_, err = GetValue[string]("/a1/b1/c1/d1")
	if err != ErrPathNotFound {
		t.FailNow()
	}

	_, err = GetValue[string]("/a1/b1/c2")
	if err != ErrPathNotFound {
		t.FailNow()
	}

	_, err = GetValue[string]("/a1/b1/c2/d1")
	if err != ErrPathNotFound {
		t.FailNow()
	}

	t.Log("Should overwrite a value entry with a non-value one, on user explicit choice")
	err = ForceValue("/a1/b1/c1/d1", "d")
	if err != nil {
		t.Fatal()
	}

	v, err = GetValue[string]("/a1/b1/c1/d1")
	if err != nil {
		t.FailNow()
	}

	if v != "d" {
		t.FailNow()
	}

	t.Log("Should panic on GetValueOrPanic error")

	err = catchPanic(t, "/nonexisting", GetValueOrPanic[string])
	if !errors.Is(err, ErrPathNotFound) {
		t.FailNow()
	}

	t.Log("Should panic on getting empty value with GetValueOrPanicEmpty")

	err = SetValue("empty", "")
	if err != nil {
		t.FailNow()
	}

	err = catchPanic(t, "/empty", GetValueOrPanicEmpty[string])
	if !errors.Is(err, ErrValueEmpty) {
		t.FailNow()
	}
}

func TestDelete(t *testing.T) {
	resetDB(t)

	t.Log("Should delete an entry and all its children")

	err := SetValue("/a1/b1/c1/d1", "d")
	if err != nil {
		t.FailNow()
	}

	err = SetValue("/a1/b1/c2/d1", "d")
	if err != nil {
		t.FailNow()
	}

	err = DeleteEntry("a1/b1")
	if err != nil {
		t.FailNow()
	}

	e, err := Exists("a1")
	if err != nil || !e {
		t.FailNow()
	}

	e, err = Exists("a1/b1")
	if err != nil || e {
		t.FailNow()
	}

	e, err = Exists("a1/b1/c1")
	if err != nil || e {
		t.FailNow()
	}

	e, err = Exists("a1/b1/c1/d1")
	if err != nil || e {
		t.FailNow()
	}

	e, err = Exists("a1/b1/c2")
	if err != nil || e {
		t.FailNow()
	}

	e, err = Exists("a1/b1/c2/d1")
	if err != nil || e {
		t.FailNow()
	}

	t.Log("Should return ErrPathInvalid on deleting the entry on root path")
	err = DeleteEntry("/")
	if err != ErrPathInvalid {
		t.FailNow()
	}

	err = DeleteEntry("")
	if err != ErrPathInvalid {
		t.FailNow()
	}
}

/*
TODO: See api.go

type TestData struct {
	Prop1 string
	Prop2 int
	Prop3 bool
}

func (t *TestData) String() string {
	j, _ := json.Marshal(t)
	return string(j)
}

func (t *TestData) FromString(s string) error {
	return json.Unmarshal([]byte(s), t)
}
*/

func TestTypedSetGet(t *testing.T) {
	resetDB(t)

	err := SetValue("/v/string", "string")
	if err != nil {
		t.FailNow()
	}

	err = SetValue("/v/uint", 1234)
	if err != nil {
		t.FailNow()
	}

	err = SetValue("/v/int", -1234)
	if err != nil {
		t.FailNow()
	}

	err = SetValue("/v/float64", -1234.5678)
	if err != nil {
		t.FailNow()
	}

	err = SetValue("/v/bool", true)
	if err != nil {
		t.FailNow()
	}

	/*
		TODO: See api.go

		err = SetValue("/v/data", &TestData{Prop1: "Prop1", Prop2: 1234, Prop3: true})
		if err != nil {
			t.FailNow()
		}
	*/

	s, err := GetValue[string]("/v/string")
	if err != nil || s != "string" {
		t.FailNow()
	}

	u, err := GetValue[uint]("/v/uint")
	if err != nil || u != 1234 {
		t.FailNow()
	}

	i, err := GetValue[int]("/v/int")
	if err != nil || i != -1234 {
		t.FailNow()
	}

	f, err := GetValue[float64]("/v/float64")
	if err != nil || f != -1234.5678 {
		t.FailNow()
	}

	b, err := GetValue[bool]("/v/bool")
	if err != nil || !b {
		t.FailNow()
	}

	/*
		TODO: See api.go

		d, err := GetValue[TestData]("/v/data")
		if err != nil {
			t.FailNow()
		}

		if d.Prop1 != "Prop1" {
			t.FailNow()
		}

		if d.Prop2 != 1234 {
			t.FailNow()
		}

		if !d.Prop3 {
			t.FailNow()
		}

	*/
}

func TestToJSON(t *testing.T) {
	resetDB(t)

	t.Log("Should convert the root Entry to JSON")

	err := SetValue("/a1/b1/c1/d1", "d1")
	if err != nil {
		t.FailNow()
	}

	err = SetValue("/a1/b1/c1/d2", "d2")
	if err != nil {
		t.FailNow()
	}

	err = SetValue("/a1/b1/c2/d1", "d1")
	if err != nil {
		t.FailNow()
	}

	err = SetValue("/a1/b1/c2/d2", "d2")
	if err != nil {
		t.FailNow()
	}

	err = SetValue("/a2/b1", "b1")
	if err != nil {
		t.FailNow()
	}

	err = SetValue("/a3", "a3")
	if err != nil {
		t.FailNow()
	}

	j, err := EntryToJSON("")
	if err != nil {
		t.FailNow()
	}

	var entries Entry
	err = json.Unmarshal([]byte(j), &entries)
	if err != nil {
		t.FailNow()
	}

	if entries.Children["a1"] == nil ||
		entries.Children["a1"].Children["b1"] == nil ||
		entries.Children["a1"].Children["b1"].Children["c1"] == nil ||
		entries.Children["a1"].Children["b1"].Children["c1"].Children["d1"] == nil ||
		entries.Children["a1"].Children["b1"].Children["c1"].Children["d2"] == nil ||
		entries.Children["a1"].Children["b1"].Children["c2"].Children["d1"] == nil ||
		entries.Children["a2"].Children["b1"] == nil ||
		entries.Children["a3"] == nil {
		t.FailNow()
	}

	if entries.Children["a1"].Children["b1"].Children["c1"].Children["d1"].Value != "d1" {
		t.FailNow()
	}

	if entries.Children["a1"].Children["b1"].Children["c1"].Children["d2"].Value != "d2" {
		t.FailNow()
	}

	if entries.Children["a1"].Children["b1"].Children["c2"].Children["d1"].Value != "d1" {
		t.FailNow()
	}

	if entries.Children["a1"].Children["b1"].Children["c2"].Children["d2"].Value != "d2" {
		t.FailNow()
	}

	if entries.Children["a2"].Children["b1"].Value != "b1" {
		t.FailNow()
	}

	if entries.Children["a3"].Value != "a3" {
		t.FailNow()
	}

	t.Log("Should convert all the values from root Entry to JSON")

	resetDB(t)

	err = SetValue("/a1/b1/c1", "c1")
	if err != nil {
		t.FailNow()
	}

	err = SetValue("/a1/b1/c2", "c2")
	if err != nil {
		t.FailNow()
	}

	err = SetValue("/a1/b2/c1", "c1")
	if err != nil {
		t.FailNow()
	}

	j, err = ValuesToJSON("")
	if err != nil {
		t.FailNow()
	}

	ji := make(map[string]interface{})
	err = json.Unmarshal([]byte(j), &ji)
	if err != nil {
		t.FailNow()
	}

	jb, err := json.Marshal(ji)
	if err != nil {
		t.FailNow()
	}

	compare := make(map[string]map[string]map[string]interface{})
	compare["a1"] = make(map[string]map[string]interface{})
	compare["a1"]["b1"] = make(map[string]interface{})
	compare["a1"]["b1"]["c1"] = "c1"
	compare["a1"]["b1"]["c2"] = "c2"
	compare["a1"]["b2"] = make(map[string]interface{})
	compare["a1"]["b2"]["c1"] = "c1"

	jCompare, err := json.Marshal(&compare)
	if err != nil {
		t.FailNow()
	}

	if string(jb) != string(jCompare) {
		t.FailNow()
	}

}

func TestFromJson(t *testing.T) {

	t.Log("Should import values from JSON file")

	resetDB(t)

	j := `
{
	"a1": {
		"b1": {
			"c1": "c1",
			"c2": "c2"
		}
	},
	"a2": "a2"
}
`
	buf := bytes.Buffer{}
	buf.WriteString(j)

	err := SetValuesFromJSON(&buf, false)
	if err != nil {
		t.FailNow()
	}

	v, err := GetValue[string]("a1/b1/c1")
	if err != nil {
		t.FailNow()
	}

	if v != "c1" {
		t.FailNow()
	}

	v, err = GetValue[string]("a1/b1/c2")
	if err != nil {
		t.FailNow()
	}

	if v != "c2" {
		t.FailNow()
	}

	v, err = GetValue[string]("a2")
	if err != nil {
		t.FailNow()
	}

	if v != "a2" {
		t.FailNow()
	}

	t.Log("Should import entries from JSON file")

	resetDB(t)

	j = `
{
	"children": {
		"a1": {
			"children": {
				"b1": {
					"children": {
						"c1": {
							"value": "c1"
						},
						"c2": {
							"value": "c2"
						}
					}
				}
			}
		},
		"a2": {
			"value": "a2"
		}
	}
}
`

	buf = bytes.Buffer{}
	buf.WriteString(j)

	err = SetEntriesFromJSON(&buf, false)
	if err != nil {
		t.FailNow()
	}

	v, err = GetValue[string]("a1/b1/c1")
	if err != nil {
		t.FailNow()
	}

	if v != "c1" {
		t.FailNow()
	}

	v, err = GetValue[string]("a1/b1/c2")
	if err != nil {
		t.FailNow()
	}

	if v != "c2" {
		t.FailNow()
	}

	v, err = GetValue[string]("a2")
	if err != nil {
		t.FailNow()
	}

	if v != "a2" {
		t.FailNow()
	}

	t.Log("Should only merge values from JSON file")

	resetDB(t)

	j = `
{
	"e1": {
		"e2": "merged"
	},
	"n1": {
		"n2": "merged"
	}
}
`
	err = SetValue("e1/e2", "original")
	if err != nil {
		t.FailNow()
	}

	buf = bytes.Buffer{}
	buf.WriteString(j)

	err = SetValuesFromJSON(&buf, true)
	if err != nil {
		t.FailNow()
	}

	v, err = GetValue[string]("e1/e2")
	if err != nil {
		t.FailNow()
	}

	if v != "original" {
		t.FailNow()
	}

	v, err = GetValue[string]("n1/n2")
	if err != nil {
		t.FailNow()
	}

	if v != "merged" {
		t.FailNow()
	}

	t.Log("Should only merge entries from JSON file")

	resetDB(t)

	j = `
{
	"children": {
		"e1": {
			"children": {
				"e2": {
					"value": "merged"
				}
			}
		},
		"n1": {
			"children": {
				"n2": {
					"value": "merged"
				}
			}
		}
	}
}
`
	err = SetValue("e1/e2", "original")
	if err != nil {
		t.FailNow()
	}

	buf = bytes.Buffer{}
	buf.WriteString(j)

	err = SetEntriesFromJSON(&buf, true)
	if err != nil {
		t.FailNow()
	}

	v, err = GetValue[string]("e1/e2")
	if err != nil {
		t.FailNow()
	}

	if v != "original" {
		t.FailNow()
	}

	v, err = GetValue[string]("n1/n2")
	if err != nil {
		t.FailNow()
	}

	if v != "merged" {
		t.FailNow()
	}
}

func TestHooks(t *testing.T) {
	t.Log("Should call pre and post set hooks")

	resetDB(t)

	var path = "a/b/hook"
	const v = "called"

	err := SetValue(path, "a")
	if err != nil {
		t.FailNow()
	}

	var preCalled, postCalled bool

	SetPreSetHook(path, func(path, value string) error {
		if value != v {
			return fmt.Errorf("hook value is different")
		}

		preCalled = true

		return nil
	})

	SetPostSetHook(path, func(path, value string) error {
		if value != v {
			return fmt.Errorf("hook value is different")
		}

		postCalled = true

		return nil
	}, false)

	err = SetValue(path, v)
	if err != nil {
		t.FailNow()
	}

	if !preCalled || !postCalled {
		t.FailNow()
	}

	t.Log("Should call async post set hook")

	resetDB(t)

	path = "a/b/asyncHook"
	postCalled = false
	c := make(chan interface{})

	SetPostSetHook(path, func(path, value string) error {
		if value != v {
			return fmt.Errorf("hook value is different")
		}

		postCalled = true
		c <- nil

		return nil
	}, true)

	err = SetValue(path, v)
	if err != nil {
		t.FailNow()
	}

	timer := time.NewTimer(1 * time.Second)

	select {
	case <-timer.C:
	case <-c:
	}

	if !postCalled {
		t.FailNow()
	}

}

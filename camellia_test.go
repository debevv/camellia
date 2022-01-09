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

const currentDBVersion = 1

func resetDB(t *testing.T) {
	if Initialized() {
		_, err := Init("")
		if err != nil {
			t.FailNow()
		}

		err = Close()
		if err != nil {
			t.Fatal(err)
		}

		err = os.Remove(testDBPath)
		var perr *fs.PathError
		if err != nil && !errors.As(err, &perr) {
			t.Fatal(err)
		}
	}

	err := Close()
	if err != ErrNotInitialized {
		t.FailNow()
	}

	if Initialized() {
		t.FailNow()
	}

	if GetDBPath() != "" {
		t.FailNow()
	}

	_, err = Init("")
	if err == nil {
		t.FailNow()
	}

	created, err := Init(testDBPath)
	if !created || err != nil {
		t.Fatal(err)
	}

	if !Initialized() {
		t.FailNow()
	}

	if GetDBPath() != testDBPath {
		t.FailNow()
	}

	if GetSupportedDBVersion() != currentDBVersion {
		t.FailNow()
	}
}

func catchPanicGet[P any, R any](t *testing.T, p P, f func(P) R) (err error) {
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

func catchPanicSet[P any](t *testing.T, p1 P, p2 P, f func(P, P)) (err error) {
	defer func() {
		r := recover()
		var ok bool
		err, ok = r.(error)
		if !ok {
			t.FailNow()
		}
	}()

	f(p1, p2)
	return
}

func check(err error, t *testing.T) {
	if err != nil {
		t.Fatal(err)
	}
}

func TestMain(m *testing.M) {
	testDBFile, err := os.CreateTemp("", "camellia")
	if err != nil {
		os.Stderr.WriteString("Error creating test DB file")
		os.Exit(1)
	}

	testDBPath = testDBFile.Name()
	testDBFile.Close()

	_, err = Init(testDBPath)
	if err != nil {
		os.Exit(1)
	}

	ret := m.Run()

	err = Close()
	if err != nil {
		os.Exit(1)
	}

	os.RemoveAll(testDBPath)
	os.Exit(ret)
}

func TestSetGet(t *testing.T) {
	resetDB(t)

	t.Log("Should set a value")

	err := Set("////z////", "1")
	check(err, t)

	v, err := Get[string]("z")
	check(err, t)

	if v != "1" {
		t.FailNow()
	}

	v, err = Get[string]("//z///")
	check(err, t)

	if v != "1" {
		t.FailNow()
	}

	v, err = Get[string]("/z")
	check(err, t)

	if v != "1" {
		t.FailNow()
	}

	err = Set("y", "1")
	check(err, t)

	v, err = Get[string]("//y///")
	check(err, t)

	if v != "1" {
		t.FailNow()
	}

	v, err = Get[string]("y///")
	check(err, t)

	if v != "1" {
		t.FailNow()
	}

	err = Set("y", "2")
	check(err, t)

	v, err = Get[string]("//y///")
	check(err, t)

	if v != "2" {
		t.FailNow()
	}

	v, err = Get[string]("y///")
	check(err, t)

	if v != "2" {
		t.FailNow()
	}

	resetDB(t)

	err = Set("a/b///c/d", "d")
	check(err, t)

	t.Log("Should read the same value as the previously set one")
	v, err = Get[string]("/a/b/c///d/")
	check(err, t)

	if v != "d" {
		t.FailNow()
	}

	v, err = Get[string]("a/b/c/d")
	check(err, t)

	if v != "d" {
		t.FailNow()
	}

	t.Log("Should return ErrPathIsNotAValue on getting the value of empty value (equals root path)")
	_, err = Get[string]("")
	if err != ErrPathIsNotAValue {
		t.FailNow()
	}

	t.Log("Should return ErrPathIsNotAValue on getting the value of root path")
	_, err = Get[string]("/")
	if err != ErrPathIsNotAValue {
		t.FailNow()
	}

	t.Log("Should return ErrPathInvalid on setting the value of empty path")
	err = Set("", "a")
	if err != ErrPathInvalid {
		t.FailNow()
	}

	t.Log("Should return ErrPathInvalid on forcing the value of empty path")
	err = Force("", "a")
	if err != ErrPathInvalid {
		t.FailNow()
	}

	t.Log("Should return ErrPathIsNotAValue on setting the value of / path")
	err = Set("/", "a")
	if err != ErrPathInvalid {
		t.FailNow()
	}

	t.Log("Should return ErrPathInvalid on forcing the value of / path")
	err = Force("/", "a")
	if err != ErrPathInvalid {
		t.FailNow()
	}

	t.Log("Should return ErrPathNotFound on getting value at non-existing path")
	_, err = Get[string]("/a/b/c/d/e")
	if err != ErrPathNotFound {
		t.FailNow()
	}

	_, err = Get[string]("/z")
	if err != ErrPathNotFound {
		t.FailNow()
	}

	t.Log("Should return ErrPathIsNotAValue on getting the value of an entry that is not a value")
	_, err = Get[string]("/a/b")
	if err != ErrPathIsNotAValue {
		t.FailNow()
	}

	t.Log("Should return ErrPathIsNotAValue on setting the value of an entry that is not a value")
	err = Set("/a/b", "b")
	if err != ErrPathIsNotAValue {
		t.FailNow()
	}

	t.Log("Should overwrite a non-value entry with a value one, on user explicit choice")
	resetDB(t)

	err = Set("/a1/b1/c1/d1", "d")
	check(err, t)

	err = Set("/a1/b1/c2/d1", "d")
	check(err, t)

	err = Force("/a1/b1", "b")
	check(err, t)

	v, err = Get[string]("/a1/b1")
	check(err, t)

	if v != "b" {
		t.FailNow()
	}

	t.Log("Should delete the children of an overwritten non-value entry")
	_, err = Get[string]("/a1/b1/c1")
	if err != ErrPathNotFound {
		t.FailNow()
	}

	_, err = Get[string]("/a1/b1/c1/d1")
	if err != ErrPathNotFound {
		t.FailNow()
	}

	_, err = Get[string]("/a1/b1/c2")
	if err != ErrPathNotFound {
		t.FailNow()
	}

	_, err = Get[string]("/a1/b1/c2/d1")
	if err != ErrPathNotFound {
		t.FailNow()
	}

	t.Log("Should overwrite a value entry with a non-value one, on user explicit choice")
	err = Force("/a1/b1/c1/d1", "d")
	if err != nil {
		t.Fatal()
	}

	v, err = Get[string]("/a1/b1/c1/d1")
	check(err, t)

	if v != "d" {
		t.FailNow()
	}

	t.Log("Should panic on GetValueOrPanic error")

	err = catchPanicGet(t, "/nonexisting", GetOrPanic[string])
	if !errors.Is(err, ErrPathNotFound) {
		t.FailNow()
	}

	t.Log("Should panic on getting empty value with GetValueOrPanicEmpty")

	err = Set("/empty", "")
	check(err, t)

	err = catchPanicGet(t, "/empty", GetOrPanicEmpty[string])
	if !errors.Is(err, ErrValueEmpty) {
		t.FailNow()
	}

	t.Log("Should panic with error on SetValueOrPanic")

	err = catchPanicSet(t, "", "error", SetOrPanic[string])
	if !errors.Is(err, ErrPathInvalid) {
		t.FailNow()
	}

	t.Log("Should panic with error on ForceValueOrPanic")

	err = catchPanicSet(t, "", "error", ForceOrPanic[string])
	if !errors.Is(err, ErrPathInvalid) {
		t.FailNow()
	}

	t.Log("Should get an entry and all of its children")
	resetDB(t)

	err = Set("/a1/b1/c1/d1", "d")
	check(err, t)

	err = Set("/a1/b1/c2/d1", "d")
	check(err, t)

	a1, err := GetEntry("/a1")
	check(err, t)

	if a1.Children["b1"] == nil {
		t.FailNow()
	}

	if a1.Children["b1"].Children["c1"] == nil {
		t.FailNow()
	}

	if a1.Children["b1"].Children["c2"] == nil {
		t.FailNow()
	}

	if a1.Children["b1"].Children["c1"].Children["d1"] == nil {
		t.FailNow()
	}

	if a1.Children["b1"].Children["c2"].Children["d1"] == nil {
		t.FailNow()
	}

	t.Log("Should and entry and it children until a certain depth")
	a1, err = GetEntryDepth("/a1", 0)
	check(err, t)

	if len(a1.Children) > 0 {
		t.FailNow()
	}

	a1, err = GetEntryDepth("/a1", 1)
	check(err, t)

	if a1.Children["b1"] == nil {
		t.FailNow()
	}

	if len(a1.Children["b1"].Children) > 0 {
		t.FailNow()
	}

	t.Log("Should update LastUpdate timestamp of an Entry when creating a child")
	resetDB(t)

	SetOrPanic("a1/b1/c1", "c1")
	b1, err := GetEntry("a1/b1")
	check(err, t)

	oldTs := b1.LastUpdate

	SetOrPanic("a1/b1/c2", "c2")

	b1, err = GetEntry("a1/b1")
	check(err, t)

	if b1.LastUpdate == oldTs {
		t.FailNow()
	}

	t.Log("Should update LastUpdate timestamp of an Entry when deleting a child")
	resetDB(t)

	SetOrPanic("a1/b1/c1", "c1")
	SetOrPanic("a1/b1/c2", "c2")
	b1, err = GetEntry("a1/b1")
	check(err, t)

	oldTs = b1.LastUpdate

	err = Delete("a1/b1/c2")
	check(err, t)

	b1, err = GetEntry("a1/b1")
	check(err, t)

	if b1.LastUpdate == oldTs {
		t.FailNow()
	}
}

func TestDelete(t *testing.T) {
	resetDB(t)

	t.Log("Should delete an entry and all its children")

	err := Set("/a1/b1/c1/d1", "d")
	check(err, t)

	err = Set("/a1/b1/c2/d1", "d")
	check(err, t)

	err = Delete("a1/b1")
	check(err, t)

	e, err := Exists("a1")
	check(err, t)
	if !e {
		t.FailNow()
	}

	e, err = Exists("a1/b1")
	check(err, t)
	if e {
		t.FailNow()
	}

	e, err = Exists("a1/b1/c1")
	check(err, t)
	if e {
		t.FailNow()
	}

	e, err = Exists("a1/b1/c1/d1")
	check(err, t)
	if e {
		t.FailNow()
	}

	e, err = Exists("a1/b1/c2")
	check(err, t)
	if e {
		t.FailNow()
	}

	e, err = Exists("a1/b1/c2/d1")
	check(err, t)
	if e {
		t.FailNow()
	}

	t.Log("Should return ErrPathInvalid on deleting the entry on root path")
	err = Delete("/")
	if err != ErrPathInvalid {
		t.FailNow()
	}

	err = Delete("")
	if err != ErrPathInvalid {
		t.FailNow()
	}

	t.Log("Should wipe the DB")

	resetDB(t)

	err = Set("/a1/b1/c1/d1", "d1")
	check(err, t)

	err = Set("/a1/b2/c1", "c1")
	check(err, t)

	err = Set("/a2/b1", "b1")
	check(err, t)

	err = Wipe()
	check(err, t)

	root, err := GetEntry("")
	check(err, t)

	if len(root.Children) != 0 {
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

	err := Set("/v/string", "string")
	check(err, t)

	err = Set("/v/uint", 1234)
	check(err, t)

	err = Set("/v/int", -1234)
	check(err, t)

	err = Set("/v/float64", -1234.5678)
	check(err, t)

	err = Set("/v/bool", true)
	check(err, t)

	/*
		TODO: See api.go

		err = SetValue("/v/data", &TestData{Prop1: "Prop1", Prop2: 1234, Prop3: true})
		if err != nil {
			t.FailNow()
		}
	*/

	s, err := Get[string]("/v/string")
	check(err, t)
	if s != "string" {
		t.FailNow()
	}

	u, err := Get[uint]("/v/uint")
	check(err, t)
	if u != 1234 {
		t.FailNow()
	}

	i, err := Get[int]("/v/int")
	check(err, t)
	if i != -1234 {
		t.FailNow()
	}

	f, err := Get[float64]("/v/float64")
	check(err, t)
	if f != -1234.5678 {
		t.FailNow()
	}

	b, err := Get[bool]("/v/bool")
	check(err, t)
	if !b {
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

func TestRecurse(t *testing.T) {
	t.Log("Should recurse on an entry and on all of its children")
	resetDB(t)

	err := Set("/a1/b1/c1/d1", "d")
	check(err, t)

	err = Set("/a1/b1/c2/d1", "d")
	check(err, t)

	err = Set("/a2", "a")
	check(err, t)

	visited := map[string]bool{}

	err = Recurse("/a1", -1, func(entry, parent *Entry, depth uint) error {
		visited[entry.Path] = true
		return nil
	})

	check(err, t)

	if len(visited) != 6 {
		t.FailNow()
	}

	if !visited["a1"] || !visited["a1/b1"] || !visited["a1/b1/c1"] || !visited["a1/b1/c1/d1"] ||
		!visited["a1/b1/c2"] || !visited["a1/b1/c2/d1"] {
		t.FailNow()
	}

	t.Log("Should recurse on an entry and on all of its children until a certain depth")
	resetDB(t)

	err = Set("/a1/b1/c1/d1", "d")
	check(err, t)

	err = Set("/a1/b1/c2/d1", "d")
	check(err, t)

	err = Set("/a2", "a")
	check(err, t)

	visited = map[string]bool{}

	err = Recurse("/a1", 1, func(entry, parent *Entry, depth uint) error {
		visited[entry.Path] = true
		return nil
	})

	check(err, t)

	if len(visited) != 2 {
		t.FailNow()
	}

	if !visited["a1"] || !visited["a1/b1"] {
		t.FailNow()
	}

	t.Log("Should report the error of the recurse callback")
	resetDB(t)

	err = Set("a2", "a")
	check(err, t)

	myError := fmt.Errorf("error1234")

	err = Recurse("a2", 1, func(entry, parent *Entry, depth uint) error {
		return myError
	})

	if !errors.Is(err, myError) {
		t.FailNow()
	}
}

func TestToJSON(t *testing.T) {
	resetDB(t)

	t.Log("Should convert the root Entry to JSON")

	err := Set("/a1/b1/c1/d1", "d1")
	check(err, t)

	err = Set("/a1/b1/c1/d2", "d2")
	check(err, t)

	err = Set("/a1/b1/c2/d1", "d1")
	check(err, t)

	err = Set("/a1/b1/c2/d2", "d2")
	check(err, t)

	err = Set("/a2/b1", "b1")
	check(err, t)

	err = Set("/a3", "a3")
	check(err, t)

	j, err := EntryToJSON("")
	check(err, t)

	var entries Entry
	err = json.Unmarshal([]byte(j), &entries)
	check(err, t)

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

	err = Set("/a1/b1/c1", "c1")
	check(err, t)

	err = Set("/a1/b1/c2", "c2")
	check(err, t)

	err = Set("/a1/b2/c1", "c1")
	check(err, t)

	j, err = ValuesToJSON("")
	check(err, t)

	ji := make(map[string]interface{})
	err = json.Unmarshal([]byte(j), &ji)
	check(err, t)

	jb, err := json.Marshal(ji)
	check(err, t)

	compare := make(map[string]map[string]map[string]interface{})
	compare["a1"] = make(map[string]map[string]interface{})
	compare["a1"]["b1"] = make(map[string]interface{})
	compare["a1"]["b1"]["c1"] = "c1"
	compare["a1"]["b1"]["c2"] = "c2"
	compare["a1"]["b2"] = make(map[string]interface{})
	compare["a1"]["b2"]["c1"] = "c1"

	jCompare, err := json.Marshal(&compare)
	check(err, t)

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
		},
		"b2": "overwritten"
	},
	"a2": "a2"
}
`
	buf := bytes.Buffer{}
	buf.WriteString(j)

	err := Set("a1/b2", "original")
	check(err, t)

	err = Set("a1/b3", "original")
	check(err, t)

	err = SetValuesFromJSON(&buf, false)
	check(err, t)

	v, err := Get[string]("a1/b1/c1")
	check(err, t)

	if v != "c1" {
		t.FailNow()
	}

	v, err = Get[string]("a1/b1/c2")
	check(err, t)

	if v != "c2" {
		t.FailNow()
	}

	v, err = Get[string]("a2")
	check(err, t)

	if v != "a2" {
		t.FailNow()
	}

	v, err = Get[string]("a1/b2")
	check(err, t)

	if v != "overwritten" {
		t.FailNow()
	}

	v, err = Get[string]("a1/b3")
	check(err, t)

	if v != "original" {
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
	check(err, t)

	v, err = Get[string]("a1/b1/c1")
	check(err, t)
	if v != "c1" {
		t.FailNow()
	}

	v, err = Get[string]("a1/b1/c2")
	check(err, t)
	if v != "c2" {
		t.FailNow()
	}

	v, err = Get[string]("a2")
	check(err, t)
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
	err = Set("e1/e2", "original")
	check(err, t)

	buf = bytes.Buffer{}
	buf.WriteString(j)

	err = SetValuesFromJSON(&buf, true)
	check(err, t)

	v, err = Get[string]("e1/e2")
	check(err, t)
	if v != "original" {
		t.FailNow()
	}

	v, err = Get[string]("n1/n2")
	check(err, t)
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
	err = Set("e1/e2", "original")
	check(err, t)

	buf = bytes.Buffer{}
	buf.WriteString(j)

	err = SetEntriesFromJSON(&buf, true)
	check(err, t)

	v, err = Get[string]("e1/e2")
	check(err, t)
	if v != "original" {
		t.FailNow()
	}

	v, err = Get[string]("n1/n2")
	check(err, t)
	if v != "merged" {
		t.FailNow()
	}
}

func testHooks(t *testing.T, shouldBeCalled bool) {
	resetDB(t)

	wipeHooks()

	var p = "a/b/hook"
	const v = "called"

	err := Set(p, "a")
	check(err, t)

	var preCalled, postCalled bool

	SetPreSetHook(p, func(path, value string) error {
		if value != v {
			return fmt.Errorf("hook value is different")
		}

		if path != p {
			return fmt.Errorf("hook path is different")
		}

		preCalled = true

		return nil
	})

	SetPostSetHook(p, func(path, value string) error {
		if value != v {
			return fmt.Errorf("hook value is different")
		}

		if path != p {
			return fmt.Errorf("hook path is different")
		}

		postCalled = true

		return nil
	}, false)

	err = Set(p, v)
	check(err, t)

	if shouldBeCalled != preCalled || shouldBeCalled != postCalled {
		t.FailNow()
	}

	t.Log("Should call async post set hook")

	resetDB(t)

	p = "a/b/asyncHook"
	postCalled = false
	c := make(chan interface{})

	SetPostSetHook(p, func(path, value string) error {
		if value != v {
			return fmt.Errorf("hook value is different")
		}

		if path != p {
			return fmt.Errorf("hook path is different")
		}

		postCalled = true
		c <- nil

		return nil
	}, true)

	err = Set(p, v)
	check(err, t)

	timer := time.NewTimer(1 * time.Second)

	select {
	case <-timer.C:
	case <-c:
	}

	if shouldBeCalled != postCalled {
		t.FailNow()
	}

}

func TestHooks(t *testing.T) {
	t.Log("Should call hooks by default")
	testHooks(t, true)

	t.Log("Should not call hooks")
	SetHooksEnabled(false)
	testHooks(t, false)

	t.Log("Should call hooks")
	SetHooksEnabled(true)
	testHooks(t, true)
}

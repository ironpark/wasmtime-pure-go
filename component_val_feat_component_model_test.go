package wasmtime

import (
	"reflect"
	"testing"
)

func TestComponentValRoundTrip(t *testing.T) {
	store := NewStore(NewEngine())
	s := func(v string) ComponentVal { return ComponentVal{Kind: ComponentValKindString, Str: v} }
	for _, v := range []ComponentVal{
		{Kind: ComponentValKindBool, Bool: true},
		{Kind: ComponentValKindS8, Int: -5},
		{Kind: ComponentValKindU64, Int: 1 << 40},
		{Kind: ComponentValKindF64, Float: 1.5},
		{Kind: ComponentValKindChar, Int: '가'},
		s("hello"),
		s(""),
		{Kind: ComponentValKindList, Elems: []ComponentVal{s("a"), s("b")}},
		{Kind: ComponentValKindTuple, Elems: []ComponentVal{s("k"), {Kind: ComponentValKindU8, Int: 7}}},
		{Kind: ComponentValKindRecord, Names: []string{"command", "args"}, Elems: []ComponentVal{
			s("node"), {Kind: ComponentValKindList, Elems: []ComponentVal{s("x")}}}},
		{Kind: ComponentValKindFlags, Names: []string{"read", "write"}},
		{Kind: ComponentValKindVariant, Str: "some-case", Payload: &ComponentVal{Kind: ComponentValKindS32, Int: 3}},
		{Kind: ComponentValKindVariant, Str: "empty"},
		{Kind: ComponentValKindEnum, Str: "mac"},
		{Kind: ComponentValKindOption},
		{Kind: ComponentValKindOption, Payload: &ComponentVal{Kind: ComponentValKindString, Str: "x"}},
		{Kind: ComponentValKindResult, Ok: true},
		{Kind: ComponentValKindResult, Payload: &ComponentVal{Kind: ComponentValKindString, Str: "failed"}},
	} {
		var c wasmtime_component_val_t
		if err := writeComponentVal(store.Context(), &c, v); err != nil {
			t.Fatalf("write %+v: %v", v, err)
		}
		got := readComponentVal(&c)
		wasmtime_component_val_delete(&c)
		if !reflect.DeepEqual(got, v) {
			t.Errorf("round trip of %+v gave %+v", v, got)
		}
	}
}

func TestComponentValBadRecordFails(t *testing.T) {
	store := NewStore(NewEngine())
	var c wasmtime_component_val_t
	err := writeComponentVal(store.Context(), &c, ComponentVal{Kind: ComponentValKindRecord, Names: []string{"a"}})
	if err == nil || c.kind != WASMTIME_COMPONENT_BOOL {
		t.Fatalf("err = %v, kind %d; want an error and a bool left", err, c.kind)
	}
}

package wasmtime

import (
	"errors"
	"fmt"
	"unsafe"
)

// ComponentValKind is the kind of a [ComponentVal], as Wasmtime numbers
// them.
type ComponentValKind uint8

const (
	ComponentValKindBool     ComponentValKind = WASMTIME_COMPONENT_BOOL
	ComponentValKindS8       ComponentValKind = WASMTIME_COMPONENT_S8
	ComponentValKindU8       ComponentValKind = WASMTIME_COMPONENT_U8
	ComponentValKindS16      ComponentValKind = WASMTIME_COMPONENT_S16
	ComponentValKindU16      ComponentValKind = WASMTIME_COMPONENT_U16
	ComponentValKindS32      ComponentValKind = WASMTIME_COMPONENT_S32
	ComponentValKindU32      ComponentValKind = WASMTIME_COMPONENT_U32
	ComponentValKindS64      ComponentValKind = WASMTIME_COMPONENT_S64
	ComponentValKindU64      ComponentValKind = WASMTIME_COMPONENT_U64
	ComponentValKindF32      ComponentValKind = WASMTIME_COMPONENT_F32
	ComponentValKindF64      ComponentValKind = WASMTIME_COMPONENT_F64
	ComponentValKindChar     ComponentValKind = WASMTIME_COMPONENT_CHAR
	ComponentValKindString   ComponentValKind = WASMTIME_COMPONENT_STRING
	ComponentValKindList     ComponentValKind = WASMTIME_COMPONENT_LIST
	ComponentValKindRecord   ComponentValKind = WASMTIME_COMPONENT_RECORD
	ComponentValKindTuple    ComponentValKind = WASMTIME_COMPONENT_TUPLE
	ComponentValKindVariant  ComponentValKind = WASMTIME_COMPONENT_VARIANT
	ComponentValKindEnum     ComponentValKind = WASMTIME_COMPONENT_ENUM
	ComponentValKindOption   ComponentValKind = WASMTIME_COMPONENT_OPTION
	ComponentValKindResult   ComponentValKind = WASMTIME_COMPONENT_RESULT
	ComponentValKindFlags    ComponentValKind = WASMTIME_COMPONENT_FLAGS
	ComponentValKindResource ComponentValKind = WASMTIME_COMPONENT_RESOURCE
)

// ComponentVal is a component model value, copied out of Wasmtime or to be
// copied into it. Which fields mean something follows Kind:
//
//   - integers and chars: Int; bools: Bool
//   - floats: Float
//   - string, enum: Str
//   - list, tuple: Elems
//   - record: Elems, named by Names
//   - flags: Names, the flags set
//   - variant: Str is the case, Payload its value or nil
//   - option: Payload, nil for none
//   - result: Ok, and Payload its value or nil
//   - resource: Resource
//
// Maps are not supported.
type ComponentVal struct {
	Kind     ComponentValKind
	Bool     bool
	Int      int64
	Float    float64
	Str      string
	Elems    []ComponentVal
	Names    []string
	Payload  *ComponentVal
	Ok       bool
	Resource *ComponentResource
}

// ComponentResource is a resource handle in a [ComponentVal].
//
// To pass a resource of the host's own, set Rep and Type: it is handed over
// as an `own` the guest may borrow from, and dropped once the call it is an
// argument of returns. One read from Wasmtime, an argument of a host
// function or a result, is the handle Wasmtime gave, valid while that call
// lasts; [ComponentResource.ToHost] reads it as the host's.
type ComponentResource struct {
	Rep, Type uint32
	any       *wasmtime_component_resource_any_t
}

// Owned reports whether a resource read from Wasmtime is an `own`, rather
// than a `borrow`.
func (r *ComponentResource) Owned() bool {
	return r.any != nil && wasmtime_component_resource_any_owned(r.any)
}

// ToHost reads a resource read from Wasmtime as a host resource: its rep
// and the type it was defined with ([ComponentLinkerInstance.AddResource]).
// It fails for a resource the guest defined.
func (r *ComponentResource) ToHost(store Storelike) (rep, ty uint32, err error) {
	if r == nil || r.any == nil {
		return 0, 0, errors.New("wasmtime: not a resource read from a call")
	}
	var host *wasmtime_component_resource_host_t
	if e := wasmtime_component_resource_any_to_host(store.Context(), r.any, &host); e != nil {
		return 0, 0, mkError(e)
	}
	defer wasmtime_component_resource_host_delete(host)
	return wasmtime_component_resource_host_rep(host), wasmtime_component_resource_host_type(host), nil
}

func componentUnion[T any](v *wasmtime_component_val_t) *T {
	return (*T)(unsafe.Pointer(&v.of))
}

// componentValAt is the i'th value of an array of them at p.
func componentValAt(p *wasmtime_component_val_t, i int) *wasmtime_component_val_t {
	return (*wasmtime_component_val_t)(unsafe.Add(unsafe.Pointer(p), uintptr(i)*unsafe.Sizeof(*p)))
}

func byteVecString(n *wasm_byte_vec_t) string { return goStringN(n.data, int(n.size)) }

// setByteVec makes n an owned copy of s.
func setByteVec(n *wasm_byte_vec_t, s string) {
	wasm_byte_vec_new(n, uintptr(len(s)), goStrPtr(s))
}

// readComponentVal copies a value out of Wasmtime's memory.
func readComponentVal(p *wasmtime_component_val_t) ComponentVal {
	v := ComponentVal{Kind: ComponentValKind(p.kind)}
	switch v.Kind {
	case ComponentValKindBool:
		v.Bool = *componentUnion[bool](p)
	case ComponentValKindS8:
		v.Int = int64(*componentUnion[int8](p))
	case ComponentValKindU8:
		v.Int = int64(*componentUnion[uint8](p))
	case ComponentValKindS16:
		v.Int = int64(*componentUnion[int16](p))
	case ComponentValKindU16:
		v.Int = int64(*componentUnion[uint16](p))
	case ComponentValKindS32:
		v.Int = int64(*componentUnion[int32](p))
	case ComponentValKindU32, ComponentValKindChar:
		v.Int = int64(*componentUnion[uint32](p))
	case ComponentValKindS64:
		v.Int = *componentUnion[int64](p)
	case ComponentValKindU64:
		v.Int = int64(*componentUnion[uint64](p))
	case ComponentValKindF32:
		v.Float = float64(*componentUnion[float32](p))
	case ComponentValKindF64:
		v.Float = *componentUnion[float64](p)
	case ComponentValKindString, ComponentValKindEnum:
		v.Str = byteVecString(componentUnion[wasm_byte_vec_t](p))
	case ComponentValKindList, ComponentValKindTuple:
		l := componentUnion[wasmtime_component_vallist_t](p)
		for i := range int(l.size) {
			v.Elems = append(v.Elems, readComponentVal(componentValAt(l.data, i)))
		}
	case ComponentValKindRecord:
		r := componentUnion[wasmtime_component_valrecord_t](p)
		for _, e := range unsafe.Slice(r.data, r.size) {
			v.Names = append(v.Names, byteVecString(&e.name))
			v.Elems = append(v.Elems, readComponentVal(&e.val))
		}
	case ComponentValKindFlags:
		f := componentUnion[wasmtime_component_valflags_t](p)
		for _, n := range unsafe.Slice(f.data, f.size) {
			v.Names = append(v.Names, byteVecString(&n))
		}
	case ComponentValKindVariant:
		vr := componentUnion[wasmtime_component_valvariant_t](p)
		v.Str = byteVecString(&vr.discriminant)
		v.Payload = readComponentPayload(vr.val)
	case ComponentValKindOption:
		v.Payload = readComponentPayload(*componentUnion[*wasmtime_component_val_t](p))
	case ComponentValKindResult:
		r := componentUnion[wasmtime_component_valresult_t](p)
		v.Ok = r.is_ok
		v.Payload = readComponentPayload(r.val)
	case ComponentValKindResource:
		v.Resource = &ComponentResource{any: *componentUnion[*wasmtime_component_resource_any_t](p)}
	}
	return v
}

func readComponentPayload(p *wasmtime_component_val_t) *ComponentVal {
	if p == nil {
		return nil
	}
	v := readComponentVal(p)
	return &v
}

// writeComponentVal builds v in place at p, which holds nothing that needs
// freeing. Everything is allocated through Wasmtime's own constructors, so
// that Wasmtime can free it once the value is handed over. On an error p
// is left a bool, which owns nothing, and what was built so far is freed.
func writeComponentVal(ctx *wasmtime_context_t, p *wasmtime_component_val_t, v ComponentVal) (err error) {
	defer func() {
		if err != nil {
			wasmtime_component_val_delete(p)
			*p = wasmtime_component_val_t{kind: WASMTIME_COMPONENT_BOOL}
		}
	}()
	*p = wasmtime_component_val_t{kind: uint8(v.Kind)}
	switch v.Kind {
	case ComponentValKindBool:
		*componentUnion[bool](p) = v.Bool
	case ComponentValKindS8:
		*componentUnion[int8](p) = int8(v.Int)
	case ComponentValKindU8:
		*componentUnion[uint8](p) = uint8(v.Int)
	case ComponentValKindS16:
		*componentUnion[int16](p) = int16(v.Int)
	case ComponentValKindU16:
		*componentUnion[uint16](p) = uint16(v.Int)
	case ComponentValKindS32:
		*componentUnion[int32](p) = int32(v.Int)
	case ComponentValKindU32, ComponentValKindChar:
		*componentUnion[uint32](p) = uint32(v.Int)
	case ComponentValKindS64:
		*componentUnion[int64](p) = v.Int
	case ComponentValKindU64:
		*componentUnion[uint64](p) = uint64(v.Int)
	case ComponentValKindF32:
		*componentUnion[float32](p) = float32(v.Float)
	case ComponentValKindF64:
		*componentUnion[float64](p) = v.Float
	case ComponentValKindString, ComponentValKindEnum:
		setByteVec(componentUnion[wasm_byte_vec_t](p), v.Str)
	case ComponentValKindList, ComponentValKindTuple:
		// A tuple's vector is laid out as a list's.
		l := componentUnion[wasmtime_component_vallist_t](p)
		wasmtime_component_vallist_new_uninit(l, uintptr(len(v.Elems)))
		for i := range int(l.size) {
			*componentValAt(l.data, i) = wasmtime_component_val_t{kind: WASMTIME_COMPONENT_BOOL}
		}
		for i, e := range v.Elems {
			if err := writeComponentVal(ctx, componentValAt(l.data, i), e); err != nil {
				return err
			}
		}
	case ComponentValKindRecord:
		if len(v.Names) != len(v.Elems) {
			return fmt.Errorf("wasmtime: a record of %d names and %d values", len(v.Names), len(v.Elems))
		}
		r := componentUnion[wasmtime_component_valrecord_t](p)
		wasmtime_component_valrecord_new_uninit(r, uintptr(len(v.Elems)))
		entries := unsafe.Slice(r.data, r.size)
		for i := range entries {
			entries[i] = wasmtime_component_valrecord_entry_t{val: wasmtime_component_val_t{kind: WASMTIME_COMPONENT_BOOL}}
			wasm_byte_vec_new_empty(&entries[i].name)
		}
		for i, e := range v.Elems {
			wasm_byte_vec_delete(&entries[i].name)
			setByteVec(&entries[i].name, v.Names[i])
			if err := writeComponentVal(ctx, &entries[i].val, e); err != nil {
				return err
			}
		}
	case ComponentValKindFlags:
		f := componentUnion[wasmtime_component_valflags_t](p)
		wasmtime_component_valflags_new_uninit(f, uintptr(len(v.Names)))
		for i, n := range v.Names {
			setByteVec(&unsafe.Slice(f.data, f.size)[i], n)
		}
	case ComponentValKindVariant:
		vr := componentUnion[wasmtime_component_valvariant_t](p)
		setByteVec(&vr.discriminant, v.Str)
		vr.val, err = newComponentPayload(ctx, v.Payload)
	case ComponentValKindOption:
		*componentUnion[*wasmtime_component_val_t](p), err = newComponentPayload(ctx, v.Payload)
	case ComponentValKindResult:
		r := componentUnion[wasmtime_component_valresult_t](p)
		r.is_ok = v.Ok
		r.val, err = newComponentPayload(ctx, v.Payload)
	case ComponentValKindResource:
		res := v.Resource
		if res == nil {
			return errors.New("wasmtime: a resource value without a resource")
		}
		var any *wasmtime_component_resource_any_t
		if res.any != nil {
			any = wasmtime_component_resource_any_clone(res.any)
		} else {
			host := wasmtime_component_resource_host_new(true, res.Rep, res.Type)
			e := wasmtime_component_resource_host_to_any(ctx, host, &any)
			wasmtime_component_resource_host_delete(host)
			if e != nil {
				*p = wasmtime_component_val_t{kind: WASMTIME_COMPONENT_BOOL}
				return mkError(e)
			}
		}
		*componentUnion[*wasmtime_component_resource_any_t](p) = any
	default:
		*p = wasmtime_component_val_t{kind: WASMTIME_COMPONENT_BOOL}
		return fmt.Errorf("wasmtime: cannot write a component value of kind %d", v.Kind)
	}
	return err
}

// newComponentPayload is v on Wasmtime's heap, for an option, result or
// variant to point at; nil for none.
func newComponentPayload(ctx *wasmtime_context_t, v *ComponentVal) (*wasmtime_component_val_t, error) {
	if v == nil {
		return nil, nil
	}
	var tmp wasmtime_component_val_t
	if err := writeComponentVal(ctx, &tmp, *v); err != nil {
		return nil, err
	}
	// val_new moves tmp's contents to the heap.
	return wasmtime_component_val_new(&tmp), nil
}

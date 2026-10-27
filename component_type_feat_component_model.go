package wasmtime

import (
	"runtime"
	"unsafe"
)

// ComponentType describes the type of a [Component] — its lists of imports
// and exports. Walk the lists with [ComponentType.ImportNth] and
// [ComponentType.ExportNth]; each entry is a [ComponentItem] whose
// discriminator identifies the kind of item it carries.
//
// A ComponentType is independently owned — closing the parent [Component]
// does not invalidate it. Close it explicitly (or leave it to the
// finalizer) when finished.
type ComponentType struct {
	_ptr *wasmtime_component_type_t

	// engine is the [Engine] the parent [Component] was compiled with. The
	// type uses the engine for the import / export queries below, so the
	// engine must outlive the type. Held privately because exposing it does
	// not enable any user-facing capability that the [Component] does not
	// already provide.
	engine *Engine
}

func mkComponentType(ptr *wasmtime_component_type_t, engine *Engine) *ComponentType {
	ct := &ComponentType{_ptr: ptr, engine: engine}
	runtime.SetFinalizer(ct, func(ct *ComponentType) {
		ct.Close()
	})
	return ct
}

func (ct *ComponentType) ptr() *wasmtime_component_type_t {
	ret := ct._ptr
	if ret == nil {
		panic("object has been closed already")
	}
	maybeGC()
	return ret
}

// ImportCount returns the number of imports declared by this component.
func (ct *ComponentType) ImportCount() int {
	n := wasmtime_component_type_import_count(ct.ptr(), ct.engine.ptr())
	runtime.KeepAlive(ct)
	runtime.KeepAlive(ct.engine)
	return int(n)
}

// ExportCount returns the number of exports declared by this component.
func (ct *ComponentType) ExportCount() int {
	n := wasmtime_component_type_export_count(ct.ptr(), ct.engine.ptr())
	runtime.KeepAlive(ct)
	runtime.KeepAlive(ct.engine)
	return int(n)
}

// ImportNth returns the name and the [ComponentItem] for the `i`-th import.
// Returns `("", nil)` if `i` is out of range.
func (ct *ComponentType) ImportNth(i int) (string, *ComponentItem) {
	return ct.itemNth(i, true)
}

// ExportNth returns the name and the [ComponentItem] for the `i`-th export.
// Returns `("", nil)` if `i` is out of range.
func (ct *ComponentType) ExportNth(i int) (string, *ComponentItem) {
	return ct.itemNth(i, false)
}

func (ct *ComponentType) itemNth(i int, isImport bool) (string, *ComponentItem) {
	var nameP *byte
	var nameLen uintptr
	var extern *wasmtime_component_extern_t
	var item wasmtime_component_item_t
	var found bool
	if isImport {
		found = wasmtime_component_type_import_nth(
			ct.ptr(), ct.engine.ptr(), uintptr(i),
			&nameP, &nameLen, &extern)
	} else {
		found = wasmtime_component_type_export_nth(
			ct.ptr(), ct.engine.ptr(), uintptr(i),
			&nameP, &nameLen, &extern)
	}
	runtime.KeepAlive(ct)
	runtime.KeepAlive(ct.engine)
	if !bool(found) {
		return "", nil
	}
	name := goStringN(nameP, int(nameLen))
	wasmtime_component_extern_type(extern, &item)
	wasmtime_component_extern_delete(extern)
	return name, mkComponentItem(item, ct.engine)
}

// Close deallocates this component type explicitly.
func (ct *ComponentType) Close() {
	if ct._ptr == nil {
		return
	}
	runtime.SetFinalizer(ct, nil)
	wasmtime_component_type_delete(ct._ptr)
	ct._ptr = nil
}

// ComponentItemKind discriminates which sub-type a [ComponentItem] carries.
type ComponentItemKind uint8

const (
	ComponentItemKindComponent         ComponentItemKind = WASMTIME_COMPONENT_ITEM_COMPONENT
	ComponentItemKindComponentInstance ComponentItemKind = WASMTIME_COMPONENT_ITEM_COMPONENT_INSTANCE
	ComponentItemKindModule            ComponentItemKind = WASMTIME_COMPONENT_ITEM_MODULE
	ComponentItemKindComponentFunc     ComponentItemKind = WASMTIME_COMPONENT_ITEM_COMPONENT_FUNC
	ComponentItemKindResource          ComponentItemKind = WASMTIME_COMPONENT_ITEM_RESOURCE
	ComponentItemKindCoreFunc          ComponentItemKind = WASMTIME_COMPONENT_ITEM_CORE_FUNC
	ComponentItemKindType              ComponentItemKind = WASMTIME_COMPONENT_ITEM_TYPE
)

// ComponentItem is one entry in a component's import or export list.
//
// The struct is a discriminated union: a `kind` tag plus a payload that
// depends on the tag. This release exposes a payload accessor for type-
// alias entries via [ComponentItem.TypeAlias]. Accessors for the remaining
// kinds (component, component-instance, module, component-func, resource,
// core-func) will be wired up in follow-up work.
type ComponentItem struct {
	val    wasmtime_component_item_t
	engine *Engine
	closed bool
}

func mkComponentItem(val wasmtime_component_item_t, engine *Engine) *ComponentItem {
	it := &ComponentItem{val: val, engine: engine}
	runtime.SetFinalizer(it, func(it *ComponentItem) {
		it.Close()
	})
	return it
}

// Kind returns the discriminator of this item.
func (it *ComponentItem) Kind() ComponentItemKind {
	if it.closed {
		panic("object has been closed already")
	}
	maybeGC()
	return ComponentItemKind(go_component_item_kind(&it.val))
}

// TypeAlias returns the [ComponentValType] embedded in this item when its
// kind is [ComponentItemKindType] (a WIT `type X = ...` alias). Returns
// `nil` for any other kind.
//
// The returned [ComponentValType] is independently owned and must be closed
// (or left to the finalizer).
func (it *ComponentItem) TypeAlias() *ComponentValType {
	if it.Kind() != ComponentItemKindType {
		return nil
	}
	var cloned wasmtime_component_valtype_t
	go_component_item_get_type(&it.val, &cloned)
	runtime.KeepAlive(it)
	return mkComponentValType(cloned)
}

// InstanceType returns the type of the instance this item is when its kind
// is [ComponentItemKindComponentInstance], such as an interface a component
// imports; nil for any other kind.
func (it *ComponentItem) InstanceType() *ComponentInstanceType {
	if it.Kind() != ComponentItemKindComponentInstance {
		return nil
	}
	ptr := wasmtime_component_instance_type_clone(*(**wasmtime_component_instance_type_t)(unsafe.Pointer(&it.val.of)))
	runtime.KeepAlive(it)
	ty := &ComponentInstanceType{_ptr: ptr, engine: it.engine}
	runtime.SetFinalizer(ty, func(ty *ComponentInstanceType) { ty.Close() })
	return ty
}

// ComponentInstanceType is the type of a component instance: its exports.
type ComponentInstanceType struct {
	_ptr   *wasmtime_component_instance_type_t
	engine *Engine
}

func (ty *ComponentInstanceType) ptr() *wasmtime_component_instance_type_t {
	ret := ty._ptr
	if ret == nil {
		panic("object has been closed already")
	}
	maybeGC()
	return ret
}

// ExportCount returns the number of the instance's exports.
func (ty *ComponentInstanceType) ExportCount() int {
	n := wasmtime_component_instance_type_export_count(ty.ptr(), ty.engine.ptr())
	runtime.KeepAlive(ty)
	runtime.KeepAlive(ty.engine)
	return int(n)
}

// ExportNth returns the name and the [ComponentItem] of the `i`-th export.
// Returns `("", nil)` if `i` is out of range.
func (ty *ComponentInstanceType) ExportNth(i int) (string, *ComponentItem) {
	var nameP *byte
	var nameLen uintptr
	var extern *wasmtime_component_extern_t
	found := wasmtime_component_instance_type_export_nth(ty.ptr(), ty.engine.ptr(), uintptr(i), &nameP, &nameLen, &extern)
	runtime.KeepAlive(ty)
	runtime.KeepAlive(ty.engine)
	if !found {
		return "", nil
	}
	name := goStringN(nameP, int(nameLen))
	var item wasmtime_component_item_t
	wasmtime_component_extern_type(extern, &item)
	wasmtime_component_extern_delete(extern)
	return name, mkComponentItem(item, ty.engine)
}

// Close deallocates this type explicitly.
func (ty *ComponentInstanceType) Close() {
	if ty._ptr == nil {
		return
	}
	runtime.SetFinalizer(ty, nil)
	wasmtime_component_instance_type_delete(ty._ptr)
	ty._ptr = nil
}

// Close deallocates this item explicitly.
func (it *ComponentItem) Close() {
	if it.closed {
		return
	}
	runtime.SetFinalizer(it, nil)
	wasmtime_component_item_delete(&it.val)
	it.closed = true
}

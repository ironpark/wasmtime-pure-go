package wasmtime

import (
	"fmt"
	"runtime"

	"github.com/ebitengine/purego"
)

// ComponentFunc is a function a component instance exports. Like a
// [ComponentInstance], it belongs to the store it was looked up in.
type ComponentFunc struct {
	val wasmtime_component_func_t
}

// GetFunc returns the function at the export index, or nil if the export
// is not a function.
func (i *ComponentInstance) GetFunc(store Storelike, idx *ComponentExportIndex) *ComponentFunc {
	f := &ComponentFunc{}
	ok := wasmtime_component_instance_get_func(&i.val, store.Context(), idx.ptr(), &f.val)
	runtime.KeepAlive(i)
	runtime.KeepAlive(store)
	runtime.KeepAlive(idx)
	if !ok {
		return nil
	}
	return f
}

// Call calls the function with args, and returns its nresults results.
//
// A resource of the host's own among the args, one with Rep and Type, is
// dropped once the call returns: it is lent to the guest for the call.
func (f *ComponentFunc) Call(store Storelike, args []ComponentVal, nresults int) ([]ComponentVal, error) {
	ctx := store.Context()
	cargs := make([]wasmtime_component_val_t, len(args))
	defer func() {
		for i, a := range args {
			p := &cargs[i]
			if a.Kind == ComponentValKindResource && a.Resource != nil && a.Resource.any == nil && p.kind == WASMTIME_COMPONENT_RESOURCE {
				if e := wasmtime_component_resource_any_drop(ctx, *componentUnion[*wasmtime_component_resource_any_t](p)); e != nil {
					wasmtime_error_delete(e)
				}
			}
			wasmtime_component_val_delete(p)
		}
		runtime.KeepAlive(store)
	}()
	for i := range cargs {
		cargs[i].kind = WASMTIME_COMPONENT_BOOL
	}
	for i, a := range args {
		if err := writeComponentVal(ctx, &cargs[i], a); err != nil {
			return nil, err
		}
	}
	// Results start as bools, which own nothing, as Wasmtime asks.
	cres := make([]wasmtime_component_val_t, nresults)
	for i := range cres {
		cres[i].kind = WASMTIME_COMPONENT_BOOL
	}
	e := wasmtime_component_func_call(&f.val, ctx, sliceData(cargs), uintptr(len(cargs)), sliceData(cres), uintptr(len(cres)))
	runtime.KeepAlive(f)
	if e != nil {
		return nil, mkError(e)
	}
	out := make([]ComponentVal, nresults)
	for i := range out {
		out[i] = readComponentVal(&cres[i])
		wasmtime_component_val_delete(&cres[i])
	}
	return out, nil
}

// sliceData is a pointer to the first of s, or nil for none.
func sliceData[T any](s []T) *T {
	if len(s) == 0 {
		return nil
	}
	return &s[0]
}

// ComponentCaller is the store a host function of a component is called
// in, valid until the function returns.
type ComponentCaller struct {
	ctx *wasmtime_context_t
}

// Context implements [Storelike].
func (c *ComponentCaller) Context() *wasmtime_context_t {
	if c.ctx == nil {
		panic("cannot use a component caller after its host function returns")
	}
	return c.ctx
}

// Data implements [Storelike].
func (c *ComponentCaller) Data() interface{} {
	return storeDataFromContext(c.Context()).data
}

// ComponentHostFunc implements a function a component imports. It takes
// the call's arguments and returns its results; an error traps the guest.
//
// A borrowed resource among the args is given back once it returns.
type ComponentHostFunc func(caller *ComponentCaller, args []ComponentVal) ([]ComponentVal, error)

type componentFuncEntry struct {
	name string
	fn   ComponentHostFunc
}

var (
	gComponentFuncs handleTable[*componentFuncEntry]

	componentFuncCallback = purego.NewCallback(func(
		env uintptr,
		ctx *wasmtime_context_t,
		ty uintptr,
		args uintptr,
		nargs uintptr,
		results uintptr,
		nresults uintptr,
	) *wasmtime_error_t {
		return goComponentFunc(env, ctx, args, nargs, results, nresults)
	})
	finalizeComponentFuncCallback = voidCallback(gComponentFuncs.remove)

	// Host resources are lent to the guest and owned by the host, so there
	// is nothing for the guest's drop of one to release.
	componentResourceDropCallback = purego.NewCallback(func(env uintptr, ctx *wasmtime_context_t, rep uintptr) *wasmtime_error_t {
		return nil
	})
)

func goComponentFunc(env uintptr, ctx *wasmtime_context_t, args, nargs, results, nresults uintptr) (out *wasmtime_error_t) {
	entry := gComponentFuncs.get(env)
	caller := &ComponentCaller{ctx: ctx}
	defer func() { caller.ctx = nil }()
	defer func() {
		if r := recover(); r != nil {
			out = wasmtime_error_new(fmt.Sprintf("%s: %v", entry.name, r))
		}
	}()
	cargs := cSlice[wasmtime_component_val_t](args, nargs)
	in := make([]ComponentVal, len(cargs))
	for i := range cargs {
		in[i] = readComponentVal(&cargs[i])
	}
	// A borrow lent to the host, as a method's self is, has to be given
	// back before the call returns.
	defer func() {
		for _, a := range in {
			if a.Kind != ComponentValKindResource || a.Resource.Owned() {
				continue
			}
			if e := wasmtime_component_resource_any_drop(ctx, a.Resource.any); e != nil {
				if out == nil {
					out = e
				} else {
					wasmtime_error_delete(e)
				}
			}
		}
	}()
	res, err := entry.fn(caller, in)
	if err != nil {
		return wasmtime_error_new(fmt.Sprintf("%s: %v", entry.name, err))
	}
	if len(res) != int(nresults) {
		return wasmtime_error_new(fmt.Sprintf("%s: %d results, the guest expects %d", entry.name, len(res), nresults))
	}
	// Wasmtime fills each result with a bool, which owns nothing.
	cres := cSlice[wasmtime_component_val_t](results, nresults)
	for i, r := range res {
		if err := writeComponentVal(ctx, &cres[i], r); err != nil {
			return wasmtime_error_new(fmt.Sprintf("%s: %v", entry.name, err))
		}
	}
	return nil
}

// ComponentLinkerInstance is an instance being defined in a
// [ComponentLinker]: the root namespace, or an instance under it, by name.
// An instance added under another is defined in and closed before the
// other is used again.
type ComponentLinkerInstance struct {
	_ptr   *wasmtime_component_linker_instance_t
	linker *ComponentLinker
}

func mkComponentLinkerInstance(ptr *wasmtime_component_linker_instance_t, linker *ComponentLinker) *ComponentLinkerInstance {
	li := &ComponentLinkerInstance{_ptr: ptr, linker: linker}
	runtime.SetFinalizer(li, func(li *ComponentLinkerInstance) { li.Close() })
	return li
}

func (li *ComponentLinkerInstance) ptr() *wasmtime_component_linker_instance_t {
	ret := li._ptr
	if ret == nil {
		panic("object has been closed already")
	}
	maybeGC()
	return ret
}

// Root returns the linker's root namespace, the world's own imports.
func (l *ComponentLinker) Root() *ComponentLinkerInstance {
	ptr := wasmtime_component_linker_root(l.ptr())
	runtime.KeepAlive(l)
	return mkComponentLinkerInstance(ptr, l)
}

// AddWasip2 defines WASI preview 2 in the linker, which the store's
// [WasiConfig] configures.
func (l *ComponentLinker) AddWasip2() error {
	e := wasmtime_component_linker_add_wasip2(l.ptr())
	runtime.KeepAlive(l)
	if e != nil {
		return mkError(e)
	}
	return nil
}

// AddInstance defines an instance named name, such as an interface of a
// package ("zed:extension/http-client"), and returns it for defining in.
func (li *ComponentLinkerInstance) AddInstance(name string) (*ComponentLinkerInstance, error) {
	var out *wasmtime_component_linker_instance_t
	e := wasmtime_component_linker_instance_add_instance(li.ptr(), goStrPtr(name), uintptr(len(name)), &out)
	runtime.KeepAlive(li)
	runtime.KeepAlive(name)
	if e != nil {
		return nil, mkError(e)
	}
	return mkComponentLinkerInstance(out, li.linker), nil
}

// AddFunc defines a function named name, implemented by fn.
func (li *ComponentLinkerInstance) AddFunc(name string, fn ComponentHostFunc) error {
	idx := gComponentFuncs.add(&componentFuncEntry{name: name, fn: fn})
	e := wasmtime_component_linker_instance_add_func(li.ptr(), goStrPtr(name), uintptr(len(name)),
		componentFuncCallback, idx, finalizeComponentFuncCallback)
	runtime.KeepAlive(li)
	runtime.KeepAlive(name)
	if e != nil {
		return mkError(e)
	}
	return nil
}

// AddResource defines a resource type named name as the host's own, told
// apart from the host's others by ty: the Type of the [ComponentResource]
// values of it. The host owns what it lends, so the guest dropping one
// releases nothing.
func (li *ComponentLinkerInstance) AddResource(name string, ty uint32) error {
	rt := wasmtime_component_resource_type_new_host(ty)
	defer wasmtime_component_resource_type_delete(rt)
	e := wasmtime_component_linker_instance_add_resource(li.ptr(), goStrPtr(name), uintptr(len(name)),
		rt, componentResourceDropCallback, 0, 0)
	runtime.KeepAlive(li)
	runtime.KeepAlive(name)
	if e != nil {
		return mkError(e)
	}
	return nil
}

// Close releases the instance, which ends defining in it.
func (li *ComponentLinkerInstance) Close() {
	if li._ptr == nil {
		return
	}
	runtime.SetFinalizer(li, nil)
	wasmtime_component_linker_instance_delete(li._ptr)
	li._ptr = nil
}

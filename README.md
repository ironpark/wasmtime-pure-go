# wasmtime-pure-go

A port of [wasmtime-go](https://github.com/bytecodealliance/wasmtime-go) (v50) that calls the
Wasmtime C API through [purego](https://github.com/ebitengine/purego) instead of cgo, so it
builds with `CGO_ENABLED=0` and cross-compiles without a C toolchain. The public API matches
upstream.

```sh
go get github.com/ironpark/wasmtime-pure-go
```

```go
import wasmtime "github.com/ironpark/wasmtime-pure-go"
```

Prebuilt, zstd-compressed Wasmtime shared libraries are embedded for darwin, linux and windows
(amd64/arm64) and extracted to the user cache directory on first use. See the package
documentation for overriding the library path or building without the embedded libraries.

## License

- wasmtime-go - Apache-2.0 ([LICENSE-wasmtime-go](LICENSE-wasmtime-go))
- Wasmtime shared libraries - Apache-2.0 WITH LLVM-exception ([embedded/LICENSE-wasmtime](embedded/LICENSE-wasmtime))

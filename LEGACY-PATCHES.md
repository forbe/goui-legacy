# Legacy Windows compatibility patches

This is `github.com/golang-gui/goui@v0.2.0` with the changes needed to start and
draw on older Windows. Upstream v0.2.0 is the first commit here; everything after
it is ours. Measured on Windows 11, Windows 10 (pre-1607) and Windows 7 SP1.

| # | File | Problem on old Windows | Change |
|---|------|------------------------|--------|
| 1 | `platform/windows/sdk/winapi/api_windows.go:535`, `:556` | `AdjustWindowRectExForDpi` is missing before Windows 10 1607, so window creation aborts with `Failed to find AdjustWindowRectExForDpi` | Keep the DPI-aware call where it exists, otherwise fall back to `AdjustWindowRectEx` at DPI 96 |
| 2 | `platform/windows/win32/platform_windows.go:128`, `:141` | The painter chain D2D → OpenGL → software silently degraded, which made a blank window undiagnosable | Log the downgrade once |
| 3 | `platform/typography/directwrite/context_windows.go:235` | `ID2D1Factory1` and `CLSID_WICImagingFactory2` are not always present | Request 1.1 / WIC2 first, then settle for `ID2D1Factory` / WIC1 |
| 4 | `platform/typography/directwrite/context_windows.go:729` | `D2D1_DRAW_TEXT_OPTIONS_ENABLE_COLOR_FONT` makes `EndDraw` return `E_INVALIDARG` on a DirectWrite that cannot enumerate color glyph runs, so no text is drawn at all | Probe `IID_IDWriteFactory2` once at context creation and only pass the flag when it answers |
| 5 | `platform/graphics/software/painter.go:415` | A text raster failure was swallowed, leaving invisible text with no trace | Log the first failure; an empty bitmap stays silent because empty text is normal |

Note on patch 4: capability cannot be inferred from the Direct2D version. Windows 7
SP1 with the platform update reports Direct2D 1.1 and has WIC2, yet its DirectWrite
is 1.0 (`IID_IDWriteFactory2` → `E_NOINTERFACE`) and rejects the color-font option.
Gate on DirectWrite, not on Direct2D.

These patches do not change behaviour on current Windows: the newer path is taken
whenever it is available, and Windows 11 produces an empty stderr either way.

## Module path

`go.mod` on `main` declares `github.com/golang-gui/goui`, the upstream path, so a
consumer keeps its imports and points at this fork with a directory `replace`:

```
replace github.com/golang-gui/goui => D:/codes/goui-legacy
```

That works offline but not over the network: a `replace` naming a version requires
the fetched module to declare that version's own path, so this fork cannot be
required or replaced by version while it keeps the upstream path. The
`module-path-rename` branch declares `github.com/forbe/goui-legacy` and rewrites
every internal import to match; use it if you want `require` to work.

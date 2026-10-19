# Legacy Windows compatibility patches

This is `github.com/golang-gui/goui@v0.2.0` with the changes needed to start and
draw on older Windows. Upstream v0.2.0 is the first commit here; everything after
it is ours. Measured on Windows 11, Windows 10 (pre-1607) and Windows 7 SP1.

| # | File | Problem on old Windows | Change |
|---|------|------------------------|--------|
| 1 | `platform/windows/sdk/winapi/api_windows.go:535`, `:556` | `AdjustWindowRectExForDpi` is missing before Windows 10 1607, so window creation aborts with `Failed to find AdjustWindowRectExForDpi` | Keep the DPI-aware call where it exists, otherwise fall back to `AdjustWindowRectEx` at DPI 96 |
| 2 | `platform/windows/win32/platform_windows.go:128`, `:141` | The painter chain D2D → OpenGL → software silently degraded, which made a blank window undiagnosable | Log the downgrade once |
| 3 | `platform/typography/directwrite/context_windows.go:235` | `ID2D1Factory1` and `CLSID_WICImagingFactory2` are not always present | Request 1.1 / WIC2 first, then settle for `ID2D1Factory` / WIC1 |
| 4 | `platform/typography/directwrite/context_windows.go:561` (`TextLayout.DrawOptions`), used at `:735` and `platform/graphics/direct2d/painter_windows.go:935` | `D2D1_DRAW_TEXT_OPTIONS_ENABLE_COLOR_FONT` makes `EndDraw` return `E_INVALIDARG` on a DirectWrite that cannot enumerate color glyph runs, so no text is drawn at all | Probe `IID_IDWriteFactory2` once at context creation; both draw paths now take their options from `DrawOptions`, which adds the flag only when the probe answered |
| 5 | `platform/graphics/software/painter.go:415` | A text raster failure was swallowed, leaving invisible text with no trace | Log the first failure; an empty bitmap stays silent because empty text is normal |

Both `ENABLE_COLOR_FONT` call sites are covered: the offscreen raster path every
painter uses, and the window Direct2D painter's own `DrawTextLayout` (which also
discarded the draw error before; it now reports the first failure once).

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

## Unrelated old-Windows limit: bundled fonts

`typography.Context.AddFont` (`context_windows.go:94`) builds a custom font set and
collection, which needs `IDWriteFactory3` — Windows 10 only (Fall Creators Update per
Microsoft's custom-font-sets doc). It also takes a file path, and no `gui`/`ui` layer
call exposes it. On Windows 7 an application font cannot reach DirectWrite by family
name through this API.

Asking for a family that is not installed is not an error either: DirectWrite picks a
fallback face and draws. Measured through this package on Windows 11, an unknown family
and the empty family give identical layout metrics (`w=243.31 h=24.89`, mixed
Chinese/Latin string at 14 pt), while the installed `Alibaba PuHuiTi 2.0` measures
`w=251.81 h=26.13`. So the `theme/fonts/*.ttf` copies shipped here are inert packaging:
a machine without that font shows its own default face, still legible but not the
bundled design.

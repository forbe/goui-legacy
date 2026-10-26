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
| 6 | `platform/typography/directwrite/context_windows.go:783` (`textPainter.Init`) | When `CreateWicBitmapRenderTarget` fails (NT 6.x RTM without Platform Update), `p.bitmap` succeeds but `p.render` stays nil; subsequent `Destroy` releases `p.bitmap` but leaves `p.render` as a dangling pointer from a previous init, causing `SetDpi` to crash with access violation on the released object | Zero out `p.render` and `p.bitmap` at entry; on render-target failure, release the already-created bitmap and leave both nil |
| 7 | `platform/windows/win32/platform_windows.go` (`NewTypography`) + new package `platform/typography/gotext/` | Systems without `dwrite.dll` at all (Server 2008 non-R2 without KB2670831) fail `create typography` and the application cannot start | Pure-Go fallback backend on `github.com/go-text/typesetting` (fontscan + HarfBuzz shaping + `x/image/vector` rasterization into premultiplied RGBA). Any DirectWrite context failure logs once and returns the fallback; painter, gui and ui are untouched. `AddFontData` works here without the Win10 1703 gate. Not yet measured on a real dwrite-less machine; unit-tested on Windows 11. Review follow-ups, all with regression tests in `gotext/regress_test.go`: text ending in a newline panicked with `index out of range` on the per-rune font table; every hard line break restarted its line stack at y=0, so multi-line text drew as one overlapping line; the context now serializes shaping, registration and destroy under a mutex, because go-text's FontMap, shaper and face caches are not concurrent-safe; shaper, segmenter, line wrapper and rasterizer are reused per layout instead of rebuilt on each reshape |
| 7b | same, via `GOUI_PLAT_TYPOGRAPHY=gotext` (`platform/common/env.go` + `win32.NewTypography`) | On WIC1-era systems that still have dwrite 1.0 (Server 2008 R2 / Win7), DirectWrite wins and embedded fonts stay unavailable (`LoadFontData` needs Factory5 / Win10 1703+) | The env var forces the pure-Go backend on any system, giving embedded fonts and a local rehearsal path; unset keeps normal behavior |
| 8 | `platform/typography/directwrite/context_windows.go` (`textPainter.Init`) | `32bppPRGBA` exists only in WIC2; on WIC1 systems (Server 2008 R2 / Win7 without KB2670831) `CreateWicBitmapRenderTarget` fails with `WINCODEC_ERR_UNSUPPORTEDPIXELFORMAT` (`0x88982F80`) and no text is drawn even though DirectWrite context creation succeeded | First `Init` tries PRGBA; a pixel-format failure permanently switches to `PBGRA` + `DXGI_FORMAT_B8G8R8A8_UNORM` (both WIC1/D2D 1.0 supported) and `GetBitmap` swaps R/B in place to keep the premultiplied-RGBA contract. Win11 keeps the unchanged PRGBA path; PBGRA path awaiting real-machine test |
| 9 | `widgets/drop_down.go` (`Open`) | A transparent drop-down popup needs `DCompositionCreateDevice` (Windows 8+) and DWM composition. On Windows 7, or a server with composition off, creation failed and every click on the control did nothing — the error only reached `ConnectOpenError`, which applications rarely connect | On a popup *creation* failure (`gui.PopoverCreationError`) reporting `ErrUnsupported` / `ErrUnavailable`, rebuild the popover opaque and retry once, then keep it; the same codes from a placement query and every other error are returned unchanged, matching `gui.PopoverMenu`. Tests: `TestDropDownOpenFallsBackToOpaquePopup`, `TestDropDownOpenKeepsErrorForOtherFailures` |
| 10 | `platform/windows/sdk/com/api_windows.go` (`Initialize`, `Uninitialize`, `InitializeOLE`, `UninitializeOLE`) | "COM is already initialized" was one process-wide bool, but an apartment belongs to a thread. Any other OS thread — a goroutine the scheduler moved, a text rasterizer running off the GUI thread — read the bool, skipped `CoInitializeEx`, and the next `CoCreateInstance` failed with `CoInitialize has not been called`. The bool was also read and written by different threads with no synchronization | Track the apartment per `GetCurrentThreadId` in a `sync.Map`, so `IsInitialized` answers about the calling thread and each thread initializes at most once. Evidence: `platform/typography/test` at `-count=2` failed at `HEAD` and passes now; `-count=1` was already green, which is why the bug hid |

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

## Bundled fonts, and what old Windows can do with them

`theme/fonts` holds the shipped font files as bytes; `ui.App.AddFontData` and
`gui.Application.AddFontData` register them with DirectWrite without a file on disk.
Registration is idempotent per content hash, so calling it from every rebuild costs one
hash. The route needs the factory's in-memory font loader, `IDWriteFactory5`, which
Microsoft lists from Windows 10 build 15063 up.

Older systems cannot take it: the path-based `typography.Context.AddFont` needs
`IDWriteFactory3` (Windows 10) and the in-memory loader needs `IDWriteFactory5`, so on
Windows 7 `AddFontData` reports `errors.ErrUnsupported` and nothing else changes.

Asking for a family that is not installed is not an error: DirectWrite picks a fallback
face and draws. Measured on Windows 11, an unknown family and the empty family give
identical layout metrics (`w=243.31 h=24.89`, mixed Chinese/Latin string at 14 pt),
while `Alibaba PuHuiTi 2.0` measures `w=251.81 h=26.13`. A machine that cannot register
the bundled bytes therefore shows its own default face, still legible but not the
bundled design; `platform/typography/context.go:53` states the same contract ("the
selected face may be a fallback").

Reaching the loader required a binding fix: `Factory5Class` did not embed
`Factory4Class`, so its five methods were read from the top of the vtable and
`CreateInMemoryFontFileLoader` called `IUnknown::AddRef` instead — which "succeeded"
while leaving the loader nil.

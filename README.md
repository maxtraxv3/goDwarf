# goDwarf

A feature-rich ClanLord game client for Android, built with Go and Ebiten.

## File Access

goDwarf needs file access to Read and Write your macro, Text logs and debug logs files to and from: `Documents/goDwarf/`

## Game Data

CL_Images and CL_Sounds are downloaded automatically from the official ClanLord servers on first launch.

## Credits

### Clan Lord

- **Clan Lord** — Proprietary MMORPG by [Delta Tao Software](https://www.deltatao.com/clanlord/)
- **ClanLordClient** (original Mac client source) — Apache-2.0 — [Delta Tao Software](https://github.com/YappyGM/ClanLordClient)

### goThoom / goThoomMacro

This project builds on code and concepts from the goThoom family of ClanLord clients:

- **goThoom** (original) — MIT License — [Distortions81](https://github.com/Distortions81/goThoom)
- **goThoomMacro** (macro fork) — MIT License — [maxtraxv3](https://github.com/maxtraxv3/goThoomMacro)

Ported/adapted components: CL_Images decoder (`climg/`), CL_Sounds decoder (`clsnd/`), chat bubble renderer, text wrapping, sound system, and macro engine design.

### Libraries

- **Ebitengine** — Apache-2.0 — [Hajime Hoshi](https://github.com/hajimehoshi/ebiten)
- **ebitengine/gomobile** — BSD-3-Clause — [The Go Authors](https://github.com/ebitengine/gomobile)
- **ebitengine/oto** — Apache-2.0
- **ebitengine/purego** — Apache-2.0
- **ebitengine/hideconsole** — Apache-2.0
- **golang.org/x/text** — BSD-3-Clause — The Go Authors
- **golang.org/x/image** — BSD-3-Clause — The Go Authors
- **golang.org/x/sync** — BSD-3-Clause — The Go Authors
- **golang.org/x/sys** — BSD-3-Clause — The Go Authors
- **go-text/typesetting** — BSD-3-Clause
- **jezek/xgb** — MIT
- **rivo/uniseg** — MIT

### Fonts

- **Noto Sans** (Regular, Bold, Italic, BoldItalic) — SIL Open Font License 1.1 — [The Noto Project Authors](https://github.com/googlefonts/noto-fonts)

### Internal

- **Twofish** (`internal/twofish/`) — BSD-3-Clause — Derived from [golang.org/x/crypto](https://github.com/golang/crypto) by The Go Authors

## License

MIT License. See [LICENSE](LICENSE) for details.

Third-party licenses:
- `internal/twofish/LICENSE` — BSD-3-Clause
- `data/font/OFL.txt` — SIL Open Font License 1.1

## Building

### Prerequisites

- Go 1.26.5+
- Ebiten mobile tools (for Android)
- Android SDK (for Android builds)

### Desktop

```bash
go build -o godwarf .
```

### Android

```bash
# Install ebitenmobile
go install github.com/hajimehoshi/ebiten/v2/cmd/ebitenmobile@latest

# Build APK
bash build.sh
```

The APK will be at `build/aar/godwarf.apk`.


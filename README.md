# 简截 SnapLite

常驻托盘 / 菜单栏的轻量截图工具，Windows 与 macOS 通用。Go 编写，界面用 [Viva](https://github.com/vivaui/viva) v0.2.1（GPU 原生，无 WebView）。
需求、设计与实现说明见 [docs/](docs/README.md)。

## 功能

- **区域截图**：冻结所有显示器画面，框选区域（支持窗口吸附、8 倍放大镜），完成后复制到剪切板，或保存为 PNG。
- **长截图**：框选可滚动区域，自动发送滚轮并拼接（自动识别固定顶栏/底栏）；没有辅助功能权限时退化为手动滚动。结果在编辑器窗口中查看、标注、复制、保存。
- **标注**：矩形、椭圆、箭头、画笔；六种颜色、三档粗细；撤销 / 重做；`Shift` 约束为正方形 / 正圆 / 45°。
- **剪切板历史**：记录最近 100 条图片和文字（去重、500 MB 上限、重启保留），密码管理器标记为隐藏的内容不记录；面板可搜索、键盘操作。

## 快捷键

| 操作 | macOS | Windows |
| --- | --- | --- |
| 截图 | `⌥F1` | `右Alt+F1` |
| 长截图 | `⌥F2` | `右Alt+F2` |
| 剪切板历史 | `⌥F3` | `右Alt+F3` |

Mac 键盘上 F1–F3 默认是媒体键：按 `⌥ fn F1`，或在 系统设置 › 键盘 › 键盘快捷键 › 功能键 中打开“将 F1、F2 等键用作标准功能键”。Windows 只认右 Alt。
| 完成（复制） | `Enter` / 双击选区 / `⌘C` | `Enter` / 双击选区 / `Ctrl+C` |
| 保存 | `⌘S` | `Ctrl+S` |
| 退出截图 / 取消正在画的一笔 | `Esc` / 右键 | `Esc` / 右键 |
| 撤销 / 重做 | `⌘Z` / `⌘⇧Z` | `Ctrl+Z` / `Ctrl+Y`、`Ctrl+Shift+Z` |
| 选工具 矩形/椭圆/箭头/画笔 | `⌘1`–`⌘4` | `Ctrl+1`–`Ctrl+4` |
| 复制放大镜下的颜色 | `⌘⇧C` | `Ctrl+Shift+C` |
| 微调选区 | 方向键（`Shift` 10px） | 同左 |

全局快捷键可在配置文件里修改（见下）。

## 构建

需要 Go 1.26+。macOS 需要 Xcode Command Line Tools（平台层用 cgo + Objective-C）；Windows 版是纯 Go，可在 macOS 上交叉编译。

```bash
go build -o build/snaplite ./cmd/snaplite                                  # 本机
GOOS=windows GOARCH=amd64 go build -o build/snaplite.exe ./cmd/snaplite    # Windows x64
GOOS=windows GOARCH=arm64 go build -o build/snaplite-arm64.exe ./cmd/snaplite

./scripts/package-mac.sh                                                    # build/简截.app（LSUIElement、屏幕录制说明、ad-hoc 签名）
go run github.com/vivaui/viva/cmd/viva@v0.2.1 package -target windows -arch amd64 -format zip -o build/windows
go run ./cmd/genicon/main.go                                                # 重新生成 assets/icon.png、tray-template.png
go build -tags probe -o build/platform-probe ./cmd/platform-probe           # 平台层真机探针（会触发权限弹窗）
```

测试（不打开任何窗口，UI 用 Viva 的 `app.Driver` 验证）：

```bash
gofmt -l . && go vet ./... && go test ./... -count=1
GOOS=windows GOARCH=amd64 go vet ./... && GOOS=windows GOARCH=arm64 go build ./...
```

## 权限（macOS）

- **屏幕录制**：首次截图时系统会询问；拒绝后简截会弹出说明窗口，可一键打开「系统设置 › 隐私与安全性 › 屏幕录制」。授权后需重启简截。权限绑定代码签名，开发期每次重新构建可能需要重新授权。
- **辅助功能**：只有长截图的自动滚动需要。未授权时自动退化为手动滚动，控制条上有「开启自动滚动」。
- 打包后的 `.app` 设置了 `LSUIElement`，不显示 Dock 图标；直接运行 `build/snaplite` 时由程序把激活策略改为 Accessory。

Windows 不需要额外权限；无法向以管理员身份运行的窗口发送滚轮（UIPI），此时长截图会提示改为手动滚动。

## 配置与数据

数据目录：macOS `~/Library/Application Support/SnapLite`，Windows `%APPDATA%\SnapLite`。

- `config.json`：首次启动写出默认值。可改快捷键（如 `"Option+F1"`、`"RightAlt+F1"`、`"CmdOrCtrl+Shift+A"`，按键支持字母、数字、F1–F12；0.1.0 写入的旧默认值会自动换成新默认值）、`saveDir`、历史条数 / 字节上限、是否记录文本、长截图模式（`auto` / `manual`）与最大高度。
- `history/`：`index.json` 与图片 PNG。
- `snaplite.log`：错误与崩溃日志（保留约 1 MB）。

## 已知限制

- 真实窗口行为（遮罩置顶与覆盖菜单栏/任务栏、立即获得键盘焦点、多显示器与混合缩放、权限弹窗、Windows 真机）只在 `app.Driver` 和交叉编译层面验证过，需按 [docs/07](docs/07-测试与验收.md) §3 真机验收。
- Viva v0.2.1 的保存对话框不能指定初始目录，对话框打开在系统记住的目录。
- Viva 不把无修饰键的字母/数字交给非文本窗口，所以选工具与复制颜色用 `⌘/Ctrl` 组合键（见上表）。
- Windows 上历史面板搜索框获得焦点时 ↑↓ 移动光标（Viva 的 `NavigationKeys` 目前只在 macOS 实现），可按 `Tab` 把焦点移到列表。
- macOS 没有剪切板变化通知，历史记录用 800 ms 的低频定时器检查（唯一的常驻定时器）；Windows 是事件驱动。
- 工具栏出现时的 120 ms 渐显未实现（直接出现）；长截图选区外框提示（P1，可选）未做。
- 跨显示器选区、录屏、OCR、贴图等不在本期范围。

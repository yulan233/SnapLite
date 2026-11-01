# 简截 SnapLite

[English](README.md) | 中文

常驻托盘 / 菜单栏的轻量截图工具，Windows 与 macOS 通用。Go 编写，界面用 [Viva](https://github.com/vivaui/viva) v0.2.1（GPU 原生，无 WebView）。
需求、设计与实现说明见 [docs/](docs/README.md)（中文）。

## 功能

- **区域截图**：冻结所有显示器画面，框选区域（支持窗口吸附、8 倍放大镜），完成后复制到剪切板，或保存为 PNG / JPEG。多显示器且缩放比例不同时（例如 100% 主屏 + 125% 副屏），每块屏幕都按自己的物理像素截满。
- **钉图**：截图工具栏「钉住」把截图钉在选区原位；剪切板历史里的图片和文字也能钉到桌面。钉图浮在其他窗口之上、不抢焦点，可拖动，双击关闭，右键可复制、保存、取消钉住；托盘「关闭全部钉图」。钉图不保存，退出即关闭。
- **截图质量**：设置 › 截图 可选 原图（PNG 无损）/ 高 / 中 / 低（JPEG 92% / 80% / 60%），只影响保存的文件，复制到剪切板始终是无损 PNG；分辨率可选“标准 1×”，把高分屏截图缩小到逻辑尺寸。
- **标注**：矩形、椭圆、箭头、画笔、马赛克；六种颜色、三档粗细；撤销 / 重做；`Shift` 约束为正方形 / 正圆 / 45°。
- **马赛克**：按住拖动涂抹，涂过的地方变成方块，每块是原图该处的平均色；三档粗细对应不同的笔刷与方块大小。马赛克总在其他标注下面，屏幕上看到的就是导出的结果。
- **剪切板历史**：记录最近 100 条图片和文字（去重、500 MB 上限、重启保留），密码管理器标记为隐藏的内容不记录；面板可搜索、键盘操作；点击条目复制，「粘贴」按钮、`⌘↵` 或 `⌘1–9` 直接粘贴到之前的应用（macOS 需辅助功能权限）；右键某一行可 复制 / 粘贴 / 钉到桌面 / 保存到本地… / 删除。
- **界面语言**：中文 / English，设置 › 通用 › 语言，默认跟随系统，切换后立即生效。
- 长截图已于 2026-11-01 取消。

## 快捷键

| 操作 | macOS | Windows |
| --- | --- | --- |
| 截图 | `⌥F1` | `右Alt+F1` |
| 剪切板历史 | `⌥F2` | `右Alt+F2` |
| 完成（复制） | `Enter` / 双击选区 / `⌘C` | `Enter` / 双击选区 / `Ctrl+C` |
| 保存 | `⌘S` | `Ctrl+S` |
| 退出截图 / 取消正在画的一笔 | `Esc` / 右键 | `Esc` / 右键 |
| 撤销 / 重做 | `⌘Z` / `⌘⇧Z` | `Ctrl+Z` / `Ctrl+Y`、`Ctrl+Shift+Z` |
| 选工具 矩形/椭圆/箭头/画笔/马赛克 | `⌘1`–`⌘5` | `Ctrl+1`–`Ctrl+5` |
| 复制放大镜下的颜色 | `⌘⇧C` | `Ctrl+Shift+C` |
| 微调选区 | 方向键（`Shift` 10px） | 同左 |
| 关闭钉图 | 双击 / `Esc`（钉图有焦点时） | 同左 |

Mac 键盘上 F1–F2 默认是媒体键：按 `⌥ fn F1`，或在 系统设置 › 键盘 › 键盘快捷键 › 功能键 中打开“将 F1、F2 等键用作标准功能键”。

Windows 只认右 Alt：简截注册 Alt+F1，收到时检查按下的是不是右 Alt（AltGr 布局另注册 Ctrl+Alt+F1）。所以简截运行时，左 Alt+F1 也会被占用但什么都不做。这样做是为了不安装全局键盘钩子（见下方“杀毒软件误报”）。全局快捷键可在 设置 › 快捷键 或配置文件里修改。

## 构建

需要 Go 1.26+。macOS 需要 Xcode Command Line Tools（平台层用 cgo + Objective-C）；Windows 版是纯 Go，可在 macOS 上交叉编译。

```bash
go build -o build/snaplite ./cmd/snaplite                                  # 本机
GOOS=windows GOARCH=amd64 go build -o build/snaplite.exe ./cmd/snaplite    # Windows x64
GOOS=windows GOARCH=arm64 go build -o build/snaplite-arm64.exe ./cmd/snaplite

./scripts/setup-mac-signing.sh                                              # 每台 Mac 一次：创建本地签名证书（见“权限”）
./scripts/package-mac.sh                                                    # build/简截.app（LSUIElement、屏幕录制说明、签名）
./scripts/package-mac.sh amd64                                              # Intel Mac 版（在 Apple 芯片上交叉编译）
go run github.com/vivaui/viva/cmd/viva@v0.2.1 package -target windows -arch amd64 -format zip -o build/windows
go run ./cmd/genicon/main.go                                                # 重新生成 assets/icon.png、tray-template.png
go build -tags probe -o build/platform-probe ./cmd/platform-probe           # 平台层真机探针（会触发权限弹窗）
```

测试（不打开任何窗口，UI 用 Viva 的 `app.Driver` 验证）：

```bash
gofmt -l . && go vet ./... && go test ./... -count=1
GOOS=windows GOARCH=amd64 go vet ./... && GOOS=windows GOARCH=arm64 go build ./...
```

## 发布

推送 `v` 开头的标签后，[`.github/workflows/release.yml`](.github/workflows/release.yml) 在 macOS runner 上跑测试，把标签号写进 `viva.toml` 的版本号，打出 macOS（arm64 / x64）与 Windows（x64 / arm64）四个 zip，并创建 GitHub Release（发布说明按提交自动生成）：

```bash
git tag v0.2.0 && git push origin v0.2.0
```

发布包没有正式签名：macOS 下载后需右键「打开」或执行 `xattr -dr com.apple.quarantine 简截.app`，Windows 会有 SmartScreen 提示。以后要签名时，`viva package` 会读取环境变量（`APPLE_CERTIFICATE`、`APPLE_ID` 等用于签名与公证，`WINDOWS_CERTIFICATE` 等用于 Windows 签名，见 `viva help package`），放进仓库 Secrets；`scripts/package-mac.sh` 也认 `SNAPLITE_SIGN_IDENTITY` 指定的签名身份。

## 权限（macOS）

- **屏幕录制**：首次截图时系统会询问；拒绝后简截会弹出说明窗口，可一键打开「系统设置 › 隐私与安全性 › 屏幕录制」。授权后需重启简截。
  macOS 把这个权限绑定到代码签名。ad-hoc 签名每次构建都会变，所以以前每次重新打包都要重新授权。在本机执行一次 `./scripts/setup-mac-signing.sh`，它会在登录钥匙串里创建自签名证书「SnapLite Local Signing」并信任它用于代码签名（会要求输入一次密码）；之后 `package-mac.sh` 都用它签名，授权一次即可，重新打包不再失效。CI 发布包仍是 ad-hoc 签名，升级版本后需要重新授权。
- **辅助功能**：只有剪切板历史的“粘贴到之前的应用”需要。未授权时内容仍会复制，并弹出系统授权提示。
- 打包后的 `.app` 设置了 `LSUIElement`，不显示 Dock 图标；直接运行 `build/snaplite` 时由程序把激活策略改为 Accessory。

Windows 不需要额外权限；无法向以管理员身份运行的窗口模拟粘贴（UIPI），此时内容只复制。

## 杀毒软件误报（Windows）

截图 + 读剪切板 + 模拟按键 + 开机自启，再加上没有代码签名，正好是启发式引擎眼里“键盘记录器”的特征，360 曾把简截报为 `Trojan.Generic`。已做的处理：

- 去掉了全局低级键盘钩子（`SetWindowsHookEx(WH_KEYBOARD_LL)`），右 Alt 快捷键改用 `RegisterHotKey`。
- exe 带有版本信息（公司名、描述、版本号、图标），见 `viva.toml` 的 `[windows]`。

彻底解决要靠代码签名证书（Authenticode）签 exe，并把签名后的版本提交到 360 开放平台（open.soft.360.cn）做白名单 / 误报申诉。

## 配置与数据

数据目录：macOS `~/Library/Application Support/SnapLite`，Windows `%APPDATA%\SnapLite`。

- `config.json`：首次启动写出默认值。可改快捷键（如 `"Option+F1"`、`"RightAlt+F1"`、`"CmdOrCtrl+Shift+A"`，按键支持字母、数字、F1–F12；旧版本写入的默认值——包括剪切板历史原来的 `Option+F3` / `RightAlt+F3`——会自动换成新默认值）、`saveDir`、历史条数 / 字节上限、是否记录文本、点击历史条目的动作（`history.clickAction`：`copy` / `paste`）、截图质量（`capture.quality`：`png` / `high` / `medium` / `low`）、分辨率（`capture.scale`：`original` / `1x`）与界面语言（`language`：空为跟随系统，`zh` / `en`）。旧版的 `longshot` 字段会在下次保存时消失。这些大多也能在托盘「设置…」里改。
- `history/`：`index.json` 与图片 PNG。
- `snaplite.log`：错误与崩溃日志（保留约 1 MB）。

## 已知限制

- 真实窗口行为（遮罩置顶与覆盖菜单栏/任务栏、立即获得键盘焦点、多显示器与混合缩放、权限弹窗、Windows 真机）只在 `app.Driver` 和交叉编译层面验证过，需按 [docs/07](docs/07-测试与验收.md) §3 真机验收。
- Viva v0.2.1 的保存对话框不能指定初始目录，对话框打开在系统记住的目录。
- Viva 不把无修饰键的字母/数字交给非文本窗口，所以选工具与复制颜色用 `⌘/Ctrl` 组合键（见上表）。
- Windows 上历史面板搜索框获得焦点时 ↑↓ 移动光标（Viva 的 `NavigationKeys` 目前只在 macOS 实现），可按 `Tab` 把焦点移到列表。
- macOS 没有剪切板变化通知，历史记录用 800 ms 的低频定时器检查（唯一的常驻定时器）；Windows 是事件驱动。
- 工具栏出现时的 120 ms 渐显未实现（直接出现）。
- 钉图的置顶、拖动、双击与右键菜单只在 `app.Driver` 下验证过，需真机验收；macOS 上点击钉图会激活简截（这样钉图才能收到 `Esc`）。
- 切换语言后，已打开窗口的标题栏要等下次打开才会换成新语言。
- 跨显示器选区、录屏、OCR、长截图等不在本期范围。

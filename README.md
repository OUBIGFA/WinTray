<div align="center">

# WinTray

**Windows 开机自动整理桌面的托盘工具**

[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Platform](https://img.shields.io/badge/platform-Windows%2010%2F11-lightgrey.svg)]()
[![Go 1.22](https://img.shields.io/badge/Go-1.22-00ADD8.svg)]()

[English](README.en.md) | 简体中文

</div>

---

## 简介

WinTray 是一款面向 Windows 的开机整理工具。它在系统托盘常驻运行，在开机自启流程触发时，按规则自动处理指定程序的窗口（例如自动最小化或隐藏），省去每次开机后手动收拾桌面的麻烦。

---

## 功能特性

- **托盘常驻**：系统通知区图标，支持一键打开设置、查看日志、退出程序
- **受管程序列表**：可维护任意数量的程序，每个程序独立配置执行行为
- **开机自启**：写入当前用户 `Run` 注册表项，随 Windows 登录自动启动
- **自动隐藏窗口**：程序列表中配置后，`--autorun` 流程触发时自动最小化并隐藏目标窗口
- **窗口处理重试**：支持 0–120 秒的可配置重试等待，应对启动慢的程序
- **清理并恢复默认**：可在主窗口或托盘菜单一键清理本地配置/日志并恢复默认状态
- **双语界面**：内置简体中文 / English，随时切换，即时生效
- **单实例保护**：防止重复启动，避免配置冲突

---

## 系统要求

| 项目 | 要求 |
|---|---|
| 操作系统 | Windows 10 / 11 |
| 运行时 | 无需额外依赖（独立可执行文件） |
| 源码构建 | Go 1.22+ |

---

## 下载与使用

WinTray 仅提供**便携版**，无需安装。

前往 [Releases](../../releases) 页面，下载 `WinTray-Portable.zip`，解压后直接运行 `WinTray.exe` 即可。

- 配置与日志写入 `%LOCALAPPDATA%\WinTray\`，不依赖任何注册表安装项
- 不再使用时关闭程序、删除文件夹即可彻底移除

---

## 数据目录

| 类型 | 路径 |
|---|---|
| 配置文件 | `%LOCALAPPDATA%\WinTray\settings.json` |
| 运行日志 | `%LOCALAPPDATA%\WinTray\wintray.log` |

---

## 启动参数

| 参数 | 说明 |
|---|---|
| `--background` | 后台启动，不弹主窗口（适用于开机自启场景） |
| `--autorun` | 按"开机执行"配置自动执行受管任务 |
| `--cleanup-restore` | 仅执行清理恢复流程：清空 `%LOCALAPPDATA%\WinTray\` 数据目录并退出 |

---

## 本地构建

```powershell
powershell -ExecutionPolicy Bypass -File build/package.ps1 -OutputDir dist
```

构建产物：

| 文件 | 说明 |
|---|---|
| `dist/WinTray.exe` | 主程序 |
| `dist/WinTray.exe.manifest` | 应用清单（DPI 感知等） |
| `dist/checksums.txt` | SHA256 校验文件 |
| `publish/WinTray-Portable.zip` | 便携版压缩包 |

---

## 自动发布（GitHub Actions）

推送 `v*` 格式的 tag 后，CI 自动构建并上传 Release 附件：

```bash
git tag v0.1.0
git push origin v0.1.0
```

Release 产物：`WinTray.exe`、`WinTray.exe.manifest`、`checksums.txt`

---

## 项目结构

```text
.
├─ .github/workflows/      # CI/CD 与 Release 自动化
├─ build/                  # 打包脚本（package.ps1）与应用清单
├─ cmd/wintray/            # 程序入口
└─ internal/               # 核心业务实现
```

---

## 常见问题

**Q：程序运行后没有主窗口怎么进入设置？**
A：右键系统托盘中的 WinTray 图标，选择"打开设置"即可。

**Q：开机自启生效后如何取消？**
A：在设置页取消勾选"开机自启"，程序自动清理对应的注册表项。

**Q：加入列表的程序没有被自动最小化？**
A：确认该程序配置了"自动隐藏窗口"选项，且 WinTray 是以 `--autorun` 参数触发的（开机自启时由系统自动传入）。如程序启动较慢，可适当增大重试等待时间。

---

## 许可证

本项目基于 [MIT 许可证](LICENSE) 发布。

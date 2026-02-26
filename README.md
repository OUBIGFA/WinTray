# WinTray

[English README](README.en.md)

WinTray 是一个面向 Windows 的托盘工具，用于在开机自动触发时，按规则管理指定程序窗口（例如自动隐藏/关闭前台窗口），减少开机后的手动整理操作。

## 核心特性

- 托盘常驻运行，支持一键打开设置、查看日志、退出
- 可维护受管程序列表，按程序粒度配置执行行为
- 支持开机自启（写入当前用户 `Run` 注册表项）
- 支持后台启动参数，适配自启场景
- 支持窗口处理重试机制（0-120 秒）
- 内置中文/英文界面切换
- 单实例保护，避免重复启动

## 运行环境

- Windows 10/11
- Go 1.22+（仅源码构建时需要）

## 快速开始

### 方式 1：从 Release 下载（推荐）

1. 打开仓库的 `Releases` 页面
2. 下载 `WinTray.exe`
3. 双击运行，首次配置受管程序

### 方式 2：本地构建

```powershell
powershell -ExecutionPolicy Bypass -File build/package.ps1 -OutputDir dist
```

构建产物：

- `dist/WinTray.exe`
- `dist/WinTray.exe.manifest`
- `dist/checksums.txt`
- `publish/WinTray-Portable.zip` (便携版压缩包)

## 使用说明

- 添加受管程序后，可配置：
  - 是否开机执行该程序
  - 启动后是否自动最小化并隐藏窗口
- 托管动作由 `--autorun` 启动流程自动触发
- 日志文件路径：`%LOCALAPPDATA%\WinTray\wintray.log`
- 配置文件路径：`%LOCALAPPDATA%\WinTray\settings.json`

## 启动参数

- `--background`：后台启动（不弹主窗口）
- `--autorun`：按“开机执行”配置自动执行受管任务

## 发布到 GitHub Release

仓库内置工作流：`.github/workflows/release.yml`

发布步骤：

```bash
git tag v0.1.0
git push origin v0.1.0
```

推送标签后会自动构建并上传以下 Release 附件：

- `WinTray.exe`
- `WinTray.exe.manifest`
- `checksums.txt`

## 项目结构

```text
.
├─ .github/workflows/      # CI/CD 与 Release 自动化
├─ build/                  # 打包脚本与 manifest
├─ cmd/wintray/            # 程序入口
└─ internal/               # 核心业务实现
```

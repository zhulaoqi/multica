# 把本机接入测试环境的守护进程

测试环境的网页和接口是分开的。守护进程必须跑在要执行任务的那台电脑上，由它自己向接口登记。在另一台电脑的网页里点确认，不能代替这台电脑登记。


| 用途  | 地址                                 |
| --- | ---------------------------------- |
| 网页  | `http://172.30.11.181:31383`       |
| 接口  | `http://172.30.11.181:31469`       |
| 登录页 | `http://172.30.11.181:31383/login` |


`--server-url` 填接口 `31469`，`--app-url` 填网页 `31383`。两个都写成 `31383` 时，登录会连到网页，守护进程登记不上。

不要用官方 Homebrew 的 `multica`，也不要执行不带地址的 `multica setup`。那个客户端会连到 `https://api.multica.ai`，登记的是公有云。

登记之前，这台电脑上要先有三个命令。缺一个，`multica daemon start` 会直接退出，服务器上看不到这台机器。


| 命令             | 干什么                                    | 什么时候要装                     |
| -------------- | -------------------------------------- | -------------------------- |
| `multica`      | 登录测试环境，启动守护进程，把这台电脑登记上去                | 每台电脑都要                     |
| `git`          | 拉代码、交分支。Cursor 在 Windows 上还依赖 Git Bash | 执行 `git --version` 没有输出时要装 |
| `cursor-agent` | 守护进程真正调用的 Cursor 命令行。装的是 Cursor 编辑器不够  | 这台电脑要用 Cursor 跑任务时要装       |


编辑器里的 Cursor 和命令行里的 `cursor-agent` 是两样东西。现在的 Cursor 安装脚本装出来的命令名叫 `agent`，守护进程不认这个名字，后面要再补一个叫 `cursor-agent` 的入口。

下面先把三个命令装好，再按顺序执行注册。macOS 和 Windows 的命令不同，只做自己系统那一节。

## macOS

### 1. 安装 multica

```bash
curl -fsSL http://172.30.11.181:31383/install/install.sh | bash
```

脚本会把 `multica` **放到** `/usr/local/bin`**。这个目录写不进去时，放到** `~/.local/bin`。以脚本最后打印的路径为准。

关掉这个终端，新开一个，再继续。当前窗口还在用安装之前的 PATH，会找不到刚装上的命令。

### 2. 安装 git

先看有没有：

```bash
git --version
```

有版本号就跳过。没有的话安装苹果的命令行工具，装完再执行一次 `git --version`：

```bash
xcode-select --install
```

### 3. 安装 cursor-agent

只用别的编程工具、不用 Cursor 时，跳过这一节。

```bash
curl https://cursor.com/install -fsS | bash
```

再新开一个终端。确认命令在不在：

```bash
command -v agent
command -v cursor-agent
```

`cursor-agent` 已经有路径时，不用做链接。只有 `agent`、没有 `cursor-agent` 时，在同一个目录补上守护进程要找的名字：

```bash
ln -sf "$(command -v agent)" "$(dirname "$(command -v agent)")/cursor-agent"
command -v cursor-agent
```

然后在这台 Mac 上登录 Cursor。账号保存在本机，网页代替不了：

```bash
cursor-agent login
```

`cursor-agent` 还不存在、只有 `agent` 时，改执行 `agent login`。

### 4. 按顺序注册

三个命令都要先能找到。在新终端里检查：

```bash
command -v multica
git --version
command -v cursor-agent
```

第三条只在使用 Cursor 时需要。都有输出之后，按这个顺序执行。

准备令牌：用已经登录的浏览器打开 `http://172.30.11.181:31383`，进入 **设置 → API Token**，新建一个令牌。完整内容只显示一次。

回到这台 Mac：

```bash
multica setup self-host \
  --server-url http://172.30.11.181:31469 \
  --app-url http://172.30.11.181:31383

multica login --token

multica daemon start

multica daemon status
```

`multica login --token` 会等你粘贴刚才的令牌。`daemon status` 显示进程仍在运行，才算登记成功。刷新网页的运行时页面。

开机后自动拉起：

```bash
multica daemon autostart enable
```

## Windows

用 PowerShell。每装完一个会改 PATH 的软件，都要关掉窗口再新开一个 PowerShell，否则下一步会报「无法将某项识别为 cmdlet」。

### 1. 安装 multica

```powershell
irm http://172.30.11.181:31383/install/install.ps1 | iex
```

脚本成功时会打印两行，类似：

```text
Added C:\Users\<用户名>\AppData\Local\Multica\bin to the user PATH.
Multica CLI installed to C:\Users\<用户名>\AppData\Local\Multica\bin\multica.exe
```

以脚本实际打印的路径为准。然后关掉这个窗口，新开一个 PowerShell。

新窗口里执行 `multica` 仍然找不到时，先让这个窗口读到新的 PATH：

```powershell
$env:Path = [Environment]::GetEnvironmentVariable("Path", "User") + ";" + [Environment]::GetEnvironmentVariable("Path", "Machine")
Get-Command multica
```

### 2. 安装 git

先看有没有：

```powershell
git --version
```

有版本号就跳过。没有的话打开 [https://git-scm.com/download/win](https://git-scm.com/download/win) ，安装 Git for Windows。Cursor 的安装程序会检查 Git Bash，没有它会提示 `Git for Windows (with Git Bash) is required`，后面的任务也跑不起来。

装完后关掉 PowerShell，再新开一个，执行 `git --version`，要能打出版本号。

### 3. 安装 cursor-agent

只用别的编程工具、不用 Cursor 时，跳过这一节。Cursor 编辑器已经装好也不算，守护进程找的是命令行。

```powershell
irm 'https://cursor.com/install?win32=true' | iex
```

成功时最后会写 `Start using Cursor Agent: agent`。这表示装上的命令名叫 `agent`。关掉窗口，新开一个 PowerShell。

直接输入 `agent login` 会失败，报错是「在此系统上禁止运行脚本」。`agent` 指向 `agent.ps1`。用下面这条登录，不用改整机策略：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File "$env:LOCALAPPDATA\cursor-agent\agent.ps1" login
```

按 Cursor 自己的页面完成登录。登录必须在这台 Windows 上做。

守护进程不找 `agent`，只找 `cursor-agent`。在安装目录补一个同名启动脚本：

```powershell
$dir = "$env:LOCALAPPDATA\cursor-agent"
Set-Content -Path "$dir\cursor-agent.cmd" -Value "@echo off`r`npowershell -NoProfile -ExecutionPolicy Bypass -File `"%~dp0agent.ps1`" %*"
Get-Command cursor-agent
```

`Get-Command cursor-agent` 必须打出 `cursor-agent.cmd` 的路径。没有路径就不要继续，守护进程起不来。

### 4. 按顺序注册

新开一个 PowerShell，先确认三个命令：

```powershell
Get-Command multica
git --version
Get-Command cursor-agent
```

第三条只在使用 Cursor 时需要。`multica` 或 `cursor-agent` 找不到时，先刷新 PATH 再查一次：

```powershell
$env:Path = [Environment]::GetEnvironmentVariable("Path", "User") + ";" + [Environment]::GetEnvironmentVariable("Path", "Machine")
```

三个都有结果之后，按这个顺序执行。

准备令牌：用已经登录的浏览器打开 `http://172.30.11.181:31383`，进入 **设置 → API Token**，新建一个令牌。完整内容只显示一次。

回到这台 Windows：

```powershell
multica setup self-host --server-url http://172.30.11.181:31469 --app-url http://172.30.11.181:31383

multica login --token

multica daemon start

multica daemon status
```

`multica login --token` 会等你粘贴令牌。命令成功时会列出这个账号能进入的工作区，并提示接着执行 `multica daemon start`。

`daemon start` 如果打印下面这句后退出，说明 `cursor-agent` 不在 PATH 里，这台电脑没有登记：

```text
no agent CLI found: install claude, ... cursor-agent, ... and ensure it is on PATH
```

回到上一节，确认 `Get-Command cursor-agent` 有路径，再执行 `multica daemon start`。

`daemon status` 显示进程仍在运行后，刷新网页的运行时页面。

开机登录后自动拉起：

```powershell
multica daemon autostart enable
```

## 确认是哪一台

运行时页面上的机器名就是这台电脑的主机名，名称后面的「你」表示所有者是当前登录的账号。

在这台电脑上执行：

```bash
hostname
```

Windows 的 PowerShell 里同样执行 `hostname`。输出和页面上的名称一致，就是这一台。

服务器收到登记后，这台电脑会按登录账号加入它有权访问的每个工作区，每个工作区一条 Cursor 运行时。进程停掉后，页面会变为离线。
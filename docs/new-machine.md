# 新电脑设置 / Setting up a new machine

[中文](#中文) · [English](#english)

## 中文

在一台什么都没装的 Mac 上，从 Twinnel 里一步步装好代理和 tailnet：后台助手、mihomo、订阅、Tailscale 共存和防泄漏、TUN、系统 DNS 指向 mihomo、Tailscale 安装并登录，最后跑一遍健康检查。

### 怎么用

- **App**：没有可接管的 mihomo 时，概览的「开始使用」里会出现「设置这台 Mac」。打开后填订阅链接，点「开始设置」。每一步都有状态；失败的一步可以单独「重试」；已经完成的步骤会自动跳过，所以中途退出后再打开会从停下的地方继续。
- **命令行**（Linux 没有 app 时也用它）：

  ```sh
  TWINNEL_SUBSCRIPTION_URL='https://…' twinnel setup      # 订阅链接是机密，别写在参数里
  twinnel setup status                                     # 只看哪些步骤已完成，不做改动
  twinnel setup --step mihomo --proxy subscription         # 单独重试一步，换下载方式
  twinnel setup --json --yes                               # 不提问，结果是 JSON
  ```

  其他参数：`--no-dns`（不动系统 DNS）、`--no-tailscale`、`--channel alpha`、`--import 备份.zip`（见下）。

### 步骤

| 步骤 | 做什么 |
|---|---|
| 安装后台助手 | `twinnel helper install`，需要一次管理员密码 |
| 添加订阅 | 保存订阅链接并下载一次（这时还没有代理，直连）；或者从备份导入 |
| 下载 mihomo | 从 GitHub 下载官方版本，校验 sha256，放在 root 所有的 `<状态目录>/kernels`；再下载 GEO 数据库（失败只提示） |
| 合成配置 | 内置基础设置（`overrides/base-tun-fakeip.yaml`：TUN、fake-ip、DNS 监听 127.0.0.1:53、控制接口只在本机）、Tailscale 共存层和防泄漏层（默认 Google、Meta 组）；用 `mihomo -t` 验证 |
| 启动 mihomo | 写入配置（自动生成控制接口密钥），安装 LaunchDaemon `com.l3ony2k.twinnel.mihomo` 并启动，等控制接口应答 |
| 系统 DNS | 先确认 127.0.0.1:53 有应答，再把每个启用的网络服务的 DNS 设为 127.0.0.1；原来的值存在 `<状态目录>/dns-backup.json` |
| 安装 Tailscale | 已经装了任何 Tailscale 就保留；否则装开源的 tailscaled（不装 App Store / 独立版 app，它们占用系统 VPN 通道） |
| 登录 Tailscale | 通过 LocalAPI 发起交互登录，在浏览器打开登录页，等到「已连接」 |
| 健康检查 | 刷新 tailnet 设备后跑一遍全部检查 |

### Tailscale 从哪来

- **Linux**：pkgs.tailscale.com 的官方静态包 `tailscale_<版本>_<架构>.tgz`，用同目录下的 `.sha256` 校验。
- **macOS**：pkgs.tailscale.com 没有 macOS 的静态包（只有 app），Tailscale 的 GitHub release 也没有附件。所以用 Homebrew 的 tailscale bottle（开源 tailscaled，Homebrew 编译）：版本和 sha256 来自 formulae.brew.sh 的 API，文件从 ghcr.io 下载并校验。只有 Apple 芯片的 bottle；Intel Mac 会提示用 `brew install tailscale`。
- 装在 `<状态目录>/tailscale/<版本>/`，LaunchDaemon 用 `com.tailscale.tailscaled`（和 `tailscaled install-system-daemon` 相同），socket 是默认的 `/var/run/tailscaled.socket`。

### 下载不了怎么办（先有鸡还是先有蛋）

GitHub、pkgs.tailscale.com、ghcr.io 在一些网络里连不上，而这时代理还没装好。Twinnel 的做法：

1. 先拿订阅：订阅地址通常能直连，所以它排在所有 GitHub 下载之前。
2. 下载失败时可以换路线重试这一步：
   - **用订阅的节点下载**（`--proxy subscription`）：后台助手用合成好的配置临时启动一个 mihomo——不开 TUN、不开 DNS、没有控制接口、只在 127.0.0.1 的随机端口监听、所有连接走第一个策略组——以 nobody 身份运行，下载完就停。
   - **其他代理**（`--proxy http://…` / `socks5://…`）：比如局域网里另一台电脑的代理。
   - **直连**（`--proxy direct`）。
3. mihomo 启动以后（TUN 已开），Tailscale 的下载自然走代理。

**限制**：临时 mihomo 需要一个 mihomo 程序。一台全新的机器在 mihomo 下载成功之前没有，所以「用订阅的节点下载」对第一次下载 mihomo 本身只有在 app 自带种子内核时才有用（后台助手会找 `<状态目录>/kernels/seed/mihomo`；目前发布包还不带，是否随 app 打包 mihomo（GPL-3）需要维护者决定）。在那之前，第一次下载 mihomo 只能直连或用其他代理。

### 撤销

- `twinnel helper uninstall`（以及 app 里的卸载）会**最先**把系统 DNS 改回 `dns-backup.json` 里记录的值，再做其他事；改回失败就停下，什么都不删。
- 设置装上的 mihomo 服务和数据由卸载计划处理（`--purge` 在 mihomo 还用着 Twinnel 的数据时会拒绝）。Tailscale 不动。

### 从备份导入

从另一台机器导出的 zip（停止使用 → 导出配置）可以代替订阅链接：`twinnel setup --import 备份.zip`。导入本身由后台助手的 `config.import` 完成（另一个分支在做）；这个分支只留了接口（`setup.Env.ImportConfig`），没有它时导入会提示「这个版本还不能导入备份」。备份里有节点密码，可能还有订阅链接：用完请删除。

## English

On a Mac with nothing installed, Twinnel sets up the proxy and the tailnet from inside the app: the helper, mihomo, your subscription, the Tailscale coexistence and leak-guard layers, TUN, the system DNS pointing at mihomo, Tailscale installed and logged in, then a health check.

### How

- **App**: when there is no mihomo to take over, Overview's "Get started" offers "Set up this Mac". Enter the subscription URL and click Set Up. Each step shows its state; a failed step has Try Again; steps already done are skipped, so reopening the page continues where it stopped.
- **CLI** (also the way on Linux, which has no app):

  ```sh
  TWINNEL_SUBSCRIPTION_URL='https://…' twinnel setup      # the URL is a secret: not on the command line
  twinnel setup status                                     # which steps are done; changes nothing
  twinnel setup --step mihomo --proxy subscription         # retry one step another way
  twinnel setup --json --yes                               # no questions, JSON result
  ```

  Also: `--no-dns`, `--no-tailscale`, `--channel alpha`, `--import backup.zip` (below).

### Steps

| Step | What it does |
|---|---|
| Install the helper | `twinnel helper install`, one administrator prompt |
| Add a subscription | stores the URL and fetches it once (directly: no proxy yet); or import a backup |
| Download mihomo | the official release from GitHub, sha256-verified, into root-owned `<state>/kernels`; then the GEO databases (a failure there is only a note) |
| Compose the config | the built-in base settings (`overrides/base-tun-fakeip.yaml`: TUN, fake-ip, DNS on 127.0.0.1:53, controller on loopback), the Tailscale coexist and leak-guard layers (Google and Meta groups by default); validated with `mihomo -t` |
| Start mihomo | writes the config (a controller secret is generated), installs and starts the LaunchDaemon `com.l3ony2k.twinnel.mihomo`, waits until the controller answers |
| System DNS | checks that 127.0.0.1:53 answers, then sets the DNS of every enabled network service to 127.0.0.1; the old values go to `<state>/dns-backup.json` |
| Install Tailscale | any existing Tailscale install is kept; otherwise the open-source tailscaled (never the App Store or standalone app: they take the system VPN slot) |
| Log in to Tailscale | starts an interactive login over the LocalAPI, opens the login page, waits for Running |
| Health | refreshes the tailnet's devices and runs every check |

### Where Tailscale comes from

- **Linux**: the official static tarball from pkgs.tailscale.com, verified against the `.sha256` file next to it.
- **macOS**: pkgs.tailscale.com has no macOS tarball (only the app) and Tailscale's GitHub releases have no assets, so Twinnel uses Homebrew's bottle of the tailscale formula (the open-source tailscaled built by Homebrew): version and sha256 from the formulae.brew.sh API, bytes from ghcr.io, verified. Bottles exist for Apple silicon only; an Intel Mac is told to `brew install tailscale`.
- Installed under `<state>/tailscale/<version>/`, LaunchDaemon `com.tailscale.tailscaled` (as `tailscaled install-system-daemon` makes), default socket `/var/run/tailscaled.socket`.

### When downloads fail (chicken and egg)

GitHub, pkgs.tailscale.com and ghcr.io are blocked on some networks, and there is no proxy yet. So:

1. The subscription comes first: its URL is usually reachable directly.
2. A failed download can be retried another way:
   - **Through the subscription** (`--proxy subscription`): the helper starts a temporary mihomo with the composed config — no TUN, no DNS, no controller, one random loopback port, everything to the first proxy group — as nobody, and stops it after the download.
   - **Another proxy** (`--proxy http://…` / `socks5://…`), e.g. a machine on your network.
   - **Directly** (`--proxy direct`).
3. Once mihomo runs (TUN on), the Tailscale download goes through it anyway.

**Limit**: the temporary mihomo needs a mihomo binary. A brand-new machine has none until the first kernel download succeeds, so "through the subscription" helps that first download only when the app ships a seed kernel (the helper looks for `<state>/kernels/seed/mihomo`; releases don't ship one yet — bundling mihomo (GPL-3) with the app is the maintainer's call). Until then the first kernel download is direct or through another proxy.

### Undo

- `twinnel helper uninstall` (and Uninstall in the app) writes the system DNS back from `dns-backup.json` **first**; if that fails it stops and removes nothing.
- The mihomo service and data the setup installed are handled by the uninstall plan (`--purge` refuses while mihomo runs from Twinnel's data). Tailscale is left alone.

### Importing a backup

A zip exported on another machine (Stop Using → Export) can replace the subscription URL: `twinnel setup --import backup.zip`. The import itself is the helper's `config.import` (built on another branch); this branch has only the hook (`setup.Env.ImportConfig`) and says "importing a backup is not available in this version yet" without it. The zip holds node passwords and maybe subscription URLs: delete it when you're done.

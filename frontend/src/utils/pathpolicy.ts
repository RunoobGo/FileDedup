// 路径归属与卷身份的纯判据。前端计数（「将处理 N 项」「已排除 M 项」）
// 全靠它们，因此必须与后端 internal/ops/keep.go 的 inDir 保持**同一语义**。
//
// ★ 2026-09-20（审查 M3）：此前 dirContains 按原样比大小写，而注释声称
// 「与后端 ops.inDir 同语义」。后端实际会按卷的大小写语义折叠
// （internal/fscase），于是在 macOS/Windows 默认的不敏感卷上，用户手输
// `C:\Users\Dups` 匹配不到扫描结果的 `c:\users\dups\a.bin`：后端会处理它，
// 前端却把它算成"被排除"。计数说假话正是本项目最忌讳的缺陷类别。

// fsCaseSensitiveForPath 从**扫描结果路径的形状**判定该卷的大小写语义。
//
// 为什么不用 runtime.GOOS / navigator.userAgent：前端拿不到 Go 侧的
// runtime.GOOS（wailsjs 绑定里没有该导出），而 UA 在跨平台构建下会说谎。
// 扫描路径由后端 filepath.Join 生成，分隔符与盘符形状**必然**反映运行机器：
//   - `X:\...` → Windows（NTFS 默认不敏感）
//   - `/Volumes/...` → macOS 数据卷（APFS 根卷默认不敏感）
//   - 其余 POSIX → 按 Linux/BSD 惯例区分大小写
//
// 这与 internal/fscase 的**平台默认**取值一致；后端真正的权威是**按卷实测**
// （写混合大小写探针再反向 Lstat），前端拿不到探测结果。偏差方向只是
// "计数偏一档"，执行范围最终由后端 ApplyProcessPolicy 决定，
// 不会因此多删或少删任何文件。
export function fsCaseSensitiveForPath(p: string): boolean {
  if (!p) return true
  if (/^[A-Za-z]:[\\/]/.test(p) || p.includes('\\')) return false // Windows
  if (p.startsWith('/Volumes/')) return false // macOS
  return true
}

// foldPath 归一化路径用于「同一路径/前缀」比较：统一为 "/" 分隔，
// 不敏感卷上再折叠大小写。与 fscase.Fold 逐行等价。
export function foldPath(p: string, sensitive: boolean): string {
  const q = p.replace(/\\/g, '/')
  return sensitive ? q : q.toLowerCase()
}

// normDir 归一优先目录：去空白、去尾部分隔符。根目录 "/" 或 "C:\"
// 剥完仍保留（否则会退化成"前缀匹配一切"的意外行为）。
export function normDir(dir: string): string {
  let d = (dir ?? '').trim()
  if (!d) return ''
  if (d.length > 1) d = d.replace(/[/\\]+$/, '')
  if (d.length === 1 && d === '/') return '/'
  // "C:\" 剥完剩 "C:"，补回分隔符以免 "C:foo" 被误判为在 "C:" 下
  if (/^[A-Za-z]:$/.test(d)) d += '\\'
  return d
}

// dirContains 报告 path 是否位于 dir 之下（含子目录、含 dir 自身）。
// 与后端 inDir 同语义：先折叠分隔符与（不敏感卷上的）大小写，再比前缀，
// 且只在**完整路径段**边界上命中（/a 不含 /ab/c.bin）。
//
// 盘根要单独收口：`C:\` 折叠去尾分隔符后剩 `c:`，若直接当前缀会把
// `C:foo`（同卷相对路径）也命中。后端 inDirFold 在同一处有同样的口径问题
// （Windows 路径经 filepath.Clean 仍是 `C:\`、折叠成 `c:/` 后剥尾只剩 `c:`），
// 但它的 dir 来自扫描根与选择器，实际到不了盘根；前端**允许用户手输**，
// 所以这里补齐。
export function dirContains(dir: string, path: string, sensitive: boolean): boolean {
  const d = normDir(dir)
  if (!d) return false
  const prefix = foldPath(d, sensitive).replace(/\/+$/, '')
  const folded = foldPath(path, sensitive)
  if (prefix === '' || prefix === '/') return folded.startsWith('/') // POSIX 根：全部绝对路径
  if (/^[a-z]:$/i.test(prefix)) return folded.startsWith(prefix + '/') // 盘根：该卷全部
  return folded === prefix || folded.startsWith(prefix + '/')
}

// isGroupCrossVolume 判断一个重复组是否跨卷。
//
// 保守优先：**任何**成员缺少可信卷标识 → 返回 false（按同卷处理、不显示
// 入口）。理由：跨卷判错的代价不对称——假阳性让用户点进一个必然失败的流程
// （未提权时全部报"需要权限"），假阴性只是少一个入口，用户仍可先移动再处理。
//
// ★ 2026-09-20（审查 M7）：修正前写作 `some(f => !f.volumeResolved || …)`，
// 任一**其他**成员未解析时反而判成"跨卷"——正是注释要避免的假阳性。
export function isGroupCrossVolume(
  files: { volume: string; volumeResolved: boolean }[],
): boolean {
  if (files.length < 2) return false
  if (files.some(f => !f.volumeResolved)) return false
  const first = files[0].volume
  return files.some(f => f.volume !== first)
}

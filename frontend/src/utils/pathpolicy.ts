// 前端**保留**的路径判据：只剩卷比较这一条（isGroupCrossVolume），
// 因为它的输入是后端已经算好的 volume 字段，前端只做相等比较、不解释内容。
//
// ★ 2026-09-20（AS-H6 / 决策 D-1 方案 b）：这里原有的一套
// 「路径是否属于目录」判据（fsCaseSensitiveForPath / foldPath / normDir / dirContains）
// 已经删除。它是 internal/ops/keep.go 的 inDir 之外的**第二份实现**，
// 两处独立演化必然漂移，这次漂移的方向是**少报命中**：界面把后端其实会处理的
// 文件说成"已排除"，等于对"不会动的文件"做了假承诺。
// 现在「将处理 N / 已排除 M」直接用后端 App.FilterInDirs 的真值
// （见 stores/scan.ts 的 refreshProcCounts），前端只负责显示。

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

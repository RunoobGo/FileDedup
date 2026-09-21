// 剪贴板写入（M83 起）：唯一出口，失败路径**必须**产出一次抛错。
//
// 为什么单独立一个文件：`FailedDrawer.vue` 改前写的是
//   navigator.clipboard?.writeText(text).catch((e) => toast.notifyError('复制失败', e))
// 可选链短路时整个表达式是 `undefined`，`.catch` 从未挂上 ⇒ 没有剪贴板的环境
// （非安全上下文、无授权、部分 Linux WebKit 后端）点「复制全部」**完全静默**：
// 用户以为自己复制到了，粘贴时才发现什么都没有。这不是概率问题，是必然（见 P-20-4 第一格）。
//
// `clip` 参数是给测试留的接缝：Node 24 的全局 `navigator` 是 getter-only
// （实测 Object.getOwnPropertyDescriptor 得 {get:true, set:false, configurable:true}），
// 测试里没法靠改全局造"有剪贴板"的环境，注入参数比篡改全局干净。

/** 只用到 writeText；用结构化类型而不是 DOM 的 Clipboard，测试桩才不必假装是浏览器。 */
export interface ClipboardLike {
  writeText(text: string): Promise<void>
}

function globalClipboard(): ClipboardLike | undefined {
  return globalThis.navigator?.clipboard as ClipboardLike | undefined
}

// copyText 把 text 写进剪贴板；拿不到剪贴板或写入被拒一律抛，错误文本给人话。
// 成功时不抛、不改写内容（制表符分隔是排障用的列结构）。
export async function copyText(text: string, clip: ClipboardLike | undefined = globalClipboard()): Promise<void> {
  if (!clip) {
    throw new Error('本机没有可用的剪贴板接口（可能是非安全上下文或未授权），请手动选取后复制')
  }
  try {
    await clip.writeText(text)
  } catch (e: any) {
    const detail = String(e?.message ?? e ?? '未知原因')
    throw new Error(`剪贴板写入被拒：${detail}`)
  }
}

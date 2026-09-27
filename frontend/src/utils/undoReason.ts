// 「这条记录为什么不可回撤」的**唯一文案方**（M79，2026-09-22 裁定"判据归后端、文案归前端"）。
//
// 为什么要有这个文件：这两段解释原先存在两份——后端 `app.go` 的 error 正文（进 toast）
// 与 `RecordsView.vue` 的徽标 title，两份措辞已经各自漂移；而文案写死在后端意味着
// 改一个字都要重发二进制。裁定把**判据**留在后端（`undoableFor` / `undoReasonCodeFor`：
// 哪一类操作在这个平台不可撤、原因是哪一码），把**中文**收进这里，后端只下发码。
//
// ★ 两体并存而非合并成一段：toast 体逐字等于原 `app.go` 那两句，title 体逐字等于原
// `RecordsView.vue` 那两句。合并会改写其中一条腿今天的话——那是"文案改写"，不是搬家。
// 未知码必须**可见**：兜底句点名那个码，别让"后端加了一码、前端没跟"退化成静默空白。

// 与 app.go 的 undoCodeWindowsTrash / undoCodePermanentDelete 逐字相同（前后端契约）。
export const UNDO_CODE_WINDOWS_TRASH = 'undo-code-windows-trash'
export const UNDO_CODE_PERMANENT_DELETE = 'undo-code-permanent-delete'

const WINDOWS_TRASH_TEXT =
  'Windows 回收站操作不支持应用内回撤：系统 API 不返回' +
  '「每个文件落在回收站的哪个位置」的映射，应用无法定位文件而把它搬回原处。' +
  '文件本身仍在回收站里，请点上方「打开系统回收站」，右键选择「还原」即可取回。'

const PERMANENT_DELETE_TEXT =
  '永久删除不支持回撤：文件已从磁盘移除，没有任何可恢复的来源。' +
  '若还需保留这些文件，请重新扫描后改用「移入回收站」或「移动」。'

const WINDOWS_TRASH_TITLE =
  'Windows 回收站不支持应用内回撤：系统 API 不返回每个文件的落点映射，' +
  '应用无法定位后搬回原处。文件仍在回收站里，请用「打开系统回收站」右键「还原」。'

const PERMANENT_DELETE_TITLE =
  '永久删除不支持回撤：文件已从磁盘移除，没有可恢复的来源。' +
  '若仍需保留，请改用「移入回收站」或「移动」。'

/** 认得的码；不是码的一律返回空串（调用方据此原样透传）。 */
function knownCode(code: string): string {
  return code === UNDO_CODE_WINDOWS_TRASH || code === UNDO_CODE_PERMANENT_DELETE ? code : ''
}

/**
 * toast 体：后端回撤失败 error 的**正文**（已被 toast.errText 归一成字符串）。
 * 认不出码时原样返回入参——回撤链上还有"在途""记录不存在"这类非码 error，
 * 把它们改写成"不可回撤的解释"就是假话。
 */
export function undoBlockedText(code: string): string {
  switch (knownCode(code)) {
    case UNDO_CODE_WINDOWS_TRASH:
      return WINDOWS_TRASH_TEXT
    case UNDO_CODE_PERMANENT_DELETE:
      return PERMANENT_DELETE_TEXT
    default:
      return code
  }
}

/** 徽标 title 体；未知码的兜底不带裸码（悬浮说明里塞内部标识符没意义）。 */
export function undoBlockedTitle(code: string): string {
  switch (knownCode(code)) {
    case UNDO_CODE_WINDOWS_TRASH:
      return WINDOWS_TRASH_TITLE
    case UNDO_CODE_PERMANENT_DELETE:
      return PERMANENT_DELETE_TITLE
    default:
      return '这条记录不支持应用内回撤（未登记的原因码）'
  }
}

/**
 * 记录列表这一侧把 `kind` 映成码——**不是**可撤性判据（那由后端的 `undoable` 定，
 * 徽标只在 `undoable === false` 时才渲染，所以非 Windows 的 trash 走不到这一支），
 * 只决定"给一条已经判了不可撤的记录印哪一句"。
 */
export function undoTitleCodeForKind(kind: string): string {
  return kind === 'trash' ? UNDO_CODE_WINDOWS_TRASH : UNDO_CODE_PERMANENT_DELETE
}

/**
 * 「移入回收站」确认模态的正文（M282，2026-09-27）。
 *
 * ★ 为什么这句也住在本文件：改动前它是 `ConfirmDialog.vue` 里内联的一句
 * 「文件将移入系统回收站，可随时还原。」，而同一批文件在「记录」页拿到的徽标是
 * 「不可回撤」——真机读数把这两句各拍了一张（`b5_modal2.png` / `b5_detail.png`）。
 * 两句**各说一件事**（能还原的是 OS 回收站，做不到的是应用内一键回撤），但用户先读
 * 前者、后读后者，就得到一条自相矛盾的承诺。M79 已经把"这条记录为什么不可回撤"收进
 * 本文件当唯一文案方，M282 是同一族的另一半：**动手之前**那句预告。
 *
 * ★ 措辞必须平台中立：前端拿不到 GOOS（`wails.ts` 没有平台字段），所以这里不能写
 * "本应用不提供回撤"——macOS/Linux 的回收站去向**是**可回撤的，那样写在那些平台上
 * 是假话。写法改成"以记录页该条操作的徽标为准"，把准绳交给后端的判据（`undoable`）。
 */
export const TRASH_MODAL_DESC =
  '文件将移入系统回收站（不是永久删除，数据还在回收站里）。' +
  '要取回它们，请打开系统回收站后右键「还原」。' +
  '本应用是否提供一键回撤，以「记录」页该条操作的徽标为准：' +
  '标着「不可回撤」时（Windows 的回收站操作即如此），只能到系统回收站里手动还原。'


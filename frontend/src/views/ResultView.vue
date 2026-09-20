<script setup lang="ts">
// 结果页（M3 版）：统计条 + 保留策略工具栏 + 操作按钮 + 执行反馈 + 组列表。
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useScanStore } from '../stores/scan'
import { useToastStore } from '../stores/toast'
import GroupCard from '../components/GroupCard.vue'
import ConfirmDialog from '../components/ConfirmDialog.vue'
import Icon from '../components/Icon.vue'
import { humanBytes, formatCount } from '../utils/format'
import { isGroupCrossVolume } from '../utils/pathpolicy'
import type { OpKind } from '../wails'

const store = useScanStore()
const toast = useToastStore()
const confirmKind = ref<OpKind | null>(null)

// 跨卷检测（2026-09-20）：决定是否显示「软链接合并」入口，以及是否显示悬空风险条。
//
// 为什么必须"组内至少一组跨卷"才显示按钮，而不是永远显示：
// 硬链接跨卷必然失败（文件系统级限制，无解），软链接在同卷则只有净损失——
// 它需要管理员权限，还会引入悬空风险，而硬链接零权限且删除任一名字数据都还在。
// 提示一个"能用但更差"的选项不是自由，是把用户往坑里带（见 ops/symlink.go 头注释）。
//
// 两个前提：
//   ① 必须用**已解析**的卷身份（volumeResolved）。Windows 上盘符不等于卷
//      （挂载点会把别的卷挂到某目录下），拿路径前缀猜会判错，宁可不出按钮。
//   ② 已加载的组里有一组跨卷即可（组列表是分页的，未加载的组里也可能有跨卷组——
//      这是刻意的：按钮是全局入口，作用在"已在列表中选中的项"上，
//      选中集合不可能包含未加载的组）。
const crossVolumeGroups = computed(() => {
  let n = 0
  for (const g of store.groups) {
    if (isGroupCrossVolume(g.files)) n++
  }
  return n
})
const hasCrossVolume = computed(() => crossVolumeGroups.value > 0)

// isGroupCrossVolume 判断一个重复组是否跨卷 —— 实现收在 utils/pathpolicy.ts，
// 保守语义（任一成员缺可信卷身份即按同卷处理）在那里说明。
//
// ★ 2026-09-20（审查 M7）：修正前的本地实现写作
// `some(f => !f.volumeResolved || f.volume !== first.volume)`，
// 于是**其他**成员未解析时反而判成跨卷，正好做出注释禁止的假阳性——
// 用户点进一个必然失败的流程。

const keepKind = ref('shortest')
const keepDirInput = ref('')

// store.confirmOpen 单一出口：弹框期间挂起全局快捷键（App.vue 用它拦截
// Space/Cmd+A/Delete，避免确认框打开时选中集被悄悄扩大），关闭后自动恢复。
// 修复点：原先由各按钮/回调手工赋值，trash/hardlink 分支漏置、onConfirm 漏复位，
// 导致确认过一次后拦截条件永久为假。
watch(confirmKind, v => { store.confirmOpen = !!v })

// showWarnings 提示明细（残留、权限汇总等）。
//
// 2026-09-20：措辞由"有 N 个临时文件未能删除"改为中性表述。原因：Warnings
// 现在承载两类完全不同的内容——① 残留文件（可自行删除）、② 环境级失败指引
// （软链接权限：**必须提权/开开发者模式后重试**）。沿用旧措辞会把后者
// 描述成"临时文件未删除"，用户按提示去删文件，而真正该做的是提权。
function showWarnings() {
  const ws = store.opsResult?.Warnings ?? []
  if (!ws.length) return
  toast.push(`有 ${ws.length} 条需要你关注的提示，可悬停查看明细`, 'error', 12000)
  for (const w of ws.slice(0, 5)) toast.push(w, 'error', 12000)
  if (ws.length > 5) toast.push(`……另有 ${ws.length - 5} 条，详见提示条悬停内容`, 'error', 12000)
}

// warnLabel 按内容给出合适的按钮短标签：权限类提示是"必读指引"，
// 不该与"临时文件残留"共用一个含糊的"提示"。
const warnLabel = computed(() => {
  const ws = store.opsResult?.Warnings ?? []
  return ws.some(w => w.includes('权限')) ? '权限' : '提示'
})

// C12：切页时本组件卸载，但局部 confirmKind 不会自动复位——切回结果页会
// 自动重开确认弹窗（且与 store.confirmOpen 脱钩，快捷键拦截失真）。卸载时复位。
// 此处仍手工清 confirmOpen：组件作用域已停，上面的 watch 不会再触发。
onUnmounted(() => {
  confirmKind.value = null
  store.confirmOpen = false
})

onMounted(() => {
  if (store.hasResult && store.groups.length === 0) store.loadResultPage(false)
})

// 列表替换时的勾选清理由 store 统一处理（Y8：默认不勾选，需显式选择）
function changeSort() { store.loadResultPage(false) }
function changeExt() { store.loadResultPage(false) }

function onScroll(e: Event) {
  const el = e.target as HTMLElement
  if (el.scrollTop + el.clientHeight >= el.scrollHeight - 200) {
    // Y7：滚动追加受放量上限约束；loadingPage 防止快速滚动重复排队拉取
    if (
      store.groups.length < store.totalGroups &&
      store.groups.length < store.loadCap &&
      !store.loadingPage
    ) {
      store.loadResultPage(true)
    }
  }
}

// 已达上限时按钮标注「继续加载」并放开一档
const capped = computed(() => store.groups.length >= store.loadCap && store.totalGroups > store.groups.length)
const moreLabel = computed(() =>
  capped.value
    ? `继续加载（已加载 ${formatCount(store.groups.length)} 组，点击再取一档）`
    : `加载更多（${formatCount(store.groups.length)}/${formatCount(store.totalGroups)}）`,
)

async function applyKeep() {
  await store.applyKeep(keepKind.value, store.keepDirs)
}

const keepDirsDisabled = computed(() => keepKind.value === 'directory' && store.keepDirs.length === 0)

async function pickKeepDir() {
  const { api } = await import('../wails')
  try {
    const dir = await api.selectDirectory()
    if (dir) store.addKeepDir(dir)
  } catch (e: any) {
    toast.notifyError('选择目录失败', e)
  }
}

function addKeepDirInput() {
  store.addKeepDir(keepDirInput.value)
  keepDirInput.value = ''
}

// ---------- 处理策略（2026-09-20）：仅执行时的过滤器 ----------
//
// 为什么开关放 store、不放心这个组件的局部 ref：
// 结果页是可卸载的（切到设置/记录页会销毁），而 store.procDirs 是跨页保留的。
// 若 procKind 是局部 ref，用户切走再切回时开关会回到"全部勾选项"，
// 而 store.procDirs 里的目录还在——界面上开关说"不启用"，
// 后端收到的请求（executeOp 读的是 store.procDirs，不是这个开关）却仍在收窄。
// 那就是"界面说的"和"实际做的"分叉，正是本功能最该避免的一类缺陷。
// 因此启用判据只有一个：store.procActive（= store.procDirs 里有非空白项）。
// 这个下拉框只是 store.procDirs 的一个视图，选"全部"就等于清空列表。
//
// 2026-09-20 审查修正：原先 get 直接绑 procActive，形成死锁——
// 添加目录的面板是 v-if="procKind === 'dirs'"，而 procKind 只有在
// procDirs 已非空时才是 'dirs'，且 setter 忽略 'dirs' → 用户永远打不开
// 面板，功能整条不可达。现拆成两层：面板开合是本组件的 UI 状态，
// 是否生效仍以 store.procActive 为准（下面的命中数展示即为证据）。
const procPanelOpen = ref(store.procActive)
watch(() => store.procActive, (v) => { if (v) procPanelOpen.value = true })
const procKind = computed<'all' | 'dirs'>({
  get: () => (procPanelOpen.value ? 'dirs' : 'all'),
  set: (v) => {
    procPanelOpen.value = v === 'dirs'
    if (v === 'all') store.clearProcDirs()
  },
})
const procDirInput = ref('')

async function pickProcDir() {
  const { api } = await import('../wails')
  try {
    const dir = await api.selectDirectory()
    if (dir) store.addProcDir(dir)
  } catch (e: any) {
    toast.notifyError('选择目录失败', e)
  }
}

function addProcDirInput() {
  store.addProcDir(procDirInput.value)
  procDirInput.value = ''
}

// 只改 confirmKind：store.confirmOpen 的复位由上方 watch 统一负责（不再手工赋值）
function onConfirm(targetDir?: string) {
  const kind = confirmKind.value!
  confirmKind.value = null
  store.executeOp(kind, targetDir, kind === 'delete')
}
</script>

<template>
  <div class="result-view">
    <!-- v0.5.0 功能 3：历史结果提示条 -->
    <div v-if="store.histResult" class="panel hist-banner">
      当前为历史结果（保存于 {{ new Date(store.histResult.savedAt * 1000).toLocaleString('zh-CN', { hour12: false }) }}），清理前会逐文件校验内容。
    </div>
    <!-- 统计条（P1-2：无重复组且无失败项时整块隐藏，不再渲染全 0 统计与无效的排序/过滤控件） -->
    <div v-if="store.totalGroups > 0 || store.failed.length" class="statbar panel">
      <template v-if="store.totalGroups > 0">
        <div class="stat"><b>{{ formatCount(store.totalGroups) }}</b> 重复组</div>
        <div class="stat"><b>{{ humanBytes(store.reclaimableTotal) }}</b> 可释放</div>
      </template>
      <!-- P2-3：失败项入口原是可点击 <div>，鼠标可达但键盘完全到不了（无 tabindex、
           无键盘处理、无 role）。改用原生 <button>，role/聚焦/Enter/Space 全部由元素自带。 -->
      <button v-if="store.failed.length" type="button" class="stat warn"
        @click="store.failedOpen = true" title="查看失败清单">
        <b>{{ formatCount(store.failed.length) }}</b> 失败项
      </button>
      <div class="spacer"></div>
      <template v-if="store.totalGroups > 0">
        <select v-model="store.resultSort" @change="changeSort" aria-label="结果排序方式">
          <option value="reclaimable">按可释放空间</option>
          <option value="size">按文件大小</option>
          <option value="count">按文件数</option>
        </select>
        <input v-model="store.resultExt" @change="changeExt" type="text" aria-label="按扩展名过滤"
          placeholder="扩展名，如 .jpg" style="width: 120px" />
        <button v-if="store.totalGroups > store.groups.length" class="btn-ghost"
          :class="{ capped }" @click="store.loadMore()">
          {{ moreLabel }}
        </button>
      </template>
    </div>

    <!-- 保留策略 + 操作工具栏（M3） -->
    <!-- P0-4：拆成两条稳定的整行——第 1 行「保留策略 + 选择状态」，第 2 行「操作按钮」。
         原先依赖 flex 换行，在 MinWidth(920) 下会塌成 3 行并把「永久删除」挤到单独一行。 -->
    <div v-if="store.hasResult && store.totalGroups > 0" class="toolbar panel">
      <div class="trow">
        <div class="keep">
          <!-- P2-3：`.lbl` 是 span，不构成 label 关联；补 aria-label 让下拉框有名称 -->
          <span class="lbl" aria-hidden="true">保留策略</span>
          <select v-model="keepKind" aria-label="保留策略">
            <option value="shortest">路径最短（默认）</option>
            <option value="newest">最新</option>
            <option value="oldest">最旧</option>
            <option value="directory">指定目录优先级…</option>
          </select>
          <button class="btn-ghost" :disabled="keepDirsDisabled || store.busy"
            :title="store.busyTip || '按所选策略标出保留项'" @click="applyKeep">应用</button>
          <button class="btn-ghost" :disabled="store.busy"
            :title="store.busyTip || '清除决策回到默认建议'" @click="store.clearKeep()">重置</button>
        </div>
        <span class="sel-info" aria-live="polite">
          <template v-if="store.selectedFiles.length">
            已选 <b>{{ store.selectedFiles.length }}</b> 项 / <b>{{ humanBytes(store.selectedBytes) }}</b>
            <!-- 收窄时把生效数也摆出来。两个数并排，落差一眼可见；
                 只显示"已选 N"而操作按钮按 effectiveCount 禁用，
                 用户会看到"选了 40 项，按钮却是灰的"这种无从解释的状态。 -->
            <template v-if="store.procFiltering">
              ，其中<b>{{ store.effectiveCount }}</b> 项在优先文件夹内 / <b>{{ humanBytes(store.effectiveBytes) }}</b>
            </template>
          </template>
          <template v-else>未选择——点「全选」选中全部冗余项</template>
        </span>
      </div>
      <!-- 多目录优先级列表：序号即优先级，命中多个目录时保留最靠前目录内的文件 -->
      <div v-if="keepKind === 'directory'" class="keepdirs">
        <div v-for="(d, i) in store.keepDirs" :key="d" class="kd-row">
          <span class="kd-idx" :title="`优先级 ${i + 1}`">{{ i + 1 }}</span>
          <span class="kd-path" :title="d">{{ d }}</span>
          <button class="btn-ghost xs" :disabled="i === 0" title="上移（提高优先级）"
            aria-label="上移目录" @click="store.moveKeepDir(i, -1)">↑</button>
          <button class="btn-ghost xs" :disabled="i === store.keepDirs.length - 1"
            title="下移（降低优先级）" aria-label="下移目录" @click="store.moveKeepDir(i, 1)">↓</button>
          <button class="btn-ghost xs" title="移除该目录" aria-label="移除目录"
            @click="store.removeKeepDir(i)">✕</button>
        </div>
        <div class="kd-add">
          <button class="btn-ghost" @click="pickKeepDir">＋ 添加目录</button>
          <input v-model="keepDirInput" type="text" aria-label="粘贴目录路径"
            placeholder="粘贴目录路径后回车添加" @keyup.enter="addKeepDirInput" />
          <span v-if="!store.keepDirs.length" class="kd-hint">
            尚未添加目录：按优先级从高到低排列，组内文件命中多个目录时保留最靠前目录内的那份
          </span>
        </div>
      </div>
      <!-- 处理策略（2026-09-20）：只作为**执行时**的过滤器，不改变勾选。
           语义是"优先处理的文件夹"——只处理落在这里面的重复文件。
           与保留策略的关系：处理策略只能让操作做得更少；保留项在执行器里是
           硬拒绝的，所以「保留策略优先」是结构性的，不依赖这里的任何判断。
           放在 .trow 之下、.ops 之上：用户先定保留谁，再定动谁，最后点操作。 -->
      <div class="procrow">
        <div class="keep">
          <span class="lbl" aria-hidden="true">处理策略</span>
          <select v-model="procKind" aria-label="处理策略">
            <option value="all">全部勾选项（默认）</option>
            <option value="dirs">仅勾选文件夹内的文件…</option>
          </select>
          <!-- 这里刻意**没有**"应用"按钮：处理策略不是后端决策，
               它是执行时随请求发出去的过滤器（OpRequest.ProcessDirs），
               所以不需要像保留策略那样先"应用"再执行。 -->
          <span v-if="procKind === 'dirs'" class="proc-hint">
            保留策略仍优先：保留项即使在优先文件夹内也不会被处理
          </span>
        </div>
      </div>
      <div v-if="procKind === 'dirs'" class="procdirs">
        <div v-for="(d, i) in store.procDirs" :key="d" class="kd-row">
          <!-- 无序号、无 ↑↓：这是**并集**不是优先级。
               保留策略要在多个目录里挑一个（所以有序、所以要排序按钮）；
               处理策略是"这些目录里的都算"，给排序按钮等于暗示存在优先级，
               那是错的心理模型。 -->
          <span class="kd-path" :title="d">{{ d }}</span>
          <button class="btn-ghost xs" title="移除该目录" aria-label="移除目录"
            @click="store.removeProcDir(i)">✕</button>
        </div>
        <div class="kd-add">
          <button class="btn-ghost" @click="pickProcDir">＋ 添加目录</button>
          <input v-model="procDirInput" type="text" aria-label="粘贴优先处理的目录路径"
            placeholder="粘贴目录后回车添加" @keyup.enter="addProcDirInput" />
          <span v-if="!store.procDirs.length" class="kd-hint">
            尚未添加目录：未添加时不影响操作，全部勾选项照常处理
          </span>
        </div>
      </div>
      <div class="ops">
        <button class="btn-ghost" :class="{ 'btn-emph': store.selectedFiles.length === 0 }"
          title="选中全部冗余项（保留项不可勾选）" @click="store.selectAll()">全选</button>
        <button class="btn-ghost" @click="store.clearSelection()">清除</button>
        <button class="btn-primary" :disabled="store.effectiveCount === 0 || store.busy"
          :title="store.busyTip || '把所选重复文件移入系统回收站'" @click="confirmKind = 'trash'">移入回收站</button>
        <button class="btn-ghost" :disabled="store.effectiveCount === 0 || store.busy"
          :title="store.busyTip || '移动到本会话授权的目录'" @click="confirmKind = 'move'">移动到…</button>
        <button class="btn-ghost" :disabled="store.effectiveCount === 0 || store.busy"
          :title="store.busyTip || '替换为指向保留文件的硬链接（同卷）'"
          @click="confirmKind = 'hardlink'">硬链接合并</button>
        <!-- 跨卷软链接合并（2026-09-20）：仅当已加载的重复组里存在跨卷组时出现。
             同卷组不显示——同卷用硬链接是零权限、无悬空风险的严格更优解，
             给一个"能用但更差"的选项不是自由。 -->
        <button v-if="hasCrossVolume" class="btn-ghost" :disabled="store.effectiveCount === 0 || store.busy"
          :title="store.busyTip || '把所选冗余项替换为指向保留文件的软链接（可跨卷；需要权限，且保留文件被删/移动后会失效）'"
          @click="confirmKind = 'symlink'">软链接合并（跨卷）</button>
        <button class="btn-danger" :disabled="store.effectiveCount === 0 || store.busy"
          :title="store.busyTip || '不可恢复，需二次确认'" @click="confirmKind = 'delete'">永久删除</button>
      </div>
      <!-- 收窄发生时的**就地**说明。确认框里已经说了一次，但用户点"取消"后
           回到这一屏如果看不到任何痕迹，下一次可能就忘了自己开着处理策略——
           按钮禁用的原因也就无从得知（effectiveCount 为 0 时按钮是灰的）。 -->
      <p v-if="store.procFiltering" class="proc-note">
        <Icon name="filter" :size="13" />
        处理范围已收窄：已勾选 <b>{{ store.selectedFiles.length }}</b> 项，
        其中 <b>{{ store.procExcluded }}</b> 项不在优先文件夹内，本次不会改动。
        <template v-if="store.procExcluded === store.selectedFiles.length">
          <b>当前没有任何勾选项在优先文件夹内，操作按钮已灰化。</b>
          请调整优先文件夹或清除该设置。
        </template>
      </p>
      <!-- 上一次执行的实际范围回执。ops:filtered 事件带回来的真实数字，
           与前端预估值互为印证（预估值基于路径前缀，这里基于后端权威判据）。 -->
      <p v-if="store.lastFilter && store.lastFilter.unmatched.length" class="proc-note warn">
        <Icon name="alert" :size="13" />
        这些优先文件夹内没有可处理的重复文件：<b>{{ store.lastFilter.unmatched.join('、') }}</b>
        ——请检查路径是否写对。
      </p>
      <!-- 跨卷合并的**常驻**风险说明（不可折叠、不可关闭）。
           为什么不做成一次性提示或 tooltip：软链接失效的代价是"文件打不开"，
           而且往往在几周后才发生（用户整理磁盘、拔掉移动硬盘时）。
           那时用户早已忘记自己做过什么，如果界面上从没说过这件事，
           他只会认为"这个软件把我的文件弄坏了"。故风险必须常驻在入口旁边。 -->
      <p v-if="hasCrossVolume" class="symlink-risk">
        <Icon name="alert" :size="13" />
        列表中有 <b>{{ crossVolumeGroups }}</b> 组重复文件跨磁盘卷。跨卷无法建立硬链接，
        可用「软链接合并」——磁盘只保留一份数据，原路径仍可访问。
        <b>但软链接只是指向保留文件路径的替身</b>：删除或移动保留文件、或拔掉它所在的磁盘，
        这些路径就会失效（打不开）。请把保留项放在**不会移动**的磁盘上；
        同卷的文件请优先用硬链接。软链接还需要管理员权限（或开启 Windows 开发者模式）。
      </p>
    </div>

    <!-- 操作执行反馈（M3-T06） -->
    <div v-if="store.opsRunning" class="opsbar panel">
      正在处理 <b>{{ store.opsProgress?.Done ?? 0 }}</b> / {{ store.opsProgress?.Total }} …
      <div class="bar"><div class="fill"
        :style="{ width: ((store.opsProgress?.Done ?? 0) / Math.max(1, store.opsProgress?.Total ?? 1)) * 100 + '%' }"></div></div>
      <!-- P2：大批量清理（网络盘、回收站卡住）必须能中止，否则界面永久锁在"执行中" -->
      <button class="btn-ghost" title="停止派发剩余条目；已完成的部分不会回滚"
        @click="store.cancelOp()">中止</button>
    </div>
    <div v-else-if="store.opsResult" class="opsbar panel done">
      <template v-if="store.opsResult.OK.length">
        <!-- 三个字节口径互不重叠，必须分别措辞（见 wails.ts OpsResult 注释）。
             混用会让用户去资源管理器核对时发现"对不上"，进而怀疑操作失败：
               LinkedBytes    数据没少，只是不再重复存第二份 → "占用不变"
               SymlinkedBytes 数据少了一整份（dup 只剩链接对象）→ 但要注意悬空风险
               Reclaimed      数据真的从磁盘移除了 → "释放" -->
        <span v-if="store.opsResult.SymlinkedBytes" class="ok">
          <Icon name="check" :size="13" /> 成功 {{ formatCount(store.opsResult.OK.length) }}（已合并为软链接 {{ humanBytes(store.opsResult.SymlinkedBytes) }}，磁盘只保留一份数据）
        </span>
        <span v-else-if="store.opsResult.LinkedBytes" class="ok">
          <Icon name="check" :size="13" /> 成功 {{ formatCount(store.opsResult.OK.length) }}（已合并为硬链接 {{ humanBytes(store.opsResult.LinkedBytes) }}，占用不变）
        </span>
        <span v-else class="ok"><Icon name="check" :size="13" /> 成功 {{ formatCount(store.opsResult.OK.length) }}（释放 {{ humanBytes(store.opsResult.Reclaimed) }}）</span>
        <button class="btn-ghost" @click="store.openTrash()">打开回收站</button>
      </template>
      <span v-if="store.opsResult.Skipped.length" class="skip"><Icon name="skip" :size="13" /> 已跳过 {{ formatCount(store.opsResult.Skipped.length) }}（文件已消失）</span>
      <!-- P2：中止后必须说清"还有多少没处理"，否则用户无法判断是否需要重跑 -->
      <span v-if="store.opsResult.Cancelled?.length" class="skip"><Icon name="skip" :size="13" /> 未处理 {{ formatCount(store.opsResult.Cancelled.length) }}（已中止，仍在列表中）</span>
      <!-- 操作已成功但有需要关注的情况（2026-09-19 残留；2026-09-20 起也承载
           软链接的权限汇总指引）。不并入失败——操作本身确实完成了——
           但必须显式告知。标签按内容区分：权限类是"必读指引"，
           与"临时文件残留"性质不同，共用一个含糊的"提示"会让用户误判该做什么。 -->
      <button v-if="store.opsResult.Warnings?.length" type="button" class="skip"
        :title="store.opsResult.Warnings.join('\n')"
        @click="showWarnings()"><Icon name="alert" :size="13" /> {{ warnLabel }} {{ formatCount(store.opsResult.Warnings.length) }}（查看）</button>
      <button v-if="store.opsResult.Failed.length" type="button" class="fail"
        @click="store.failedOpen = true"><Icon name="alert" :size="13" /> 失败 {{ formatCount(store.opsResult.Failed.length) }}（查看）</button>
      <button class="x" title="关闭结果提示" aria-label="关闭结果提示"
        @click="store.opsResult = null"><Icon name="close" :size="13" /></button>
    </div>

    <!-- 组列表 -->
    <div class="list" @scroll="onScroll">
      <!-- P1-2：空态给出行动出口，而不是一句提示 + 大片留白 -->
      <div v-if="!store.hasResult" class="empty panel">
        <div class="empty-title">暂无结果</div>
        <p class="empty-desc">先在「扫描」页选择目录并执行一次扫描。</p>
        <div class="empty-actions">
          <button class="btn-primary" @click="store.switchView('scan')">去扫描</button>
        </div>
      </div>
      <div v-else-if="store.totalGroups === 0" class="empty panel">
        <Icon class="empty-ico" name="check-circle" :size="32" :stroke="1.7" />
        <div class="empty-title">没有发现重复文件</div>
        <p class="empty-desc">当前扫描范围内没有可合并的重复项，换个目录再试试。</p>
        <div class="empty-actions">
          <button class="btn-primary" @click="store.switchView('scan')">扫描其它目录</button>
          <button v-if="store.failed.length" class="btn-ghost" @click="store.failedOpen = true">
            查看失败项（{{ store.failed.length }}）
          </button>
        </div>
      </div>
      <GroupCard v-for="g in store.groups" :key="g.groupID" :group="g" />
    </div>

    <!-- close/confirm 都只改 confirmKind，store.confirmOpen 由 watch 统一复位 -->
    <ConfirmDialog v-if="confirmKind" :kind="confirmKind" @close="confirmKind = null" @confirm="onConfirm" />
  </div>
</template>

<style scoped>
.result-view { flex: 1; display: flex; flex-direction: column; overflow: hidden; }
.statbar, .toolbar { margin: var(--sp-3) var(--page-gutter) 0; padding: 10px 14px; display: flex; align-items: center; gap: var(--sp-3); flex-wrap: wrap; }
.hist-banner { flex: none; margin: var(--sp-3) var(--page-gutter) 0; padding: 8px 14px; font-size: var(--fs-sm); color: var(--text-2); }
.stat { font-size: var(--fs-sm); color: var(--text-2); white-space: nowrap; }
.stat b { font-size: var(--fs-lg); color: var(--text); margin-right: 4px; font-variant-numeric: tabular-nums; }
.stat.warn, .stat.warn b { color: var(--danger-ink); }
/* P2-3：失败项入口现在是 <button>，抹掉原生按钮的底与内边距，使其与其它 .stat 视觉一致 */
button.stat { background: none; padding: 0; font-family: inherit; }
button.stat:hover b { text-decoration: underline; }
.spacer { flex: 1; }
/* P0-4：纵向两行固定结构，不再依赖 flex 换行 —— 任何窗口宽度下都是 2 行，
   破坏性按钮永远与其它操作同处一行（原先 920px 下会孤立成第三行）。 */
.toolbar { margin-top: 8px; flex-direction: column; align-items: stretch; gap: var(--sp-3); }
.trow { display: flex; align-items: center; justify-content: space-between; gap: var(--sp-3); }
.keep, .ops { display: flex; align-items: center; gap: var(--sp-2); flex-wrap: nowrap; }
.keep { min-width: 0; }
.ops { justify-content: flex-end; }
/* 处理策略行。与 .trow 同构（同样左对齐一组控件），但少一个右侧状态位，
   所以不套 justify-content: space-between —— 那样会把提示文字推到最右边。 */
.procrow { display: flex; align-items: center; gap: var(--sp-2); }
.proc-hint { color: var(--text-3); font-size: var(--fs-sm); min-width: 0; }
/* 处理策略的目录列表。复用 keepdirs 的 .kd-* 样式（同一套视觉语言：
   路径 + 行内删除），但**不**含序号与 ↑↓ —— 见模板里的注释。 */
.procdirs { display: flex; flex-direction: column; gap: 4px; }
/* 收窄说明。info 色：这是"告知范围"不是"出错"；warn 变体才用于
   "你加的目录里没有可处理的文件"那种需要用户动手修的情况。 */
.proc-note {
  display: flex; align-items: flex-start; gap: 6px;
  margin: 0; padding: 8px 10px; border-radius: var(--r-sm);
  background: var(--bg-hover); color: var(--text-2);
  font-size: var(--fs-sm); line-height: 1.6; user-select: text;
}
.proc-note b { color: var(--text); font-variant-numeric: tabular-nums; }
.proc-note svg { flex: none; margin-top: 3px; }
.proc-note.warn { background: var(--warn-weak, var(--bg-hover)); color: var(--warn-ink, var(--text-2)); }
.proc-note.warn b { color: inherit; }
/* 跨卷软链接的常驻风险说明。视觉上刻意"存在感强但不刺眼"：
   用 info 色而非警告红——软链接合并是**正确可用**的操作，不是错误；
   但需要用户读完。因此：不折叠、可选中复制（user-select: text），
   行高放宽便于阅读。 */
.symlink-risk {
  display: flex; align-items: flex-start; gap: 6px;
  margin: 0; padding: 8px 10px; border-radius: var(--r-sm);
  background: var(--primary-weak); color: var(--text-2);
  font-size: var(--fs-sm); line-height: 1.6; user-select: text;
}
.symlink-risk svg { flex: none; margin-top: 3px; color: var(--primary-ink); }
.symlink-risk b { color: var(--text); }
.lbl { color: var(--text-2); font-size: var(--fs-sm); }
.sel-info { font-size: var(--fs-sm); color: var(--text-2); flex: none; }
.sel-info b { color: var(--primary-ink); }
.opsbar { margin: 8px 16px 0; padding: 10px 14px; display: flex; align-items: center; gap: var(--sp-4); font-size: var(--fs-sm); }
.opsbar .bar { flex: 1; height: 6px; background: var(--bg-hover); border-radius: var(--r-sm); overflow: hidden; }
.opsbar .fill { height: 100%; background: var(--primary); transition: width 0.2s; }
/* P2-1：结果条三种态各自带一个线性图标；用 inline-flex + gap 保证图标与文字间距确定，
   不再依赖模板里那个易被压缩掉的空白字符。 */
.done .ok, .done .skip, .done .fail { display: inline-flex; align-items: center; gap: 4px; }
.done .ok { color: var(--success-ink); }
.done .skip { color: var(--text-3); }
.done .fail { color: var(--danger-ink); background: none; padding: 0; font-family: inherit; }
.done .fail:hover { text-decoration: underline; }
.done .x { margin-left: auto; background: none; color: var(--text-3); padding: 2px 6px; display: inline-flex; align-items: center; }
.list { flex: 1; overflow-y: auto; padding: var(--sp-2) var(--page-gutter) var(--sp-5); }
/* P1-2：空态 = 标题 + 说明 + 行动按钮 */
.empty { text-align: center; color: var(--text-2); padding: 56px 24px; }
/* P2-1：原 「🎉」 换成统一的线性图标（check-circle，成功色），随主题变色、无 emoji 固有彩色 */
.empty-ico { color: var(--success-ink); display: block; margin: 0 auto 10px; }
.empty-title { font-size: var(--fs-lg); font-weight: 600; color: var(--text); }
.empty-desc { margin-top: 8px; font-size: var(--fs-sm); color: var(--text-3); }
.empty-actions { margin-top: var(--sp-4); display: flex; gap: var(--sp-3); justify-content: center; }
/* Y7：达到放量上限时以警示色提示需显式继续加载（边框属图形，需 ≥3:1） */
.btn-ghost.capped { color: var(--warn-ink); border-color: var(--warn-ink); }
/* Y8：未勾选时弱强调「全选」，引导用户显式选择（不再默认全选） */
.btn-ghost.btn-emph { border-color: var(--primary); color: var(--primary-ink); }
/* 多目录优先级列表 */
.keepdirs { margin-top: 2px; display: flex; flex-direction: column; gap: 4px; }
.kd-row { display: flex; align-items: center; gap: var(--sp-2); font-size: var(--fs-sm); }
.kd-idx { flex: none; width: 18px; height: 18px; border-radius: 50%; background: var(--primary);
  color: #fff; font-size: 11px; display: inline-flex; align-items: center; justify-content: center; }
.kd-path { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--text); }
.btn-ghost.xs { padding: 1px 6px; font-size: 12px; line-height: 1.5; }
.kd-add { display: flex; align-items: center; gap: var(--sp-2); }
.kd-add input { flex: 1; min-width: 160px; }
.kd-hint { color: var(--text-3); font-size: var(--fs-sm); }
</style>

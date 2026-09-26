<script setup lang="ts">
// 失败清单抽屉（M2-T08）：扫描/操作失败项查看与导出。
// failed 数据由 store 统一维护（scan:done / ops:done 均在 store 处理），此处仅展示。
import { ref } from 'vue'
import { useScanStore } from '../stores/scan'
import { useToastStore } from '../stores/toast'
import { formatCount } from '../utils/format'
import { copyText } from '../utils/clipboard'
import { useModal } from '../composables/useModal'
import { api } from '../wails'
import Icon from './Icon.vue'

const store = useScanStore()
const toast = useToastStore()

// P2-3：浮层语义 + 焦点管理（打开聚焦、Tab 循环、关闭归还焦点）。
// 本组件由 App.vue 常驻挂载、靠 store.failedOpen 控制显隐，因此必须把「是否打开」传进去。
const dlgRef = ref<HTMLElement | null>(null)
useModal(dlgRef, () => store.failedOpen)

// 功能 4（2026-09-23）：逐行「打开该项 / 打开所在文件夹」。
//
// 为什么走路径版而不是结果行那套 api.revealInFolder(id)：失败项压根没进结果集
// （扫描中途失败的条目没有 byID 记录），ID 到不了它们。
// 这里不做任何"这条路径能不能开"的预判（AS-H6）：判据全在 Go 侧 checkRevealPath，
// 视图把按钮藏起来只会把「打不开并说明原因」变成「没点可点」——后者是 M83
// 那一族"点了没反应"的镜像形态。两条失败腿都必须有回声：
//   - 校验拒绝 / Start 失败 → 返回值 reject，这里 catch 后弹 toast；
//   - 起得来但随即非零退出 → 后端 warnBackground 经 app:error 事件上屏（M58）。
// 目录行上两个按钮会落到同一个动作（后端对目录的 reveal 就是打开自身），这是
// **有意不区分**：前端要区分就得自己 stat 或猜路径形状，那正是判据搬家。
function openFailed(p: string) {
  api.openPath(p).catch((e: any) => toast.notifyError('打开失败', e))
}

function revealFailedDir(p: string) {
  api.revealPath(p).catch((e: any) => toast.notifyError('打开所在文件夹失败', e))
}

async function copyAll() {
  const text = store.failed.map(f => `${f.Stage}\t${f.Path}\t${f.Err}`).join('\n')
  // C10：剪贴板写入可能被拒绝（无授权/非安全上下文），rejection 须接住。
  // M83（04 §6.11 FE-10）：改前是"对 navigator.clipboard 做一次可选链调用再挂 .catch"——
  // 短路时整个表达式为 undefined，那个 .catch 从未挂上 ⇒ 没有剪贴板的环境点了**完全静默**。
  // 现在两条失败路径（没有剪贴板 / 写入被拒）都从 copyText 抛到这里。
  try {
    await copyText(text)
  } catch (e: any) {
    toast.notifyError('复制失败', e)
  }
}
</script>

<template>
  <transition name="fade">
    <div v-if="store.failedOpen" class="mask" @click.self="store.failedOpen = false">
      <div
        ref="dlgRef"
        class="drawer panel"
        role="dialog"
        aria-modal="true"
        aria-labelledby="failed-drawer-title"
        tabindex="-1"
      >
        <div class="d-head">
          <span id="failed-drawer-title">失败清单（{{ formatCount(store.failed.length) }}）</span>
          <div>
            <button class="btn-ghost" @click="copyAll">复制全部</button>
            <button class="btn-ghost" @click="store.failedOpen = false">关闭</button>
          </div>
        </div>
        <div class="d-body">
          <div v-if="store.failed.length === 0" class="empty">无失败项</div>
          <template v-else>
            <!-- P1-4：三列栅格 + 列头，路径列起点对齐 -->
            <div class="d-cols" aria-hidden="true">
              <span>阶段</span><span>路径</span><span>错误原因</span><span>操作</span>
            </div>
            <div v-for="(f, i) in store.failed" :key="i" class="item">
              <span class="stage">{{ f.Stage }}</span>
              <span class="path" :title="f.Path">{{ f.Path }}</span>
              <span class="err" :title="f.Err">{{ f.Err }}</span>
              <!-- 功能 4：两条出口都不问"这条路径看着能不能开"（判据在后端） -->
              <span class="ops">
                <button
                  class="op"
                  type="button"
                  title="用系统默认应用打开该项（目录则进入该目录）"
                  aria-label="打开该项"
                  @click.stop="openFailed(f.Path)"
                >
                  <Icon name="external" :size="15" />
                </button>
                <button
                  class="op"
                  type="button"
                  title="打开所在文件夹并选中"
                  aria-label="在文件夹中打开该项"
                  @click.stop="revealFailedDir(f.Path)"
                >
                  <Icon name="folder-open" :size="15" />
                </button>
              </span>
            </div>
          </template>
        </div>
      </div>
    </div>
  </transition>
</template>

<style scoped>
.mask {
  position: fixed; inset: 0; background: rgba(0, 0, 0, 0.35);
  display: flex; justify-content: flex-end; z-index: 100;
}
.drawer {
  width: 640px; max-width: 90vw; height: 100%;
  display: flex; flex-direction: column; border-radius: 0;
}
.d-head {
  display: flex; justify-content: space-between; align-items: center;
  padding: var(--sp-4); font-weight: 600; border-bottom: 1px solid var(--border);
}
.d-head > div { display: flex; gap: var(--sp-2); }
.d-body {
  flex: 1; overflow-y: auto; padding: var(--sp-2);
  /* P1-4：阶段 / 路径 / 错误 / 操作 四列共用同一套轨道，保证列头与各行起点对齐。
     操作列给固定宽度而不是 auto：`.d-cols` 与 `.item` 是**两个**栅格容器，
     auto 会各自按内容求解（列头是两个字、行内是两枚按钮），轨道宽度就漂了。 */
  --fail-cols: 56px minmax(0, 1.2fr) minmax(0, 1fr) 62px;
}
.empty { color: var(--text-3); text-align: center; padding: 40px 0; }
.d-cols {
  display: grid; grid-template-columns: var(--fail-cols); gap: var(--sp-2);
  padding: 0 var(--sp-2) 6px; font-size: var(--fs-sm); color: var(--text-3);
  border-bottom: 1px solid var(--border); margin-bottom: var(--sp-1);
}
.item {
  display: grid; grid-template-columns: var(--fail-cols); gap: var(--sp-2);
  align-items: start; padding: 6px var(--sp-2); border-radius: var(--r-md); font-size: var(--fs-sm);
}
.item:hover { background: var(--bg-hover); }
.stage { color: var(--warn-ink); white-space: nowrap; }
.path { font-family: var(--mono); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; user-select: text; }
/* 原先被 max-width:180px + nowrap 截成 49~167px，最关键的错误原因全被切掉。
   改为占满整列并允许换行 —— 抽屉的唯一用途就是排障，错误信息必须完整可读。 */
.err { color: var(--danger-ink); overflow-wrap: anywhere; user-select: text; }
/* 功能 4：逐行两枚图标按钮。尺寸沿用 GroupCard 的 .op（28×28，P1-5 的
   图标按钮下限），两枚并排 28+28+6=62px，正是 --fail-cols 末列的宽度。 */
.ops { display: flex; gap: 6px; }
.op {
  background: none; color: var(--text-3);
  display: flex; align-items: center; justify-content: center;
  width: 28px; height: 28px; padding: 0; border-radius: var(--r-md); line-height: 1;
}
.op:hover { color: var(--primary); background: var(--bg-hover); }
</style>
